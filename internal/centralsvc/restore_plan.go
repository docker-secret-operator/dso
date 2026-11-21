package centralsvc

// Restore policy for an existing active database (Phase 3a).
//
// planRestore turns what is known about the active database -- the ownership
// state and structural finding from assessActive, the recorded schema version,
// and the sizes of its -wal and -journal companions -- into the single decision
// Restore will follow. It is PURE: it reads no file, opens no database, prompts no
// one, preserves nothing and mutates nothing. Everything it knows arrives in its
// argument, so the whole policy can be tested exhaustively without any I/O, and
// Restore (a later phase) can only act on a plan, never decide on its own.
//
// The policy (R1-R11 and the Phase 3 design):
//
//	Missing, no non-empty orphan companion    no active database: unchanged flow
//	Missing, a non-empty orphan -wal/-journal/-shm
//	                                          REFUSE: nothing is deleted or replaced
//	Initialized, Clean, schema is the latest, no non-empty -wal or -journal
//	                                          existing snapshot flow, no preservation
//	Initialized, Clean, and the schema is older (R3), or a non-empty -wal (R4), or a
//	non-empty -journal (R8)                   raw-preserve first, THEN the snapshot flow
//	Initialized, structure Mismatch or Unknown (R1)
//	                                          raw-preserve, second confirmation, replace
//	                                          WITHOUT opening the original
//	BindingCorrupt, DSO-unbound, Empty (R2)   raw-preserve, replace WITHOUT opening --
//	                                          EXCEPT BindingCorrupt and DSO-unbound with a
//	                                          non-empty -journal: REFUSE (R8, below)
//	Foreign, TooNew, NonSQLite, Inconclusive  REFUSE; a database with a hot rollback
//	                                          journal is Inconclusive, so it is refused
//
// R8 (a non-empty rollback journal is treated like a non-empty -wal): the
// classifier never looks at a -journal, so what it saw may not be what SQLite
// would open once the journal is applied. A NEGATIVE or PARTIAL finding -- DSO-
// unbound, or a binding that looks corrupt -- beside a non-empty journal cannot be
// trusted, exactly as the classifier already distrusts such a finding beside a
// non-empty -wal, so the planner refuses it as Inconclusive and gives hot-journal
// recovery guidance. A POSITIVE finding (Initialized) is preserved first and then
// follows the snapshot flow; Empty is a fact about the file, not an inference, so
// it is preserved and replaced without being opened; Foreign is refused anyway.
//
// Unknown structure is never Clean: only structureClean, with a known schema
// version, reaches the snapshot flow. Preservation is a precondition, never a
// permission: the plan says what must happen before anything else; whether a
// replacement may follow is decided by the confirmations the plan names.
//
// An assessment is also summarised as a fingerprint, so that the assessment taken
// after preservation can be required to be equivalent to the one the plan was
// made from.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// RestoreFlow names how Restore proceeds for an existing or missing active database.
type RestoreFlow int

const (
	// RestoreFlowRefuse is the zero value, so an unset plan refuses.
	RestoreFlowRefuse RestoreFlow = iota
	// RestoreFlowNoActive: there is no active database; restore into the empty path.
	RestoreFlowNoActive
	// RestoreFlowSnapshot: the existing flow -- open the original, take the safety
	// snapshot, verify a checkpoint, replace. Used only for an Initialized database
	// whose structure is Clean; when the plan also says Preserve, preservation comes first.
	RestoreFlowSnapshot
	// RestoreFlowReplaceWithoutOpen: replace the active file without ever opening
	// the original through SQLite; the raw preservation is the recovery copy.
	RestoreFlowReplaceWithoutOpen
)

func (f RestoreFlow) String() string {
	switch f {
	case RestoreFlowRefuse:
		return "refuse"
	case RestoreFlowNoActive:
		return "no-active-database"
	case RestoreFlowSnapshot:
		return "snapshot"
	case RestoreFlowReplaceWithoutOpen:
		return "replace-without-open"
	}
	return fmt.Sprintf("RestoreFlow(%d)", int(f))
}

// RestorePlan is the decision for one restore. The zero value refuses and
// permits nothing.
type RestorePlan struct {
	Flow RestoreFlow
	// State and Structure are the assessment the plan was made from, as text for
	// messages.
	State            string
	Structure        string
	StructureReasons []string
	RecordedVersion  int
	VersionKnown     bool
	// Preserve: raw-preserve the original BEFORE anything opens, migrates,
	// checkpoints or replaces it. PreserveReasons say why.
	Preserve        bool
	PreserveReasons []string
	// SecondConfirmation: after preservation, a mandatory second explicit
	// confirmation is required (R1). No flag, input mode or callback default may
	// satisfy it on its own.
	SecondConfirmation bool
	// OpenOriginal: SQLite may open the original (after preservation, when
	// Preserve is set). False for every flow that replaces without opening.
	OpenOriginal bool

	fp assessmentFingerprint
}

// planInput is everything planRestore knows.
type planInput struct {
	Assessment activeAssessment
	Sidecars   sqlite.ActiveSidecarsInfo
	// LatestVersion is the newest schema version this binary supports. It must be
	// positive for a plan that reaches the snapshot decision.
	LatestVersion int
}

// Refusal categories. Every refusal matches ErrRestoreRefused and exactly one of these.
var (
	ErrRestoreRefused = errors.New("centralsvc: restore refused")

	ErrRestoreRefusedForeign      = errors.New("the active file is a SQLite database that DSO Central did not create")
	ErrRestoreRefusedTooNew       = errors.New("the active database was created by a newer version of dso")
	ErrRestoreRefusedNotSQLite    = errors.New("the active file is not a SQLite database")
	ErrRestoreRefusedInconclusive = errors.New("the state of the active database could not be established safely")
	ErrRestoreRefusedOrphans      = errors.New("companion files exist beside a missing database")
	ErrRestoreRefusedInvalid      = errors.New("the assessment of the active database is inconsistent")

	// ErrRestoreReassessmentMismatch: the assessment taken after preservation is
	// not equivalent to the one the plan was made from.
	ErrRestoreReassessmentMismatch = errors.New("centralsvc: the active database changed between assessment and preservation")
)

// RestoreRefusedError is a refusal made before anything was written or opened.
type RestoreRefusedError struct {
	Kind      error // one of the ErrRestoreRefused* categories
	State     string
	Structure string
	Reasons   []string
	// Guidance is manual-recovery guidance; it never asks the program to delete
	// anything.
	Guidance []string
	Cause    error // the underlying assessment error, if any
}

func (e *RestoreRefusedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "restore refused: %v", e.Kind)
	if e.State != "" {
		fmt.Fprintf(&b, " (state: %s)", e.State)
	}
	for _, r := range e.Reasons {
		b.WriteString("\n  - " + r)
	}
	for _, g := range e.Guidance {
		b.WriteString("\n" + g)
	}
	return b.String()
}

// Is makes errors.Is match ErrRestoreRefused and the specific category.
func (e *RestoreRefusedError) Is(target error) bool {
	return target == ErrRestoreRefused || (e.Kind != nil && target == e.Kind)
}

// Unwrap exposes the underlying assessment error.
func (e *RestoreRefusedError) Unwrap() error { return e.Cause }

// planRestore decides. See the file comment for the policy.
func planRestore(in planInput) (RestorePlan, error) {
	a, sc := in.Assessment, in.Sidecars
	fp := fingerprintOf(a, sc)
	base := RestorePlan{
		State:            a.State.String(),
		Structure:        a.Structure.String(),
		StructureReasons: append([]string(nil), a.StructureReasons...),
		RecordedVersion:  a.RecordedVersion,
		VersionKnown:     a.VersionKnown,
		fp:               fp,
	}
	refuse := func(kind error, reasons []string, guidance []string) (RestorePlan, error) {
		return RestorePlan{}, &RestoreRefusedError{
			Kind: kind, State: a.State.String(), Structure: a.Structure.String(),
			Reasons: reasons, Guidance: guidance, Cause: a.Err,
		}
	}

	// A state and the existence of the main file must agree; if they do not, the
	// inputs cannot be trusted and nothing proceeds.
	if (a.State == stateMissing) == sc.MainExists {
		return refuse(ErrRestoreRefusedInvalid,
			[]string{fmt.Sprintf("the assessment says %q but the main database file exists = %v", a.State, sc.MainExists)},
			[]string{"This is an internal inconsistency; nothing was changed. Run the command again."})
	}

	switch a.State {
	case stateMissing:
		if orphans := sc.OrphanNonEmpty(); len(orphans) > 0 {
			return refuse(ErrRestoreRefusedOrphans,
				[]string{"the database file is missing but these companions are not empty: " + strings.Join(orphans, ", ")},
				[]string{
					"Restore will not delete or overwrite them. They may hold data from a database that no longer exists.",
					"Inspect them, then move them aside yourself (keep them until you are sure), and run restore again.",
				})
		}
		p := base
		p.Flow = RestoreFlowNoActive
		return p, nil

	case stateInitialized:
		if a.Err != nil {
			return refuse(ErrRestoreRefusedInvalid, []string{"an initialized database carries an error: " + a.Err.Error()}, nil)
		}
		return planInitialized(base, in)

	case stateBindingCorrupt, stateDSOUnbound, stateEmpty:
		if a.State != stateEmpty && sc.Journal.NonEmpty() {
			// R8: see the file comment. Cause is deliberately nil: wrapping the
			// finding would make an Inconclusive refusal match ErrNotInitialized or
			// ErrBindingCorrupt.
			return RestorePlan{}, &RestoreRefusedError{
				Kind: ErrRestoreRefusedInconclusive, State: a.State.String(), Structure: a.Structure.String(),
				Reasons: []string{fmt.Sprintf(
					"the database was assessed as %s, but a non-empty -journal is present and the assessment does not take a rollback journal into account, so that finding cannot be trusted", a.State)},
				Guidance: append(hotJournalGuidance(), "Make sure no other process is using the database and run restore again; nothing was changed."),
			}
		}
		p := base
		p.Flow = RestoreFlowReplaceWithoutOpen
		p.Preserve = true
		p.PreserveReasons = []string{fmt.Sprintf("the active database is %s, so it is preserved byte for byte and never opened", a.State)}
		for _, name := range preservedCompanions(sc) {
			p.PreserveReasons = append(p.PreserveReasons, "a non-empty "+name+" is present and will be preserved with it")
		}
		return p, nil

	case stateForeign:
		return refuse(ErrRestoreRefusedForeign, nil, []string{
			"This file is not a DSO Central database, so restore will not overwrite it, and there is no override.",
			"If you are sure you want to replace it, move it (and any -wal, -shm and -journal beside it) aside yourself, then run restore again.",
		})

	case stateTooNew:
		return refuse(ErrRestoreRefusedTooNew, nil, []string{
			"Use a dso version that supports this database's schema, or move the database aside yourself and run restore again.",
		})

	case stateNonSQLite:
		return refuse(ErrRestoreRefusedNotSQLite, nil, []string{
			"Restore will not overwrite a file that is not a SQLite database. Move it aside yourself and run restore again.",
		})

	case stateInconclusive:
		var guidance []string
		if sc.Journal.NonEmpty() {
			guidance = append(guidance, hotJournalGuidance()...)
		}
		guidance = append(guidance, "Make sure no other process is using the database and run restore again; nothing was changed.")
		return refuse(ErrRestoreRefusedInconclusive, inconclusiveReasons(a), guidance)
	}
	return refuse(ErrRestoreRefusedInconclusive, []string{fmt.Sprintf("unrecognised state %d", int(a.State))}, nil)
}

func planInitialized(base RestorePlan, in planInput) (RestorePlan, error) {
	a, sc := in.Assessment, in.Sidecars
	p := base
	// Only a Clean structure with a known schema version is trusted. Mismatch,
	// Unknown (the zero value) and Clean-without-a-version all fall through to the
	// no-open, second-confirmation flow: Unknown is never treated as Clean.
	if a.Structure != structureClean || !a.VersionKnown {
		p.Flow = RestoreFlowReplaceWithoutOpen
		p.Preserve = true
		p.SecondConfirmation = true
		switch a.Structure {
		case structureMismatch:
			p.PreserveReasons = []string{"the database is initialized but its structure does not match the DSO schema"}
		default:
			p.PreserveReasons = []string{"the database is initialized but its structure could not be established"}
		}
		p.PreserveReasons = append(p.PreserveReasons, a.StructureReasons...)
		for _, name := range preservedCompanions(sc) {
			p.PreserveReasons = append(p.PreserveReasons, "a non-empty "+name+" is present and will be preserved with it")
		}
		return p, nil
	}

	if in.LatestVersion <= 0 {
		return RestorePlan{}, &RestoreRefusedError{
			Kind: ErrRestoreRefusedInvalid, State: a.State.String(), Structure: a.Structure.String(),
			Reasons: []string{"the newest supported schema version was not supplied"},
			Cause:   a.Err,
		}
	}
	var reasons []string
	if a.RecordedVersion < in.LatestVersion {
		reasons = append(reasons, fmt.Sprintf("the database's schema version (%d) is older than this binary's (%d), so opening it would migrate it", a.RecordedVersion, in.LatestVersion))
	}
	if a.RecordedVersion > in.LatestVersion {
		// A Clean structure cannot have a version this binary does not know; if the
		// inputs say so anyway, they cannot be trusted.
		return RestorePlan{}, &RestoreRefusedError{
			Kind: ErrRestoreRefusedInvalid, State: a.State.String(), Structure: a.Structure.String(),
			Reasons: []string{fmt.Sprintf("a clean database claims schema version %d, newer than the supported %d", a.RecordedVersion, in.LatestVersion)},
		}
	}
	if sc.WAL.NonEmpty() {
		reasons = append(reasons, "a non-empty -wal is present (opening the database would replay and checkpoint it)")
	}
	if sc.Journal.NonEmpty() {
		reasons = append(reasons, "a non-empty -journal is present")
	}
	p.Flow = RestoreFlowSnapshot
	p.OpenOriginal = true
	if len(reasons) > 0 {
		p.Preserve = true
		p.PreserveReasons = reasons
	}
	return p, nil
}

// hotJournalGuidance is the manual-recovery guidance for a database whose state
// cannot be trusted because a non-empty rollback journal sits beside it. It never
// asks the program, or the operator, to delete anything.
func hotJournalGuidance() []string {
	return []string{
		"A rollback journal (-journal) is present. A hot journal can only be recovered by SQLite itself, which needs write access that this read-only inspection does not have.",
		"Stop every process that uses the database, copy the database and its -wal, -shm and -journal files to a safe place, then let SQLite recover the original (for example by opening it once read-write with the sqlite3 tool), or move all of them aside, and run restore again.",
	}
}

// preservedCompanions names the non-empty -wal and -journal that a preservation
// will carry (the -shm is advisory and never a reason).
func preservedCompanions(sc sqlite.ActiveSidecarsInfo) []string {
	var out []string
	if sc.WAL.NonEmpty() {
		out = append(out, "-wal")
	}
	if sc.Journal.NonEmpty() {
		out = append(out, "-journal")
	}
	return out
}

func inconclusiveReasons(a activeAssessment) []string {
	if a.Err != nil {
		return []string{a.Err.Error()}
	}
	return []string{"no reason was recorded"}
}

// ---- the assessment fingerprint ----------------------------------------------------------------

// assessmentFingerprint is what must not change between the assessment a plan
// was made from and the assessment taken after preservation.
type assessmentFingerprint struct {
	state           activeState
	structure       structuralFinding
	versionKnown    bool
	recordedVersion int
	walNonEmpty     bool
	journalNonEmpty bool
}

func fingerprintOf(a activeAssessment, sc sqlite.ActiveSidecarsInfo) assessmentFingerprint {
	f := assessmentFingerprint{
		state:           a.State,
		structure:       a.Structure,
		versionKnown:    a.VersionKnown,
		walNonEmpty:     sc.WAL.NonEmpty(),
		journalNonEmpty: sc.Journal.NonEmpty(),
	}
	if a.VersionKnown { // an unknown version is never a version number
		f.recordedVersion = a.RecordedVersion
	}
	return f
}

// fingerprintsEquivalent compares two fingerprints. An inconclusive assessment
// is never equivalent to anything, including another inconclusive one: it
// establishes nothing. Otherwise every field must be equal, and the differing
// fields are named.
func fingerprintsEquivalent(before, after assessmentFingerprint) (bool, []string) {
	var diffs []string
	if before.state == stateInconclusive {
		diffs = append(diffs, "the original assessment was inconclusive")
	}
	if after.state == stateInconclusive {
		diffs = append(diffs, "the new assessment is inconclusive")
	}
	if before.state != after.state {
		diffs = append(diffs, fmt.Sprintf("state changed from %s to %s", before.state, after.state))
	}
	if before.structure != after.structure {
		diffs = append(diffs, fmt.Sprintf("structure changed from %s to %s", before.structure, after.structure))
	}
	if before.versionKnown != after.versionKnown || before.recordedVersion != after.recordedVersion {
		diffs = append(diffs, fmt.Sprintf("recorded schema version changed from %s to %s", versionText(before), versionText(after)))
	}
	if before.walNonEmpty != after.walNonEmpty {
		diffs = append(diffs, fmt.Sprintf("-wal non-empty changed from %v to %v", before.walNonEmpty, after.walNonEmpty))
	}
	if before.journalNonEmpty != after.journalNonEmpty {
		diffs = append(diffs, fmt.Sprintf("-journal non-empty changed from %v to %v", before.journalNonEmpty, after.journalNonEmpty))
	}
	return len(diffs) == 0, diffs
}

func versionText(f assessmentFingerprint) string {
	if !f.versionKnown {
		return "unknown"
	}
	return fmt.Sprintf("%d", f.recordedVersion)
}

// ReassessmentMismatchError reports a plan that no longer matches the active
// database. It matches ErrRestoreReassessmentMismatch.
type ReassessmentMismatchError struct{ Differences []string }

func (e *ReassessmentMismatchError) Error() string {
	return ErrRestoreReassessmentMismatch.Error() + ": " + strings.Join(e.Differences, "; ")
}

func (e *ReassessmentMismatchError) Is(target error) bool {
	return target == ErrRestoreReassessmentMismatch
}

// matchesReassessment reports whether a fresh assessment and sidecar reading are
// equivalent to the ones this plan was made from; if not, it returns a
// *ReassessmentMismatchError and the restore must not continue. An inconclusive
// fresh assessment is never equivalent.
func (p RestorePlan) matchesReassessment(a activeAssessment, sc sqlite.ActiveSidecarsInfo) error {
	if ok, diffs := fingerprintsEquivalent(p.fp, fingerprintOf(a, sc)); !ok {
		return &ReassessmentMismatchError{Differences: diffs}
	}
	return nil
}

package centralsvc

// Backup/restore orchestration (Phase 2 Step 5.4, approved design/decision-
// resolution). This file implements:
//   - root-key binding verification against a restore candidate file
//     (ValidateCandidateKey), mirroring binding.go's verifyBinding but
//     applied to a standalone file instead of this Service's own live
//     Store -- Bind itself is tied to a mutable Store and would attempt to
//     CREATE a binding on an unbound database, which is never the right
//     behaviour for a read-only restore candidate;
//   - the full restore state machine (Restore): validate -> mandatory
//     safety snapshot of the active store -> close -> replace -> reopen ->
//     append the forward-only restore audit event.
//
// Explicitly NOT implemented here, per the approved scope: an external
// rollback-detection anchor, Step 5.2 socket authentication, automatic
// backup/audit retention, and bulk secret decryption during restore. See
// docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md and
// docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md
// (historical references: neither document is present in this checkout or its
// git history, so the decisions they record could not be re-verified here).

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ErrRestoreNotConfirmed is returned by Restore when RestoreOptions.Confirm
// is set and returns false. The active database is left untouched.
var ErrRestoreNotConfirmed = errors.New("centralsvc: restore was not confirmed")

// ErrRestoreSecondConfirmationDeclined is returned when a plan requires the
// second, mandatory confirmation (an initialized database whose structure is
// Mismatch or Unknown) and it was not given: declined, unavailable, or answered
// with anything but the exact word. The raw preservation made before it was
// asked is kept, and the active database was not opened, migrated, checkpointed
// or replaced.
var ErrRestoreSecondConfirmationDeclined = errors.New("centralsvc: the second confirmation was not given; restore was not performed")

// ErrRestoreJournalNotPreserved: the active database has a non-empty rollback
// journal that is not covered by a verified preservation, so replacing the
// database could let that stale journal be applied to the restored one.
var ErrRestoreJournalNotPreserved = errors.New("centralsvc: a non-empty rollback journal is not covered by a verified preservation")

// ErrRestoreActiveFileChanged: the active database file is no longer the file
// that was assessed (replaced, removed, or, for a missing database, created).
var ErrRestoreActiveFileChanged = errors.New("centralsvc: the active database file is not the one that was assessed")

// SecondConfirmationWord is what the operator must type, exactly, to give the
// second confirmation.
const SecondConfirmationWord = "replace"

// SecondConfirmationAccepted reads one line from r and reports whether it is
// exactly SecondConfirmationWord (case-sensitive, no padding; only the line
// terminator is ignored). Anything else declines: "y", other words, an empty
// line, end of input before a complete line, a read error, or a nil reader.
// Whether input counts as interactive is the caller's decision; this function
// has no flags and cannot be told to accept.
func SecondConfirmationAccepted(r io.Reader) bool {
	if r == nil {
		return false
	}
	line, err := bufio.NewReader(io.LimitReader(r, 256)).ReadString('\n')
	if err != nil { // io.EOF without a newline included: an unfinished line is not a confirmation
		return false
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") == SecondConfirmationWord
}

// PreservationInfo describes a completed raw preservation for the second
// confirmation prompt.
type PreservationInfo struct {
	Dir   string
	Files []sqlite.PreservedFile
}

// dryRunNotice is what a dry run reports about the active database.
const dryRunNotice = "The active database was not examined: classification, raw preservation and the safety checks run only in a real restore, which may still be refused (for example for a foreign database, a hot rollback journal, or leftover companion files beside a missing database)."

// Stage names passed to the test hook, in the order they occur.
const (
	stagePlanned         = "planned"
	stageConfirmed       = "confirmed"
	stagePreserved       = "preserved"
	stageReassessed      = "reassessed"
	stageSecondPrompt    = "second-prompt"
	stageSecondConfirmed = "second-confirmed"
	stageGatesPassed     = "gates-passed"
	stageOpenOriginal    = "open-original"
	stageSnapshot        = "snapshot"
	stagePreJournalGate  = "pre-journal-gate"
	stageReplaced        = "replaced"
)

// ValidateCandidateKey verifies that kp is the root key the restore candidate
// at path is bound to, without opening the candidate for write, migrating
// it, or otherwise modifying it -- the source backup must never be altered
// by validation.
//
// This mirrors Service.Bind's own canary-unwrap verification (binding.go's
// verifyBinding): it reads the candidate's binding.* store_meta rows and
// asks kp to unwrap the stored canary under kp's own id, exactly as Bind
// does for a live database. It does not call Bind itself, because Bind is
// tied to a mutable Store and, for an unbound database, would attempt to
// CREATE a binding -- never correct for a read-only restore candidate, which
// is by construction already bound if it was ever written to by this
// system.
//
// Limitation, stated explicitly per the approved decision resolution
// (decision 2): a successful return proves the configured root key matches
// the one this candidate is bound to. It does NOT prove every secret
// version's ciphertext in the candidate will successfully decrypt --
// SQLite-level structural validity (checked separately by
// sqlite.ValidateCandidate) and AES-GCM-level ciphertext authenticity are
// independent properties, and this function does not perform bulk
// decryption to check the latter.
//
// Like sqlite.ValidateCandidate, this reads a private, disposable copy of
// the candidate (sqlite.CopyCandidateForValidation), never the original
// path -- opening the original directly, even through a nominally
// read-only connection, can checkpoint and modify its -wal/-shm companions
// (confirmed by direct experiment; see sqlite.ValidateCandidate's own doc
// comment for the full explanation).
func ValidateCandidateKey(ctx context.Context, path string, kp crypto.KeyProvider) error {
	staged, err := sqlite.StageCandidate(ctx, path)
	if err != nil {
		return fmt.Errorf("staging a private copy of restore candidate %s for key validation: %w", path, err)
	}
	defer staged.Close()
	return validateStagedCandidateKey(ctx, staged, path, kp)
}

// validateStagedCandidateKey is ValidateCandidateKey against an already-
// staged snapshot, so Restore validates the exact artifact it later activates.
func validateStagedCandidateKey(ctx context.Context, staged *sqlite.StagedCandidate, path string, kp crypto.KeyProvider) error {
	workPath := staged.Path
	db, err := sqlite.OpenReadOnly(workPath)
	if err != nil {
		return fmt.Errorf("opening restore candidate for key validation: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT key, value FROM store_meta WHERE key LIKE 'binding.%'`)
	if err != nil {
		return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
	}
	var version, keyID, canary []byte
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
		}
		switch k {
		case metaBindingVersion:
			version = v
		case metaBindingKeyID:
			keyID = v
		case metaBindingCanary:
			canary = v
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
	}
	if version == nil || keyID == nil || canary == nil {
		return fmt.Errorf("%w: restore candidate has no key-binding metadata", ErrBindingCorrupt)
	}
	if string(version) != bindingFormat {
		return fmt.Errorf("%w (unsupported binding format)", ErrBindingCorrupt)
	}

	id := kp.ID()
	bindAAD, err := crypto.BindingAAD(id)
	if err != nil {
		return fmt.Errorf("building key-binding context: %w", err)
	}
	unwrapped, err := kp.Unwrap(ctx, canary, bindAAD)
	if err == nil && len(unwrapped) == crypto.KeySize {
		crypto.Zero(unwrapped)
		return nil
	}
	crypto.Zero(unwrapped)
	if string(keyID) == id {
		// The metadata names this very key but the canary does not open: the
		// canary was altered or damaged, not a different key presented.
		return ErrBindingCorrupt
	}
	return fmt.Errorf("%w: restore candidate is bound to root key %q but the configured key is %q",
		ErrWrongRootKey, string(keyID), id)
}

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// CandidatePath is the backup file to restore from. It is never
	// modified, deleted, or overwritten by Restore.
	CandidatePath string
	// DBPath is the active database's file path. On a non-dry-run restore,
	// this file is replaced.
	DBPath string
	// KeyProvider is the configured root key, verified against the
	// candidate's own binding before anything else is touched.
	KeyProvider crypto.KeyProvider
	// DryRun performs every validation check and returns, without touching
	// the active database, creating a safety snapshot, replacing any file,
	// or appending a restore event.
	DryRun bool
	// Actor is the caller-supplied attribution string recorded on the
	// restore event's ActorLabel, exactly like every other operation's actor
	// parameter -- attribution only, never authenticated identity.
	Actor string
	// Confirm, if non-nil, is called once validation succeeds, before the
	// active database is touched (DryRun skips this entirely: it never
	// reaches this call). It receives the validated candidate's info so the
	// caller can show it to the operator before asking for confirmation.
	// Returning false aborts the restore with ErrRestoreNotConfirmed and
	// leaves the active database completely untouched.
	Confirm func(info sqlite.CandidateInfo) bool

	// LockTimeout overrides how long Restore waits for the exclusive
	// central lock (see sqlite.AcquireExclusiveCentralLock) before failing
	// with sqlite.ErrCentralLockBusy. <=0 uses the package default.
	LockTimeout time.Duration

	// Logger receives Error-level reports of best-effort audit-append
	// failures (A-2). nil discards them.
	Logger *zap.Logger

	// OnPlan, if non-nil, receives the decision made for the active database
	// (what will be preserved, whether a second confirmation is required) after
	// any refusal has been ruled out and before Confirm is asked, so a caller can
	// tell the operator what they are confirming. It must not block.
	OnPlan func(plan RestorePlan)
	// ConfirmAfterPreservation is the mandatory second confirmation. It is asked
	// only when the plan requires it, after the raw preservation succeeded and the
	// post-preservation reassessment matched. nil means "not confirmed": no
	// option, flag or default can satisfy it. Returning false stops Restore with
	// ErrRestoreSecondConfirmationDeclined.
	ConfirmAfterPreservation func(plan RestorePlan, preservation PreservationInfo) bool

	// testHook, when non-nil, is called with a stage name at each stage of the
	// restore (see the stage* constants). Tests use it to change files at an exact
	// point, or to kill the process there.
	testHook func(stage string)
	// testAssess, testSidecars and testPreserve replace assessActive,
	// sqlite.ActiveSidecars and sqlite.PreserveActiveFiles for one Restore call.
	testAssess   func(path string) activeAssessment
	testSidecars func(path string) (sqlite.ActiveSidecarsInfo, error)
	testPreserve func(dbPath string, expect sqlite.FileIdentity) (*sqlite.Preservation, error)
	// testBeforeRename runs inside the replacement, after the old companions were
	// removed and before the rename.
	testBeforeRename func()

	// testReplaceDatabaseFile, testReopen and testAppendRestoreEvent, when
	// non-nil, replace the real replaceDatabaseFile/sqlite.Open/
	// AppendAuditEvent calls for this one Restore call. Unexported: a
	// caller outside this package cannot set them. This is the same
	// dependency-injection test-seam pattern internal/cli/central.go
	// already uses (centralAuditLoggerOverride) for the same reason: OS-
	// level filesystem-permission failures are unreliable to provoke
	// deterministically (they behave differently as root or in some CI
	// sandboxes), so tests inject a failure at an exact point in the state
	// machine instead of trying to break real file permissions.
	testReplaceDatabaseFile func(dbPath, candidatePath string) error
	testReopen              func(ctx context.Context, path string) (*sqlite.Store, error)
	testAppendRestoreEvent  func(ctx context.Context, st *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error)
	// testSafetySnapshot, when non-nil, replaces active.SafetySnapshot for
	// this one Restore call -- same seam pattern as the three above, added
	// specifically to let a test inject a *sqlite.BackupDirectorySyncError
	// (a real, valid snapshot path paired with a non-fatal durability
	// warning) without needing to reach into the sqlite package's own
	// unexported syncBackupDir test seam from outside it.
	testSafetySnapshot func(ctx context.Context, active *sqlite.Store, dbPath string) (string, error)
	// testReopenOriginal, when non-nil, replaces the reopen of the ORIGINAL
	// active database used only to record a pre-activation
	// replacement failure (distinct from testReopen, which only affects the
	// post-replacement reopen of the restored database).
	testReopenOriginal func(ctx context.Context, path string) (*sqlite.Store, error)
}

// RestoreResult reports what Restore did (or, for a dry run or an aborted
// confirmation, would have done).
type RestoreResult struct {
	CandidateInfo  sqlite.CandidateInfo
	SafetySnapshot string // path of the mandatory pre-restore safety snapshot; "" if none was needed/taken
	RestoreEventID string // "" on dry run, a declined confirmation, or failure before the event could be recorded
	DryRun         bool
	// RawPreservation is the directory of the raw preservation of the original
	// active database, "" if none was taken. It is kept on every outcome,
	// including failures after it was made; Restore never removes it.
	RawPreservation string
	// Plan is the decision made for the active database (zero for a dry run).
	Plan RestorePlan
	// DryRunNotice is set on a dry run: the active database was not examined.
	DryRunNotice string
	// DisplacedDir is the directory holding the original active database files that
	// the replacement moved aside, "" if none are retained. It is never removed.
	DisplacedDir string
	// DisplacedFiles lists those retained originals (main, -wal, -journal, -shm).
	// On a failed restore it lists what is retained when the operation ended.
	DisplacedFiles []sqlite.DisplacedFile
	// VanishedFiles lists, by original path, companion files that existed when the
	// original database was examined and had disappeared by the time they were to be
	// moved aside. They are neither retained nor restored: nothing in this restore put
	// them anywhere, so a restore that lists any did NOT carry every original file
	// forward. A file that was already absent is never listed. Empty when nothing
	// vanished.
	VanishedFiles []string
}

// Restore implements the approved Step 5.4 restore state machine:
//
//  1. Validate the candidate completely (sqlite.ValidateCandidate +
//     ValidateCandidateKey) -- nothing about the active database is touched
//     by this step.
//  2. If DryRun, return here: no safety snapshot, no replacement, no event.
//  3. If Confirm is set, call it with the validated candidate info; a false
//     return aborts with ErrRestoreNotConfirmed, active database untouched.
//  4. If an active database exists at DBPath, open it and take a mandatory,
//     independently verified safety snapshot (Store.SafetySnapshot) before
//     anything else -- this is the recovery path if any later step fails.
//  5. Verify, not merely attempt, a full WAL checkpoint (Store.
//     CheckpointTruncate) -- aborting here, with the active database still
//     untouched, if it cannot complete -- then close the active database.
//  6. Replace the active database file with the candidate's content,
//     without ever modifying or deleting the candidate itself. The
//     PREVIOUS database's -wal/-shm companions are removed before this
//     step (not after), which step 5's verified checkpoint is what makes
//     safe: see replaceDatabaseFile.
//  7. Reopen the restored file through the normal sqlite.Open path (the same
//     validation and, if needed, forward migration every other database
//     open already goes through).
//  8. Append the forward-only restore audit event to the restored chain.
//
// Restore never claims success before step 7 (reopen) has actually
// succeeded: every error returned before that point leaves the result's
// RestoreEventID empty and is accompanied, where a safety snapshot exists,
// by that snapshot's path so the operator can recover the previous usable
// store. Restore never claims that the restored database is the newest
// legitimate state solely because its audit chain verified -- that chain
// verification is exactly the check sqlite.ValidateCandidate already
// performed in step 1, and the approved design is explicit that it cannot
// detect whole-database rollback (see the design document §7).
//
// Concurrency limitation (not mitigated by code): there is no cross-process
// lock. The verified checkpoint in step 5 only proves no conflicting
// connection blocked it at that instant; another process that has the
// database open could still commit between that checkpoint and the removal of
// the -wal/-shm companions in step 6, and that commit would be lost (and
// removing a live -shm out from under a process is unsafe). Run restore only
// while no other process (e.g. a running `dso` server) has the database open.
// Same-UID file permissions do not provide locking.
//
// Restore does not implement an external rollback anchor, does not decrypt
// any secret value, and has no caller-specific authorization: anyone able to
// run `dso central` as the owning OS user can call this, identically to
// every other `dso central` operation today (Step 5.2's Option B design is
// not implemented; see the Step 5.4 decision resolution, decision 5).
func Restore(ctx context.Context, opts RestoreOptions) (RestoreResult, error) {
	absCandidate, err := filepath.Abs(opts.CandidatePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolving restore candidate path: %w", err)
	}
	absDB, err := filepath.Abs(opts.DBPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolving active database path: %w", err)
	}
	if absCandidate == absDB {
		return RestoreResult{}, fmt.Errorf("restore candidate and active database must not be the same file (%s)", absDB)
	}

	maxSchema, err := sqlite.LatestSchemaVersion()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("determining supported schema version: %w", err)
	}
	// Stage ONE private, self-contained snapshot of the candidate (any -wal
	// frames folded into its main file) and use that single artifact for every
	// validation check AND as the source of the activated database. The state
	// validated is therefore, by construction, the state activated, and the
	// original candidate files are never opened by SQLite or modified. The
	// staged copy lives until Restore returns.
	staged, err := sqlite.StageCandidate(ctx, absCandidate)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed validation (rejected before any change; active database untouched): %w", err)
	}
	defer staged.Close()
	info, err := sqlite.ValidateStagedCandidate(ctx, staged, absCandidate, maxSchema)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed validation (rejected before any change; active database untouched): %w", err)
	}
	if err := validateStagedCandidateKey(ctx, staged, absCandidate, opts.KeyProvider); err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed key validation (rejected before any change; active database untouched): %w", err)
	}

	result := RestoreResult{CandidateInfo: info, DryRun: opts.DryRun}
	if opts.DryRun {
		// A dry run never looks at the active database: no classification, no
		// sidecar reading, no lock.
		result.DryRunNotice = dryRunNotice
		return result, nil
	}

	// Exclusive central lock for the rest of this function: from here
	// through the final reopen below, no other dso central process may
	// have absDB open at all (see docs/security/restore-concurrency-design.md).
	// Taken AFTER candidate validation (which only ever touches absCandidate,
	// never absDB) and the dry-run return, so a dry run or a failed
	// validation never blocks anything else; taken BEFORE Confirm, because
	// the moment an operator is asked to confirm, this restore has
	// committed to the database it is about to touch, and nothing else may
	// act on that database while that decision is pending. A second,
	// concurrent Restore (or any other `dso central` command) against the
	// same path fails here with sqlite.ErrCentralLockBusy after
	// opts.LockTimeout, rather than blocking indefinitely or racing.
	restoreLock, err := sqlite.AcquireExclusiveCentralLock(absDB, opts.LockTimeout)
	if err != nil {
		return result, fmt.Errorf("acquiring exclusive restore lock on %s: %w", absDB, err)
	}
	defer func() { _ = restoreLock.Close() }()

	// restoreLock.Open, not sqlite.Open: this Restore call already holds the
	// exclusive central lock on absDB above, and only the RestoreLock value
	// itself (never a field on Options) can open without taking a fresh,
	// self-conflicting one.
	openFn := func(ctx context.Context, path string) (*sqlite.Store, error) {
		return restoreLock.Open(ctx, path, sqlite.Options{})
	}
	reopenFn := openFn // the restored database, after replacement
	if opts.testReopen != nil {
		reopenFn = opts.testReopen
	}
	openOriginalFn := openFn // the original active database
	if opts.testReopenOriginal != nil {
		openOriginalFn = opts.testReopenOriginal
	}
	appendFn := func(ctx context.Context, st *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error) {
		return st.AppendAuditEvent(ctx, ev)
	}
	if opts.testAppendRestoreEvent != nil {
		appendFn = opts.testAppendRestoreEvent
	}
	// recordRestoreEvent appends a restore event to st, best-effort (A-2):
	// failures are logged at Error level and never change Restore's result.
	// Callers must only pass a store that is the ORIGINAL active database or
	// the successfully reopened restored database -- never a candidate or a
	// database in an uncertain state.
	recordRestoreEvent := func(st *sqlite.Store, outcome store.AuditOutcome, reason string) string {
		id, _ := recordAuditBestEffort(ctx, opts.Logger, func(ctx context.Context, ev store.AuditEvent) (store.AuditEvent, error) {
			return appendFn(ctx, st, ev)
		}, store.AuditEvent{EventType: store.AuditRestore, Outcome: outcome, ReasonCategory: reason, ActorLabel: opts.Actor})
		return id
	}
	// recordFailureOnOriginal is for a replacement failure, when no handle to
	// the original active database is open: it reopens the original (still-intact) database
	// and records the failure there. If there was no active database, or it
	// cannot be reopened, the failure is left unaudited -- nothing is ever
	// written to an uncertain database.
	recordFailureOnOriginal := func(originalOpened bool, reason string) {
		if !originalOpened {
			// The original was never opened by this call (no active database, or a
			// flow that replaces without opening): nothing is written to it.
			return
		}
		orig, err := openOriginalFn(ctx, absDB)
		if err != nil {
			if opts.Logger != nil {
				opts.Logger.Error("restore failure audit event not recorded: original active database could not be reopened",
					zap.String("reason_category", reason), zap.Error(err))
			}
			return
		}
		defer orig.Close()
		recordRestoreEvent(orig, store.AuditFailure, reason)
	}

	stage := func(name string) {
		if opts.testHook != nil {
			opts.testHook(name)
		}
	}

	// ---- 1. Examine the active database, under the exclusive lock ----------------------
	// Nothing below opens, migrates, checkpoints, preserves or replaces anything
	// until this has produced a plan. Every failure to look fails closed.
	insp, err := inspectActive(absDB, opts)
	if err != nil {
		return result, err
	}
	plan, err := planRestore(planInput{Assessment: insp.assessment, Sidecars: insp.sidecars, LatestVersion: maxSchema})
	if err != nil {
		return result, err // a refusal: nothing was opened, written or created
	}
	result.Plan = plan
	stage(stagePlanned)
	if opts.OnPlan != nil {
		opts.OnPlan(plan)
	}

	// ---- 2. The normal confirmation ------------------------------------------------------
	// A declined confirmation is deliberately NOT audited: recording it would
	// open and write to the active database, and a refusal must leave that
	// database untouched (no migration, no new row). It also precedes any
	// preservation, so a declined restore writes nothing at all.
	if opts.Confirm != nil && !opts.Confirm(info) {
		return result, ErrRestoreNotConfirmed
	}
	stage(stageConfirmed)

	originalOpened := false // set once this call has opened the original through SQLite

	// ---- 3. Raw preservation, before anything can open or change the original ------
	var pres *sqlite.Preservation
	if plan.Preserve {
		preserveFn := sqlite.PreserveActiveFiles
		if opts.testPreserve != nil {
			preserveFn = opts.testPreserve
		}
		pres, err = preserveFn(absDB, insp.id)
		if pres != nil {
			result.RawPreservation = pres.Dir() // kept on every later outcome
		}
		if err != nil {
			// Including a durability error that left a published, valid preservation:
			// preservation is not permission, and an unconfirmed one does not proceed.
			return result, preservationFailure(err, pres)
		}
		stage(stagePreserved)

		// ---- 4. Post-preservation reassessment (mandatory) ------------------------
		re, err := inspectActive(absDB, opts)
		if err != nil {
			return result, fmt.Errorf("reassessing the active database after preservation failed: %w "+
				"(restore stopped; the active database was not opened or replaced; the completed preservation at %s was kept)", err, pres.Dir())
		}
		if err := plan.matchesReassessment(re.assessment, re.sidecars); err != nil {
			return result, fmt.Errorf("%w (restore stopped; the active database was not opened or replaced; the completed preservation at %s was kept)", err, pres.Dir())
		}
		if err := sameActiveFile(insp.fi, re.fi); err != nil {
			return result, fmt.Errorf("%w (restore stopped; the active database was not opened or replaced; the completed preservation at %s was kept)", err, pres.Dir())
		}
		stage(stageReassessed)
	}

	// ---- 5. The second confirmation (R1) -----------------------------------------------------
	if plan.SecondConfirmation {
		stage(stageSecondPrompt)
		if opts.ConfirmAfterPreservation == nil || !opts.ConfirmAfterPreservation(plan, preservationInfo(pres)) {
			return result, fmt.Errorf("%w (the completed preservation at %s was kept; the active database was not opened, migrated, checkpointed or replaced)",
				ErrRestoreSecondConfirmationDeclined, pres.Dir())
		}
		stage(stageSecondConfirmed)
	}

	// ---- 5b. Prepare the replacement file ------------------------------------------------
	// Copied and fsynced BEFORE the gates, and before anything about the active
	// database or its companions is moved, so a failure here (no space, I/O)
	// changes nothing. Abandon removes only this operation's own temporary file, and
	// never after the replacement was installed. (When a test replaces the whole
	// replacement step there is nothing to prepare.)
	var repl *sqlite.Replacement
	if opts.testReplaceDatabaseFile == nil {
		repl, err = restoreLock.PrepareReplacement(absDB, staged.Path)
		if err != nil {
			return result, fmt.Errorf("preparing the replacement database failed (restore stopped; the active database and its companion files were not touched%s): %w",
				recoveryNote(result), err)
		}
		defer func() { _ = repl.Abandon() }()
	}

	// ---- 6. The gates immediately before the first Open or the replacement -------
	// Two independent checks, neither a substitute for the other: the
	// preservation still matches the original byte for byte (so what was
	// preserved is what would be replaced), and the file at the path is still the
	// file that was assessed.
	if err := activeFileGates(absDB, plan, insp, pres); err != nil {
		return result, gateFailure(err, pres)
	}
	stage(stageGatesPassed)

	var durabilityErr error
	if plan.Flow == RestoreFlowSnapshot {
		stage(stageOpenOriginal)
		active, err := openFn(ctx, absDB)
		if err != nil {
			return result, fmt.Errorf("opening active database for safety snapshot: %w", err)
		}
		originalOpened = true
		snapshotFn := active.SafetySnapshot
		if opts.testSafetySnapshot != nil {
			snapshotFn = func(ctx context.Context, dbPath string) (string, error) {
				return opts.testSafetySnapshot(ctx, active, dbPath)
			}
		}
		snapshotPath, err := snapshotFn(ctx, absDB)
		var snapDirSyncErr *sqlite.BackupDirectorySyncError
		if err != nil && !errors.As(err, &snapDirSyncErr) {
			recordRestoreEvent(active, store.AuditFailure, store.AuditReasonSnapshotFailed)
			_ = active.Close()
			return result, fmt.Errorf("creating mandatory safety snapshot: %w (restore aborted; active database was not replaced)", err)
		}
		result.SafetySnapshot = snapshotPath
		stage(stageSnapshot)
		if snapDirSyncErr != nil {
			// The snapshot file itself is valid and usable (see
			// SafetySnapshot's doc comment); only its directory-entry
			// durability is unconfirmed. Carry on -- aborting a restore
			// over an otherwise-successful safety snapshot would be worse
			// than reporting the warning at the end, the same tolerance
			// replaceFn's own unconfirmedDurabilityError gets below.
			durabilityErr = errors.Join(durabilityErr, snapDirSyncErr)
		}
		// Verified checkpoint, not Close's own best-effort one (whose error
		// is intentionally discarded there -- see CheckpointTruncate's doc
		// comment): replaceDatabaseFile below removes the active database's
		// -wal/-shm companions BEFORE it is replaced, which is only safe to
		// do once we have confirmed, not merely hoped, that no frame in
		// those files is still the only copy of a committed write. If the
		// checkpoint cannot complete, abort here -- before anything about
		// the active database has changed -- rather than proceeding on an
		// unverified assumption.
		if err := active.CheckpointTruncate(ctx); err != nil {
			recordRestoreEvent(active, store.AuditFailure, store.AuditReasonCheckpointFailed)
			_ = active.Close()
			return result, fmt.Errorf("checkpointing active database before replacement: %w "+
				"(restore aborted; active database was not replaced; safety snapshot at %s)", err, snapshotPath)
		}
		if err := active.Close(); err != nil {
			return result, fmt.Errorf("closing active database before replacement: %w (safety snapshot at %s; the active database has not been modified yet)",
				err, snapshotPath)
		}
	}

	// ---- 7. The rollback-journal gate, then the replacement ----------------------
	// A stale rollback journal left beside the replaced file would be applied to
	// the restored database by SQLite when it is next opened, silently reverting
	// its contents while integrity_check still reports ok. So a non-empty journal
	// may be removed only when a verified preservation holds it.
	stage(stagePreJournalGate)
	if err := journalGate(absDB, pres, plan.Flow != RestoreFlowSnapshot); err != nil {
		return result, fmt.Errorf("%w (restore stopped; the active database was not replaced%s)", err, recoveryNote(result))
	}

	// ---- 8. Baseline, displacement and installation -------------------------------------
	// The baseline is taken as late as possible, after every gate and after the
	// journal gate: for the snapshot flow AFTER SQLite's own open, checkpoint and
	// Close (which legitimately change the files, so the pre-snapshot bytes are not
	// the baseline); for the flows that never opened the original, from the verified
	// preservation; for a missing database, "no main file".
	//
	// ReplaceActiveDatabase then MOVES the original files aside and installs the
	// replacement with a no-replace rename; nothing is deleted or overwritten (see
	// sqlite/displace.go). It does not stop a process that ignores the DSO lock from
	// writing in that narrow window, or holding the old files open: such writes land
	// in the retained files, not in the restored database.
	var dres *sqlite.DisplaceResult
	var repErr error
	if opts.testReplaceDatabaseFile != nil {
		repErr = opts.testReplaceDatabaseFile(absDB, staged.Path)
	} else {
		var baseline *sqlite.Baseline
		var berr error
		switch plan.Flow {
		case RestoreFlowSnapshot:
			baseline, berr = restoreLock.BaselineAfterClose(absDB)
		case RestoreFlowNoActive:
			baseline, berr = restoreLock.BaselineNoActive(absDB)
		default:
			if pres == nil {
				berr = errors.New("no verified preservation to take the baseline from")
			} else {
				baseline, berr = pres.VerifiedBaseline()
			}
		}
		if berr != nil {
			return result, fmt.Errorf("the active database changed before it could be replaced: %w (restore stopped; the active database was not replaced%s)",
				berr, recoveryNote(result))
		}
		var dopts []sqlite.DisplaceOption
		if opts.testBeforeRename != nil {
			dopts = append(dopts, sqlite.WithBeforeInstall(opts.testBeforeRename))
		}
		dres, repErr = restoreLock.ReplaceActiveDatabase(absDB, repl, baseline, dopts...)
		if dres != nil {
			result.DisplacedDir, result.DisplacedFiles = dres.Dir, dres.Files
			result.VanishedFiles = vanishedPaths(absDB, dres.Vanished)
		}
	}
	if repErr != nil {
		var ide *sqlite.InstallDurabilityError
		var ice *sqlite.InstalledFileChangedError
		var ue *unconfirmedDurabilityError
		var de *sqlite.DisplacementError
		switch {
		case errors.As(repErr, &ice):
			// The replacement WAS renamed into place, but the file now at the active
			// path could not be confirmed to be it. Do not treat this as a completed
			// restore and do not touch the file: nothing is reopened, migrated,
			// checkpointed or audited, and no restore event is recorded. The original
			// files stay in the aside directory.
			observed, conclusion := installedKindWording(ice.Kind)
			return result, fmt.Errorf("the restore replaced the active database at %s, but %s: %w "+
				"(%s; the database was not reopened, migrated or recorded; inspect it before use%s)%s%s",
				absDB, observed, repErr, conclusion, recoveryNote(result), displacedNote(result), vanishedNote(result))
		case errors.As(repErr, &ide):
			// The replacement IS installed: only the directory sync that makes the
			// rename crash-durable failed. Carry on (reopen, audit) and report it at
			// the end.
			durabilityErr = errors.Join(durabilityErr, ide)
		case errors.As(repErr, &ue):
			durabilityErr = errors.Join(durabilityErr, ue)
		default:
			if errors.As(repErr, &de) {
				result.DisplacedDir, result.DisplacedFiles = de.Dir, de.Retained
				if de.Install == sqlite.InstallUnknown {
					// The install rename failed and it is not known whether the
					// replacement is in place, so nothing was rolled back and the
					// active path may hold the restored database. Nothing is opened
					// or recorded.
					return result, fmt.Errorf("installing the replacement database failed and its outcome could not be established: %w "+
						"(the file at %s may be the restored database or may not exist; nothing was rolled back, reopened or recorded; inspect it before use%s)%s%s",
						repErr, absDB, recoveryNote(result), displacedNote(result), vanishedNote(result))
				}
				if !de.OriginalInPlace() {
					// The original files are not all back (or not all accounted for). The
					// wording comes from the structured result, never from RolledBack alone.
					// Nothing is opened or recorded: if the main file is not at its path,
					// opening absDB would create a new, empty database.
					return result, fmt.Errorf("replacing active database failed before activation and the original files could not all be put back: %w "+
						"(%s; nothing was deleted or overwritten%s)", repErr, displacementState(de, absDB), recoveryNote(result))
				}
			}
			// Activation did not happen and the original is in place. Record only if it
			// was opened and can be reopened.
			recordFailureOnOriginal(originalOpened, store.AuditReasonDatabaseReplaceFailed)
			switch {
			case result.SafetySnapshot != "" && result.RawPreservation != "":
				return result, fmt.Errorf("replacing active database failed before activation: %w "+
					"(the active database at %s was not replaced; the safety snapshot at %s and the raw preservation at %s were kept)",
					repErr, absDB, result.SafetySnapshot, result.RawPreservation)
			case result.SafetySnapshot != "" && de != nil && len(de.Vanished) > 0:
				// The main file is in place, but files that were part of the database vanished
				// during the restore (listed above): the snapshot may well be needed.
				return result, fmt.Errorf("replacing active database failed before activation: %w "+
					"(the active database at %s was not replaced, but %d of its files vanished during the restore (see above); the safety snapshot at %s was kept)",
					repErr, absDB, len(de.Vanished), result.SafetySnapshot)
			case result.SafetySnapshot != "":
				return result, fmt.Errorf("replacing active database failed before activation: %w "+
					"(the active database at %s was not replaced; the safety snapshot at %s is not needed)",
					repErr, absDB, result.SafetySnapshot)
			case result.RawPreservation != "":
				return result, fmt.Errorf("replacing active database failed before activation: %w "+
					"(the active database at %s was not replaced; the complete original state is also preserved at %s)",
					repErr, absDB, result.RawPreservation)
			}
			return result, fmt.Errorf("replacing active database failed before activation: %w (no database was created at %s)", repErr, absDB)
		}
	}
	stage(stageReplaced)

	restored, err := reopenFn(ctx, absDB)
	if err != nil {
		if result.SafetySnapshot == "" && result.RawPreservation != "" {
			return result, fmt.Errorf("reopening restored database: %w (RECOVERY: the original active database is preserved byte for byte at %s -- copy those files back beside %s; "+
				"the replacement WAS ACTIVATED: the database at %s is now the candidate's content, which failed to reopen -- it is NOT automatically recovered)%s",
				err, result.RawPreservation, absDB, absDB, displacedNote(result)+vanishedNote(result))
		}
		if result.SafetySnapshot != "" {
			return result, fmt.Errorf("reopening restored database: %w (RECOVERY: restore the safety snapshot at %s to %s%s; "+
				"the replacement WAS ACTIVATED: the database at %s is now the candidate's content, which failed to reopen -- it is NOT automatically recovered; recover manually from the snapshot)%s",
				err, result.SafetySnapshot, absDB, rawNote(result), absDB, displacedNote(result)+vanishedNote(result))
		}
		return result, fmt.Errorf("reopening restored database: %w%s", err, displacedNote(result)+vanishedNote(result))
	}
	defer restored.Close()

	// Best-effort (A-2): the restore has already succeeded and the restored
	// database is usable, so a failure to record the event is logged at Error
	// level and does not change the result. RestoreEventID stays empty then.
	result.RestoreEventID = recordRestoreEvent(restored, store.AuditSuccess, "")
	if durabilityErr != nil {
		recorded := "the restore event was recorded"
		if result.RestoreEventID == "" {
			recorded = "the restore event could not be recorded (see the error log)"
		}
		return result, fmt.Errorf("restore completed and %s, but durability of the replacement could not be confirmed: %w%s%s",
			recorded, durabilityErr, snapshotNote(result), displacedNote(result)+vanishedNote(result))
	}
	return result, nil
}

// installedKindWording says, for each way the installed file can fail its
// verification, what was observed and what that does and does not establish. It
// claims no more than the kind proves: only "unreadable" and an unrecognised kind
// are an inability to verify rather than an observed difference.
func installedKindWording(k sqlite.InstalledFileKind) (observed, conclusion string) {
	const differs = "the active database may NOT be the restored content"
	switch k {
	case sqlite.InstalledGone:
		return "nothing is at that path any more", differs + ": the installed file is missing"
	case sqlite.InstalledNotRegular:
		return "what is at that path is not a regular file", differs
	case sqlite.InstalledReplaced:
		return "the file at that path is a different file than the one installed", differs
	case sqlite.InstalledContentChanged:
		return "the size or content of the installed file differs from what was written", differs
	case sqlite.InstalledUnstable:
		return "the installed file was still changing while it was verified", differs + ": its content was not stable"
	case sqlite.InstalledUnreadable:
		return "the installed file could not be opened or read, so it was not verified",
			"it has not been confirmed whether the active database is the restored content"
	}
	return "the installed file could not be verified", "it has not been confirmed whether the active database is the restored content"
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// LEGACY: Restore no longer replaces the database with replaceDatabaseFile or
// replaceDatabaseFileOpts. It uses the displacement in sqlite/displace.go
// (PrepareReplacement, BaselineAfterClose, ReplaceActiveDatabase), which moves the
// original files aside and deletes nothing. These two functions DELETE the old
// companions and rename over the main file; they are retained only because the
// existing journal-gate tests exercise them directly, and must not be called from
// production code. Retiring them is a separate decision.
//
// replaceDatabaseFile is replaceDatabaseFileOpts for a caller that has not
// established that a non-empty rollback journal is preserved: it removes an
// absent or empty -journal and refuses to touch a non-empty one.
func replaceDatabaseFile(dbPath, candidatePath string) error {
	return replaceDatabaseFileOpts(dbPath, candidatePath, false, nil)
}

// replaceDatabaseFileOpts copies candidatePath's content into a new temporary
// file beside dbPath, fsyncs it, and atomically renames it onto dbPath
// (os.Rename replaces an existing destination atomically on the same
// filesystem -- there is never a window with no file at dbPath at all).
// candidatePath (Restore passes the validated staged snapshot, never the
// original candidate) is only ever opened read-only: it is never modified or
// deleted.
//
// dbPath's directory is checked with the same internal/securefs directory-
// chain/symlink discipline the database and key files already use,
// regardless of whether an active database previously existed there --
// Restore's caller only re-verifies this incidentally (via sqlite.Open) when
// a prior active database existed; the no-prior-database path has nothing
// else to check it, so this function checks it itself rather than relying
// on that incidental coverage.
//
// Any -wal, -shm and -journal companion of the PREVIOUS database at dbPath is
// removed BEFORE the rename, not after: by the time this function runs, the
// caller has either confirmed with a verified checkpoint (Store.
// CheckpointTruncate, not Store.Close's own best-effort one) that no frame in
// those files is the only copy of a committed write, or holds a verified raw
// preservation of them, so removing them first is not a data-loss risk, and it
// eliminates the alternative window entirely -- there is no ordering under
// which the NEW main file content and the OLD database's companions can ever
// coexist on disk at the same time, because the old ones are already gone
// before the new main file exists at dbPath at all.
//
// The -journal needs particular care. A rollback journal left beside the new
// main file would be applied to it by SQLite the next time it is opened, which
// silently reverts the restored contents while integrity_check still says ok.
// So the journal is removed with the other companions, and a failure to remove
// any companion is an error here (before the rename), not something the rename
// can hide. A NON-EMPTY journal is removed only when allowNonEmptyJournal is
// true, which the caller may set only after it has verified that the journal is
// held by a completed preservation; otherwise nothing at all is removed or
// replaced and the error matches ErrRestoreJournalNotPreserved.
// beforeRename, if non-nil, runs after the companions are gone and before the
// rename (a test seam).
func replaceDatabaseFileOpts(dbPath, candidatePath string, allowNonEmptyJournal bool, beforeRename func()) error {
	dir := filepath.Dir(dbPath)
	if err := securefs.EnsureDir(dir); err != nil {
		return fmt.Errorf("active database directory is not safe: %w", err)
	}

	// Decide about the journal before anything is removed.
	if fi, err := os.Lstat(dbPath + "-journal"); err == nil {
		if !fi.Mode().IsRegular() {
			return fmt.Errorf("%s-journal is not a regular file; refusing to replace the database", dbPath)
		}
		if fi.Size() > 0 && !allowNonEmptyJournal {
			return fmt.Errorf("%w: refusing to remove the non-empty %s-journal", ErrRestoreJournalNotPreserved, dbPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("examining %s-journal: %w", dbPath, err)
	}

	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(dbPath + sfx); err != nil && !errors.Is(err, os.ErrNotExist) {
			// A surviving companion beside the new main file is the hazard this
			// ordering exists to prevent, so a failed removal stops the replacement.
			return fmt.Errorf("removing %s%s before replacement: %w", dbPath, sfx, err)
		}
	}
	if beforeRename != nil {
		beforeRename()
	}

	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.restoring-%s", dbPath, hex.EncodeToString(rnd[:]))

	src, err := os.Open(candidatePath) // #nosec G304 -- candidatePath was already validated read-only by sqlite.ValidateCandidate/securefs.OpenPrivateFile; opened here read-only and never written to
	if err != nil {
		return fmt.Errorf("opening restore candidate: %w", err)
	}
	defer src.Close()

	dst, err := securefs.CreateExclusive(tmp, 0o600)
	if err != nil {
		return fmt.Errorf("creating temporary restore file: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copying restore candidate: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("syncing restored database: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing restored database: %w", err)
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activating restored database: %w", err)
	}
	d, err := os.Open(dir) // #nosec G304 -- directory of the database file being replaced, already checked safe above
	if err != nil {
		return &unconfirmedDurabilityError{fmt.Errorf("opening %s to sync the rename: %w", dir, err)}
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return &unconfirmedDurabilityError{fmt.Errorf("syncing directory %s after rename: %w", dir, err)}
	}
	return nil
}

// unconfirmedDurabilityError is returned by replaceDatabaseFile only AFTER the
// rename has activated the new database: the replacement is in place and
// usable, but the directory fsync that makes the rename crash-durable failed.
type unconfirmedDurabilityError struct{ err error }

func (e *unconfirmedDurabilityError) Error() string { return e.err.Error() }
func (e *unconfirmedDurabilityError) Unwrap() error { return e.err }

// ---- Phase 3b helpers ----------------------------------------------------------------------------------

// activeInspection is everything learned about the active database, once.
type activeInspection struct {
	fi         os.FileInfo // Lstat of the main file when it existed (nil when missing)
	id         sqlite.FileIdentity
	assessment activeAssessment
	sidecars   sqlite.ActiveSidecarsInfo
}

// inspectActive examines the active database without changing it. It takes the
// identity of the main file, runs the read-only assessment, then reads the
// companions. Every failure to look is a refusal: a permission error, a path
// component that is not a directory, a symlink, a directory or any other
// non-regular file is never read as "missing".
func inspectActive(absDB string, opts RestoreOptions) (activeInspection, error) {
	sidecarsFn := sqlite.ActiveSidecars
	if opts.testSidecars != nil {
		sidecarsFn = opts.testSidecars
	}
	assessFn := assessActive
	if opts.testAssess != nil {
		assessFn = opts.testAssess
	}
	cannotLook := func(err error) (activeInspection, error) {
		guidance := []string{"Nothing was changed. Fix the problem named above and run restore again."}
		if errors.Is(err, sqlite.ErrSidecarNotRegular) {
			guidance = []string{
				"Restore will not follow or replace a symbolic link, directory or other special file in place of the database or its companions. Nothing was changed.",
				"Inspect it, move it aside yourself, and run restore again.",
			}
		}
		return activeInspection{}, &RestoreRefusedError{
			Kind:     ErrRestoreRefusedInconclusive,
			State:    stateInconclusive.String(),
			Reasons:  []string{"the active database's files could not be examined: " + err.Error()},
			Guidance: guidance,
			Cause:    err,
		}
	}

	first, err := sidecarsFn(absDB)
	if err != nil {
		return cannotLook(err)
	}
	var insp activeInspection
	if first.MainExists {
		fi, err := os.Lstat(absDB)
		if err != nil {
			return cannotLook(err)
		}
		id, err := sqlite.StatIdentity(absDB)
		if err != nil {
			return cannotLook(err)
		}
		insp.fi, insp.id = fi, id
	}
	insp.assessment = assessFn(absDB)
	// The companions are read AFTER the assessment, so the plan sees them as the
	// assessment (which may create an empty -wal or rewrite the -shm) left them.
	if insp.sidecars, err = sidecarsFn(absDB); err != nil {
		return cannotLook(err)
	}
	return insp, nil
}

// sameActiveFile reports whether the file at the active path is still the file
// that was assessed. before is nil exactly when the database was missing.
func sameActiveFile(before os.FileInfo, now os.FileInfo) error {
	if before == nil || now == nil || !os.SameFile(before, now) {
		return fmt.Errorf("%w", ErrRestoreActiveFileChanged)
	}
	return nil
}

func preservationInfo(p *sqlite.Preservation) PreservationInfo {
	if p == nil {
		return PreservationInfo{}
	}
	return PreservationInfo{Dir: p.Dir(), Files: p.Files()}
}

// preservationFailure explains why a raw preservation failed, without ever
// claiming that free space was checked in advance.
func preservationFailure(err error, pres *sqlite.Preservation) error {
	var durability *sqlite.PreserveDurabilityError
	var why string
	switch {
	case errors.Is(err, sqlite.ErrPreserveNoSpace):
		why = "preservation could not complete because there was not enough storage; this was detected while the preservation was being written (free space is not checked in advance)"
	case errors.Is(err, sqlite.ErrPreserveUnstable):
		why = "preservation could not complete because the active database files kept changing while they were copied (is another process using the database?)"
	case errors.Is(err, sqlite.ErrPreserveIdentityChanged):
		why = "preservation could not complete because the active database file is not the one that was assessed"
	case errors.Is(err, sqlite.ErrPreserveVerification):
		why = "preservation could not complete because a copy did not read back identically"
	case errors.Is(err, sqlite.ErrPreserveNoReplaceUnsupported):
		why = "preservation cannot be published safely on this platform or filesystem"
	case errors.Is(err, sqlite.ErrPreserveDestinationExists):
		why = "preservation could not complete because a preservation with the same name already exists"
	case errors.Is(err, sqlite.ErrPreserveOutcomeUnknown):
		why = "preservation could not complete: publishing it was interrupted and its outcome could not be established"
	case errors.As(err, &durability):
		why = "the preservation was written, but its directory entry could not be confirmed on stable storage"
	default:
		why = "preserving the active database failed"
	}
	msg := "restore stopped: " + why + "; the active database was not opened, migrated, checkpointed or replaced"
	if pres != nil {
		msg += fmt.Sprintf("; the completed preservation at %s was kept", pres.Dir())
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// activeFileGates are the checks made immediately before the first Open or the
// replacement: when the original was preserved, the preservation must still
// match it exactly; and the file at the path must still be the file that was
// assessed (or, for a missing database, the path must still be empty and no
// companion may have appeared).
func activeFileGates(absDB string, plan RestorePlan, insp activeInspection, pres *sqlite.Preservation) error {
	if plan.Flow == RestoreFlowNoActive {
		if _, err := os.Lstat(absDB); err == nil {
			return fmt.Errorf("%w: a file appeared at %s", ErrRestoreActiveFileChanged, absDB)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("examining %s: %w", absDB, err)
		}
		sc, err := sqlite.ActiveSidecars(absDB)
		if err != nil {
			return err
		}
		if orphans := sc.OrphanNonEmpty(); len(orphans) > 0 {
			return &RestoreRefusedError{
				Kind: ErrRestoreRefusedOrphans, State: stateMissing.String(),
				Reasons:  []string{"companion files appeared beside the missing database: " + strings.Join(orphans, ", ")},
				Guidance: []string{"Restore will not delete or overwrite them. Move them aside yourself and run restore again."},
			}
		}
		return nil
	}
	if pres != nil {
		if err := pres.Unchanged(); err != nil {
			return err
		}
	}
	cur, err := os.Lstat(absDB)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRestoreActiveFileChanged, err)
	}
	return sameActiveFile(insp.fi, cur)
}

func gateFailure(err error, pres *sqlite.Preservation) error {
	kept := ""
	if pres != nil {
		kept = fmt.Sprintf("; the completed preservation at %s was kept", pres.Dir())
	}
	if errors.Is(err, sqlite.ErrPreserveSourceChanged) {
		return fmt.Errorf("the active database changed after it was preserved: %w (restore stopped; the active database was not opened or replaced%s)", err, kept)
	}
	return fmt.Errorf("%w (restore stopped; the active database was not opened or replaced%s)", err, kept)
}

// journalGate decides, immediately before the replacement, whether the
// active database's -journal may be removed by it. An absent or empty journal
// always may. A non-empty one may only if a completed preservation recorded a
// journal and the journal on disk is byte-for-byte what was recorded -- so the
// only copy of its data is the preservation, and removing it loses nothing.
// When requireUnchanged is set (the flows that never opened the original) the
// whole preservation must additionally still match the original. It never
// removes anything itself.
func journalGate(absDB string, pres *sqlite.Preservation, requireUnchanged bool) error {
	sc, err := sqlite.ActiveSidecars(absDB)
	if err != nil {
		return fmt.Errorf("examining the rollback journal: %w", err)
	}
	if !sc.Journal.NonEmpty() {
		return nil
	}
	if pres == nil {
		return fmt.Errorf("%w (%s-journal holds %d bytes and nothing was preserved)", ErrRestoreJournalNotPreserved, absDB, sc.Journal.Size)
	}
	var rec sqlite.PreservedFile
	found := false
	for _, f := range pres.Files() {
		if f.Name == "journal" {
			rec, found = f, true
		}
	}
	if !found {
		return fmt.Errorf("%w (%s-journal holds %d bytes but the preservation at %s has no journal)", ErrRestoreJournalNotPreserved, absDB, sc.Journal.Size, pres.Dir())
	}
	sum, size, err := hashFile(absDB + "-journal")
	if err != nil {
		return fmt.Errorf("reading %s-journal: %w", absDB, err)
	}
	if sum != rec.SHA256 || size != rec.Size {
		return fmt.Errorf("%w (%s-journal differs from the journal preserved at %s)", ErrRestoreJournalNotPreserved, absDB, pres.Dir())
	}
	if requireUnchanged {
		if err := pres.Unchanged(); err != nil {
			return fmt.Errorf("%w (%v)", ErrRestoreJournalNotPreserved, err)
		}
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path) // #nosec G304 -- the active database's own companion
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func recoveryNote(r RestoreResult) string {
	switch {
	case r.SafetySnapshot != "" && r.RawPreservation != "":
		return fmt.Sprintf("; the safety snapshot at %s and the raw preservation at %s were kept", r.SafetySnapshot, r.RawPreservation)
	case r.SafetySnapshot != "":
		return fmt.Sprintf("; the safety snapshot at %s was kept", r.SafetySnapshot)
	case r.RawPreservation != "":
		return fmt.Sprintf("; the raw preservation at %s was kept", r.RawPreservation)
	}
	return ""
}

// displacedNote tells the operator where the original database files that this
// restore moved aside are retained, "" when none are. Nothing is deleted there.
//
// It distinguishes what the retained main file is: in a flow where SQLite opened
// and closed the original first (the safety snapshot flow), the retained main file
// is the database as SQLite left it after that close; a raw preservation, when one
// was made, is the byte-for-byte state from before the restore and is a separate
// directory.
func displacedNote(r RestoreResult) string {
	if r.DisplacedDir == "" {
		return ""
	}
	ctx := ""
	if r.SafetySnapshot != "" {
		ctx = "; in this flow SQLite had opened and closed the database first, so the main file there is its state after that close"
	}
	if r.RawPreservation != "" {
		ctx += fmt.Sprintf("; the byte-for-byte preservation from before the restore is separate, at %s", r.RawPreservation)
	}
	return fmt.Sprintf(" (the original database files -- the main file and any -wal, -shm and -journal -- moved aside by this restore remain in %s; nothing was deleted%s)", r.DisplacedDir, ctx)
}

// vanishedPaths maps the names the displacement reports ("main", "wal",
// "journal", "shm") to original paths. Only files that existed when the database
// was examined and then disappeared are ever named: an already-absent file is not.
func vanishedPaths(dbPath string, names []string) []string {
	var out []string
	for _, n := range names {
		switch n {
		case "main":
			out = append(out, dbPath)
		case "wal":
			out = append(out, dbPath+"-wal")
		case "journal":
			out = append(out, dbPath+"-journal")
		case "shm":
			out = append(out, dbPath+"-shm")
		}
	}
	return out
}

// vanishedNote tells the operator that files vanished during the replacement and
// were therefore not carried forward, "" when none did.
func vanishedNote(r RestoreResult) string {
	if len(r.VanishedFiles) == 0 {
		return ""
	}
	return fmt.Sprintf(" (note: %s existed when the original database was examined but vanished during the replacement, so %s neither retained nor restored)",
		strings.Join(r.VanishedFiles, ", "), map[bool]string{true: "it is", false: "they are"}[len(r.VanishedFiles) == 1])
}

// snapshotNote names the safety snapshot, "" when none was taken.
func snapshotNote(r RestoreResult) string {
	if r.SafetySnapshot == "" {
		return ""
	}
	return fmt.Sprintf(" (safety snapshot at %s)", r.SafetySnapshot)
}

// displacementState says, from the structured displacement result only, where the
// original main database file is. It never claims that the database path is empty:
// at most that the original main file is not there.
func displacementState(de *sqlite.DisplacementError, dbPath string) string {
	var main string
	switch de.Main {
	case sqlite.MainAtOriginal:
		main = fmt.Sprintf("the original main database file is back at %s, but not every one of its companion files is accounted for (see above): do not use that database until they are located", dbPath)
	case sqlite.MainUntouched:
		main = "this restore moved nothing"
	case sqlite.MainNone:
		main = "there was no main database file to displace"
	case sqlite.MainRetained:
		retained := ""
		for _, f := range de.Retained {
			if f.Name == "main" {
				retained = f.Retained
			}
		}
		main = fmt.Sprintf("the original main database file is retained at %s and is NOT at %s (anything found at %s was not put there by this restore and was not touched)", retained, dbPath, dbPath)
	case sqlite.MainUnknown:
		main = fmt.Sprintf("the original main database file could not be located (see above), so %s is not known to hold it", dbPath)
	case sqlite.MainVanished:
		main = fmt.Sprintf("the original main database file vanished before it could be moved (see above) and is not at %s", dbPath)
	default:
		main = "the state of the original files is described above"
	}
	var extra []string
	if len(de.Vanished) > 0 {
		extra = append(extra, fmt.Sprintf("%d file(s) vanished during the restore and are neither retained nor restored", len(de.Vanished)))
	}
	if len(de.Unknown) > 0 {
		extra = append(extra, fmt.Sprintf("%d file(s) could not be located", len(de.Unknown)))
	}
	if len(de.RestoredInPlace) > 0 {
		extra = append(extra, fmt.Sprintf("%d companion file(s) were restored and could not be displaced again", len(de.RestoredInPlace)))
	}
	if len(extra) > 0 {
		main += "; " + strings.Join(extra, "; ")
	}
	return main
}

func rawNote(r RestoreResult) string {
	if r.RawPreservation == "" {
		return ""
	}
	return fmt.Sprintf(", or copy the raw preservation at %s back", r.RawPreservation)
}

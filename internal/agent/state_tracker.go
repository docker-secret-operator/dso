package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/faultinject"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// RotationState tracks in-flight rotations to enable recovery after crashes
type RotationState struct {
	ProviderName string `json:"provider_name"`
	SecretName   string `json:"secret_name"`
	// OriginalContainerID is the legacy comma-joined form, still written so older
	// readers keep working. OriginalContainerIDs is the structured source of truth
	// (empty in state files written by older releases; OriginalIDs() handles both).
	OriginalContainerID  string   `json:"original_container_id"`
	OriginalContainerIDs []string `json:"original_container_ids,omitempty"`
	NewContainerID       string   `json:"new_container_id"`
	// RotationID uniquely identifies this rotation attempt; Phase is the last
	// phase DSO acknowledged ("rotation_requested", "completed", ...). Recovery
	// never trusts Phase over what Docker actually shows: a crash between a Docker
	// action and its acknowledgement leaves Phase behind reality.
	RotationID string `json:"rotation_id,omitempty"`
	Phase      string `json:"phase,omitempty"`

	// Transition journal (see internal/rotation/journal.go). Written BEFORE each
	// destructive step (intent_*) and after it; no plaintext secret value is ever
	// stored, only an opaque version identifier.
	ServiceID             string   `json:"service_id,omitempty"`
	OwnerID               string   `json:"owner_id,omitempty"`
	Strategy              string   `json:"strategy,omitempty"`
	CandidateContainerIDs []string `json:"candidate_container_ids,omitempty"`
	SecretVersion         string   `json:"secret_version,omitempty"`
	Transition            string   `json:"transition,omitempty"`
	PendingIntent         string   `json:"pending_intent,omitempty"` // an intent_* with no acknowledgement yet
	ExpectedProxyTarget   string   `json:"expected_proxy_target,omitempty"`
	ObservedProxyTarget   string   `json:"observed_proxy_target,omitempty"`
	Committed             bool     `json:"committed,omitempty"` // swap_committed acknowledged
	CleanupStatus         string   `json:"cleanup_status,omitempty"`
	Seq                   int      `json:"seq,omitempty"`
	OperationState        string   `json:"operation_state,omitempty"` // named condition, e.g. partially_published
	ManualRecovery        bool     `json:"manual_recovery,omitempty"`
	// N19 managed file delivery (all omitempty: journals written before it load
	// unchanged, with GatedDelivery false = the legacy rule). GatedDelivery is set when
	// the CURRENT candidate was created with the startup gate; DeliveryVerified only by
	// secrets_delivered (every file delivered and probed). While GatedDelivery is true a
	// candidate is verified only if DeliveryVerified too (see recordCandidateVerified).
	// DeliveredVersions are keyed identities (hmac:<key id>:<mac>), never plaintext.
	GatedDelivery     bool     `json:"gated_delivery,omitempty"`
	DeliveryVerified  bool     `json:"delivery_verified,omitempty"`
	DeliveredVersions []string `json:"delivered_versions,omitempty"`
	// DeliveredByCandidate keeps the keyed identities of EVERY candidate that reached
	// secrets_delivered in this record (key: candidate container ID), never reset by a
	// later candidate. Several targets of one dispatch share one record, so the single
	// DeliveredVersions slice is last-writer only; this map is the per-target/per-replica
	// evidence the ledger bridge cross-checks against the dispatch's version (an applied
	// version is never recorded unless EVERY delivered candidate carried it).
	DeliveredByCandidate map[string][]string `json:"delivered_by_candidate,omitempty"`
	// DispatchTargets is the number of containers the engine's dispatch covers
	// (0 = not recorded: a record written before M1 or by a strategy on its own).
	// DispatchFinalized is set only by the engine's CompleteRotationID, i.e. after
	// EVERY target of the dispatch finished without error. Several targets share
	// ONE record, and the strategies' own "completed"/"committed" transitions are
	// written by whichever target finishes first, so the record's status alone
	// never proves that the whole dispatch was delivered (item g). The ledger
	// bridge refuses delivery evidence from a multi-target record that is not
	// finalised.
	DispatchTargets   int       `json:"dispatch_targets,omitempty"`
	DispatchFinalized bool      `json:"dispatch_finalized,omitempty"`
	Detail            string    `json:"detail,omitempty"`
	Status            string    `json:"status"` // in_progress, completed, rollback_required, recovered, critical_error
	StartTime         time.Time `json:"start_time"`
	LastUpdate        time.Time `json:"last_update"`
	ErrorMessage      string    `json:"error_message,omitempty"` // For critical_error status

	// Attempt model (S2: data model only; nothing reads or writes these yet).
	// JournalSchema is an informational per-record marker (absent/0 = legacy v1); it
	// cannot be relied on after a write by an older binary, which drops it. Attempts is
	// the per-operation attempt list. Both are omitempty so a record without them is
	// byte-identical to a v1 record.
	JournalSchema int            `json:"journal_schema,omitempty"`
	Attempts      []AttemptState `json:"attempts,omitempty"`

	// key is the record's slot in StateTracker.states. It is never persisted: it is
	// derived from the record's identity on load (see normalizeLoaded) so that a
	// stale or cloned snapshot can address exactly the record it was taken from,
	// including preserved legacy records that are not addressable by RotationID.
	key string

	// legacyFileUnverified is derived (never persisted) by the tracker when it hands a
	// record out (StateTracker.view): the record is an UNRESOLVED restart/rolling record
	// written before N19 (no gate flag) for a secret that is delivered as a file TODAY.
	// Its delivery was an archive upload whose target (tmpfs vs writable layer) was never
	// verified, so the legacy format alone does not establish required file delivery
	// (recordCandidateVerified).
	legacyFileUnverified bool
}

// AttemptState is one attempt of an operation (S2 data model; unused until S3).
// ManualReason is free text here: the code vocabulary is a later, separately
// approved step.
type AttemptState struct {
	Attempt          int    `json:"attempt"`
	ParentAttempt    int    `json:"parent_attempt,omitempty"`
	Strategy         string `json:"strategy"`
	TargetOriginalID string `json:"target_original_id"`
	ServiceName      string `json:"service_name,omitempty"`
	State            string `json:"state"`
	BeganSeq         int    `json:"began_seq"`
	EndedSeq         int    `json:"ended_seq,omitempty"`
	Detail           string `json:"detail,omitempty"`
	RecoveryPass     int    `json:"recovery_pass,omitempty"`
	RecoveryStep     string `json:"recovery_step,omitempty"`
	ManualReason     string `json:"manual_reason,omitempty"`
	ComposeProject   string `json:"compose_project,omitempty"`
	ComposeService   string `json:"compose_service,omitempty"`
}

// ErrIllegalTransition is returned for an intent-class write the attempt model refuses
// (an intent or attempt_begin for an attempt that is not open, or an inadmissible
// BeginAttempt). Nothing is written when it is returned.
var ErrIllegalTransition = errors.New("illegal journal transition")

// AttemptBegin describes an attempt to be opened by BeginAttempt.
type AttemptBegin struct {
	RotationID       string
	Strategy         string
	TargetOriginalID string
	// ServiceName is stored on the attempt as supplied. No write-once or conflict check
	// is applied (A-07 is deferred), and the record-level ServiceID is not touched.
	ServiceName   string
	ParentAttempt int
}

// isManualEscalation is the single manual-escalation predicate (A-05): an event that
// asks for operator action on one of the three terminal-failure transitions. Other
// transitions that carry the flag only persist it.
func isManualEscalation(e rotation.JournalEvent) bool {
	if !e.ManualRecovery {
		return false
	}
	switch e.Transition {
	case rotation.TrFailed, rotation.TrSignalFailed, rotation.TrRolledBack:
		return true
	}
	return false
}

// manualMessage is the deterministic ErrorMessage of a manual escalation, built from
// the incoming event's own State and Detail, never from values merged from earlier
// events. A signal escalation keeps its signal-specific text.
func manualMessage(e rotation.JournalEvent) string {
	if e.Transition == rotation.TrSignalFailed {
		return "signal rotation ended in state " + e.State + "; manual recovery required"
	}
	msg := "manual recovery required"
	switch {
	case e.State != "":
		msg += ": " + e.State
	case e.Detail != "":
		msg += ": " + e.Detail
	}
	return msg
}

// deriveStatus is the ordered status derivation (J-3, rules 1 to 5): manual dominates;
// critical_error is sticky; an open attempt keeps the record in_progress; a terminal
// mapping applies only when no attempt is open; otherwise the previous status carries
// forward (a plain failed event has no mapping). For a record without attempts it
// reduces to the legacy event rules plus manual escalation. terminal is the status the
// event maps to ("completed", "recovered") or "".
func deriveStatus(prev string, attempts []AttemptState, manual bool, terminal string) string {
	open := false
	for _, a := range attempts {
		if a.State == "escalated" {
			manual = true
		}
		if a.State == "open" {
			open = true
		}
	}
	switch {
	case manual:
		return "critical_error"
	case prev == "critical_error":
		return "critical_error"
	case open:
		return "in_progress"
	case terminal != "":
		return terminal
	}
	return prev
}

// effectiveAttempts is the read view of a record's attempts: the attempts themselves,
// or, for a record written before the attempt model, one implicit attempt 1 whose state
// is derived from the stored status.
func effectiveAttempts(r *RotationState) []AttemptState {
	if len(r.Attempts) > 0 {
		return r.Attempts
	}
	state := "failed"
	switch r.Status {
	case "in_progress":
		state = "open"
	case "completed":
		state = "completed"
	case "recovered":
		state = "rolled_back"
	case "rollback_required":
		state = "failed"
	case "critical_error":
		state = "escalated"
	}
	target := ""
	if ids := r.OriginalIDs(); len(ids) > 0 {
		target = ids[0]
	}
	return []AttemptState{{Attempt: 1, Strategy: r.Strategy, TargetOriginalID: target, ServiceName: r.ServiceID, State: state}}
}

// mirrorStrategy is the legacy record-level Strategy for a record with attempts: the
// strategy of the open attempt, or of the most recently begun one when none is open.
func mirrorStrategy(attempts []AttemptState) string {
	// The highest-numbered open attempt wins; with none open, the highest-numbered one.
	pick := func(onlyOpen bool) string {
		best, strategy := 0, ""
		for _, a := range attempts {
			if a.Strategy == "" || (onlyOpen && a.State != "open") {
				continue
			}
			if a.Attempt >= best {
				best, strategy = a.Attempt, a.Strategy
			}
		}
		return strategy
	}
	if m := pick(true); m != "" {
		return m
	}
	return pick(false)
}

// BeginAttempt opens a new attempt of a rotation (A-02) and persists it, with the
// per-record journal_schema marker, in one staged write. It is an intent-class call:
// when it returns an error nothing was written and the strategy must not proceed.
//
// Admission: the record is not critical_error, rollback_required or finalised; no other
// attempt of the same target is open; and a parent, when given, is a terminal
// rolled_back or failed attempt with no pending intent and no committed swap. The
// supplied ServiceName is stored as is (no write-once or conflict check) and the
// record-level ServiceID is not touched.
func (st *StateTracker) BeginAttempt(ctx context.Context, b AttemptBegin) (int, error) {
	_ = ctx
	if b.RotationID == "" {
		return 0, errors.New("attempt without a rotation ID")
	}
	if b.TargetOriginalID == "" {
		return 0, errors.New("attempt without a target container")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	tx := st.beginStage()
	state := st.byID(b.RotationID)
	if state != nil {
		state = tx.touch(state)
	} else {
		if err := st.lookupErr(b.RotationID); errors.Is(err, ErrRotationIdentityAmbiguous) {
			return 0, err
		}
		state = &RotationState{RotationID: b.RotationID, Status: "in_progress", StartTime: time.Now(), key: opKey(b.RotationID)}
		tx.states[state.key] = state
	}
	refuse := func(why string) (int, error) {
		return 0, fmt.Errorf("%w: attempt for rotation %q refused: %s", ErrIllegalTransition, b.RotationID, why)
	}
	switch {
	case state.Status == "critical_error":
		return refuse("the record needs manual recovery")
	case state.Status == "rollback_required":
		return refuse("the engine already requested a rollback")
	case state.DispatchFinalized:
		return refuse("the dispatch is finalised")
	}
	next := 1
	for _, a := range state.Attempts {
		if a.Attempt >= next {
			next = a.Attempt + 1
		}
		if a.State == "open" && a.TargetOriginalID == b.TargetOriginalID {
			return refuse("another attempt of target " + b.TargetOriginalID + " is open")
		}
	}
	if b.ParentAttempt != 0 {
		var parent *AttemptState
		for i := range state.Attempts {
			if state.Attempts[i].Attempt == b.ParentAttempt {
				parent = &state.Attempts[i]
			}
		}
		switch {
		case parent == nil:
			return refuse("the parent attempt does not exist")
		case parent.State != "rolled_back" && parent.State != "failed":
			return refuse("the parent attempt is " + parent.State + ", not rolled_back or failed")
		case state.PendingIntent != "" || state.Committed:
			return refuse("the parent attempt left an unresolved intent or a committed swap")
		}
	}
	state.Seq++
	state.Attempts = append(state.Attempts, AttemptState{
		Attempt: next, ParentAttempt: b.ParentAttempt, Strategy: b.Strategy, TargetOriginalID: b.TargetOriginalID,
		ServiceName: b.ServiceName, State: "open", BeganSeq: state.Seq,
	})
	state.JournalSchema = 2
	state.Transition, state.Phase = rotation.TrAttemptBegin, rotation.TrAttemptBegin
	state.PendingIntent = ""
	state.LastUpdate = time.Now()
	if m := mirrorStrategy(state.Attempts); m != "" {
		state.Strategy = m
	}
	state.Status = deriveStatus(state.Status, state.Attempts, false, "")
	if err := st.commitStage(tx); err != nil {
		return 0, err
	}
	return next, nil
}

// clone returns a deep copy. The tracker hands out clones, never its live records:
// recovery and the dispatch guard read records while Record() may be updating them
// under st.mu, and a shared pointer would make those reads a data race.
func (r *RotationState) clone() *RotationState {
	c := *r
	c.OriginalContainerIDs = append([]string(nil), r.OriginalContainerIDs...)
	c.CandidateContainerIDs = append([]string(nil), r.CandidateContainerIDs...)
	c.DeliveredVersions = append([]string(nil), r.DeliveredVersions...)
	if r.Attempts != nil {
		// AttemptState holds only value fields, so copying the elements is a deep copy.
		c.Attempts = append(make([]AttemptState, 0, len(r.Attempts)), r.Attempts...)
	}
	if r.DeliveredByCandidate != nil {
		c.DeliveredByCandidate = make(map[string][]string, len(r.DeliveredByCandidate))
		for k, v := range r.DeliveredByCandidate {
			c.DeliveredByCandidate[k] = append([]string(nil), v...)
		}
	}
	return &c
}

// view is the single place the tracker hands a record out: a deep copy decorated with
// the derived legacy-file flag. The caller must hold st.mu (read or write).
func (st *StateTracker) view(r *RotationState) *RotationState {
	c := r.clone()
	if st.FileModeSecret != nil && r.Seq > 0 && !r.GatedDelivery &&
		(r.Strategy == "restart" || r.Strategy == "rolling") &&
		r.Status != "completed" && r.Status != "recovered" &&
		r.ProviderName != "" && r.SecretName != "" && st.FileModeSecret(r.ProviderName, r.SecretName) {
		c.legacyFileUnverified = true
	}
	return c
}

// OriginalIDs returns the container IDs of this rotation as a slice, from the
// structured field when present, otherwise by splitting the legacy comma-joined
// string (blank entries dropped).
func (r *RotationState) OriginalIDs() []string {
	if len(r.OriginalContainerIDs) > 0 {
		return append([]string(nil), r.OriginalContainerIDs...)
	}
	return splitContainerIDs(r.OriginalContainerID)
}

func splitContainerIDs(joined string) []string {
	var out []string
	for _, p := range strings.Split(joined, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// StateTracker persists rotation state to enable recovery from crashes
type StateTracker struct {
	stateDir string
	logger   *zap.Logger
	mu       sync.RWMutex
	states   map[string]*RotationState

	// corrupt is set when the persisted state file existed but could not be
	// parsed. The unreadable file is preserved (renamed) as evidence and recovery
	// performs NO mutations while this is set: with unknown state the only safe
	// action is to leave every workload alone and report.
	corrupt     bool
	corruptPath string
	// unreadable is set when rotations.json exists but could not be READ (permission
	// denied, I/O error, not a regular file). The file is left exactly where it is, so
	// persistState must never replace it: doing so would destroy every unresolved
	// record it holds. corrupt is set with it, so recovery and the guard stand down.
	unreadable bool

	// inherited holds the keys (in states) of records that were loaded from disk
	// still in_progress: they belong to a PREVIOUS process, so no goroutine of this
	// one is working on them. The operation guard treats such a record as an
	// unresolved operation until recovery resolves it.
	inherited map[string]bool

	// migrationLegacy holds the exact bytes of a legacy-shaped rotations.json until
	// the migrated file has replaced it (see backupLegacyJournal).
	migrationLegacy []byte

	// FileModeSecret, when set (the trigger engine wires it from the configuration, before
	// any record is read), says whether a secret is delivered as a file. It lets the
	// tracker flag unresolved pre-N19 file-mode records (see RotationState.legacyFileUnverified).
	// nil: no record is flagged (bare test trackers, env-only deployments).
	FileModeSecret func(provider, secret string) bool
}

// Corrupt reports whether the persisted state could not be read at startup, and
// where the unreadable file was preserved.
func (st *StateTracker) Corrupt() (bool, string) {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.corrupt, st.corruptPath
}

// NewStateTracker creates a state tracker with persistent storage
func NewStateTracker(stateDir string, logger *zap.Logger) (*StateTracker, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	st := &StateTracker{
		stateDir: stateDir,
		logger:   logger,
		states:   make(map[string]*RotationState),
	}

	// Load any previously saved states
	if err := st.loadStates(); err != nil {
		logger.Warn("Failed to load previous rotation states", zap.Error(err))
	}

	return st, nil
}

// Operation identity (F-S6-1).
//
// A journal record is one OPERATION and is identified by its RotationID, the
// identity the trigger engine mints per dispatch and hands to the strategies through
// the context. The record lives at opKey(RotationID); every completion, rollback,
// recovery and delete call addresses it by that identity. The former
// provider:secret:original-container-ids key is not an identity (two dispatches of
// one secret share it, as do successive signal rotations, whose container ids never
// change) and is never written any more; it only survives as the lookup rule of the
// deprecated key-based wrappers below, which refuse to act when it is ambiguous.
//
// What stays out of the identity on purpose: the project, service, owner and
// strategy are attributes OF the operation (ServiceID, OwnerID, Strategy), not part
// of its identity, because one dispatch legitimately covers several services of the
// same secret under one RotationID (see the ledger's delivery unit and the S6
// report). The operation guard keeps matching on those attributes.
const journalKeyPrefix = "journal:"

func opKey(rotationID string) string { return journalKeyPrefix + rotationID }

var (
	// ErrRotationNotFound: no record carries this rotation identity.
	ErrRotationNotFound = errors.New("rotation record not found")
	// ErrRotationIDInUse: a record with this rotation identity already exists; a
	// new operation never overwrites an existing one.
	ErrRotationIDInUse = errors.New("rotation id is already in use")
	// ErrRotationIdentityAmbiguous: the identity is shared by several preserved
	// legacy records (or by a legacy record whose key contradicts its content).
	// Nothing is modified; an operator must review the preserved records.
	ErrRotationIdentityAmbiguous = errors.New("rotation identity is ambiguous in the journal; operator review required")
)

// byID returns the live record whose identity is rotationID, or nil. Only an
// identity-sound record (stored at opKey(its own RotationID)) qualifies. The caller
// must hold st.mu.
func (st *StateTracker) byID(rotationID string) *RotationState {
	if rotationID == "" {
		return nil
	}
	if r := st.states[opKey(rotationID)]; r != nil && r.RotationID == rotationID {
		return r
	}
	return nil
}

// lookupErr says why byID found nothing: the identity is unknown, or it exists only
// in preserved, non-addressable legacy records (fail closed). The caller must hold
// st.mu.
func (st *StateTracker) lookupErr(rotationID string) error {
	if rotationID == "" {
		return fmt.Errorf("%w: empty rotation id", ErrRotationNotFound)
	}
	for _, r := range st.states {
		if r.RotationID == rotationID {
			return fmt.Errorf("%w: %q", ErrRotationIdentityAmbiguous, rotationID)
		}
	}
	return fmt.Errorf("%w: %q", ErrRotationNotFound, rotationID)
}

// recordByID returns a copy of the record of rotationID, or nil (also nil when the
// identity is ambiguous).
func (st *StateTracker) recordByID(rotationID string) *RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if r := st.byID(rotationID); r != nil {
		return st.view(r)
	}
	return nil
}

// StartRotation marks a rotation as in progress
func (st *StateTracker) StartRotation(providerName, secretName, originalContainerID, newContainerID string) error {
	return st.StartRotationWithID("", providerName, secretName, originalContainerID, newContainerID)
}

// StartRotationWithID is StartRotation with a caller-chosen rotation ID (""
// generates one), so the trigger engine can hand the same ID to the strategies via
// the context and the journal records land on this record.
func (st *StateTracker) StartRotationWithID(rotationID, providerName, secretName, originalContainerID, newContainerID string) error {
	return st.StartRotationWithVersion(rotationID, providerName, secretName, originalContainerID, newContainerID, "")
}

// StartRotationWithVersion is StartRotationWithID that also records the target
// secret version of the dispatch in the journal record (owner decision O-2), so
// a journal-committed rotation states which version it delivered and the ledger
// can be reconciled from the journal alone after a crash (window W6).
// secretVersion is "" (unknown, the pre-O-2 behaviour) or a keyed HMAC identity
// in the FormatJournalSecretVersion encoding "hmac:<key id>:<mac>": never a
// secret value, never an unkeyed hash. Anything else is refused and nothing is
// recorded. Older journals without the field load unchanged (omitempty).
//
// The new record is stored under its own rotation identity and NEVER replaces any
// other record: not an in-flight operation of the same secret, not a completed
// predecessor whose evidence the ledger or recovery still needs (F-B2), not an
// unresolved one. A rotation id that is already present is refused (nothing is
// modified).
func (st *StateTracker) StartRotationWithVersion(rotationID, providerName, secretName, originalContainerID, newContainerID, secretVersion string) error {
	return st.startRotation(rotationID, providerName, secretName, originalContainerID, newContainerID, secretVersion, 0)
}

// StartRotationForDispatch is StartRotationWithVersion for a managed dispatch
// that also records how many target containers the dispatch covers
// (DispatchTargets), so the ledger bridge can tell a fully finished dispatch from
// one that only some of its targets completed.
func (st *StateTracker) StartRotationForDispatch(rotationID, providerName, secretName, originalContainerID, newContainerID, secretVersion string, targets int) error {
	return st.startRotation(rotationID, providerName, secretName, originalContainerID, newContainerID, secretVersion, targets)
}

func (st *StateTracker) startRotation(rotationID, providerName, secretName, originalContainerID, newContainerID, secretVersion string, targets int) error {
	if secretVersion != "" && !validJournalHMACVersion(secretVersion) {
		return errors.New("journal secret version must be a keyed HMAC identity (hmac:<key id>:<mac>)")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if rotationID == "" {
		rotationID = newRotationID()
	}
	if st.byID(rotationID) != nil {
		return fmt.Errorf("%w: %q", ErrRotationIDInUse, rotationID)
	}
	if err := st.lookupErr(rotationID); errors.Is(err, ErrRotationIdentityAmbiguous) {
		return err
	}

	key := opKey(rotationID)
	tx := st.beginStage()
	tx.states[key] = &RotationState{
		ProviderName:         providerName,
		SecretName:           secretName,
		OriginalContainerID:  originalContainerID,
		OriginalContainerIDs: splitContainerIDs(originalContainerID),
		NewContainerID:       newContainerID,
		RotationID:           rotationID,
		SecretVersion:        secretVersion,
		DispatchTargets:      targets,
		Phase:                "rotation_requested",
		Status:               "in_progress",
		StartTime:            time.Now(),
		LastUpdate:           time.Now(),
		key:                  key,
	}
	return st.commitStage(tx)
}

// CompleteRotationID marks the rotation rotationID completed. It touches only that
// record: a stale completion of an older operation cannot complete a newer one. An
// operator-required critical_error is never turned into "completed" by this call.
func (st *StateTracker) CompleteRotationID(rotationID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := st.byID(rotationID)
	if state == nil {
		return st.lookupErr(rotationID)
	}
	return st.completeLocked(state)
}

func (st *StateTracker) completeLocked(state *RotationState) error {
	if state.Status == "critical_error" {
		return nil
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "completed"
	state.Phase = "completed"
	state.DispatchFinalized = true
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

// MarkRollbackID marks the rotation rotationID as requiring rollback. The engine
// calls this for ANY dispatch error, after the strategy has already journalled its
// own verdict. A critical_error (an operator is required, e.g. a partially published
// signal rotation) must never be downgraded to an ordinary, non-blocking failed
// attempt: that would silently lift the guard.
func (st *StateTracker) MarkRollbackID(rotationID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := st.byID(rotationID)
	if state == nil {
		return st.lookupErr(rotationID)
	}
	return st.rollbackLocked(state)
}

func (st *StateTracker) rollbackLocked(state *RotationState) error {
	if state.Status == "critical_error" {
		return nil
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "rollback_required"
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

// DeleteStateID removes the record of rotationID (a dispatch that never ran: its
// targets were busy, blocked or absent, or its start could not be recorded durably).
// It removes only that record and refuses to remove an operator-required
// critical_error record, which is durable evidence.
func (st *StateTracker) DeleteStateID(rotationID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := st.byID(rotationID)
	if state == nil {
		return st.lookupErr(rotationID)
	}
	return st.deleteLocked(state)
}

func (st *StateTracker) deleteLocked(state *RotationState) error {
	if state.Status == "critical_error" {
		return fmt.Errorf("rotation %q is critical_error and is not deleted: it is evidence an operator needs", state.RotationID)
	}
	tx := st.beginStage()
	delete(tx.states, state.key)
	tx.stageInherited()
	delete(tx.inherited, state.key)
	return st.commitStage(tx)
}

// MarkRecoveredID / MarkCriticalErrorID update the record of rotationID.
func (st *StateTracker) MarkRecoveredID(rotationID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := st.byID(rotationID)
	if state == nil {
		return st.lookupErr(rotationID)
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "recovered"
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

func (st *StateTracker) MarkCriticalErrorID(rotationID, errorMsg string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := st.byID(rotationID)
	if state == nil {
		return st.lookupErr(rotationID)
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "critical_error"
	state.ErrorMessage = errorMsg
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

// resolveLegacy maps the deprecated (provider, secret, original container ids)
// triple to exactly one record, or fails closed. Candidates are the records whose
// own fields carry that triple (never a map key). Among them the OPEN ones
// (in_progress of this process, rollback_required, and with includeUnresolved also
// critical_error and inherited in_progress) are the only valid targets of a
// transition; if exactly one is open it is the target, if several are open the call
// is ambiguous and nothing is modified. With none open, a single remaining candidate
// is the target, several are ambiguous. Not found is (nil, nil), as before. The
// caller must hold st.mu.
func (st *StateTracker) resolveLegacy(providerName, secretName, originalContainerID string, includeUnresolved bool) (*RotationState, error) {
	var all, open []*RotationState
	for k, r := range st.states {
		if r.ProviderName != providerName || r.SecretName != secretName || r.OriginalContainerID != originalContainerID {
			continue
		}
		unresolved := r.Status == "critical_error" || (r.Status == "in_progress" && st.inherited[k])
		if unresolved && !includeUnresolved {
			continue
		}
		all = append(all, r)
		if unresolved || r.Status == "in_progress" || r.Status == "rollback_required" {
			open = append(open, r)
		}
	}
	switch {
	case len(open) == 1:
		return open[0], nil
	case len(open) > 1:
		return nil, fmt.Errorf("%w: %d open operations of %s:%s", ErrRotationIdentityAmbiguous, len(open), providerName, secretName)
	case len(all) == 1:
		return all[0], nil
	case len(all) > 1:
		return nil, fmt.Errorf("%w: %d operations of %s:%s", ErrRotationIdentityAmbiguous, len(all), providerName, secretName)
	}
	return nil, nil
}

// CompleteRotation marks a rotation as completed.
//
// Deprecated: identifies the operation by (provider, secret, container ids), which is
// not an identity. Use CompleteRotationID. Acts only when the triple resolves to
// exactly one record.
func (st *StateTracker) CompleteRotation(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, err := st.resolveLegacy(providerName, secretName, originalContainerID, false)
	if state == nil {
		return err
	}
	return st.completeLocked(state)
}

// MarkRollback marks a rotation as requiring rollback.
//
// Deprecated: use MarkRollbackID (see CompleteRotation).
func (st *StateTracker) MarkRollback(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, err := st.resolveLegacy(providerName, secretName, originalContainerID, false)
	if state == nil {
		return err
	}
	return st.rollbackLocked(state)
}

// GetPendingRotations returns all rotations that require recovery
func (st *StateTracker) GetPendingRotations() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var pending []*RotationState
	now := time.Now()
	for _, key := range st.sortedKeys() {
		state := st.states[key]
		// Include in_progress rotations older than 5 minutes (assume crashed)
		if state.Status == "in_progress" && now.Sub(state.LastUpdate) > 5*time.Minute {
			pending = append(pending, st.view(state))
		}
		// Include explicit rollback_required or critical_error states
		if state.Status == "rollback_required" || state.Status == "critical_error" {
			pending = append(pending, st.view(state))
		}
	}
	return pending
}

// GetUnfinishedRotationsAtStartup returns every rotation that never reached a
// terminal state. It must only be used right after the agent started: no rotation
// can be in flight in a process that has just begun, so an "in_progress" record
// (of any age) is by definition an interrupted one. GetPendingRotations keeps its
// five-minute heuristic for the running agent.
func (st *StateTracker) GetUnfinishedRotationsAtStartup() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()
	var out []*RotationState
	for _, key := range st.sortedKeys() {
		state := st.states[key]
		switch state.Status {
		case "in_progress", "rollback_required", "critical_error":
			out = append(out, st.view(state))
		}
	}
	return out
}

// CleanupOldStates removes completed and recovered rotations older than retentionPeriod.
// This prevents state file bloat from long-running agents.
func (st *StateTracker) CleanupOldStates(retentionPeriod time.Duration) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := time.Now().Add(-retentionPeriod)
	var removed []string

	for key, state := range st.states {
		if state.Status == "completed" || state.Status == "recovered" {
			if state.LastUpdate.Before(cutoff) {
				removed = append(removed, key)
			}
		}
	}

	if len(removed) > 0 {
		// Staged (A-03): the records leave memory only once the journal without them
		// has been committed.
		tx := st.beginStage()
		for _, key := range removed {
			delete(tx.states, key)
		}
		st.logger.Debug("Cleaning up old rotation states",
			zap.Int("removed_count", len(removed)),
			zap.Duration("retention_period", retentionPeriod))
		return st.commitStage(tx)
	}

	return nil
}

// MarkRecoveredRecord / MarkCriticalRecord update a record by identity (see
// liveRecord). They are compare-and-set against the snapshot they were given: when
// the live record has moved on since the snapshot was taken (a different Status or
// Seq, e.g. it completed in between) the call is refused and nothing is modified, so
// a stale snapshot can never resolve, escalate or overwrite a newer state.
func (st *StateTracker) MarkRecoveredRecord(rec *RotationState) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	live, err := st.liveFor(rec)
	if err != nil {
		return err
	}
	now := time.Now()
	tx := st.beginStage()
	staged := tx.touch(live)
	staged.Status, staged.LastUpdate = "recovered", now
	if err := st.commitStage(tx); err != nil {
		return err
	}
	// The caller's snapshot is updated only once the change is committed.
	rec.Status, rec.LastUpdate = "recovered", now
	return nil
}

func (st *StateTracker) MarkCriticalRecord(rec *RotationState, msg string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	live, err := st.liveFor(rec)
	if err != nil {
		return err
	}
	now := time.Now()
	tx := st.beginStage()
	staged := tx.touch(live)
	staged.Status, staged.ErrorMessage, staged.LastUpdate = "critical_error", msg, now
	if err := st.commitStage(tx); err != nil {
		return err
	}
	rec.Status, rec.ErrorMessage, rec.LastUpdate = "critical_error", msg, now
	return nil
}

// liveFor resolves rec to the live record and checks it is still what rec saw.
func (st *StateTracker) liveFor(rec *RotationState) (*RotationState, error) {
	live := st.liveRecord(rec)
	if live == nil {
		return nil, fmt.Errorf("rotation record %q not found in the journal", rec.RotationID)
	}
	if live != rec && (live.Status != rec.Status || live.Seq != rec.Seq) {
		return nil, fmt.Errorf("rotation record %q changed since the snapshot (%s seq %d, now %s seq %d): stale snapshot refused",
			rec.RotationID, rec.Status, rec.Seq, live.Status, live.Seq)
	}
	return live, nil
}

// liveRecord resolves a (possibly cloned) record to the tracker's own record by
// identity, never by secret name: the record's own slot when the clone still names
// the same operation there (this is what addresses a preserved legacy record that
// is not reachable by RotationID), else the RotationID, else - for records without
// one, written by older releases - provider/secret/original/start time. A
// RotationID that is ambiguous resolves to nothing. The caller must hold st.mu.
func (st *StateTracker) liveRecord(rec *RotationState) *RotationState {
	for _, s := range st.states {
		if s == rec {
			return s
		}
	}
	if rec.key != "" {
		if s := st.states[rec.key]; s != nil && s.RotationID == rec.RotationID && s.StartTime.Equal(rec.StartTime) {
			return s
		}
	}
	if rec.RotationID != "" {
		return st.byID(rec.RotationID)
	}
	for _, key := range st.sortedKeys() {
		s := st.states[key]
		if s.RotationID == "" && s.ProviderName == rec.ProviderName && s.SecretName == rec.SecretName &&
			s.OriginalContainerID == rec.OriginalContainerID && s.StartTime.Equal(rec.StartTime) {
			return s
		}
	}
	return nil
}

// sortedKeys returns the record slots in a fixed order, so every reader that
// iterates the journal (recovery, the ledger snapshot, the guard) sees the same
// order regardless of Go's randomised map iteration. The caller must hold st.mu.
func (st *StateTracker) sortedKeys() []string {
	keys := make([]string, 0, len(st.states))
	for k := range st.states {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// snapshotRecords returns the current records in a deterministic order (read-only
// use by recovery).
func (st *StateTracker) snapshotRecords() []*RotationState {
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*RotationState, 0, len(st.states))
	for _, k := range st.sortedKeys() {
		out = append(out, st.view(st.states[k]))
	}
	return out
}

// MarkRecovered marks a rotation as recovered after automatic recovery completed.
//
// Deprecated: use MarkRecoveredID (see CompleteRotation).
func (st *StateTracker) MarkRecovered(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, err := st.resolveLegacy(providerName, secretName, originalContainerID, true)
	if state == nil {
		return err
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "recovered"
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

// MarkCriticalError marks a rotation as having a critical error that requires operator intervention.
//
// Deprecated: use MarkCriticalErrorID (see CompleteRotation).
func (st *StateTracker) MarkCriticalError(providerName, secretName, originalContainerID, errorMsg string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, err := st.resolveLegacy(providerName, secretName, originalContainerID, true)
	if state == nil {
		return err
	}
	tx := st.beginStage()
	state = tx.touch(state)
	state.Status = "critical_error"
	state.ErrorMessage = errorMsg
	state.LastUpdate = time.Now()
	return st.commitStage(tx)
}

// DeleteState removes a completed rotation from tracking.
//
// Deprecated: use DeleteStateID (see CompleteRotation). Never removes an unresolved
// record, and acts only when the triple resolves to exactly one record.
func (st *StateTracker) DeleteState(providerName, secretName, originalContainerID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	state, err := st.resolveLegacy(providerName, secretName, originalContainerID, false)
	if state == nil {
		return err
	}
	return st.deleteLocked(state)
}

// stagedWrite is the copy-on-write view a writer mutates (A-03, JSM 4.15). states is a
// copy of the record map; a record is cloned (touch) before it is changed, so the live
// records are never modified. inherited is copied only when a writer changes it.
type stagedWrite struct {
	st        *StateTracker
	states    map[string]*RotationState
	inherited map[string]bool // nil until stageInherited
	clones    map[*RotationState]*RotationState
}

// beginStage starts a staged write. The caller must hold st.mu for writing, until
// commitStage returns.
func (st *StateTracker) beginStage() *stagedWrite {
	states := make(map[string]*RotationState, len(st.states)+1)
	for k, v := range st.states {
		states[k] = v
	}
	return &stagedWrite{st: st, states: states, clones: map[*RotationState]*RotationState{}}
}

// touch returns the staged clone of a live record, to be mutated instead of the live
// record. A record that is already a staged clone or was created in the stage is
// returned unchanged.
func (tx *stagedWrite) touch(r *RotationState) *RotationState {
	if c, ok := tx.clones[r]; ok {
		return c
	}
	for _, c := range tx.clones {
		if c == r {
			return r
		}
	}
	slot := ""
	if r.key != "" && tx.states[r.key] == r {
		slot = r.key
	} else {
		for k, v := range tx.states {
			if v == r {
				slot = k
				break
			}
		}
	}
	if slot == "" {
		return r // created in this stage, not live
	}
	if _, live := tx.st.states[slot]; !live || tx.st.states[slot] != r {
		return r
	}
	c := r.clone()
	tx.states[slot] = c
	tx.clones[r] = c
	return c
}

// stageInherited copies the inherited set so a writer can change it.
func (tx *stagedWrite) stageInherited() {
	if tx.inherited != nil {
		return
	}
	tx.inherited = make(map[string]bool, len(tx.st.inherited))
	for k, v := range tx.st.inherited {
		tx.inherited[k] = v
	}
}

// commitStage persists the staged records and, only if that succeeded (the rename is
// the commit point), publishes them to memory. On error memory is unchanged and the
// journal file is unchanged (persistMap returns no error after the rename).
func (st *StateTracker) commitStage(tx *stagedWrite) error {
	if err := st.persistMap(tx.states); err != nil {
		return err
	}
	for k := range st.states {
		if _, ok := tx.states[k]; !ok {
			delete(st.states, k)
		}
	}
	for k, v := range tx.states {
		st.states[k] = v
	}
	if tx.inherited != nil {
		st.inherited = tx.inherited
	}
	return nil
}

// persistState persists the tracker's current st.states. Writers do not call it
// directly any more: they stage their change on a copy and commit it with
// commitStage, which publishes to memory only after the rename (A-03 / JSM 4.15).
// It remains for callers that already hold the final state in st.states (Close,
// the migration persist in loadStates and the white-box tests). The caller must
// hold st.mu.
func (st *StateTracker) persistState() error {
	return st.persistMap(st.states)
}

// persistMap writes states to rotations.json with the temp, fsync, rename protocol.
//
// Commit point (F-18): os.Rename. Every error this function returns is returned
// BEFORE the rename, so a non-nil error means the journal file is unchanged. A failure
// of the directory fsync that follows a successful rename does not undo the commit:
// it is logged (syncStateDir) and the function returns nil.
func (st *StateTracker) persistMap(states map[string]*RotationState) error {
	if st.unreadable {
		// Fail closed: callers abort the action a journal write announces. The unread
		// journal on disk is the only record of unresolved operations; never replace it.
		return fmt.Errorf("rotation journal %s is unreadable: refusing to overwrite it (fix its permissions or restore it, then restart the agent)", st.corruptPath)
	}
	if st.migrationLegacy != nil {
		// The file on disk still has the legacy shape: never replace it before its
		// bytes are preserved. A failure here refuses every journal write (callers
		// fail closed) and is retried by the next write.
		if err := st.backupLegacyJournal(st.migrationLegacy); err != nil {
			return fmt.Errorf("legacy rotation journal could not be preserved, refusing to rewrite it: %w", err)
		}
		if migrationFault != nil {
			if err := migrationFault("after_backup"); err != nil {
				return err
			}
		}
	}
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Crash-safe replace: write a temp file, fsync it, rename over the state
	// file, then fsync the directory so the rename itself is durable. This
	// protects against a process crash at any point (the state file is always
	// the old or the new complete version) and, with the fsyncs, against power
	// loss on filesystems that honour them. It does not make the Docker action
	// and its acknowledgement one atomic transaction.
	tmpFile := stateFile + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 -- fixed name under the agent state dir
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync state file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close state file: %w", err)
	}
	_ = faultinject.Hit("state:before_persist_rename")
	if st.migrationLegacy != nil && migrationFault != nil {
		if err := migrationFault("before_persist"); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpFile, stateFile); err != nil {
		return fmt.Errorf("failed to rename state file: %w", err)
	}
	st.migrationLegacy = nil
	_ = faultinject.Hit("state:after_persist_rename")
	st.syncStateDir("rotation journal")
	return nil
}

// syncDirFn fsyncs a directory. It is a package-level variable only so the tests can
// make the post-rename directory sync fail; production never changes it.
var syncDirFn = func(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- agent state dir
	if err != nil {
		return err
	}
	serr := d.Sync()
	if cerr := d.Close(); serr == nil {
		serr = cerr
	}
	return serr
}

// syncStateDir makes a rename in the state directory durable. It runs only after the
// rename succeeded, so its failure is a durability outcome, not a failed commit: it is
// logged at Warn and never returned (F-18). EINVAL and ENOTSUP mean the filesystem
// does not support directory fsync and are not logged. No metric is emitted (OD-X8).
func (st *StateTracker) syncStateDir(what string) {
	err := syncDirFn(st.stateDir)
	if err == nil || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return
	}
	if st.logger != nil {
		st.logger.Warn("State directory fsync failed after the rename; the committed "+what+
			" may not survive a power loss",
			zap.String("state_dir", st.stateDir), zap.Error(err))
	}
}

// Record implements rotation.Journal. It upserts the record for e.RotationID
// (creating a journal-only record when the rotation was not started by the trigger
// engine, e.g. a manual rotation), merges the event, and persists durably before
// returning: a nil return means the event survives a process crash. An error means
// the caller must NOT perform the action the event announces.
func (st *StateTracker) Record(ctx context.Context, e rotation.JournalEvent) error {
	if e.RotationID == "" {
		return errors.New("journal event without a rotation ID")
	}
	// An attempt-bearing attempt_begin is never an ordinary event: attempts are opened by
	// BeginAttempt only. An unattributed one (attempt number 0) is a plain legacy
	// transition record that creates no attempt; the exhaustive tracker tests write every
	// transition constant through Record, and a record carrying it without attempts is
	// read as legacy (JSM 4.12, re-upgrade after an older binary's write).
	if e.Transition == rotation.TrAttemptBegin && e.Attempt > 0 {
		return fmt.Errorf("%w: attempt_begin is written by BeginAttempt only", ErrIllegalTransition)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	// Staged (A-03): the event is merged into a copy; memory changes only after the
	// journal file has been committed, so a refused or failed event leaves no record,
	// status, sequence number or intent behind.
	tx := st.beginStage()
	state := st.byID(e.RotationID)
	if state != nil {
		state = tx.touch(state)
	} else {
		// Unknown identity: a journal-only record (a rotation not started by the
		// trigger engine). An identity that exists only in preserved ambiguous
		// legacy records fails closed instead: no event may attach to a record that
		// cannot be told apart from another operation.
		if err := st.lookupErr(e.RotationID); errors.Is(err, ErrRotationIdentityAmbiguous) {
			return err
		}
		state = &RotationState{
			RotationID: e.RotationID, Status: "in_progress", StartTime: time.Now(), key: opKey(e.RotationID),
		}
		tx.states[state.key] = state
	}
	// One operation has one owner. An event from a different owner is a foreign
	// writer: refuse it before anything is merged (fail closed).
	if e.OwnerID != "" && state.OwnerID != "" && state.OwnerID != e.OwnerID {
		return fmt.Errorf("journal event for rotation %q from owner %q refused: the record belongs to another owner", e.RotationID, e.OwnerID)
	}
	// N19: a version recorded for delivered values must be a keyed identity, never a
	// plaintext or an unkeyed hash. Refused BEFORE anything below is merged.
	for _, v := range e.DeliveredVersions {
		if !validJournalHMACVersion(v) {
			return errors.New("journal delivered version must be a keyed HMAC identity (hmac:<key id>:<mac>)")
		}
	}
	// Attempt addressing. Only an event without an attempt number (0) is unattributed: it
	// never changes the attempt list and follows the legacy record-level rules. An event
	// that carries an attempt number must match a stored attempt; with none it is refused
	// (intent class) or dropped, and never reaches the record-level merge below. Record has
	// no implicit attempt 1: only BeginAttempt creates attempts.
	var cur *AttemptState
	if e.Attempt > 0 {
		for i := range state.Attempts {
			if state.Attempts[i].Attempt == e.Attempt {
				cur = &state.Attempts[i]
			}
		}
		switch st.attemptVerdict(cur, e) {
		case verdictRefuse:
			return fmt.Errorf("%w: %s for attempt %d of rotation %q is not allowed in the attempt's state", ErrIllegalTransition, e.Transition, e.Attempt, e.RotationID)
		case verdictDrop:
			st.logger.Warn("Journal event dropped: it does not apply to the attempt's current state",
				zap.String("rotation_id", e.RotationID), zap.Int("attempt", e.Attempt), zap.String("transition", e.Transition))
			return nil
		case verdictIdempotent:
			return nil
		}
	}
	if e.ServiceID != "" {
		state.ServiceID = e.ServiceID
	}
	if e.OwnerID != "" {
		state.OwnerID = e.OwnerID
	}
	if e.Strategy != "" && cur == nil {
		state.Strategy = e.Strategy
	}
	if e.SecretVersion != "" {
		state.SecretVersion = e.SecretVersion
	}
	if e.ExpectedProxyTarget != "" {
		state.ExpectedProxyTarget = e.ExpectedProxyTarget
	}
	if e.ObservedProxyTarget != "" {
		state.ObservedProxyTarget = e.ObservedProxyTarget
	}
	state.OriginalContainerIDs = unionIDs(state.OriginalContainerIDs, e.OriginalIDs)
	if len(state.OriginalContainerIDs) > 0 && state.OriginalContainerID == "" {
		state.OriginalContainerID = strings.Join(state.OriginalContainerIDs, ",")
	}
	state.CandidateContainerIDs = unionIDs(state.CandidateContainerIDs, e.CandidateIDs)
	if e.Detail != "" {
		state.Detail = e.Detail
	}
	if e.State != "" {
		state.OperationState = e.State
	}
	if e.ManualRecovery {
		state.ManualRecovery = true
	}
	switch e.Transition {
	case rotation.TrIntentCreateCandidate:
		// A new candidate: nothing about an earlier candidate's delivery carries over.
		state.GatedDelivery, state.DeliveryVerified, state.DeliveredVersions = false, false, nil
	case rotation.TrCandidateCreated:
		if e.Gated {
			state.GatedDelivery, state.DeliveryVerified, state.DeliveredVersions = true, false, nil
		}
	case rotation.TrSecretsDelivered:
		state.DeliveryVerified = true
		state.DeliveredVersions = append([]string(nil), e.DeliveredVersions...)
		if state.DeliveredByCandidate == nil {
			state.DeliveredByCandidate = map[string][]string{}
		}
		if len(e.CandidateIDs) == 0 {
			state.DeliveredByCandidate[""] = append([]string(nil), e.DeliveredVersions...)
		}
		for _, id := range e.CandidateIDs {
			state.DeliveredByCandidate[id] = append([]string(nil), e.DeliveredVersions...)
		}
	}
	state.Seq++
	state.Transition, state.Phase = e.Transition, e.Transition
	state.LastUpdate = time.Now()
	if strings.HasPrefix(e.Transition, "intent_") {
		state.PendingIntent = e.Transition
	} else {
		state.PendingIntent = ""
	}
	// Record-level fields set by the transition itself; the status comes from the
	// single derivation below.
	terminal := ""
	switch e.Transition {
	case rotation.TrSwapCommitted:
		state.Committed = true
	case rotation.TrIntentCleanup:
		state.CleanupStatus = "intent"
	case rotation.TrCompleted:
		// A debt completion keeps the status (the replacement serves) and marks the
		// retained old container; a normal completion is done.
		if e.State == "completed_cleanup_debt" {
			state.CleanupStatus = "debt"
		} else {
			state.CleanupStatus = "done"
		}
		terminal = "completed"
	case rotation.TrSignalDone:
		// Terminal. The record keeps OperationState (reload requested, adoption NOT
		// verified): "completed" never means the application loaded the secret.
		terminal = "completed"
	case rotation.TrSignalFailed:
		if !e.ManualRecovery {
			terminal = "recovered"
		}
	case rotation.TrRolledBack:
		// The candidate was discarded and the original never touched: terminal.
		terminal = "recovered"
	}
	manual := isManualEscalation(e)
	if manual {
		state.ErrorMessage = manualMessage(e)
	}
	// The addressed attempt follows the event (JSM 4.5).
	if cur != nil {
		if next := attemptEnd(cur.State, e, manual); next != cur.State {
			cur.State = next
			if next != "open" {
				cur.EndedSeq = state.Seq
				if e.Detail != "" {
					cur.Detail = e.Detail
				}
			}
			if next == "escalated" {
				cur.ManualReason = e.ManualReason
			}
		}
		if m := mirrorStrategy(state.Attempts); m != "" {
			state.Strategy = m
		}
	}
	state.Status = deriveStatus(state.Status, state.Attempts, manual, terminal)
	// A later rotation of the same service that reached a clean terminal state
	// supersedes earlier manual-recovery records of that service, so a restart does
	// not re-flag a service the operator has since repaired.
	if (e.Transition == rotation.TrSignalDone || e.Transition == rotation.TrCompleted) && state.ServiceID != "" {
		var superseded []*RotationState
		for _, other := range tx.states {
			if other != state && other.ServiceID == state.ServiceID && other.Status == "critical_error" {
				superseded = append(superseded, other)
			}
		}
		for _, other := range superseded {
			other = tx.touch(other)
			other.Status = "recovered"
			other.Detail = "superseded by a later successful rotation " + state.RotationID
			other.LastUpdate = time.Now()
		}
	}
	return st.commitStage(tx)
}

// attemptVerdict says what an event addressed to an attempt does (JSM 4.5). The rows are
// the attempt's state. An intent-class event that does not apply is refused; any other
// event is dropped; a repeated terminal event is idempotent and writes nothing.
type attemptVerdictKind int

const (
	verdictApply attemptVerdictKind = iota
	verdictIdempotent
	verdictDrop
	verdictRefuse
)

func (st *StateTracker) attemptVerdict(cur *AttemptState, e rotation.JournalEvent) attemptVerdictKind {
	intent := strings.HasPrefix(e.Transition, "intent_")
	notApplicable := verdictDrop
	if intent {
		notApplicable = verdictRefuse
	}
	if cur == nil {
		return notApplicable
	}
	manual := isManualEscalation(e)
	switch cur.State {
	case "open":
		return verdictApply
	case "completed":
		if e.Transition == rotation.TrCompleted || e.Transition == rotation.TrSignalDone {
			return verdictIdempotent
		}
	case "rolled_back":
		if manual {
			return verdictApply
		}
		if e.Transition == rotation.TrRolledBack {
			return verdictIdempotent
		}
	case "failed":
		if manual {
			return verdictApply
		}
		if e.Transition == rotation.TrFailed {
			return verdictIdempotent
		}
	case "escalated":
		if manual {
			return verdictIdempotent
		}
	}
	return notApplicable
}

// attemptEnd is the state an applied event leaves the attempt in.
func attemptEnd(current string, e rotation.JournalEvent, manual bool) string {
	switch {
	case manual:
		return "escalated"
	case current != "open":
		return current
	}
	switch e.Transition {
	case rotation.TrCompleted, rotation.TrSignalDone:
		return "completed"
	case rotation.TrRolledBack:
		return "rolled_back"
	case rotation.TrFailed, rotation.TrSignalFailed:
		return "failed"
	}
	return current
}

func unionIDs(have, add []string) []string {
	seen := map[string]bool{}
	for _, h := range have {
		seen[h] = true
	}
	out := append([]string(nil), have...)
	for _, a := range add {
		if a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

func newRotationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rot-%d", time.Now().UnixNano())
	}
	return "rot-" + hex.EncodeToString(b)
}

func (st *StateTracker) loadStates() error {
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	// #nosec G304 -- filename is a fixed literal; st.stateDir is a config-time
	// value set at agent construction, not per-request input
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No previous state
		}
		// The file exists but cannot be read (EACCES under a different uid, EIO, EISDIR,
		// EMFILE ...). Nothing is known about interrupted operations, exactly as for an
		// unparsable file, so fail safe the same way: flag it (recovery, the operation
		// guard, the orphan sweep and the proxy startup gate all stand down) and never
		// write over it. Unlike a parse error it cannot be quarantined by a rename we
		// can trust to preserve the bytes, so it stays in place until the operator fixes
		// the cause and restarts.
		st.corrupt, st.corruptPath, st.unreadable = true, stateFile, true
		return fmt.Errorf("rotation journal %s exists but could not be read (left untouched; automatic recovery and journal writes disabled): %w", stateFile, err)
	}

	raw := make(map[string]*RotationState)
	if err := json.Unmarshal(data, &raw); err != nil {
		// Keep the unreadable file as evidence and fail safe: recovery will do
		// nothing automatic while the persisted state is unknown.
		quarantined := fmt.Sprintf("%s.corrupt-%d", stateFile, time.Now().Unix())
		if rerr := os.Rename(stateFile, quarantined); rerr != nil {
			quarantined = stateFile
		}
		st.corrupt, st.corruptPath = true, quarantined
		return fmt.Errorf("failed to unmarshal state (preserved at %s; automatic recovery disabled): %w", quarantined, err)
	}

	states, report := normalizeLoaded(raw)
	st.states = states
	st.inherited = make(map[string]bool)
	for k, r := range states {
		if r.Status == "in_progress" {
			st.inherited[k] = true
		}
	}
	st.logger.Info("Loaded rotation states", zap.Int("count", len(states)))
	for _, n := range report.notes {
		st.logger.Warn("Rotation journal migration: " + n)
	}
	if report.changed {
		// The persisted file still has the legacy shape. It is rewritten by the next
		// persist, but never before its exact bytes are preserved aside.
		st.migrationLegacy = data
		st.logger.Info("Migrating the rotation journal to operation-identity keys",
			zap.Int("rekeyed", report.rekeyed), zap.Int("duplicates_dropped", report.dropped),
			zap.Int("conflicting_preserved", report.conflicts), zap.Int("ambiguous_preserved", report.ambiguous))
		return st.persistState()
	}
	return nil
}

// Persisted key shapes.
//
//	journal:<RotationID>      the operation record (current format; also the format
//	                          of journal-only records of earlier releases)
//	<provider>:<secret>:<ids> legacy trigger-engine record (releases before F-S6-1)
//	preserved:...             legacy record re-keyed to protect it (no RotationID)
//	dup:<RotationID>:<hash>   one of several DIFFERENT records that claim the same
//	                          RotationID; none of them is addressable by that id
//	ambiguous:<old key>       a record whose key contradicts its own content
//
// Only journal:<RotationID> is addressable by RotationID. Every other shape is kept
// verbatim, is visible to recovery (by container ids and rotation label) and to the
// operation guard, and is resolved only by its own slot (recovery) or by an operator.
type migrationReport struct {
	changed   bool
	rekeyed   int // legacy records moved to their operation key
	dropped   int // byte-identical duplicates folded into one record
	conflicts int // different records claiming one RotationID, preserved as dup:
	ambiguous int // records whose key contradicts their content, preserved as ambiguous:
	notes     []string
}

// normalizeLoaded maps a persisted journal onto operation-identity keys without
// losing or reinterpreting any record. It is a pure function: the same input always
// yields the same output (inputs are processed in sorted key order), and applying it
// to its own output changes nothing (idempotent).
func normalizeLoaded(raw map[string]*RotationState) (map[string]*RotationState, migrationReport) {
	var rep migrationReport
	out := make(map[string]*RotationState, len(raw))
	canon := func(r *RotationState) string {
		c := *r
		c.key = ""
		b, _ := json.Marshal(&c)
		return string(b)
	}
	put := func(key string, r *RotationState) {
		r.key = key
		out[key] = r
	}

	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	groups := map[string][]string{} // RotationID -> original keys of identity candidates
	for _, k := range keys {
		r := raw[k]
		switch {
		case r == nil:
			rep.changed = true
			rep.notes = append(rep.notes, fmt.Sprintf("record %q is null and was dropped (the pre-migration file is preserved)", k))
			continue
		case r.RotationID == "":
			// Written by a release without rotation ids: nothing to re-identify.
			put(k, r)
		case strings.HasPrefix(k, "dup:") || strings.HasPrefix(k, "ambiguous:"):
			put(k, r) // already marked by an earlier migration
		case strings.HasPrefix(k, journalKeyPrefix):
			if k == opKey(r.RotationID) {
				groups[r.RotationID] = append(groups[r.RotationID], k)
			} else {
				rep.ambiguousRecord(out, put, k, r, "key names another rotation id")
			}
		case strings.HasPrefix(k, "preserved:"):
			rep.ambiguousRecord(out, put, k, r, "preserved record carries a rotation id")
		default:
			if k == fmt.Sprintf("%s:%s:%s", r.ProviderName, r.SecretName, r.OriginalContainerID) {
				groups[r.RotationID] = append(groups[r.RotationID], k)
			} else {
				rep.ambiguousRecord(out, put, k, r, "legacy key does not match the record")
			}
		}
	}

	rids := make([]string, 0, len(groups))
	for rid := range groups {
		rids = append(rids, rid)
	}
	sort.Strings(rids)
	for _, rid := range rids {
		members := groups[rid] // sorted: keys were sorted
		first := canon(raw[members[0]])
		identical := true
		for _, k := range members[1:] {
			if canon(raw[k]) != first {
				identical = false
				break
			}
		}
		switch {
		case len(members) == 1 || identical:
			// One operation (a byte-identical duplicate is the same record).
			r := raw[members[0]]
			for _, k := range members {
				if k == opKey(rid) {
					r = raw[k]
				}
			}
			put(opKey(rid), r)
			if len(members) > 1 {
				rep.dropped += len(members) - 1
			}
			if members[0] != opKey(rid) || len(members) > 1 {
				rep.changed = true
			}
			if members[0] != opKey(rid) && len(members) == 1 {
				rep.rekeyed++
			}
		default:
			// Different records claiming one identity: which one is "the" operation is
			// not decidable. Preserve every one, address none by the id.
			rep.changed = true
			seen := map[string]bool{}
			for _, k := range members {
				r := raw[k]
				sum := sha256.Sum256([]byte(canon(r)))
				dk := "dup:" + rid + ":" + hex.EncodeToString(sum[:6])
				if seen[dk] {
					rep.dropped++
					continue
				}
				seen[dk] = true
				r.Detail = appendDetail(r.Detail, "rotation identity "+rid+" is claimed by several different journal records; none was assumed to be the operation (operator review required)")
				put(dk, r)
				rep.conflicts++
			}
			rep.notes = append(rep.notes, fmt.Sprintf("rotation id %q is claimed by %d different records: all preserved, none addressable by that id", rid, len(members)))
		}
	}
	return out, rep
}

func (rep *migrationReport) ambiguousRecord(out map[string]*RotationState, put func(string, *RotationState), key string, r *RotationState, why string) {
	r.Detail = appendDetail(r.Detail, "journal key "+key+" contradicts the record ("+why+"); preserved unchanged, not addressable by rotation id (operator review required)")
	put("ambiguous:"+key, r)
	rep.changed = true
	rep.ambiguous++
	rep.notes = append(rep.notes, fmt.Sprintf("record %q: %s: preserved as ambiguous", key, why))
}

func appendDetail(have, add string) string {
	if have == "" {
		return add
	}
	if strings.Contains(have, add) {
		return have
	}
	return have + "; " + add
}

// migrationFault is a test seam for the interruption points of the legacy-journal
// migration ("after_backup", "before_persist"); nil in production. A non-nil error
// aborts the migration at that point, as a crash there would.
var migrationFault func(point string) error

// backupLegacyJournal preserves the exact pre-migration bytes of rotations.json as
// rotations.json.legacy-<unix ts> (0600, written to a temp file, fsynced and renamed,
// then the directory fsynced). An existing backup with identical bytes is reused, so
// an interrupted migration that is retried (or repeated startups) never multiplies
// backups. It never overwrites an existing, different file.
func (st *StateTracker) backupLegacyJournal(data []byte) error {
	stateFile := filepath.Join(st.stateDir, "rotations.json")
	want := sha256.Sum256(data)
	if matches, _ := filepath.Glob(stateFile + ".legacy-*"); len(matches) > 0 {
		for _, m := range matches {
			// #nosec G304 -- glob under the fixed agent state dir
			if have, err := os.ReadFile(m); err == nil && sha256.Sum256(have) == want {
				return nil
			}
		}
	}
	base := fmt.Sprintf("%s.legacy-%d", stateFile, time.Now().Unix())
	dest := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		if i > 1000 {
			return errors.New("no free name for the legacy journal backup")
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) // #nosec G304 -- fixed name under the agent state dir
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	st.syncStateDir("legacy journal backup")
	return nil
}

// Close gracefully closes the state tracker
func (st *StateTracker) Close() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	// Final persist before closing
	return st.persistState()
}

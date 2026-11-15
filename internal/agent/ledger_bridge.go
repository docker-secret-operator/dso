package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// S4b ledger bridge (docs/design/m1-ledger-reconciliation.md sections 2, 4.2,
// 4.3, 5 and 12.5 step 4). It converts the authoritative M0 journal (the
// StateTracker records) into the plain inputs of the S4a ledger facade
// (ledger.Evidence, ledger.JournalSnapshot) and offers the calls the S6 wiring
// makes; ledger_wiring.go is its only production caller.
//
// Rules the bridge keeps:
//   - the journal is the only authority on rotation lifecycle; the bridge only
//     READS StateTracker records (it never calls Record, Mark*, DeleteState or
//     CleanupOldStates) and never touches Docker or the operation guard (X1);
//   - delivery evidence is decided by ONE rule: candidateVerified (recovery.go,
//     the N21 rule) for restart and rolling, signal_done for signal, plus the
//     terminal status rule of section 4.2 (journalDeliveryRefusal). The ledger
//     re-checks the same rule and refuses anything weaker;
//   - a signal record is delivery verified, adoption UNVERIFIED (X5);
//   - critical_error, rollback_required, recovered without a committed swap,
//     in_progress without a committed swap, Seq 0 and every pre-ready transition
//     are never evidence (X2);
//   - without a journal-carried version (owner decision O-2; absent from
//     records written before StartRotationWithVersion or by a dispatch without
//     a ledger version) reconciliation cannot apply a delivery: the replicas
//     become unknown and no applied version advances (the S4a decision). In process the version the
//     engine correlated (the hint) is used instead.

// ledgerJournalRetention mirrors the journal retention of StartAll
// (CleanupOldStates(168h) in trigger.go). A ledger delivery claim whose journal
// record is absent is accepted only when it is older than now minus this
// horizon (rule R-C).
const ledgerJournalRetention = 168 * time.Hour

var (
	// ErrBridgeNoVersion means the journal record carries no version identity
	// (O-2) and the caller supplied no hint, so a delivery cannot be applied.
	ErrBridgeNoVersion = errors.New("ledger bridge: the journal does not carry the delivered version")
	// ErrBridgeJournalStartFailed means StartRotationWithID did not return nil:
	// the ledger may not cite the operation (window W14).
	ErrBridgeJournalStartFailed = errors.New("ledger bridge: the journal did not record the operation")
	// ErrBridgeNoTracker means no StateTracker was supplied.
	ErrBridgeNoTracker = errors.New("ledger bridge: no journal (StateTracker)")
)

// Journal statuses as the StateTracker writes them.
const (
	journalStatusInProgress       = "in_progress"
	journalStatusCompleted        = "completed"
	journalStatusRollbackRequired = "rollback_required"
	journalStatusRecovered        = "recovered"
	journalStatusCriticalError    = "critical_error"
)

// journalEvidenceKind is the single strategy/transition mapping of the bridge.
// It is built on candidateVerified so the ledger's notion of "delivery
// verified" can never diverge from the M0 recovery's notion of "candidate
// verified" (N21). A record without a strategy transition (Seq 0) or with an
// unknown strategy is pre-ready.
func journalEvidenceKind(rec *RotationState) ledger.EvidenceKind {
	if rec == nil || rec.Seq <= 0 {
		return ledger.EvidencePreReady
	}
	switch rec.Strategy {
	case "restart":
		if candidateVerified(rec.Transition) {
			return ledger.EvidenceRestartVerified
		}
	case "rolling":
		if candidateVerified(rec.Transition) {
			return ledger.EvidenceRollingVerified
		}
	case "signal":
		if rec.Transition == rotation.TrSignalDone {
			return ledger.EvidenceSignalDone
		}
	}
	return ledger.EvidencePreReady
}

// journalDeliveryRefusal applies the terminal status rule of design section 4.2
// on top of journalEvidenceKind. It returns nil when the record proves a
// successful delivery, else an error wrapping ledger.ErrNoEvidence.
func journalDeliveryRefusal(rec *RotationState) error {
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ledger.ErrNoEvidence, fmt.Sprintf(format, args...))
	}
	if rec == nil {
		return refuse("no journal record")
	}
	if rec.RotationID == "" {
		return refuse("the journal record has no rotation id")
	}
	if rec.Seq <= 0 {
		return refuse("no strategy transition was journalled (Seq 0)")
	}
	kind := journalEvidenceKind(rec)
	if kind == ledger.EvidencePreReady {
		return refuse("strategy %q at transition %q is before candidate_ready", rec.Strategy, rec.Transition)
	}
	switch rec.Status {
	case journalStatusCriticalError, journalStatusRollbackRequired:
		return refuse("journal status %s is never delivery evidence", rec.Status)
	case journalStatusCompleted:
		return nil
	case journalStatusRecovered, journalStatusInProgress:
		// recovered alone proves only that M0 recovery restored a healthy workload;
		// in_progress proves nothing yet. Only a committed swap (restart/rolling)
		// shows the verified candidate took over the name (design 4.2, W5).
		if kind != ledger.EvidenceSignalDone && rec.Committed {
			return nil
		}
		return refuse("journal status %s without a committed swap is not a successful dispatch", rec.Status)
	default:
		return refuse("unknown journal status %q", rec.Status)
	}
}

// FormatJournalSecretVersion renders a ledger version identity as the opaque
// journal SecretVersion string "kind:key_id:id" (design 1.3, O-2). It carries no
// secret value. A provider identity with a source cannot be rendered and
// yields "". StartRotationWithVersion records the HMAC form in the journal (O-2).
func FormatJournalSecretVersion(v ledger.Version) string {
	switch v.Kind {
	case ledger.KindHMAC:
		if v.KeyID == "" || v.ID == "" || v.Source != "" || strings.Contains(v.KeyID, ":") {
			return ""
		}
		return string(ledger.KindHMAC) + ":" + v.KeyID + ":" + v.ID
	case ledger.KindProvider:
		if v.ID == "" || v.KeyID != "" || v.Source != "" {
			return ""
		}
		return string(ledger.KindProvider) + "::" + v.ID
	}
	return ""
}

// parseJournalSecretVersion is the inverse of FormatJournalSecretVersion. An
// empty or unrecognised string means "the journal does not know the version"
// (nil), never a guess. The ledger validates the result again (key id, shape).
func parseJournalSecretVersion(s string) *ledger.Version {
	kind, rest, ok := strings.Cut(s, ":")
	if !ok {
		return nil
	}
	keyID, id, ok := strings.Cut(rest, ":")
	if !ok || id == "" {
		return nil
	}
	switch ledger.VersionKind(kind) {
	case ledger.KindHMAC:
		if keyID == "" {
			return nil
		}
		return &ledger.Version{Kind: ledger.KindHMAC, KeyID: keyID, ID: id}
	case ledger.KindProvider:
		if keyID != "" {
			return nil
		}
		return &ledger.Version{Kind: ledger.KindProvider, ID: id}
	}
	return nil
}

// validJournalHMACVersion reports whether s is a keyed HMAC identity in the
// FormatJournalSecretVersion encoding (the only form StartRotationWithVersion
// accepts, O-2): kind hmac, a ledger key id, and a base64url 32-byte MAC.
func validJournalHMACVersion(s string) bool {
	v := parseJournalSecretVersion(s)
	if v == nil || v.Kind != ledger.KindHMAC || !ledger.ValidKeyID(v.KeyID) {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(v.ID)
	return err == nil && len(raw) == sha256.Size
}

// JournalRecordFor converts one StateTracker record into the plain journal view
// ReconcileFromJournal consumes. CandidateVerified is candidateVerified itself;
// Version is the journal-carried SecretVersion (nil when the record has none).
func JournalRecordFor(rec *RotationState) ledger.JournalRecord {
	seq := uint64(0)
	if rec.Seq > 0 {
		seq = uint64(rec.Seq)
	}
	return ledger.JournalRecord{
		RotationID:        rec.RotationID,
		Strategy:          rec.Strategy,
		Status:            ledger.JournalStatus(rec.Status),
		Committed:         rec.Committed,
		Seq:               seq,
		CandidateVerified: candidateVerified(rec.Transition),
		SignalDone:        rec.Transition == rotation.TrSignalDone,
		Version:           parseJournalSecretVersion(rec.SecretVersion),
		UpdatedAt:         rec.LastUpdate,
	}
}

// journalEvidence builds the typed Evidence for rec without judging it.
func journalEvidence(rec *RotationState, ver ledger.Version, fileMode bool, live []ledger.LiveReplica) ledger.Evidence {
	r := JournalRecordFor(rec)
	return ledger.Evidence{
		RotationID: r.RotationID, Kind: journalEvidenceKind(rec), Status: r.Status, Committed: r.Committed,
		Seq: r.Seq, Version: ver, FileMode: fileMode, Live: live,
	}
}

// deliveredVersion returns the version a rotation delivered: the journal's own
// SecretVersion (O-2) when present, else the caller's correlated hint. Two
// different identities are a conflict, never resolved by picking one.
func deliveredVersion(rec *RotationState, hint *ledger.Version) (ledger.Version, error) {
	jv := parseJournalSecretVersion(rec.SecretVersion)
	switch {
	case jv != nil && hint != nil:
		if ledger.Compare(*jv, *hint) != ledger.Same {
			return ledger.Version{}, fmt.Errorf("%w: the journal version and the correlated version differ", ledger.ErrNoEvidence)
		}
		return *jv, nil
	case jv != nil:
		return *jv, nil
	case hint != nil:
		return *hint, nil
	}
	return ledger.Version{}, ErrBridgeNoVersion
}

// EvidenceForRotation returns the delivery Evidence a journal record proves,
// for RecordDeliveryVerified. It refuses (an error wrapping ledger.ErrNoEvidence)
// every record that is not a successful delivery by the rule of design 4.2, and
// returns ErrBridgeNoVersion when neither the journal (O-2) nor hint names the
// delivered version. fileMode marks a file-delivered secret (N19: adoption stays
// unverified); live optionally refreshes the replicas' container identity.
func EvidenceForRotation(rec *RotationState, hint *ledger.Version, fileMode bool, live []ledger.LiveReplica) (ledger.Evidence, error) {
	if err := journalDeliveryRefusal(rec); err != nil {
		return ledger.Evidence{}, err
	}
	ver, err := deliveredVersion(rec, hint)
	if err != nil {
		return ledger.Evidence{}, err
	}
	return journalEvidence(rec, ver, fileMode, live), nil
}

// InterruptCauseFor states, from the journal record alone, how an operation
// without delivery evidence ended (the terminal status rule). A missing record
// (finishRotation's DeleteState: deferred, blocked by the guard, no targets)
// means the dispatch never ran.
func InterruptCauseFor(rec *RotationState) ledger.InterruptCause {
	if rec == nil {
		return ledger.CauseNeverRan
	}
	switch rec.Status {
	case journalStatusRollbackRequired:
		return ledger.CauseFailed
	case journalStatusRecovered:
		switch rec.Transition {
		case rotation.TrRolledBack:
			return ledger.CauseRolledBack
		case rotation.TrSignalFailed:
			// signal_failed with status recovered: nothing was published.
			return ledger.CauseFailed
		}
	}
	if rec.Seq <= 0 && rec.Status == journalStatusCompleted {
		return ledger.CauseNeverRan
	}
	return ledger.CauseUnresolved
}

// JournalRecordByID returns a copy of the journal record of rotationID, or nil.
func JournalRecordByID(st *StateTracker, rotationID string) *RotationState {
	if st == nil || rotationID == "" {
		return nil
	}
	for _, r := range st.snapshotRecords() {
		if r.RotationID == rotationID {
			return r
		}
	}
	return nil
}

// CorrelateAfterJournalStart writes the ledger's delivery-requested hint for a
// dispatch (design 3 step 4: after StartRotationWithID, before TriggerReload).
// startErr is what StartRotationWithID returned: the ledger may cite the
// operation only when the journal recorded it durably (W14), and the record must
// be present in the journal.
func CorrelateAfterJournalStart(v *ledger.Versions, st *StateTracker, startErr error, k ledger.Key, rotationID string, desiredSeq uint64, ver ledger.Version) error {
	if startErr != nil {
		return fmt.Errorf("%w: %w", ErrBridgeJournalStartFailed, startErr)
	}
	if st == nil {
		return ErrBridgeNoTracker
	}
	if JournalRecordByID(st, rotationID) == nil {
		return fmt.Errorf("%w: %q is not in the journal", ErrBridgeJournalStartFailed, rotationID)
	}
	return v.Correlate(k, rotationID, desiredSeq, ver)
}

// BridgeOutcome says what RecordRotationOutcome did.
type BridgeOutcome string

const (
	// OutcomeDeliveryVerified: the journal proved the delivery; applied advanced.
	OutcomeDeliveryVerified BridgeOutcome = "delivery_verified"
	// OutcomeInterrupted: no evidence; the hint was resolved with Cause.
	OutcomeInterrupted BridgeOutcome = "interrupted"
)

// BridgeResult is the result of RecordRotationOutcome.
type BridgeResult struct {
	Outcome BridgeOutcome
	// Cause is set for OutcomeInterrupted.
	Cause ledger.InterruptCause
	// Adoption is set for OutcomeDeliveryVerified.
	Adoption ledger.AdoptionState
	// Unchanged is true when the ledger already held this outcome (idempotent).
	Unchanged bool
}

// RecordRotationOutcome is the in-process terminal step (design 3 step 5: from
// finishRotation, after the journal's own terminal write). It reads the
// journal record of rotationID and either records the verified delivery
// (RecordDeliveryVerified, which advances applied in the same single update,
// W7) or resolves the requested hint with the cause the journal states. hint
// is the version the engine correlated for this dispatch; it stands in for the
// journal-carried version when the record has none (pre-O-2). A journal that proves delivery but
// names no version leaves the replicas unknown (no advance).
func RecordRotationOutcome(v *ledger.Versions, st *StateTracker, k ledger.Key, rotationID string, hint *ledger.Version, fileMode bool, live []ledger.LiveReplica) (BridgeResult, error) {
	if st == nil {
		return BridgeResult{}, ErrBridgeNoTracker
	}
	rec := JournalRecordByID(st, rotationID)
	ev, err := EvidenceForRotation(rec, hint, fileMode, live)
	if err == nil {
		res, derr := v.RecordDeliveryVerified(k, ev)
		switch {
		case errors.Is(derr, ledger.ErrNoChange):
			return BridgeResult{Outcome: OutcomeDeliveryVerified, Adoption: res.Adoption, Unchanged: true}, nil
		case derr != nil:
			return BridgeResult{}, derr
		}
		return BridgeResult{Outcome: OutcomeDeliveryVerified, Adoption: res.Adoption}, nil
	}
	cause := InterruptCauseFor(rec)
	if errors.Is(err, ErrBridgeNoVersion) {
		cause = ledger.CauseUnresolved
	}
	ierr := v.HandleInterrupted(k, rotationID, cause)
	switch {
	case errors.Is(ierr, ledger.ErrNoChange):
		return BridgeResult{Outcome: OutcomeInterrupted, Cause: cause, Unchanged: true}, nil
	case ierr != nil:
		return BridgeResult{}, ierr
	}
	return BridgeResult{Outcome: OutcomeInterrupted, Cause: cause}, nil
}

// LedgerReconcileInput is what the startup reconcile needs besides the journal.
type LedgerReconcileInput struct {
	// Now is the reconcile time (zero: time.Now). The retention horizon is Now
	// minus the journal retention.
	Now time.Time
	// FileMode lists the entries whose secret is delivered as a file (N19).
	FileMode map[ledger.Key]bool
	// Live optionally gives the live replicas per entry (read-only inspection).
	Live map[ledger.Key][]ledger.LiveReplica
}

// JournalSnapshotFromTracker builds the plain journal view of st. A corrupt
// (unreadable) journal yields Readable=false, so reconcile changes nothing.
// Records without a rotation id cannot be correlated and are left out.
func JournalSnapshotFromTracker(st *StateTracker, in LedgerReconcileInput) ledger.JournalSnapshot {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	snap := ledger.JournalSnapshot{Horizon: now.Add(-ledgerJournalRetention), FileMode: in.FileMode, Live: in.Live}
	if st == nil {
		return snap
	}
	if corrupt, _ := st.Corrupt(); corrupt {
		return snap
	}
	snap.Readable = true
	for _, r := range st.snapshotRecords() {
		if r.RotationID == "" {
			continue
		}
		snap.Records = append(snap.Records, JournalRecordFor(r))
	}
	return snap
}

// ReconcileLedgerFromTracker is the startup ledger reconcile (design 5 step 5:
// the last statement of performAutomaticRecovery, after M0 recovery settled
// every record, before settleStartup and before CleanupOldStates). It is
// read-only with respect to the journal and Docker and cannot unblock anything.
func ReconcileLedgerFromTracker(v *ledger.Versions, st *StateTracker, in LedgerReconcileInput) (ledger.ReconcileReport, error) {
	if v == nil {
		return ledger.ReconcileReport{}, errors.New("ledger bridge: no ledger")
	}
	if st == nil {
		return ledger.ReconcileReport{}, ErrBridgeNoTracker
	}
	return v.ReconcileFromJournal(JournalSnapshotFromTracker(st, in))
}

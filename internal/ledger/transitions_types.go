package ledger

import (
	"errors"
	"fmt"
	"time"
)

// This file holds the vocabulary of the S4a transition facade (design note
// docs/design/m1-ledger-reconciliation.md sections 4.2, 10 and 11): the closed
// reason vocabulary, the typed journal Evidence value, the plain journal
// snapshot the caller supplies, and the errors. The facade itself is in
// transitions.go; it records facts the M0 journal has established and never
// starts, authorises, gates or sequences Docker work (invariant X1).

// OutcomeReason is the closed vocabulary of Replica.Reason and
// LastOutcome.Reason written by the facade (design section 10.1). The schema
// itself accepts any control-free text (hand-edited files are not corrupt for
// their wording), but the facade only ever writes these values and refuses to
// be given others.
type OutcomeReason string

const (
	// WhyAdoptedUnknown: observed existing workload, nothing known about it.
	WhyAdoptedUnknown OutcomeReason = "adopted_unknown"
	// WhyDeliveryRequested: a correlation hint, written after the journal
	// recorded the operation.
	WhyDeliveryRequested OutcomeReason = "delivery_requested"
	// WhyDeliveryVerified: journal evidence of a restart or rolling success.
	WhyDeliveryVerified OutcomeReason = "delivery_verified"
	// WhyAdoptionUnverifiedSignal: signal_done; delivery verified, adoption not.
	WhyAdoptionUnverifiedSignal OutcomeReason = "adoption_unverified_signal"
	// WhyAdoptionUnverifiedFile: file-mode secret while N19 is open.
	WhyAdoptionUnverifiedFile OutcomeReason = "adoption_unverified_file"
	// WhyInterrupted: an unresolved or crashed operation; nothing is known.
	WhyInterrupted OutcomeReason = "interrupted"
	// WhyIdentityChanged: the replica was recreated or is ambiguous.
	WhyIdentityChanged OutcomeReason = "identity_changed"
	// WhyDeferred: the dispatch was deferred, blocked by the guard or never ran.
	WhyDeferred OutcomeReason = "deferred"
	// WhyRolledBack: the operation ended rolled back.
	WhyRolledBack OutcomeReason = "rolled_back"
	// WhyFailed: the operation failed.
	WhyFailed OutcomeReason = "failed"
	// WhyUnreported: a live replica the operation did not report.
	WhyUnreported OutcomeReason = "unreported"
	// WhyNoRotatingStrategy: block reason, no replica has a rotating strategy.
	WhyNoRotatingStrategy OutcomeReason = "no_rotating_strategy"
	// WhyStrategyNone: block reason, the secret-level strategy is none.
	WhyStrategyNone OutcomeReason = "strategy_none"
	// WhyEligibilityChanged: a replica became eligible and never received the
	// secret (D61-2).
	WhyEligibilityChanged OutcomeReason = "eligibility_changed"
	// WhyJournalUnreadable: the journal could not be read (reconcile skipped).
	WhyJournalUnreadable OutcomeReason = "journal_unreadable"
	// WhyAmbiguousProvider: block reason with no unblock trigger (design 10.2).
	// It is the one value this implementation adds to the section 10.1 list, because
	// the section 10.2 Unblock row names it as a block reason.
	WhyAmbiguousProvider OutcomeReason = "ambiguous_provider"
)

var allOutcomeReasons = []OutcomeReason{
	WhyAdoptedUnknown, WhyDeliveryRequested, WhyDeliveryVerified, WhyAdoptionUnverifiedSignal, WhyAdoptionUnverifiedFile,
	WhyInterrupted, WhyIdentityChanged, WhyDeferred, WhyRolledBack, WhyFailed, WhyUnreported, WhyNoRotatingStrategy,
	WhyStrategyNone, WhyEligibilityChanged, WhyJournalUnreadable, WhyAmbiguousProvider,
}

// OutcomeReasons returns the closed vocabulary (a copy).
func OutcomeReasons() []OutcomeReason { return append([]OutcomeReason(nil), allOutcomeReasons...) }

// ValidOutcomeReason reports whether s is in the closed vocabulary.
func ValidOutcomeReason(s string) bool {
	for _, r := range allOutcomeReasons {
		if string(r) == s {
			return true
		}
	}
	return false
}

// LastOutcome kinds written by the facade.
const (
	outcomeSuccess     = "success"
	outcomeFailed      = "failed"
	outcomeRolledBack  = "rolled_back"
	outcomeInterrupted = "interrupted"
	outcomeDeferred    = "deferred"
	outcomeBlocked     = "blocked"
)

var (
	// ErrInvalidTransition is wrapped by every refusal of a transition whose
	// preconditions do not hold. A refused transition changes nothing.
	ErrInvalidTransition = errors.New("ledger: invalid transition")
	// ErrNoEvidence means the journal evidence is missing, forged, weaker than
	// the rule of design section 4.2 or of the wrong kind. Nothing is changed.
	ErrNoEvidence = errors.New("ledger: no valid journal evidence")
	// ErrStale means the entry's desired_seq moved on since the caller read it.
	ErrStale = errors.New("ledger: stale desired sequence")
	// ErrNoTargets means the entry has no replica a rotation could target.
	ErrNoTargets = errors.New("ledger: entry has no targeted replica")
	// ErrKeyUnavailable means no usable keyed identity can be built (key lost,
	// unsafe, malformed or different from the ledger's). It is "unknown", never
	// "changed" (X3).
	ErrKeyUnavailable = errors.New("ledger: key material unavailable")
)

// invalidTransition builds an ErrInvalidTransition with a reason.
func invalidTransition(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidTransition, fmt.Sprintf(format, args...))
}

// LiveReplica is one live container of an entry's service as the caller read
// it (read-only Docker inspection). It carries no secret material.
type LiveReplica struct {
	// Number is the Compose container number, "" for a non-Compose target.
	Number string
	// ContainerID, ContainerName and Created identify the container now.
	ContainerID   string
	ContainerName string
	Created       string
	// Eligible is true when the replica's strategy dispatches a rotation (it is
	// not a no_rotating_strategy replica).
	Eligible bool
}

func (l LiveReplica) validate() error {
	r := Replica{Number: l.Number, ContainerID: l.ContainerID, ContainerName: l.ContainerName, Created: l.Created,
		Outcome: ReplicaAdopted, At: time.Unix(0, 0).UTC(), Delivery: DeliveryNone, Adoption: AdoptionUnknown}
	return r.validate("live replica", "")
}

// replicaRef is the identity of a replica inside an entry: (number, container
// name) (design section 1.4). The container id and creation time are
// attributes at record time, never keys.
type replicaRef struct{ number, name string }

func refOf(r Replica) replicaRef         { return replicaRef{r.Number, r.ContainerName} }
func refOfLive(l LiveReplica) replicaRef { return replicaRef{l.Number, l.ContainerName} }

// EvidenceKind says what the journal showed. It is chosen by the caller from
// the journal record using the same rule as the M0 candidateVerified function;
// the ledger only checks it against the rule of design section 4.2.
type EvidenceKind string

const (
	// EvidenceRestartVerified: a restart rotation reached candidate_ready or a
	// later transition (candidateVerified).
	EvidenceRestartVerified EvidenceKind = "restart_candidate_verified"
	// EvidenceRollingVerified: a rolling rotation reached candidate_ready or a
	// later transition (candidateVerified).
	EvidenceRollingVerified EvidenceKind = "rolling_candidate_verified"
	// EvidenceSignalDone: a signal rotation reached signal_done.
	EvidenceSignalDone EvidenceKind = "signal_done"
	// EvidencePreReady: the journal record is before candidate_ready, or has no
	// strategy transition (Seq 0). It never proves delivery.
	EvidencePreReady EvidenceKind = "pre_ready"
)

// JournalStatus is the journal record status as the M0 StateTracker names it.
type JournalStatus string

const (
	JournalInProgress       JournalStatus = "in_progress"
	JournalCompleted        JournalStatus = "completed"
	JournalRollbackRequired JournalStatus = "rollback_required"
	JournalRecovered        JournalStatus = "recovered"
	JournalCriticalError    JournalStatus = "critical_error"
)

func validJournalStatus(s JournalStatus) bool {
	switch s {
	case JournalInProgress, JournalCompleted, JournalRollbackRequired, JournalRecovered, JournalCriticalError:
		return true
	}
	return false
}

// Evidence is the typed journal fact a delivery verification must cite
// (design section 4.2). The ledger never reads the journal itself: the caller
// derives this value from a RotationState, and the ledger refuses anything
// weaker than the rule.
type Evidence struct {
	// RotationID is the journal operation id the evidence is about.
	RotationID string
	Kind       EvidenceKind
	Status     JournalStatus
	// Committed is the journal's swap-committed flag.
	Committed bool
	// Seq is the journal sequence; 0 means no strategy transition was recorded.
	Seq uint64
	// Version is the version the operation delivered (a keyed identity, never a
	// value): the journal-carried SecretVersion or the caller's own record.
	Version Version
	// FileMode is true for a file-delivered secret: adoption stays unverified
	// while N19 is open.
	FileMode bool
	// Live optionally refreshes the replicas' container id and creation time as
	// they are after the operation (a DSO rotation recreates the containers).
	Live []LiveReplica
}

// assess applies the evidence rule of design section 4.2 and returns the
// adoption verdict and reason of the replicas the evidence verifies. It is the
// single place the rule lives.
func (ev Evidence) assess() (AdoptionState, OutcomeReason, error) {
	refuse := func(format string, args ...any) (AdoptionState, OutcomeReason, error) {
		return AdoptionUnknown, "", fmt.Errorf("%w: %s", ErrNoEvidence, fmt.Sprintf(format, args...))
	}
	if !validRotationID(ev.RotationID) {
		return refuse("rotation id is empty or malformed")
	}
	if !validJournalStatus(ev.Status) {
		return refuse("unknown journal status")
	}
	if ev.Seq == 0 {
		return refuse("journal sequence is 0 (no strategy transition recorded)")
	}
	switch ev.Status {
	case JournalCriticalError, JournalRollbackRequired:
		return refuse("journal status %s is never delivery evidence", ev.Status)
	}
	switch ev.Kind {
	case EvidenceRestartVerified, EvidenceRollingVerified:
		if ev.Status != JournalCompleted && !ev.Committed {
			// recovered alone proves only that M0 recovery restored a healthy
			// workload; in_progress proves nothing yet.
			return refuse("journal status %s without a committed swap is not a successful dispatch", ev.Status)
		}
		if ev.FileMode {
			return AdoptionUnverified, WhyAdoptionUnverifiedFile, nil
		}
		return AdoptionVerified, WhyDeliveryVerified, nil
	case EvidenceSignalDone:
		if ev.Status != JournalCompleted {
			return refuse("a signal rotation needs journal status completed")
		}
		// A signal reload is never adoption verified (X5).
		return AdoptionUnverified, WhyAdoptionUnverifiedSignal, nil
	case EvidencePreReady:
		return refuse("the journal record is before candidate_ready")
	default:
		return refuse("unknown evidence kind")
	}
}

// AdoptionEvidenceKind says what proves that a delivered version was adopted
// by the application (design sections 7 and 10.2).
type AdoptionEvidenceKind string

const (
	// AdoptionEvidenceRestart: a restart or rolling success started a fresh
	// process with the version.
	AdoptionEvidenceRestart AdoptionEvidenceKind = "restart_success"
	// AdoptionEvidenceLabel: the container label dso.rotation.id names the
	// operation that created it.
	AdoptionEvidenceLabel AdoptionEvidenceKind = "container_label"
	// AdoptionEvidenceFileProbe is accepted by no transition: a file probe
	// proves delivery only, adoption stays unverified (section 7).
	AdoptionEvidenceFileProbe AdoptionEvidenceKind = "file_probe"
	// AdoptionEvidenceSignal is refused: a signal never proves adoption (X5).
	AdoptionEvidenceSignal AdoptionEvidenceKind = "signal"
	// AdoptionEvidenceEnvComparison is refused: comparing environment variables
	// would read secret values out of Docker configuration.
	AdoptionEvidenceEnvComparison AdoptionEvidenceKind = "env_comparison"
)

// AdoptionEvidence is the proof RecordAdoptionVerified requires.
type AdoptionEvidence struct {
	Kind       AdoptionEvidenceKind
	RotationID string
	Version    Version
}

// ReplicaRef names a replica of an entry by its stable identity.
type ReplicaRef struct {
	Number        string
	ContainerName string
}

// InterruptCause says how an operation ended without delivery evidence.
type InterruptCause string

const (
	// CauseUnresolved: the journal record is unresolved (critical_error,
	// inherited in_progress, recovered without commit): the replica is unknown.
	CauseUnresolved InterruptCause = "unresolved"
	// CauseNeverRan: the dispatch never ran (deferred, blocked by the guard,
	// journal record deleted): the requested hint is cleared.
	CauseNeverRan InterruptCause = "never_ran"
	// CauseFailed: the operation failed; applied is unchanged.
	CauseFailed InterruptCause = "failed"
	// CauseRolledBack: the operation was rolled back; applied is unchanged.
	CauseRolledBack InterruptCause = "rolled_back"
)

// BlockReason is the reason an entry is blocked.
type BlockReason string

const (
	BlockNoRotatingStrategy BlockReason = "no_rotating_strategy"
	BlockStrategyNone       BlockReason = "strategy_none"
	// BlockAmbiguousProvider has no unblock trigger.
	BlockAmbiguousProvider BlockReason = "ambiguous_provider"
)

func (b BlockReason) outcomeReason() (OutcomeReason, bool) {
	switch b {
	case BlockNoRotatingStrategy:
		return WhyNoRotatingStrategy, true
	case BlockStrategyNone:
		return WhyStrategyNone, true
	case BlockAmbiguousProvider:
		return WhyAmbiguousProvider, true
	}
	return "", false
}

// UnblockTrigger is one of the three unblock triggers of design section 10.2.
type UnblockTrigger string

const (
	// UnblockNewEligibleReplica (T1): a new eligible live replica appeared.
	UnblockNewEligibleReplica UnblockTrigger = "T1_new_eligible_replica"
	// UnblockStrategyNowDispatches (T2): a recorded replica's strategy now
	// dispatches (a configuration change applied at agent start).
	UnblockStrategyNowDispatches UnblockTrigger = "T2_strategy_now_dispatches"
	// UnblockSecretStrategyChanged (T3): the secret-level strategy is no longer
	// none after a configuration change applied at agent start.
	UnblockSecretStrategyChanged UnblockTrigger = "T3_secret_strategy_changed"
)

// UnblockInput is what the caller observed. The ledger checks the trigger
// against the live set and the recorded replicas; it does not trust the label.
type UnblockInput struct {
	Trigger UnblockTrigger
	Live    []LiveReplica
	// SecretStrategyNone is the secret-level strategy as configured now (T3).
	SecretStrategyNone bool
}

// JournalRecord is the minimal plain view of one journal (StateTracker)
// record that ReconcileFromJournal needs. The caller (the agent-side bridge)
// fills it; this package imports neither internal/agent nor internal/rotation.
type JournalRecord struct {
	RotationID string
	// Strategy is the journal strategy: restart, rolling or signal.
	Strategy string
	Status   JournalStatus
	// Committed is the journal's swap-committed flag.
	Committed bool
	Seq       uint64
	// CandidateVerified is the result of the journal's candidateVerified rule
	// applied to the record's Transition (restart and rolling).
	CandidateVerified bool
	// SignalDone is true when the Transition is signal_done.
	SignalDone bool
	// Version is the journal-carried version identity (owner decision O-2), or
	// nil when the journal does not know it. Without it a delivery cannot be
	// applied by reconciliation: the replicas become unknown and no applied
	// version advances (safe: at most one extra rotation).
	Version *Version
	// UpdatedAt is the record's last update time, used to ignore history older
	// than the ledger (window W11).
	UpdatedAt time.Time
}

func (r JournalRecord) kind() EvidenceKind {
	switch {
	case r.Strategy == "restart" && r.CandidateVerified:
		return EvidenceRestartVerified
	case r.Strategy == "rolling" && r.CandidateVerified:
		return EvidenceRollingVerified
	case r.Strategy == "signal" && r.SignalDone:
		return EvidenceSignalDone
	}
	return EvidencePreReady
}

// evidence converts the record to the typed Evidence the single rule judges.
func (r JournalRecord) evidence(fileMode bool, live []LiveReplica) Evidence {
	ev := Evidence{RotationID: r.RotationID, Kind: r.kind(), Status: r.Status, Committed: r.Committed, Seq: r.Seq,
		FileMode: fileMode, Live: live}
	if r.Version != nil {
		ev.Version = *r.Version
	}
	return ev
}

// JournalSnapshot is the plain journal view supplied to ReconcileFromJournal.
type JournalSnapshot struct {
	// Readable is false when the journal is corrupt or unreadable: reconcile then
	// changes nothing.
	Readable bool
	Records  []JournalRecord
	// Horizon is the journal retention horizon (now minus 7 days). A ledger
	// delivery claim whose record is absent from the journal is accepted only
	// when it is older than the horizon (rule R-C); the zero value means no
	// claim is old enough.
	Horizon time.Time
	// FileMode lists the entries whose secret is delivered as a file (N19).
	FileMode map[Key]bool
	// Live optionally gives the live replicas per entry for identity-drift
	// checks; an entry absent from the map is not drift-checked.
	Live map[Key][]LiveReplica
}

// ReconcileChange describes one replica group a reconcile changed.
type ReconcileChange struct {
	Key        Key
	RotationID string
	// Rule names the design rule applied: W3, W4, W6, W9, W10, R-A, R-B,
	// no_version or identity_changed.
	Rule string
}

// ReconcileReport is the result of ReconcileFromJournal.
type ReconcileReport struct {
	// Skipped is true when nothing was examined (journal unreadable).
	Skipped    bool
	SkipReason OutcomeReason
	Changes    []ReconcileChange
}

// ObserveKind says what Observe did.
type ObserveKind string

const (
	// ObserveAdopted: first sight; desired recorded, applied UNKNOWN, no rotation.
	ObserveAdopted ObserveKind = "adopted"
	// ObserveUnchanged: nothing to write.
	ObserveUnchanged ObserveKind = "unchanged"
	// ObserveChanged: a different comparable desired version was recorded.
	ObserveChanged ObserveKind = "changed"
	// ObserveReadopted: an incomparable identity (key or kind change): adopted
	// UNKNOWN again, never "changed".
	ObserveReadopted ObserveKind = "readopted"
	// ObserveReplicasChanged: only the replica records changed.
	ObserveReplicasChanged ObserveKind = "replicas_changed"
)

// ObserveResult is the outcome of Observe.
type ObserveResult struct {
	Kind  ObserveKind
	Entry Entry
}

// DeliveryResult is the outcome of RecordDeliveryVerified.
type DeliveryResult struct {
	Adoption AdoptionState
	Applied  bool
	Entry    Entry
}

// DecisionKind is the result class of NeedsRotation.
type DecisionKind string

const (
	DecisionRotate   DecisionKind = "rotate"
	DecisionNoRotate DecisionKind = "no_rotate"
	// DecisionUnknown means the ledger cannot say. It is treated exactly like
	// no_rotate by every caller (X3) but kept distinct so it can be reported.
	DecisionUnknown DecisionKind = "unknown"
)

// DecisionReason is the closed vocabulary of NeedsRotation reasons.
type DecisionReason string

const (
	DecisionDesiredDiffers      DecisionReason = "desired_differs_applied"
	DecisionChangedSinceAdopt   DecisionReason = "changed_since_adoption"
	DecisionEligibilityChanged  DecisionReason = "eligibility_changed"
	DecisionUpToDate            DecisionReason = "up_to_date"
	DecisionAdoptedUnknown      DecisionReason = "adopted_unknown"
	DecisionBlocked             DecisionReason = "blocked"
	DecisionNoDesired           DecisionReason = "no_desired"
	DecisionIncomparable        DecisionReason = "incomparable"
	DecisionNoEntry             DecisionReason = "no_entry"
	DecisionLedgerMissing       DecisionReason = "ledger_missing"
	DecisionLedgerCorrupt       DecisionReason = "ledger_corrupt"
	DecisionLedgerUnreadable    DecisionReason = "ledger_unreadable"
	DecisionLedgerNewerSchema   DecisionReason = "ledger_newer_schema"
	DecisionKeyLost             DecisionReason = "key_lost"
	DecisionKeyUnavailableCause DecisionReason = "key_unavailable"
)

// Decision is the result of NeedsRotation. The ledger only makes the engine
// want a rotation or not want one; the M0 guard still decides whether it may
// run (X1).
type Decision struct {
	Kind   DecisionKind
	Reason DecisionReason
}

// Rotate reports whether the decision is to rotate.
func (d Decision) Rotate() bool { return d.Kind == DecisionRotate }

// LedgerCondition tells the pure NeedsRotation what the caller knows about the
// ledger as a whole. Anything but ConditionOK yields DecisionUnknown (X3).
type LedgerCondition int

const (
	ConditionOK LedgerCondition = iota
	ConditionMissing
	ConditionCorrupt
	ConditionUnreadable
	ConditionNewerSchema
	ConditionKeyLost
	ConditionKeyUnavailable
)

// RotationInput is the input of the pure decision function.
type RotationInput struct {
	Condition LedgerCondition
	// Entry is nil when the ledger holds no record for the key.
	Entry *Entry
}

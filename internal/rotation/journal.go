package rotation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Rotation transition journal.
//
// Docker tells recovery what EXISTS (containers, names, labels, health). It cannot
// tell recovery what DSO INTENDED or how far an operation got: which container was
// the candidate, whether the proxy handoff was started or committed, whether the
// old workload was due to be retired. The journal records that intent durably
// BEFORE each destructive step and the acknowledgement AFTER it, so that after a
// crash recovery can reconcile "what was planned" with "what is observed" and
// preserve anything it cannot account for.
//
// No plaintext secret values are ever journalled; only an opaque secret version
// identifier when one is available.

// Transition names. "intent_*" entries are written before the action; the matching
// past-tense entry after it. A crash can land between the two: recovery must treat
// an intent without its acknowledgement as "may or may not have happened".
const (
	TrIntentCreateCandidate = "intent_create_candidate"
	TrCandidateCreated      = "candidate_created"
	TrCandidateReady        = "candidate_ready" // started, delivered, health/probe verified
	TrIntentHandoff         = "intent_handoff"
	TrHandoffDone           = "handoff_done"
	TrIntentSwap            = "intent_swap" // rename original -> backup, candidate -> real name
	TrSwapCommitted         = "swap_committed"
	TrIntentCleanup         = "intent_cleanup"
	TrCompleted             = "completed"
	TrIntentRollback        = "intent_rollback"
	TrRolledBack            = "rolled_back"
	TrFailed                = "failed"

	// Restart (full recreation) strategy.
	TrIntentRenameOriginal   = "intent_rename_original"
	TrIntentStopOriginal     = "intent_stop_original"
	TrOriginalStopped        = "original_stopped"
	TrIntentStartReplacement = "intent_start_replacement"
	TrReplacementStarted     = "replacement_started"

	// N19 managed file delivery (gated candidates, docs/design/n19-managed-delivery.md).
	// These phases lie strictly BEFORE TrCandidateReady and none of them is delivery
	// evidence by itself: only TrSecretsDelivered (all files delivered AND probed,
	// keyed versions recorded) followed by the gate release makes a later
	// TrCandidateReady a verified candidate.
	TrGateStarted          = "gate_started"           // candidate running, application NOT started (verified by inspect)
	TrIntentDeliverSecrets = "intent_deliver_secrets" // before the first byte is delivered
	TrDeliveryPartial      = "delivery_partial"       // some, not all, files delivered when delivery failed
	TrSecretsDelivered     = "secrets_delivered"      // every file delivered and probed; keyed versions recorded
	TrIntentReleaseGate    = "intent_release_gate"    // before the release flag is written
	TrGateReleased         = "gate_released"          // release flag written: the application may now start

	// Signal (file re-delivery + reload) strategy.
	TrIntentSignalPublish = "intent_signal_publish"
	TrSignalPublished     = "signal_published"
	TrIntentSignalReload  = "intent_signal_reload"
	TrSignalReloadSent    = "signal_reload_sent"
	TrSignalDone          = "signal_done"   // terminal: adoption still unverified
	TrSignalFailed        = "signal_failed" // terminal; State names the precise condition

	// Attempt model (A-02). Written only by StateTracker.BeginAttempt, never as an
	// ordinary event; it is not delivery evidence by itself.
	TrAttemptBegin = "attempt_begin"
)

// ManualReason codes name why an operator must act. They accompany an event with
// ManualRecovery and are stored on the escalated attempt. The set is a vocabulary,
// not a validation list: the journal stores any string, so records written with a
// code this binary does not know stay readable. Each maps to a section of
// docs/operations/MANUAL-RECOVERY-RUNBOOK.md (owner task before any release).
const (
	ManualReasonNoWorkload                      = "no_workload"
	ManualReasonArtifactIdentity                = "artifact_identity"
	ManualReasonUnresolvedIntent                = "unresolved_intent"
	ManualReasonRollingBetweenRenames           = "rolling_between_renames"
	ManualReasonRollingAfterSwapBeforeCommitAck = "rolling_after_swap_before_commit_ack"
	ManualReasonRecoveryNotConverging           = "recovery_not_converging"
	ManualReasonIdentityConflict                = "identity_conflict"
	ManualReasonPlanRefused                     = "plan_refused"
	ManualReasonLayoutInconsistent              = "layout_inconsistent"
	ManualReasonRollbackFailed                  = "rollback_failed"
	ManualReasonStateUnknown                    = "state_unknown"
	// ManualReasonCandidateNotRemoved is approved vocabulary with no emitter yet.
	ManualReasonCandidateNotRemoved = "candidate_not_removed"
)

// JournalEvent is one durable record. Zero-value fields mean "unchanged".
type JournalEvent struct {
	RotationID string
	ServiceID  string // the real container name
	OwnerID    string
	Strategy   string
	Transition string

	OriginalIDs  []string
	CandidateIDs []string

	// SecretVersion is an opaque identifier of the desired secret version (never a
	// value). Empty when the provider exposes none.
	SecretVersion string

	// ExpectedProxyTarget / ObservedProxyTarget are container IDs the proxy was
	// expected to / was observed to route to; empty when the service is not proxied.
	ExpectedProxyTarget string
	ObservedProxyTarget string

	Detail string

	// Gated marks (on TrCandidateCreated) a candidate created with the N19 startup gate.
	// DeliveredVersions (on TrSecretsDelivered) are the keyed identities
	// (hmac:<key id>:<mac>) of the exact values handed to the delivery; never plaintext
	// and never an unkeyed hash. Empty when no ledger key is available.
	Gated             bool
	DeliveredVersions []string

	// State is a named end/progress condition (e.g. the signal strategy's
	// partially_published); ManualRecovery marks that an operator must act. Neither
	// carries secret material.
	State          string
	ManualRecovery bool

	// Attempt is the 1-based number of the attempt the event belongs to. Zero means
	// the event is unattributed (every event written before the attempt model): it
	// never changes the attempt list.
	Attempt int
	// ManualReason is one of the ManualReason* codes (free string, not validated),
	// stored in the same durable write as the escalation it explains.
	ManualReason string
}

// Journal persists JournalEvents. Record must be durable when it returns nil. When
// it returns an error the caller MUST NOT perform the guarded action (fail closed).
type Journal interface {
	Record(ctx context.Context, e JournalEvent) error
}

type rotationIDKey struct{}

// WithRotationID returns a context carrying the rotation's identity, so the
// journal record created by the trigger engine and the transitions recorded deep in
// a strategy are the same record.
func WithRotationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, rotationIDKey{}, id)
}

// RotationIDFrom returns the rotation ID in ctx or "".
func RotationIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(rotationIDKey{}).(string)
	return id
}

// NewRotationID returns a fresh unique rotation identifier.
func NewRotationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rot-%d", time.Now().UnixNano())
	}
	return "rot-" + hex.EncodeToString(b)
}

// LabelRotationID marks a candidate container with the rotation that created it,
// so recovery can identify it by an operation identity that exists even when the
// crash happened before the candidate's container ID was journalled.
const LabelRotationID = "dso.rotation.id"

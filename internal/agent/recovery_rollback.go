package agent

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
)

// N21 controlled automatic rollback (owner decision, docs/design/m0-04-recovery-design.md
// section 6). It is NOT a second recovery subsystem: it is one extra step of
// recoverSingleRotation that runs on the same journal record (rotation-ID journal),
// the same ownership/identity rules, the same RoutingVerifier and the same record
// selection (journalRecordFor) as the preserve-and-escalate path, and it falls
// through to that path (which stays the default) whenever ANY safety condition is
// not positively established.
//
// Scope: the restart (recreation) strategy before candidate_ready, i.e. exactly the
// layouts N21 found unsafe to resolve automatically: the original was renamed to
// "<service>_old_<ts>" (and possibly stopped) and the replacement either does not
// exist, was created, or was started without the journal ever acknowledging that its
// secret delivery passed the readiness gate. Rolling and signal rotations, a candidate
// that reached candidate_ready, and a crash inside the swap keep their existing rules.
//
// What is rolled back is the CONTAINER layout only: the unverified replacement is
// stopped and moved aside (renamed "<service>_dso_new_<ts>", never deleted by this
// code), the untouched original gets its real name back and is started if stopped.
// Secret delivery and application recovery are NOT rolled back or verified: the old
// workload's credentials may have been revoked at the provider, and DSO cannot tell.
// That is recorded precisely (OperationState, journal detail, error message, event).

const (
	// opStateRollbackAppUnverified: container restored; the previous delivered secret
	// version is known to the ledger; application recovery is still unverified.
	opStateRollbackAppUnverified = "rollback_container_restored_application_unverified"
	// opStateRollbackPreviousUnavailable: container restored; the previous delivered
	// secret version cannot be shown. The record stays critical_error (blocked).
	opStateRollbackPreviousUnavailable = "rollback_container_restored_previous_secret_unavailable"
)

// PreviousVersionSource tells recovery whether the secret version delivered BEFORE the
// interrupted operation is known (journal/ledger). originalLabels are the original
// container's labels (they carry the compose identity of the ledger key). nil source or
// false means "cannot be shown": the rollback restores the container but never claims
// an application-level recovery and the record stays blocked.
type PreviousVersionSource interface {
	PreviousDeliveredKnown(rec *RotationState, originalLabels map[string]string) bool
}

// rollbackTransition reports whether the journal's last step of a restart operation is
// one the controlled rollback may act on: every step before candidate_ready, plus
// intent_rollback itself (a rollback this code started and a crash interrupted is
// resumed from the layout). Any other or unknown name is not eligible (fail closed).
func rollbackTransition(tr string) bool {
	switch tr {
	case rotation.TrIntentRenameOriginal, rotation.TrIntentCreateCandidate, rotation.TrCandidateCreated,
		rotation.TrIntentStopOriginal, rotation.TrOriginalStopped,
		rotation.TrIntentStartReplacement, rotation.TrReplacementStarted, rotation.TrIntentRollback:
		return true
	}
	return false
}

type rollbackCandidate struct {
	id, name string
	running  bool
}

type rollbackPlan struct {
	base           string
	originalID     string
	originalName   string
	originalRun    bool
	originalLabels map[string]string
	candidate      *rollbackCandidate // nil: no replacement exists
}

// recordsConflict names another journal record that makes acting on rec unsafe: an
// unresolved operation (in progress, rollback_required or critical_error) of the same
// service or on the same containers, or a NEWER operation of any state that names the
// same containers. An older rotation therefore never rolls back what a newer one
// owns, and a service with an unresolved sibling stays blocked (OperationGuard rule).
func (ar *AutomaticRecovery) recordsConflict(rec *RotationState) string {
	mine := append(rec.OriginalIDs(), rec.CandidateContainerIDs...)
	for _, r := range ar.st.snapshotRecords() {
		if r.RotationID == rec.RotationID {
			continue
		}
		shared := r.ServiceID != "" && r.ServiceID == rec.ServiceID
		for _, id := range append(r.OriginalIDs(), r.CandidateContainerIDs...) {
			if idIn(mine, id) {
				shared = true
			}
		}
		if !shared {
			continue
		}
		switch r.Status {
		case "in_progress", "rollback_required", "critical_error":
			return "another unresolved operation " + r.RotationID + " (" + r.Status + ") names the same service or containers"
		}
		if r.StartTime.After(rec.StartTime) {
			return "a newer operation " + r.RotationID + " names the same service or containers"
		}
	}
	return ""
}

// planRollback decides, read-only, whether the controlled rollback of rec is safe. It
// returns applicable=false (and no refusal) when the layout is not a rollback case at
// all (the original already holds its real name: the ordinary rules apply, they keep
// it). A non-empty refusal means the case applies but a safety condition could not be
// positively established; the caller then preserves everything (existing semantics).
func (ar *AutomaticRecovery) planRollback(ctx context.Context, rec *RotationState) (plan *rollbackPlan, applicable bool, refusal string) {
	// (1) operation identity
	if rec.RotationID == "" || rec.Seq == 0 {
		return nil, true, "operation identity (rotation id / journal sequence) is not known"
	}
	ids := rec.OriginalIDs()
	if rec.ServiceID == "" || len(ids) != 1 {
		return nil, true, "the operation does not identify exactly one service and one original container"
	}
	// (3) ownership requires a positive agent identity
	if ar.OwnerID == "" {
		return nil, true, "no agent owner identity: container ownership cannot be verified"
	}
	if rec.OwnerID != "" && rec.OwnerID != ar.OwnerID {
		return nil, true, "the operation belongs to another owner"
	}
	// (5) the guard rule: no unresolved sibling, no newer operation on these containers
	if why := ar.recordsConflict(rec); why != "" {
		return nil, true, why
	}
	base, origID := rec.ServiceID, ids[0]

	// (2) original positively identified
	insp, err := ar.cli.ContainerInspect(ctx, origID)
	if err != nil || insp.ContainerJSONBase == nil || insp.State == nil || insp.Config == nil {
		return nil, true, "the original container " + shortID(origID) + " is missing or unreadable: no rollback target"
	}
	if !ar.ownedByMe(insp.Config.Labels) {
		return nil, true, "the original container is not owned by this agent"
	}
	oname := strings.TrimPrefix(insp.Name, "/")
	if oname == base {
		return nil, false, "" // the original holds its real name: ordinary rules
	}
	if b, kind, ok := artifactBase(oname); !ok || kind != "old" || b != base {
		return nil, true, "the original container's name " + oname + " is not this operation's rename-away name"
	}
	if insp.State.Dead {
		return nil, true, "the original container is dead and cannot be restored"
	}
	// F-B1 selection: this operation must be the record that owns the original.
	if r := ar.journalRecordFor(origID, insp.Config.Labels); r == nil || r.RotationID != rec.RotationID {
		return nil, true, "another journal record outranks this operation for the original container"
	}

	// the replacement(s)
	list, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, true, "containers cannot be listed"
	}
	plan = &rollbackPlan{base: base, originalID: origID, originalName: oname, originalRun: insp.State.Running, originalLabels: insp.Config.Labels}
	proxied := rec.ExpectedProxyTarget != "" || insp.Config.Labels["dso.host_ports"] != ""
	var matches []container.Summary
	for _, c := range list {
		if c.ID == origID {
			continue
		}
		isCand := idIn(rec.CandidateContainerIDs, c.ID) || (c.Labels[rotation.LabelRotationID] == rec.RotationID)
		holdsName := false
		for _, n := range c.Names {
			if strings.TrimPrefix(n, "/") == base {
				holdsName = true
			}
		}
		if !isCand {
			if holdsName {
				return nil, true, "a container that is not this operation's candidate holds the real name"
			}
			continue
		}
		if lbl := c.Labels[rotation.LabelRotationID]; lbl != "" && lbl != rec.RotationID {
			return nil, true, "a candidate carries another operation's rotation id"
		}
		if !ar.ownedByMe(c.Labels) {
			return nil, true, "the candidate container is not owned by this agent (ownership ambiguous)"
		}
		matches = append(matches, c)
		if c.Labels["dso.host_ports"] != "" {
			proxied = true
		}
	}
	if len(matches) > 1 {
		return nil, true, "several containers match as this operation's candidate (ambiguous)"
	}
	if len(matches) == 1 {
		ci, err := ar.cli.ContainerInspect(ctx, matches[0].ID)
		if err != nil || ci.ContainerJSONBase == nil || ci.State == nil {
			return nil, true, "the candidate container cannot be inspected"
		}
		plan.candidate = &rollbackCandidate{id: matches[0].ID, name: strings.TrimPrefix(ci.Name, "/"), running: ci.State.Running}
	}

	// (6) routing, (7) no known-good serving workload is destroyed
	if proxied {
		if ar.Routing == nil {
			return nil, true, "live proxy routing cannot be established (no routing verifier)"
		}
		active, known := ar.Routing.ActiveBackends(ctx, rec.ServiceID)
		if !known {
			return nil, true, "live proxy routing cannot be established"
		}
		if plan.candidate != nil && idIn(active, plan.candidate.id) {
			return nil, true, "the live proxy routes traffic to the unverified candidate"
		}
	}
	if candidateVerified(rec.Transition) {
		return nil, true, "the candidate was acknowledged ready: it is not rolled back automatically"
	}
	return plan, true, ""
}

// tryControlledRollback performs the automatic rollback of rec when permitted. handled
// is true when it finished the record (recovered or escalated); false means "continue
// with the ordinary preserve-and-escalate rules", with note explaining why a rollback
// that applied to this record was not attempted.
func (ar *AutomaticRecovery) tryControlledRollback(ctx context.Context, logger *zap.Logger, rec *RotationState) (handled bool, note string) {
	if !ar.AutomaticRollback || rec.Strategy != "restart" || !rollbackTransition(rec.Transition) {
		return false, ""
	}
	// A critical_error record is an unresolved operator matter: never acted on here.
	if rec.Status != "in_progress" && rec.Status != "rollback_required" {
		return false, ""
	}
	plan, applicable, refusal := ar.planRollback(ctx, rec)
	if !applicable {
		return false, ""
	}
	if plan == nil {
		logger.Warn("Automatic rollback refused: safety not established; preserving all workloads", zap.String("reason", refusal))
		return false, "automatic rollback not attempted: " + refusal
	}
	return ar.executeRollback(ctx, logger, rec, plan), ""
}

// executeRollback runs the verified plan. Every action is preceded by a durable
// intent; a failed intent write aborts before anything changes. Nothing is deleted.
func (ar *AutomaticRecovery) executeRollback(ctx context.Context, logger *zap.Logger, rec *RotationState, plan *rollbackPlan) bool {
	escalate := func(msg string) {
		logger.Error("Automatic rollback did not complete; workloads preserved, manual recovery required", zap.String("reason", msg))
		if fresh := ar.st.recordByID(rec.RotationID); fresh != nil {
			if err := ar.st.MarkCriticalRecord(fresh, msg); err != nil {
				logger.Error("Failed to mark rotation for manual intervention", zap.Error(err))
			}
		}
		ar.emitRecoveryEvent(notify.RecoveryFailed, rec, errors.New(msg))
	}

	if err := ar.st.Record(ctx, rotation.JournalEvent{RotationID: rec.RotationID, Transition: rotation.TrIntentRollback,
		Detail: "automatic recovery rollback: restoring the original container " + shortID(plan.originalID) + "; the unverified replacement is moved aside, not deleted"}); err != nil {
		// Fail closed: nothing was changed.
		msg := "automatic rollback aborted: its intent could not be journalled (no workload was changed): " + err.Error()
		logger.Error(msg)
		ar.emitRecoveryEvent(notify.RecoveryFailed, rec, errors.New(msg))
		return true
	}

	var undo []func()
	rollbackUndo := func() {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
	}
	fail := func(step string, err error) bool {
		rollbackUndo()
		escalate("automatic rollback failed at '" + step + "' (" + err.Error() + "); earlier steps were undone best-effort and nothing was deleted: verify the containers of " + plan.base)
		return true
	}

	if c := plan.candidate; c != nil {
		if c.running {
			if err := ar.cli.ContainerStop(ctx, c.id, container.StopOptions{}); err != nil {
				return fail("stop the unverified replacement", err)
			}
			undo = append(undo, func() { _ = ar.cli.ContainerStart(ctx, c.id, container.StartOptions{}) })
		}
		if c.name == plan.base {
			aside := plan.base + "_dso_new_" + strconv.FormatInt(time.Now().Unix(), 10)
			if err := ar.cli.ContainerRename(ctx, c.id, aside); err != nil {
				return fail("move the unverified replacement aside", err)
			}
			undo = append(undo, func() { _ = ar.cli.ContainerRename(ctx, c.id, plan.base) })
		}
	}
	if plan.originalName != plan.base {
		if err := ar.cli.ContainerRename(ctx, plan.originalID, plan.base); err != nil {
			return fail("restore the original's name", err)
		}
		undo = append(undo, func() { _ = ar.cli.ContainerRename(ctx, plan.originalID, plan.originalName) })
	}
	if !plan.originalRun {
		if err := ar.cli.ContainerStart(ctx, plan.originalID, container.StartOptions{}); err != nil {
			return fail("start the original", err)
		}
	}

	if id, ok, why := ar.holderState(ctx, plan.base, ""); !ok || id != plan.originalID {
		if ok {
			why = "another container holds the real name"
		}
		escalate("automatic rollback restored the original's name but its health could not be verified (" + why + "); nothing was deleted: verify " + plan.base)
		return true
	}

	// The container layout is restored and verified. Record exactly that, and no more.
	prevKnown := ar.PreviousVersion != nil && ar.PreviousVersion.PreviousDeliveredKnown(rec, plan.originalLabels)
	state, detail := opStateRollbackAppUnverified,
		"container restored by automatic rollback (original "+shortID(plan.originalID)+" holds "+plan.base+", verified); secret delivery was NOT rolled back or re-verified; application recovery is UNVERIFIED (the old credentials may have been revoked)"
	if !prevKnown {
		state, detail = opStateRollbackPreviousUnavailable,
			"container restored by automatic rollback (original "+shortID(plan.originalID)+" holds "+plan.base+", verified); the previous secret version cannot be shown by the journal/ledger: application rollback is NOT claimed and recovery is UNVERIFIED; operator confirmation required"
		if fresh := ar.st.recordByID(rec.RotationID); fresh != nil {
			if err := ar.st.MarkCriticalRecord(fresh, detail); err != nil {
				logger.Error("Failed to mark rotation for manual intervention", zap.Error(err))
			}
		}
	}
	if err := ar.st.Record(ctx, rotation.JournalEvent{RotationID: rec.RotationID, Transition: rotation.TrRolledBack,
		State: state, Detail: detail, ManualRecovery: !prevKnown}); err != nil {
		logger.Error("Failed to journal the rollback outcome", zap.Error(err))
	}
	logger.Warn("Controlled automatic rollback completed at container level; application recovery unverified",
		zap.String("service", plan.base), zap.String("state", state), zap.Bool("previous_version_known", prevKnown))
	ar.emitRecoveryEvent(notify.RecoveryFailed, rec, errors.New(detail))
	return true
}

// isRollbackOperatorState reports whether a critical_error record was left by this
// rollback for operator confirmation; startup recovery must not re-resolve it.
func isRollbackOperatorState(operationState string) bool {
	return operationState == opStateRollbackPreviousUnavailable
}

package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/docker-secret-operator/dso/internal/faultinject"
	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

// oldContainerSuffixPattern matches the restart strategy's rename-away
// naming convention: "<originalName>_old_<unixTimestamp>" (see
// executeRollback/executeSimpleRestart in internal/watcher/controller.go,
// which construct this exact suffix). Anchored to the end of the string and
// requiring the suffix to be all digits deliberately makes this narrow: a
// bare `strings.Contains(name, "_old_")` would also match unrelated,
// operator-owned containers like "my_old_database" or "legacy_old_service".
// This regex will not.
var oldContainerSuffixPattern = regexp.MustCompile(`_old_[0-9]+$`)

// isDSOBackupContainerName reports whether cleanName matches the rolling
// strategy's backup-container naming convention.
func isDSOBackupContainerName(cleanName string) bool {
	return strings.Contains(cleanName, "_dso_backup_")
}

// isDSONewContainerName reports whether cleanName matches the rolling
// strategy's shadow/new-container naming convention.
func isDSONewContainerName(cleanName string) bool {
	return strings.Contains(cleanName, "_dso_new_")
}

// isDSORestartOldContainerName reports whether cleanName matches the
// restart strategy's rename-away naming convention. See
// oldContainerSuffixPattern's doc comment for why this must stay a narrow,
// suffix-anchored match rather than a broad substring check -- cleanup
// logic must only ever recognize container names DSO itself constructed,
// never delete an arbitrary operator container that happens to contain
// "_old_" somewhere in its name.
func isDSORestartOldContainerName(cleanName string) bool {
	return oldContainerSuffixPattern.MatchString(cleanName)
}

// IsDSORotationArtifactName reports whether cleanName (a container name with
// any leading "/" already trimmed) matches one of DSO's own rotation-
// artifact naming conventions ("_dso_backup_", "_dso_new_", or the restart
// strategy's "_old_<unix timestamp>" suffix). Exported so other DSO-owned
// tooling (specifically `dso doctor`'s orphan-container diagnostic, which
// runs as a separate process and must never re-derive or drift from this
// security-sensitive matching logic on its own) can reuse the exact same
// ownership determination this package's own cleanup/recovery logic uses,
// rather than duplicating it.
func IsDSORotationArtifactName(cleanName string) bool {
	return isDSOBackupContainerName(cleanName) ||
		isDSONewContainerName(cleanName) ||
		isDSORestartOldContainerName(cleanName)
}

// shortID returns up to the first 12 characters of id, safe for empty or short strings.
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// AutomaticRecovery implements automatic cleanup of orphaned containers
// and state recovery after agent crashes.
type AutomaticRecovery struct {
	cli    *client.Client
	logger *zap.Logger
	st     *StateTracker

	// Dispatcher optionally receives recovery outcome events. Nil when
	// notifications are unconfigured; always emitted-to after the
	// authoritative MarkRecovered/MarkCriticalError transition, never
	// before, and never in a way recovery logic can observe.
	Dispatcher *notify.Dispatcher

	// OwnerID is this agent's dso.owner identity (SEC-P0 discovery-isolation
	// fix). Set via SetOwnerID before RecoverFromCrash/CleanupOrphanedContainers
	// run. Recovery/cleanup act purely on Docker-visible naming conventions
	// and label filters, which -- like normal discovery before this fix --
	// carry no per-agent identity of their own; without this, a crash-
	// recovery or orphan sweep on a host running a second, independent DSO
	// agent could stop/remove a container that agent owns merely because it
	// happens to match DSO's rotation-artifact naming pattern (e.g. an
	// unrelated "*_dso_backup_*"-named container it created). Empty means
	// ownership enforcement is skipped (matches pre-fix behavior; not
	// reachable from a real `dso agent` process, which always has an
	// OwnerID from EnsureOwnerID).
	OwnerID string

	// Timings overrides the default health/stability waits (tests).
	Timings *recoveryTimings

	// Routing, when set, lets recovery verify LIVE traffic routing for proxied
	// services. Nil means routing is unknown, in which case recovery preserves
	// every workload of a proxied rotation (fail closed).
	Routing RoutingVerifier

	// AutomaticRollback enables the N21 controlled automatic rollback (see
	// recovery_rollback.go): a restart-strategy operation that never reached
	// candidate_ready is rolled back to its original container ONLY when every safety
	// condition is positively established; otherwise the preserve-and-escalate rules
	// below apply unchanged. The production startup path enables it; the zero value
	// keeps the pure preserve behaviour.
	AutomaticRollback bool

	// PreviousVersion tells the rollback whether the previously delivered secret
	// version can be shown. nil means "cannot be shown" (the rollback then never claims
	// an application-level recovery and leaves the record blocked).
	PreviousVersion PreviousVersionSource
}

// SetOwnerID wires this agent's dso.owner identity into the recovery/cleanup
// paths below so they enforce the same ownership boundary as normal
// discovery (internal/watcher.ReloaderController.checkOwnership).
func (ar *AutomaticRecovery) SetOwnerID(id string) {
	ar.OwnerID = id
}

// ownedByMe reports whether labels carries this agent's dso.owner value.
// Default-deny: a missing/empty dso.owner label, or one that names a
// different agent, is NOT owned by me. See OwnerID's doc comment for why an
// empty ar.OwnerID (no Config wired up) instead skips enforcement entirely.
func (ar *AutomaticRecovery) ownedByMe(labels map[string]string) bool {
	if ar.OwnerID == "" {
		return true
	}
	return labels["dso.owner"] == ar.OwnerID
}

func (ar *AutomaticRecovery) emitRecoveryEvent(t notify.EventType, rotation *RotationState, err error) {
	if ar.Dispatcher == nil {
		return
	}
	containers := rotation.OriginalIDs()
	ar.Dispatcher.Dispatch(notify.NewEvent(t, rotation.ProviderName, rotation.SecretName, containers, 0, err))
}

// NewAutomaticRecovery creates a recovery handler
func NewAutomaticRecovery(cli *client.Client, logger *zap.Logger, st *StateTracker) *AutomaticRecovery {
	return &AutomaticRecovery{
		cli:    cli,
		logger: logger,
		st:     st,
	}
}

// RoutingVerifier reports which backends currently receive traffic for a service.
// known=false means routing CANNOT be established (proxy restarted and lost its
// state, cannot be inspected, ...). A journalled handoff acknowledgement proves only
// that the proxy was switched at some point; the proxy may have restarted, lost its
// in-memory configuration or been reconfigured since, so recovery re-checks the live
// state before retiring anything that might still be serving.
type RoutingVerifier interface {
	ActiveBackends(ctx context.Context, serviceID string) (ids []string, known bool)
}

// recoveryTimings are injectable so tests do not wait.
type recoveryTimings struct {
	// healthWait bounds how long recovery waits for a holder whose HEALTHCHECK is
	// still "starting" to become healthy before deciding it is NOT proven healthy.
	healthWait time.Duration
	// minStableRunning is how long a holder WITHOUT a HEALTHCHECK must have been
	// running before "running" is accepted as evidence it is serving.
	minStableRunning time.Duration
}

var defaultRecoveryTimings = recoveryTimings{healthWait: 20 * time.Second, minStableRunning: 30 * time.Second}

// RecoverFromCrash is called once at agent startup. It inspects Docker (the
// authority) rather than trusting the rotation journal, because a crash can
// land between a Docker action and its acknowledgement.
//
// INVARIANT: recovery never deletes the last known healthy workload. A rotation
// artifact ("<name>_dso_backup_*", "<name>_dso_new_*", "<name>_old_<ts>") is
// removed only when a container that is owned by this agent, holds the real
// "<name>", and is verified healthy (HEALTHCHECK healthy, or running and stable
// when the image has none) exists and is a different container. Anything
// ambiguous (state unreadable, original missing with no healthy holder, ownership
// mismatch, holder unhealthy) is PRESERVED and reported for manual recovery.
//
// Recovery cannot see which backend the (now dead) proxy was pointing at; that is
// safe because the proxy is rebuilt from running labelled containers on restart
// and a healthy real-name holder always remains.
func (ar *AutomaticRecovery) RecoverFromCrash(ctx context.Context) error {
	if ar.st == nil {
		ar.logger.Info("State tracker not available, skipping automatic recovery")
		return nil
	}
	if corrupt, where := ar.st.Corrupt(); corrupt {
		ar.logger.Error("Persisted rotation state is unreadable: automatic recovery is DISABLED and no container will be touched; manual recovery required",
			zap.String("preserved_state_file", where))
		return nil
	}

	pending := ar.st.GetUnfinishedRotationsAtStartup()
	if len(pending) == 0 {
		ar.logger.Debug("No unfinished rotations detected, recovery not needed")
		return nil
	}
	ar.logger.Warn("Detected unfinished rotations, performing safe recovery", zap.Int("count", len(pending)))
	faultinject.Hit("recovery:start")

	for _, rotation := range pending {
		ar.recoverSingleRotation(ctx, rotation)
	}
	ar.logger.Info("Automatic recovery completed", zap.Int("rotations_processed", len(pending)))
	return nil
}

func (ar *AutomaticRecovery) timings() recoveryTimings {
	if ar.Timings != nil {
		return *ar.Timings
	}
	return defaultRecoveryTimings
}

// recoverSingleRotation resolves one interrupted rotation. Each original
// container is inspected individually (structured IDs; no comma-joined lookups),
// ownership is verified, a stopped original that still has its own name is
// started (rollback), and only then are provably redundant artifacts retired.
func (ar *AutomaticRecovery) recoverSingleRotation(ctx context.Context, rotation *RotationState) {
	logger := ar.logger.With(
		zap.String("rotation_id", rotation.RotationID),
		zap.String("secret", rotation.SecretName),
		zap.String("provider", rotation.ProviderName))

	manual := func(reason string, preserved []string) {
		msg := reason
		if len(preserved) > 0 {
			msg += " (preserved: " + strings.Join(preserved, ", ") + ")"
		}
		logger.Error("Recovery cannot establish a safe state; all workloads preserved, manual recovery required", zap.String("reason", msg))
		if err := ar.st.MarkCriticalRecord(rotation, msg); err != nil {
			logger.Error("Failed to mark rotation for manual intervention", zap.Error(err))
		}
		ar.emitRecoveryEvent(notify.RecoveryFailed, rotation, errors.New(msg))
	}

	if rotation.Strategy == "signal" {
		ar.recoverInterruptedSignal(logger, rotation, manual)
		return
	}

	// A record this agent's rollback left blocked for operator confirmation (container
	// restored, previous secret version unavailable) is never re-resolved by startup
	// recovery: only an operator releases it.
	if rotation.Status == "critical_error" && isRollbackOperatorState(rotation.OperationState) {
		logger.Warn("Rotation stays blocked: an earlier automatic rollback restored the container but application recovery needs operator confirmation",
			zap.String("operation_state", rotation.OperationState))
		return
	}

	ids := rotation.OriginalIDs()
	if len(ids) == 0 {
		// Nothing identifies the workload: we cannot tell artifacts of this
		// rotation from anything else. Preserve everything.
		manual("rotation record has no container identity", nil)
		return
	}

	// N21: controlled automatic rollback, only under positively established safety.
	handled, note := ar.tryControlledRollback(ctx, logger, rotation)
	if handled {
		return
	}
	if note != "" {
		inner := manual
		manual = func(reason string, preserved []string) { inner(reason+"; "+note, preserved) }
	}

	var preserved []string
	allResolved := true
	for _, id := range ids {
		insp, err := ar.cli.ContainerInspect(ctx, id)
		if err != nil {
			// Original gone. With a journal this is only "completed" when the swap was
			// committed AND the journalled candidate now holds the real name healthy:
			// the original was retired on purpose.
			if ar.originalRetiredByCommittedRotation(ctx, rotation, id) {
				logger.Info("Original container was retired by a committed rotation; the journalled candidate serves",
					zap.String("original_id", shortID(id)), zap.String("service", rotation.ServiceID))
				continue
			}
			// Otherwise this is the dangerous case.
			logger.Warn("Original container not found", zap.String("original_id", shortID(id)), zap.Error(err))
			allResolved = false
			preserved = append(preserved, shortID(id)+"(missing)")
			continue
		}
		name := strings.TrimPrefix(insp.Name, "/")
		var labels map[string]string
		if insp.Config != nil {
			labels = insp.Config.Labels
		}
		if !ar.ownedByMe(labels) {
			manual("original container "+shortID(id)+" is not owned by this agent (ownership mismatch)", []string{shortID(id)})
			return
		}
		base, _, isArtifact := artifactBase(name)
		if !isArtifact {
			base = name
		}

		ok, why := ar.ensureHolder(ctx, logger, base, id, insp.State != nil && !insp.State.Running && !isArtifact)
		if !ok {
			allResolved = false
			preserved = append(preserved, base+"("+why+")")
			continue
		}
	}

	// Retire redundant artifacts (only those with a verified healthy holder).
	removed, kept := ar.retireSafeArtifacts(ctx, logger)
	// Artifacts of THIS rotation's service that had to be preserved (live routing
	// unknown, journal does not authorise, holder not proven) leave the rotation
	// unresolved: the operator must be told, not shown "recovered".
	relevant := map[string]bool{rotation.ServiceID: rotation.ServiceID != ""}
	for _, id := range ids {
		if insp, err := ar.cli.ContainerInspect(ctx, id); err == nil {
			if b, _, ok := artifactBase(strings.TrimPrefix(insp.Name, "/")); ok {
				relevant[b] = true
			} else {
				relevant[strings.TrimPrefix(insp.Name, "/")] = true
			}
		}
	}
	for _, k := range kept {
		if relevant[k.base] {
			allResolved = false
			preserved = append(preserved, k.text)
		}
	}

	if !allResolved {
		manual("no verified healthy workload could be established for every original container", preserved)
		return
	}
	logger.Info("Recovery complete: a verified healthy workload exists for every service", zap.Int("artifacts_removed", removed))
	if err := ar.st.MarkRecoveredRecord(rotation); err != nil {
		logger.Error("Failed to mark rotation as recovered", zap.Error(err))
	}
	ar.emitRecoveryEvent(notify.RecoverySucceeded, rotation, nil)
}

// originalRetiredByCommittedRotation reports whether a missing original is
// explained by a rotation whose swap the journal shows as committed and whose
// journalled candidate currently holds the real name, verified healthy.
func (ar *AutomaticRecovery) originalRetiredByCommittedRotation(ctx context.Context, rec *RotationState, missingID string) bool {
	if rec.Seq == 0 || rec.ServiceID == "" || !idIn(rec.OriginalIDs(), missingID) {
		return false
	}
	committed := rec.Committed
	switch rec.Transition {
	case rotation.TrSwapCommitted, rotation.TrIntentCleanup, rotation.TrCompleted:
		committed = true
	}
	if !committed {
		return false
	}
	holderID, ok, _ := ar.holderState(ctx, rec.ServiceID, "")
	return ok && idIn(rec.CandidateContainerIDs, holderID)
}

// recoverInterruptedSignal handles a signal rotation that never reached a terminal
// state. Nothing in Docker is touched and nothing is re-delivered or re-signalled:
// DSO cannot tell whether the file was replaced, whether the application reloaded, or
// whether signalling again is safe for it, and it must not re-apply a credential that
// may since have been revoked. The interrupted operation therefore stays UNCERTAIN
// (never "successful") until an operator acts.
func (ar *AutomaticRecovery) recoverInterruptedSignal(logger *zap.Logger, rec *RotationState, manual func(string, []string)) {
	cond := map[string]string{
		rotation.TrIntentSignalPublish: "secret files may be partially published (publication started, not acknowledged)",
		rotation.TrSignalPublished:     "secret files were published and verified-or-not-yet-verified but the reload was never requested",
		rotation.TrIntentSignalReload:  "files were published; the reload signal may or may not have been delivered",
		rotation.TrSignalReloadSent:    "the reload signal was delivered; the application's outcome and readiness are unknown",
	}[rec.Transition]
	if cond == "" {
		cond = "state after the last durable step is unknown"
	}
	logger.Warn("Interrupted signal rotation left uncertain; no automatic re-delivery or re-signal",
		zap.String("last_transition", rec.Transition), zap.String("service", rec.ServiceID))
	manual("interrupted signal rotation ("+rec.Transition+"): "+cond+
		"; no automatic re-delivery or re-signal was attempted; verify the secret files and reload or restart the container", nil)
}

// ensureHolder makes sure a verified healthy container holds the real name
// `base`. If the original is stopped AND still carries its real name, it is
// started (automatic rollback); a stopped original that was renamed away is never
// auto-started (a replacement likely holds the name and ports: that would create
// a duplicate). Returns false with a short reason when no healthy holder exists.
func (ar *AutomaticRecovery) ensureHolder(ctx context.Context, logger *zap.Logger, base, originalID string, originalStoppedUnderRealName bool) (bool, string) {
	if originalStoppedUnderRealName {
		logger.Warn("Original container is stopped under its real name; starting it (rollback)", zap.String("original_id", shortID(originalID)))
		if err := ar.cli.ContainerStart(ctx, originalID, container.StartOptions{}); err != nil {
			return false, "restart failed"
		}
	}
	ok, why := ar.holderHealthy(ctx, base, "")
	return ok, why
}

// holderHealthy reports whether an owned container named exactly `base` is
// running and healthy. excludeID, if set, must not be the holder.
func (ar *AutomaticRecovery) holderHealthy(ctx context.Context, base, excludeID string) (bool, string) {
	_, ok, why := ar.holderState(ctx, base, excludeID)
	return ok, why
}

// holderState is holderHealthy that also returns the holder's container ID, which
// the journal-aware retirement rules need to establish the holder's IDENTITY.
func (ar *AutomaticRecovery) holderState(ctx context.Context, base, excludeID string) (id string, ok bool, why string) {
	id, ok, why = ar.holderStateRaw(ctx, base, excludeID)
	if !ok {
		return id, ok, why
	}
	// N21: Docker-visible health is not evidence that the secret was delivered. A
	// container the journal names as a CANDIDATE that has not been acknowledged as
	// ready (started, healthy, file-secret probe passed) is never accepted as the
	// holder, however long it has been running or whatever its HEALTHCHECK says.
	insp, err := ar.cli.ContainerInspect(ctx, id)
	if err != nil {
		return "", false, "name holder could not be re-inspected"
	}
	var labels map[string]string
	if insp.Config != nil {
		labels = insp.Config.Labels
	}
	if tr, unverified := ar.unverifiedCandidate(id, labels); unverified {
		return "", false, "name holder is a journalled candidate that never reached candidate_ready (journal at " + tr + "): its secret delivery was not verified"
	}
	return id, true, ""
}

// candidateVerified reports whether the journal has acknowledged that rec's candidate
// passed the readiness gate. TrCandidateReady is written only after the secret was
// delivered (file streams succeeded), the container started, WaitReady/WaitHealthy
// passed and the file-secret probe (ExecProbe/ExecProbeFiles) passed; every later
// transition is only ever written after it. Transition is the LAST durable step, so a
// fallback restart that follows a failed rolling attempt correctly regresses it.
func candidateVerified(transition string) bool {
	switch transition {
	case rotation.TrCandidateReady, rotation.TrIntentHandoff, rotation.TrHandoffDone,
		rotation.TrIntentSwap, rotation.TrSwapCommitted, rotation.TrIntentCleanup, rotation.TrCompleted:
		return true
	}
	return false
}

// recordCandidateVerified is candidateVerified applied to a journal RECORD. For a
// candidate created with the N19 startup gate the transition alone is not enough:
// delivery must also have been verified (secrets_delivered, durable before the gate
// release). A gated record that reached candidate_ready or later without it (a crash
// between writes cannot produce this, so it can only mean a damaged or forged record)
// is NOT verified. Records without the gate flag (all journals written before N19,
// env-only rotations) keep the plain rule.
func recordCandidateVerified(rec *RotationState) bool {
	if rec == nil || !candidateVerified(rec.Transition) {
		return false
	}
	if rec.GatedDelivery && !rec.DeliveryVerified {
		return false
	}
	return true
}

// unverifiedCandidate reports whether container id is, according to an unfinished
// journal record, a candidate (by journalled ID or rotation label) that has not been
// acknowledged ready, and the record's last transition. A record that already reached a
// terminal status, or in which the container is an ORIGINAL, does not apply.
func (ar *AutomaticRecovery) unverifiedCandidate(id string, labels map[string]string) (string, bool) {
	if ar.st == nil {
		return "", false
	}
	for _, r := range ar.st.snapshotRecords() {
		if r.Seq == 0 || r.Status == "completed" || r.Status == "recovered" || idIn(r.OriginalIDs(), id) {
			continue
		}
		isCandidate := idIn(r.CandidateContainerIDs, id) || (r.RotationID != "" && labels[rotation.LabelRotationID] == r.RotationID)
		if isCandidate && !recordCandidateVerified(r) {
			return r.Transition, true
		}
	}
	return "", false
}

func (ar *AutomaticRecovery) holderStateRaw(ctx context.Context, base, excludeID string) (id string, ok bool, why string) {
	t := ar.timings()
	deadline := time.Now().Add(t.healthWait)
	for {
		insp, err := ar.cli.ContainerInspect(ctx, base)
		if err != nil {
			return "", false, "no container holds the real name"
		}
		if excludeID != "" && insp.ID == excludeID {
			return "", false, "the artifact itself holds the name"
		}
		var labels map[string]string
		if insp.Config != nil {
			labels = insp.Config.Labels
		}
		if !ar.ownedByMe(labels) {
			return "", false, "name holder is not owned by this agent"
		}
		st := insp.State
		if st == nil || !st.Running || st.Restarting {
			return "", false, "name holder is not running"
		}
		if st.Health != nil {
			switch st.Health.Status {
			case "healthy":
				return insp.ID, true, ""
			case "unhealthy":
				return "", false, "name holder is unhealthy"
			default: // "starting": wait, then give up (not proven healthy)
				if time.Now().Before(deadline) {
					select {
					case <-ctx.Done():
						return "", false, "cancelled"
					case <-time.After(250 * time.Millisecond):
					}
					continue
				}
				return "", false, "name holder health check never became healthy"
			}
		}
		// No HEALTHCHECK: "running" is weak evidence; require it to have been
		// stable for a while so a crash-looping container is not trusted.
		if started, perr := time.Parse(time.RFC3339Nano, st.StartedAt); perr == nil && time.Since(started) >= t.minStableRunning {
			return insp.ID, true, ""
		}
		if time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return "", false, "cancelled"
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		return "", false, "name holder has no health check and has not been running long enough to trust"
	}
}

func idIn(ids []string, id string) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// journalRecordFor finds the journal record an artifact belongs to: by container
// identity (original or candidate ID) or by the rotation label stamped on
// candidates, which exists even if the crash preceded the ID being journalled.
//
// Several records can name the same container (F-B1): a completed restart R0
// whose candidate is the original of a later, interrupted R1. The choice is
// deterministic (never map order): an unresolved record (not completed or
// recovered) wins over a terminal one, then the most recently updated, then the
// highest Seq, then the rotation id. A terminal record is therefore never chosen
// while an unresolved record names the container.
func (ar *AutomaticRecovery) journalRecordFor(id string, labels map[string]string) *RotationState {
	if ar.st == nil {
		return nil
	}
	rid := labels[rotation.LabelRotationID]
	var best *RotationState
	for _, r := range ar.st.snapshotRecords() {
		named := idIn(r.OriginalIDs(), id) || idIn(r.CandidateContainerIDs, id) || (rid != "" && r.RotationID == rid)
		if named && journalRecordBefore(r, best) {
			best = r
		}
	}
	return best
}

// journalRecordBefore reports whether a is preferred over b (nil b: always) by
// the deterministic order of journalRecordFor.
func journalRecordBefore(a, b *RotationState) bool {
	if b == nil {
		return true
	}
	unresolved := func(r *RotationState) bool { return r.Status != "completed" && r.Status != "recovered" }
	if ua, ub := unresolved(a), unresolved(b); ua != ub {
		return ua
	}
	if !a.LastUpdate.Equal(b.LastUpdate) {
		return a.LastUpdate.After(b.LastUpdate)
	}
	if a.Seq != b.Seq {
		return a.Seq > b.Seq
	}
	return a.RotationID < b.RotationID
}

// journalPermits applies the transition-journal rules to retiring `artifact` while
// `holder` (verified healthy, holding the real name) exists. Docker alone cannot say
// whether the holder is the journalled candidate or whether traffic was handed to it.
//
//   - No journal at all: allowed for an unproxied service (the name-holder rule is
//     all there is); a proxy-managed artifact (dso.host_ports) is PRESERVED because
//     routing evidence is unavailable.
//   - Retiring an ORIGINAL (the previously active workload): the holder must be the
//     journalled candidate (by ID or rotation label) AND, for a proxied rotation,
//     there must be durable evidence the handoff was committed (observed target,
//     swap committed, or a swap/cleanup intent, which is only written after the
//     handoff returned).
//   - Retiring a CANDIDATE: the holder must be one of the journalled originals (the
//     previously active workload is still serving).
func (ar *AutomaticRecovery) journalPermits(ctx context.Context, artifactID string, artifactLabels map[string]string, holderID string, holderLabels map[string]string) (string, bool) {
	rec := ar.journalRecordFor(artifactID, artifactLabels)
	if rec == nil || rec.Seq == 0 {
		if artifactLabels["dso.host_ports"] != "" && (rec == nil || rec.Seq == 0) {
			return "proxy-managed workload with no transition journal: routing evidence unavailable", false
		}
		return "", true
	}
	isOriginal := idIn(rec.OriginalIDs(), artifactID)
	isCandidate := !isOriginal && (idIn(rec.CandidateContainerIDs, artifactID) || artifactLabels[rotation.LabelRotationID] == rec.RotationID)
	holderIsCandidate := idIn(rec.CandidateContainerIDs, holderID) || (holderLabels[rotation.LabelRotationID] == rec.RotationID && !idIn(rec.OriginalIDs(), holderID))
	switch {
	case isOriginal:
		if !holderIsCandidate {
			return "the healthy name holder is not the journalled candidate", false
		}
		if !recordCandidateVerified(rec) {
			return "the journalled candidate never reached candidate_ready (journal at " + rec.Transition + "): secret delivery not verified", false
		}
		if rec.ExpectedProxyTarget != "" {
			evidence := rec.ObservedProxyTarget == holderID || rec.Committed
			switch rec.Transition {
			case rotation.TrIntentSwap, rotation.TrSwapCommitted, rotation.TrIntentCleanup, rotation.TrCompleted:
				evidence = true
			}
			if !evidence {
				return "no durable evidence that traffic was handed to the healthy holder", false
			}
		}
		return ar.liveRouting(ctx, rec, artifactID, holderID, "original")
	case isCandidate:
		if !idIn(rec.OriginalIDs(), holderID) {
			return "the healthy name holder is not the journalled original", false
		}
		return ar.liveRouting(ctx, rec, artifactID, holderID, "candidate")
	}
	return "artifact is not part of its journal record", false
}

// liveRouting applies the live-routing requirement for PROXIED rotations (those
// that journalled an expected proxy target). The artifact may only be retired when
// routing is positively established: the holder is receiving traffic and the
// artifact is not. If routing cannot be established, or still reaches the artifact,
// both workloads are preserved. A rotation that never reached the handoff intent
// (empty ExpectedProxyTarget) never routed traffic to its candidate, so only the
// name-holder rule applies. For UNPROXIED services DSO has no routing signal at all:
// a healthy container holding the name is NOT proof that external service discovery
// or a load balancer sends it traffic; that limit is documented, not hidden.
func (ar *AutomaticRecovery) liveRouting(ctx context.Context, rec *RotationState, artifactID, holderID, role string) (string, bool) {
	if rec.ExpectedProxyTarget == "" {
		return "", true
	}
	if ar.Routing == nil {
		return "live proxy routing cannot be established (no routing verifier): preserving potentially active workloads", false
	}
	ids, known := ar.Routing.ActiveBackends(ctx, rec.ServiceID)
	if !known {
		return "live proxy routing cannot be established: preserving potentially active workloads", false
	}
	if !idIn(ids, holderID) {
		return "the healthy name holder is not receiving traffic according to the live proxy state", false
	}
	if idIn(ids, artifactID) {
		return "the live proxy still routes to the " + role + " that would be retired", false
	}
	return "", true
}

// artifactBase splits a rotation-artifact name into the real service name and the
// artifact kind. ok is false for any other name (never removed).
func artifactBase(cleanName string) (base, kind string, ok bool) {
	if i := strings.Index(cleanName, "_dso_backup_"); i > 0 {
		return cleanName[:i], "backup", true
	}
	if i := strings.Index(cleanName, "_dso_new_"); i > 0 {
		return cleanName[:i], "new", true
	}
	if loc := oldContainerSuffixPattern.FindStringIndex(cleanName); loc != nil && loc[0] > 0 {
		return cleanName[:loc[0]], "old", true
	}
	return "", "", false
}

// retireSafeArtifacts removes owned rotation artifacts whose service has a
// verified healthy real-name holder, and ONLY those. Each candidate is
// re-inspected immediately before removal (identity, ownership, name) and the
// holder is re-verified. Everything else is preserved and returned for reporting.
type keptArtifact struct{ base, text string }

func (ar *AutomaticRecovery) retireSafeArtifacts(ctx context.Context, logger *zap.Logger) (removed int, preserved []keptArtifact) {
	list, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		logger.Error("Failed to list containers for artifact retirement; nothing removed", zap.Error(err))
		return 0, nil
	}
	byID := map[string]container.Summary{}
	for _, c := range list {
		byID[c.ID] = c
	}
	for _, c := range list {
		for _, n := range c.Names {
			cleanName := strings.TrimPrefix(n, "/")
			base, _, isArtifact := artifactBase(cleanName)
			if !isArtifact {
				continue
			}
			if !ar.ownedByMe(c.Labels) {
				logger.Warn("Skipping rotation-artifact-named container not owned by this agent",
					zap.String("container_id", shortID(c.ID)), zap.String("container_name", cleanName))
				break
			}
			holderID, ok, why := ar.holderState(ctx, base, c.ID)
			if !ok {
				logger.Warn("Preserving rotation artifact: no verified healthy workload holds the real name",
					zap.String("container_id", shortID(c.ID)), zap.String("container_name", cleanName), zap.String("reason", why))
				preserved = append(preserved, keptArtifact{base, cleanName + "(" + why + ")"})
				break
			}
			if why, permitted := ar.journalPermits(ctx, c.ID, c.Labels, holderID, byID[holderID].Labels); !permitted {
				logger.Warn("Preserving rotation artifact: the transition journal does not establish that retiring it is safe",
					zap.String("container_id", shortID(c.ID)), zap.String("container_name", cleanName), zap.String("reason", why))
				preserved = append(preserved, keptArtifact{base, cleanName + "(" + why + ")"})
				break
			}
			// Re-verify identity right before the destructive step.
			insp, ierr := ar.cli.ContainerInspect(ctx, c.ID)
			if ierr != nil || strings.TrimPrefix(insp.Name, "/") != cleanName || insp.Config == nil || !ar.ownedByMe(insp.Config.Labels) {
				preserved = append(preserved, keptArtifact{base, cleanName + "(identity changed)"})
				break
			}
			if again, why := ar.holderHealthy(ctx, base, c.ID); !again {
				preserved = append(preserved, keptArtifact{base, cleanName + "(" + why + ")"})
				break
			}
			logger.Info("Retiring redundant rotation artifact (a verified healthy workload holds the real name)",
				zap.String("container_id", shortID(c.ID)), zap.String("container_name", cleanName))
			faultinject.Hit("recovery:before_retire_artifact")
			_ = ar.cli.ContainerStop(ctx, c.ID, container.StopOptions{})
			if rerr := ar.cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); rerr != nil {
				logger.Warn("Failed to remove artifact", zap.String("container_id", shortID(c.ID)), zap.Error(rerr))
				preserved = append(preserved, keptArtifact{base, cleanName + "(removal failed)"})
			} else {
				removed++
				faultinject.Hit("recovery:after_retire_artifact")
			}
			break
		}
	}
	return removed, preserved
}

// ValidateStateOnStartup validates the state file and detects corruption.
// Returns true if state is valid, false if corrupted (state should be reinitialized).
func (ar *AutomaticRecovery) ValidateStateOnStartup(ctx context.Context) bool {
	if ar.st == nil {
		return true // State tracker not available; nothing to validate
	}

	// Check that all pending rotations have valid Docker references
	pending := ar.st.GetPendingRotations()
	if len(pending) == 0 {
		return true
	}

	ar.logger.Info("Validating rotation state",
		zap.Int("pending_rotations", len(pending)))

	// Try to list containers; if Docker is unreachable, we can't validate
	_, err := ar.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		ar.logger.Warn("Cannot validate state: Docker is unreachable",
			zap.Error(err))
		// Don't consider this as state corruption; Docker might be temporarily slow
		return true
	}

	// Nothing is discarded here, whatever its age. Age never establishes that a
	// rollback succeeded: a rollback_required record is written by the trigger engine
	// when a dispatch returned an error, and it knows only that (not whether the
	// strategy's rollback completed, whether a candidate or a partly delivered secret
	// remains, or whether the failed rollback was interrupted by a restart). The only
	// thing that may retire the record is RecoverFromCrash, which runs right after
	// this and moves it to recovered when Docker proves a verified healthy workload
	// and no unexamined artifact, or to critical_error (which blocks the guard)
	// otherwise. Expiring old records here used to skip exactly that examination for
	// the oldest, most likely forgotten, operations.

	return true
}

// CleanupOrphanedContainers sweeps rotation artifacts at startup. It is no longer
// a blind delete-by-name: it uses the same safety rule as recovery, so an artifact
// is removed only when an owned, verified-healthy container holds the real name.
// With unreadable persisted state it does nothing.
func (ar *AutomaticRecovery) CleanupOrphanedContainers(ctx context.Context) error {
	if ar.st != nil {
		if corrupt, _ := ar.st.Corrupt(); corrupt {
			ar.logger.Error("Orphan cleanup skipped: persisted rotation state is unreadable")
			return nil
		}
	}
	removed, preserved := ar.retireSafeArtifacts(ctx, ar.logger)
	ar.logger.Info("Orphan cleanup completed",
		zap.Int("containers_removed", removed), zap.Int("containers_preserved", len(preserved)))
	return nil
}

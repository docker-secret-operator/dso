package watcher

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/util"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// N19 / N22: managed file delivery plumbing shared by the restart, rolling-fallback
// and (through rotation.RollingStrategy) rolling paths. See
// docs/design/n19-managed-delivery.md.

// ErrFileSecretUnavailable (N22) is raised before ANY change when a secret the
// container needs as a file has no usable value (cache miss, no path, empty). The
// rotation fails closed: a candidate is never started without its file secret.
var ErrFileSecretUnavailable = errors.New("file secret unavailable: rotation refused before any change")

// effectiveInject is the inject configuration that applies to sec: its own when it
// names a type, otherwise the defaults (the same rule every strategy must use; the
// old restart probe looked only at sec.Inject.Type and so skipped defaulted file
// secrets).
func (r *ReloaderController) effectiveInject(sec dsoConfig.SecretMapping) dsoConfig.InjectionConfig {
	inj := sec.Inject
	if inj.Type == "" && r.Config != nil {
		inj = r.Config.Defaults.Inject
	}
	return inj
}

// providerFor resolves the provider whose cache entry holds sec's value. An explicit
// provider wins; with none, the only configured provider; with several, the first by
// name (deterministic, unlike map iteration order).
func (r *ReloaderController) providerFor(sec dsoConfig.SecretMapping) string {
	if sec.Provider != "" || r.Config == nil {
		return sec.Provider
	}
	names := make([]string, 0, len(r.Config.Providers))
	for n := range r.Config.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// resolveFileSpecs returns the file-secret specs target needs, from the cache.
// Every secret whose EFFECTIVE inject type is "file" must resolve: a cache miss, an
// empty value or a missing inject.path is an error (fail closed). nil, nil means the
// container needs no file secret (no gate, legacy flow). Values are copied so a later
// cache update cannot change what one rotation delivers. Each spec carries the keyed
// version identity of exactly the copied values when a version function is wired.
func (r *ReloaderController) resolveFileSpecs(target *TargetContainer) ([]rotation.FileSecretSpec, error) {
	if r.Config == nil {
		return nil, nil
	}
	var specs []rotation.FileSecretSpec
	for _, sec := range r.Config.Secrets {
		uses := false
		for _, ts := range target.Secrets {
			if strings.TrimSpace(ts) == sec.Name {
				uses = true
				break
			}
		}
		if !uses {
			continue
		}
		inj := r.effectiveInject(sec)
		if inj.Type != "file" {
			continue
		}
		if inj.Path == "" {
			return nil, fmt.Errorf("%w: secret %q is configured for file injection but has no inject.path", ErrFileSecretUnavailable, sec.Name)
		}
		if r.Cache == nil {
			return nil, fmt.Errorf("%w: secret %q has no cached value (no cache)", ErrFileSecretUnavailable, sec.Name)
		}
		prov := r.providerFor(sec)
		data, found := r.Cache.Get(fmt.Sprintf("%s:%s", prov, sec.Name))
		if !found || len(data) == 0 {
			return nil, fmt.Errorf("%w: secret %q has no cached value; the replacement would start without its file secret", ErrFileSecretUnavailable, sec.Name)
		}
		cp := make(map[string]string, len(data))
		for k, v := range data {
			cp[k] = v
		}
		spec := rotation.FileSecretSpec{Path: inj.Path, Data: cp, UID: inj.UID, GID: inj.GID}
		if r.DeliveredVersion != nil {
			spec.Version = r.DeliveredVersion(prov, sec.Name, cp)
		}
		specs = append(specs, spec)
	}
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].Path < specs[j].Path })
	return specs, nil
}

// newDelivery builds the managed-delivery runner for candidate journalling with js.
func (r *ReloaderController) newDelivery(ctx context.Context, js *jstate) *rotation.Delivery {
	ops := rotation.DefaultDeliveryOps(r.cli)
	if r.deliveryOps != nil {
		ops = *r.deliveryOps
	}
	return &rotation.Delivery{
		Ops:        ops,
		RotationID: js.rid,
		Intent:     func(c context.Context, e rotation.JournalEvent) error { return r.jIntent(c, js, e) },
		Ack:        func(c context.Context, e rotation.JournalEvent) { r.jAck(c, js, e) },
		Fault:      func(p string) { r.fault("restart:" + p) },
	}
}

// deliverGatedRestart runs managed delivery for a started, gated restart/fallback
// candidate. It returns a non-nil error when the candidate must be discarded.
func (r *ReloaderController) deliverGatedRestart(ctx context.Context, js *jstate, candidateID string, specs []rotation.FileSecretSpec) error {
	if len(specs) == 0 {
		return nil
	}
	if _, err := r.newDelivery(ctx, js).Execute(ctx, candidateID, specs); err != nil {
		r.Logger.Error("Managed file delivery failed; the candidate will be discarded and the original restored",
			zap.String("candidate_id", candidateID), zap.Error(err))
		return fmt.Errorf("managed file delivery to the replacement failed: %w", err)
	}
	return nil
}

// probeSpecs re-verifies every delivered file once the application is running
// (existence + non-empty), the post-readiness half of the verification.
func (r *ReloaderController) probeSpecs(ctx context.Context, id string, specs []rotation.FileSecretSpec) error {
	ops := rotation.DefaultDeliveryOps(r.cli)
	if r.deliveryOps != nil {
		ops = *r.deliveryOps
	}
	for _, s := range specs {
		if err := ops.Probe(ctx, id, s); err != nil {
			return err
		}
	}
	return nil
}

// armRestartCandidate validates and arms the gate on the clone cfg for specs (no
// Docker call). Used by both restart variants.
func (r *ReloaderController) armRestartCandidate(cfg *container.Config, hc *container.HostConfig, specs []rotation.FileSecretSpec) error {
	if len(specs) == 0 {
		return nil
	}
	return rotation.PrepareGatedCandidate(cfg, hc, specs, 0)
}

// markGateOwned / gateOwned: a candidate whose delivery a rotation is running must not
// be touched by the start-event re-delivery below (it would race the journalled delivery
// and could release the gate before the evidence is durable).
func (r *ReloaderController) markGateOwned(id string)  { r.gateOwned.Store(id, struct{}{}) }
func (r *ReloaderController) clearGateOwned(id string) { r.gateOwned.Delete(id) }

// gateResult is what one re-delivery attempt did.
type gateResult string

const (
	gateDelivered gateResult = "delivered" // delivered and the gate released
	gateReleased  gateResult = "released"  // the gate was already open: nothing to do
	gateSkipped   gateResult = "skipped"   // not ours to touch (owned by a rotation, not a target, foreign, stopped)
	gateWaiting   gateResult = "waiting"   // values unavailable: retried when the cache warms
	gateBlocked   gateResult = "blocked"   // unresolved operation or busy service: retried by the next pass
	gateFailed    gateResult = "failed"    // delivery failed; the gate stays closed
)

// maxGateScan bounds one reconciliation pass (containers examined).
const maxGateScan = 128

// redeliverGated delivers the cached file secrets to a RUNNING container that carries
// the N19 startup gate and is NOT a rotation candidate in flight, then releases its gate.
// It runs for (a) the Docker `start` event of a promoted container the daemon restarted
// (restart policy, `docker restart`, reboot) and (b) the bounded reconciliation pass over
// containers that were already waiting at their gate when the agent started or while the
// values were unavailable (ReconcileGatedContainers).
//
// It is a re-delivery, not a rotation: it writes no journal record and makes no ledger
// claim. What keeps it safe next to rotations is that it takes the SAME per-service
// rotation lock TriggerReload takes (never waits for it) and consults the SAME
// OperationGuard, so it never runs beside a rotation of the service and never touches a
// service with an unresolved/uncertain operation (that includes a candidate gate that an
// unresolved rotation owns after an agent restart). Any other condition leaves the gate
// CLOSED (the application never runs without its secret; the gate exits 75 after its
// bound). It never starts a stopped container.
func (r *ReloaderController) redeliverGated(ctx context.Context, id, name string, attrs map[string]string) gateResult {
	if r.cli == nil && r.deliveryOps == nil {
		return gateSkipped
	}
	if r.gateNotOurs(id, name, attrs) {
		return gateSkipped
	}
	tv, ok := r.Targets.Load(id)
	if !ok {
		r.Logger.Warn("A gated container is running but is not a registered target (unregistered or ambiguous ownership); its gate stays closed",
			zap.String("id", util.ShortID(id)), zap.String("name", name))
		return gateSkipped
	}
	target := tv.(*TargetContainer)
	ops := rotation.DefaultDeliveryOps(r.cli)
	if r.deliveryOps != nil {
		ops = *r.deliveryOps
	}
	insp, err := ops.Inspect(ctx, id)
	if err != nil || insp.ContainerJSONBase == nil || insp.State == nil {
		r.Logger.Warn("A gated container could not be inspected; its gate stays closed", zap.String("id", util.ShortID(id)), zap.Error(err))
		return gateFailed
	}
	if !insp.State.Running || insp.State.Restarting {
		return gateSkipped // never start a stopped container; an operator's stop stands
	}

	// Same lock identity TriggerReload derives for this target (a Compose project/service,
	// otherwise the container ID), so a rotation of the service and a re-delivery exclude
	// each other. Never wait: a busy service is retried by the next pass.
	serviceName, projectName := strings.TrimPrefix(id, "/"), ""
	if insp.Config != nil {
		if sn, ok := insp.Config.Labels["com.docker.compose.service"]; ok {
			serviceName = sn
			projectName = insp.Config.Labels["com.docker.compose.project"]
		}
	}
	lockKey := serviceLockKey(projectName, serviceName)
	lock := &lockInfo{startTime: time.Now(), serviceName: serviceName}
	if val, busy := r.rotationLocks.LoadOrStore(lockKey, lock); busy {
		info := val.(*lockInfo)
		if !(info.idleFor() > 5*time.Minute && r.rotationLocks.CompareAndSwap(lockKey, info, lock)) {
			r.Logger.Info("Re-delivery to a gated container deferred: a rotation of its service is running",
				zap.String("id", util.ShortID(id)), zap.String("service", lockKey))
			r.gateWaiting.Store(id, struct{}{})
			return gateBlocked
		}
	}
	defer r.rotationLocks.CompareAndDelete(lockKey, lock)

	if r.Guard != nil {
		keys := []string{serviceName, id, strings.TrimPrefix(insp.Name, "/")}
		if reason, blocked := r.Guard.RotationBlocked(keys...); blocked {
			r.Logger.Warn("Re-delivery to a gated container refused: its service has an unresolved operation (or startup recovery has not settled); the gate stays closed until that is reconciled",
				zap.String("id", util.ShortID(id)), zap.String("service", serviceName), zap.String("reason", reason))
			r.gateWaiting.Store(id, struct{}{})
			return gateBlocked
		}
	}
	// A rotation may have claimed the container while the lock and guard were taken.
	if r.gateNotOurs(id, name, attrs) {
		return gateSkipped
	}

	specs, err := r.resolveFileSpecs(target)
	if err != nil {
		r.Logger.Error("Gated container is waiting but its file secrets are unavailable; the gate stays closed and delivery is retried when the values are cached (fail closed)",
			zap.String("id", util.ShortID(id)), zap.Error(err))
		r.gateWaiting.Store(id, struct{}{})
		return gateWaiting
	}
	if len(specs) == 0 {
		r.gateWaiting.Delete(id)
		return gateSkipped
	}
	// Already released (the flag exists on the tmpfs): a start event that arrives after the
	// rotation that delivered it finished, or a container this pass already served.
	flag := rotation.FileSecretSpec{Path: rotation.GateFlagDir(specs), Data: map[string]string{rotation.GateFlagName: ""}}
	if ops.Probe(ctx, id, flag) == nil {
		r.gateWaiting.Delete(id)
		return gateReleased
	}
	dl := &rotation.Delivery{Ops: ops}
	if _, err := dl.Execute(ctx, id, specs); err != nil {
		r.Logger.Error("Re-delivery to a gated container failed; the gate stays closed (fail closed)",
			zap.String("id", util.ShortID(id)), zap.Error(err))
		r.gateWaiting.Delete(id)
		return gateFailed
	}
	r.gateWaiting.Delete(id)
	r.Logger.Info("Re-delivered file secrets to a gated container and released its gate (a re-delivery, not a rotation: no journal record, no ledger claim)",
		zap.String("id", util.ShortID(id)), zap.Int("directories", len(specs)))
	return gateDelivered
}

// gateNotOurs: a candidate a rotation delivers to (in-memory marker), a not-yet-ready
// proxy candidate, a container another agent owns, or a rotation artifact (rename-away /
// shadow names) is never re-delivered to here.
func (r *ReloaderController) gateNotOurs(id, name string, attrs map[string]string) bool {
	if _, owned := r.gateOwned.Load(id); owned || r.isProxyPending(id) {
		return true
	}
	if isRotationArtifactName(name) {
		return true
	}
	return r.ownedByOtherAgent(id, name, attrs)
}

var rotationArtifactSuffix = regexp.MustCompile(`_old_[0-9]+$`)

// isRotationArtifactName recognises the names DSO gives rotation shadows and rename-away
// originals (same conventions as internal/agent recovery).
func isRotationArtifactName(name string) bool {
	return strings.Contains(name, "_dso_new_") || strings.Contains(name, "_dso_backup_") || rotationArtifactSuffix.MatchString(name)
}

// GateReconcileReport counts what one ReconcileGatedContainers pass did.
type GateReconcileReport struct {
	Examined  int
	Delivered int
	Released  int
	Waiting   int
	Blocked   int
	Failed    int
	Skipped   int
	TimedOut  int
	Truncated bool
}

// ReconcileGatedContainers is the BOUNDED reconciliation of containers that already carry
// the startup gate: at agent startup (after recovery settled), after a Docker reconnect
// and periodic reconciliation, and when the secret cache becomes warm. Without it a
// container that was waiting at its gate when the agent (re)started, or whose restart
// found no cached value, was only served by a FUTURE start event.
//
// One pass lists the labelled containers (single Docker call, at most maxGateScan
// examined), re-delivers to each RUNNING one through redeliverGated (service lock +
// OperationGuard + cache only: it never fetches from a provider), and only DIAGNOSES an
// exited one: a gate that timed out (exit 75) on a container without a restart policy is
// NOT restarted (an operator's stop must stand) but is reported once with the recovery
// command. Stopped containers are never started.
func (r *ReloaderController) ReconcileGatedContainers(ctx context.Context) GateReconcileReport {
	var rep GateReconcileReport
	if r.cli == nil {
		return rep
	}
	f := filters.NewArgs()
	f.Add("label", rotation.GateLabel)
	list, err := r.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		r.Logger.Warn("Gated-container reconciliation could not list containers; retried at the next trigger", zap.Error(err))
		return rep
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	running := map[string]bool{}
	for i, c := range list {
		if ctx.Err() != nil {
			break
		}
		if i >= maxGateScan {
			rep.Truncated = true
			r.Logger.Warn("Gated-container reconciliation examined the first containers only", zap.Int("limit", maxGateScan))
			break
		}
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		rep.Examined++
		switch {
		case c.State == "running":
			running[c.ID] = true
			switch r.redeliverGated(ctx, c.ID, name, c.Labels) {
			case gateDelivered:
				rep.Delivered++
			case gateReleased:
				rep.Released++
			case gateWaiting:
				rep.Waiting++
			case gateBlocked:
				rep.Blocked++
			case gateFailed:
				rep.Failed++
			default:
				rep.Skipped++
			}
		case strings.HasPrefix(c.Status, "Exited (75)"):
			if r.diagnoseTimedOutGate(ctx, c.ID, name, c.Labels) {
				rep.TimedOut++
			}
		}
	}
	if !rep.Truncated {
		r.gateWaiting.Range(func(k, _ interface{}) bool {
			if !running[k.(string)] {
				r.gateWaiting.Delete(k)
			}
			return true
		})
	}
	return rep
}

// diagnoseTimedOutGate reports (once per exit) a gated container that exited 75, the gate's
// timeout status, while no restart policy will bring it back. DSO does not start it: a
// stopped container is not restarted automatically, and exit 75 cannot be told apart from
// the application's own status with certainty. It returns true when a diagnostic was due.
func (r *ReloaderController) diagnoseTimedOutGate(ctx context.Context, id, name string, attrs map[string]string) bool {
	if r.cli == nil && r.deliveryOps == nil {
		return false
	}
	if r.gateNotOurs(id, name, attrs) {
		return false
	}
	ops := rotation.DefaultDeliveryOps(r.cli)
	if r.deliveryOps != nil {
		ops = *r.deliveryOps
	}
	insp, err := ops.Inspect(ctx, id)
	if err != nil || insp.ContainerJSONBase == nil || insp.State == nil || insp.State.Running || insp.State.ExitCode != rotation.GateExitTimeout {
		return false
	}
	if insp.HostConfig != nil {
		if p := insp.HostConfig.RestartPolicy.Name; p == "always" || p == "unless-stopped" || p == "on-failure" {
			return false // the daemon restarts it; the start event re-delivers
		}
	}
	key := id + "@" + insp.State.FinishedAt
	if _, seen := r.gateDiag.LoadOrStore(key, struct{}{}); seen {
		return true
	}
	cname := name
	if cname == "" {
		cname = util.ShortID(id)
	}
	r.Logger.Warn("A gated container's secret gate timed out (exit 75) before its file secrets were delivered and it has no restart policy, so it stays stopped. "+
		"DSO does not restart stopped containers. Recovery: once the secret provider is reachable run `docker start "+cname+"`; "+
		"the running agent then delivers the cached secrets and releases the gate (docs/design/n19-managed-delivery.md, section 10)",
		zap.String("container", cname), zap.String("id", util.ShortID(id)))
	return true
}

// RequestGateReconcile schedules ReconcileGatedContainers on ONE coalescing worker: any
// number of requests while a pass runs collapse into one further pass, so triggers never
// accumulate goroutines or Docker calls.
func (r *ReloaderController) RequestGateReconcile(ctx context.Context) {
	r.gateScanPending.Store(true)
	if !r.gateScanBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		for {
			for r.gateScanPending.Swap(false) {
				if ctx.Err() != nil {
					break
				}
				r.ReconcileGatedContainers(ctx)
			}
			r.gateScanBusy.Store(false)
			if ctx.Err() != nil || !r.gateScanPending.Load() || !r.gateScanBusy.CompareAndSwap(false, true) {
				return
			}
		}
	}()
}

// NotifyCacheWarm is called after a secret value was cached. It costs nothing unless a
// running gated container is waiting for values, in which case it requests one pass.
func (r *ReloaderController) NotifyCacheWarm(ctx context.Context) {
	waiting := false
	r.gateWaiting.Range(func(_, _ interface{}) bool { waiting = true; return false })
	if waiting {
		r.RequestGateReconcile(ctx)
	}
}

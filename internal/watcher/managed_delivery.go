package watcher

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
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

// redeliverGated is the Docker `start` event handler for a container that carries the
// N19 startup gate and is NOT a rotation candidate in flight: a promoted container that
// the daemon restarted (restart policy, `docker restart`, reboot) comes up with an EMPTY
// tmpfs and a re-armed gate. The cached values are delivered through the same verified
// path and the gate is released. Any failure leaves the gate CLOSED (the application never
// runs without its secret; the gate exits 75 after its bound and the restart policy
// retries). This is a re-delivery, not a rotation: it writes no journal record and
// verifies the same identity, gate and tmpfs conditions as a rotation.
func (r *ReloaderController) redeliverGated(ctx context.Context, id, name string, attrs map[string]string) {
	if r.cli == nil && r.deliveryOps == nil {
		return
	}
	if _, owned := r.gateOwned.Load(id); owned || r.isProxyPending(id) {
		return
	}
	if r.ownedByOtherAgent(id, name, attrs) {
		return
	}
	tv, ok := r.Targets.Load(id)
	if !ok {
		r.Logger.Warn("A gated container started but is not a registered target; its gate stays closed",
			zap.String("id", util.ShortID(id)))
		return
	}
	specs, err := r.resolveFileSpecs(tv.(*TargetContainer))
	if err != nil {
		r.Logger.Error("Gated container restarted but its file secrets are unavailable; the gate stays closed (fail closed)",
			zap.String("id", util.ShortID(id)), zap.Error(err))
		return
	}
	if len(specs) == 0 {
		return
	}
	ops := rotation.DefaultDeliveryOps(r.cli)
	if r.deliveryOps != nil {
		ops = *r.deliveryOps
	}
	// Already released (the flag exists on the tmpfs): a start event that arrives after the
	// rotation that delivered it finished. Nothing to do.
	flag := rotation.FileSecretSpec{Path: rotation.GateFlagDir(specs), Data: map[string]string{rotation.GateFlagName: ""}}
	if ops.Probe(ctx, id, flag) == nil {
		return
	}
	dl := &rotation.Delivery{Ops: ops}
	if _, err := dl.Execute(ctx, id, specs); err != nil {
		r.Logger.Error("Re-delivery to a restarted gated container failed; the gate stays closed (fail closed)",
			zap.String("id", util.ShortID(id)), zap.Error(err))
		return
	}
	r.Logger.Info("Re-delivered file secrets to a restarted gated container and released its gate",
		zap.String("id", util.ShortID(id)), zap.Int("directories", len(specs)))
}

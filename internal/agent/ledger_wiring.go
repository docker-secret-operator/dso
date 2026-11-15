package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// S6: wiring of the applied-version ledger into the trigger engine
// (docs/design/m1-ledger-reconciliation.md sections 3, 5, 6, 8 and 12.3 O-2,
// O-3, O-5, O-7, O-8). The rules this file keeps:
//
//   - the M0 journal (StateTracker) stays the only rotation authority. The ledger
//     only makes the engine want or not want a rotation; every dispatch still goes
//     through StartRotationWithVersion and TriggerReload, where the operation
//     guard is asked per target (X1, O-3: the ledger is advisory and never
//     bypasses or replaces the guard);
//   - a missing, corrupt, newer-schema, key-lost or otherwise unusable ledger
//     never causes a rotation (X3): a first observation without a baseline is
//     adopted, never rotated (O-5);
//   - the ledger directory is persistent (/var/lib/dso/ledger, O-8), never under
//     /run/dso; the key is created only through the S3 key provider rules;
//   - no secret value or key byte is logged; versions are keyed HMAC identities.

// defaultLedgerDir is the production ledger (and HMAC key) directory.
const defaultLedgerDir = ledger.DefaultKeyDir

// ledgerObserveTimeout bounds the read-only Docker inspection of one
// observation's live targets (it runs under the per-key observation mutex).
const ledgerObserveTimeout = 5 * time.Second

type ledgerMode int

const (
	// ledgerDisabled: configured, but unusable for this process.
	ledgerDisabled ledgerMode = iota + 1
	// ledgerEnabled: a durable ledger with usable key material.
	ledgerEnabled
)

// ledgerWiring is the engine's ledger state. A nil *ledgerWiring means "not
// configured" (legacy behaviour; see TriggerEngine.ledger).
type ledgerWiring struct {
	mode   ledgerMode
	reason string
	store  *ledger.Store
	v      *ledger.Versions
	// liveFn replaces the Docker live-set inspection (tests only).
	liveFn func(providerName, secretName string) (map[ledger.Key][]ledger.LiveReplica, error)
}

func (lw *ledgerWiring) enabled() bool { return lw != nil && lw.mode == ledgerEnabled }

// ledgerPlanKey is one ledger entry a dispatch targets, with the desired
// sequence it observed.
type ledgerPlanKey struct {
	key ledger.Key
	seq uint64
}

// ledgerPlan is what one dispatch tells the ledger. It is built under the
// observation mutex and then owned by the dispatch goroutine.
type ledgerPlan struct {
	version  ledger.Version
	keys     []ledgerPlanKey
	fileMode bool
	provider string
	secret   string
	// correlated lists the keys Correlate accepted for this dispatch.
	correlated []ledger.Key
}

// journalVersion is the O-2 journal encoding of the plan's version, or "".
func (p *ledgerPlan) journalVersion() string {
	if p == nil {
		return ""
	}
	return FormatJournalSecretVersion(p.version)
}

// ledgerVerdict is the result of one ledger observation.
type ledgerVerdict struct {
	// usable is false when the ledger could not decide (disabled, key material,
	// inspection or persistence failure, or no live target).
	usable bool
	// noTargets: nothing to record (no live target uses the secret).
	noTargets bool
	rotate    bool
	// adopted: every observed entry was (re)adopted UNKNOWN (no baseline).
	adopted bool
	plan    *ledgerPlan
}

// openLedger opens the production ledger in dir with the S3 FileKeyProvider.
func (t *TriggerEngine) openLedger(dir string) {
	kp, err := ledger.NewFileKeyProvider(dir)
	if err != nil {
		t.disableLedger(fmt.Sprintf("the ledger directory %s is refused: %v", dir, err))
		return
	}
	t.openLedgerWith(dir, kp, ledger.Config{Keys: kp})
}

// openLedgerWith opens the ledger with an explicit key source and store
// configuration (tests inject directories and clocks). Any failure, and any
// memory-only result, disables the ledger for this process.
func (t *TriggerEngine) openLedgerWith(dir string, km ledger.KeyMaterialSource, cfg ledger.Config) {
	if cfg.Events == nil {
		cfg.Events = t.logLedgerEvent
	}
	s, res, err := ledger.Open(dir, cfg)
	if err != nil {
		t.disableLedger(fmt.Sprintf("opening the ledger in %s failed: %v", dir, err))
		return
	}
	if res.Mode != ledger.ModeDurable {
		_ = s.Close()
		t.disableLedger(fmt.Sprintf("the ledger in %s is not durable (%s: %s)", dir, res.Reason, res.Detail))
		return
	}
	_, key, kerr := km.KeyMaterial()
	clear(key)
	if kerr != nil {
		_ = s.Close()
		t.disableLedger(fmt.Sprintf("the ledger key in %s is unusable: %v", dir, kerr))
		return
	}
	v, err := ledger.NewVersions(s, km)
	if err != nil {
		_ = s.Close()
		t.disableLedger(err.Error())
		return
	}
	t.ledger = &ledgerWiring{mode: ledgerEnabled, store: s, v: v}
	switch {
	case res.Preserved != "":
		t.Logger.Warn("Applied-version ledger was not usable and was preserved aside; a fresh ledger was started: every secret is re-adopted with an unknown applied version and nothing is rotated because of it",
			zap.String("dir", dir), zap.String("reason", string(res.Reason)), zap.String("preserved", res.Preserved))
	case res.Detail != "":
		t.Logger.Warn("Applied-version ledger opened with a warning", zap.String("dir", dir), zap.String("detail", res.Detail))
	default:
		t.Logger.Info("Applied-version ledger enabled", zap.String("dir", dir), zap.String("key_id", res.KeyID))
	}
}

func (t *TriggerEngine) disableLedger(reason string) {
	t.ledger = &ledgerWiring{mode: ledgerDisabled, reason: reason}
	t.Logger.Warn("Applied-version ledger DISABLED for this process; rotation continues without it. "+
		"The first observation of each secret in this process is adopted without rotating (no mass rotation); "+
		"a provider change made while the agent was down is not detected until the next change",
		zap.String("reason", reason))
}

func (t *TriggerEngine) logLedgerEvent(ev ledger.Event) {
	fields := []zap.Field{zap.String("kind", string(ev.Kind)), zap.String("reason", string(ev.Reason)), zap.String("detail", ev.Detail)}
	if ev.Err != nil {
		fields = append(fields, zap.Error(ev.Err))
	}
	switch ev.Kind {
	case ledger.EventPersistenceDegraded, ledger.EventPreserved, ledger.EventRecovery:
		t.Logger.Warn("Applied-version ledger event", fields...)
	default:
		t.Logger.Debug("Applied-version ledger event", fields...)
	}
}

func (t *TriggerEngine) closeLedger() {
	if t.ledger != nil && t.ledger.store != nil {
		_ = t.ledger.store.Close()
	}
}

// secretFileMode reports whether sec is delivered as a file (N19: adoption of a
// file delivery stays unverified).
func (t *TriggerEngine) secretFileMode(sec config.SecretMapping) bool {
	typ := sec.Inject.Type
	if typ == "" && t.Config != nil {
		typ = t.Config.Defaults.Inject.Type
	}
	return typ == "file"
}

// ledgerLiveSet returns the live replicas of every ledger entry that uses
// secretName, keyed per design 1.4: (Compose project, service) or ("",
// container name) for a non-Compose target. It is a READ-ONLY Docker
// inspection of the reloader's targets; DSO rotation artifacts are never an
// identity. An inspection failure is returned (the caller then treats the
// ledger as unusable for this observation).
func (t *TriggerEngine) ledgerLiveSet(ctx context.Context, providerName, secretName string) (map[ledger.Key][]ledger.LiveReplica, error) {
	if t.ledger != nil && t.ledger.liveFn != nil {
		return t.ledger.liveFn(providerName, secretName)
	}
	if t.Reloader == nil {
		return nil, nil
	}
	type target struct{ id, strategy string }
	var targets []target
	t.Reloader.Targets.Range(func(_, v interface{}) bool {
		tc := v.(*watcher.TargetContainer)
		for _, s := range tc.Secrets {
			if strings.TrimSpace(s) == secretName {
				targets = append(targets, target{tc.ID, tc.Strategy})
				break
			}
		}
		return true
	})
	if len(targets) == 0 {
		return nil, nil
	}
	if t.DockerClient == nil {
		return nil, errors.New("no Docker client for the live-set inspection")
	}
	out := map[ledger.Key][]ledger.LiveReplica{}
	for _, tg := range targets {
		insp, err := t.DockerClient.ContainerInspect(ctx, tg.id)
		if err != nil {
			return nil, fmt.Errorf("inspecting target %s: %w", tg.id, err)
		}
		if insp.ContainerJSONBase == nil {
			return nil, fmt.Errorf("inspecting target %s: no container data", tg.id)
		}
		name := strings.TrimPrefix(insp.Name, "/")
		if name == "" || IsDSORotationArtifactName(name) {
			continue
		}
		var labels map[string]string
		if insp.Config != nil {
			labels = insp.Config.Labels
		}
		k := ledger.Key{Provider: providerName, Secret: secretName, Service: name}
		number := ""
		if svc := labels["com.docker.compose.service"]; svc != "" {
			k.Project, k.Service = labels["com.docker.compose.project"], svc
			number = decimalOrEmpty(labels["com.docker.compose.container-number"])
		}
		out[k] = append(out[k], ledger.LiveReplica{
			Number: number, ContainerID: insp.ID, ContainerName: name, Created: insp.Created,
			Eligible: tg.strategy != "none",
		})
	}
	for k := range out {
		reps := out[k]
		sort.Slice(reps, func(i, j int) bool {
			if reps[i].Number != reps[j].Number {
				return reps[i].Number < reps[j].Number
			}
			return reps[i].ContainerName < reps[j].ContainerName
		})
	}
	return out, nil
}

func decimalOrEmpty(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return ""
		}
	}
	return s
}

func sortedLedgerKeys(m map[ledger.Key][]ledger.LiveReplica) []ledger.Key {
	keys := make([]ledger.Key, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Service < b.Service
	})
	return keys
}

// ledgerObserve is the ledger half of commitObservation (design 3 steps 1-2).
// It runs under the per-key observation mutex: it observes the value for every
// live entry of the secret (Observe, the sole writer of desired) and asks
// NeedsRotation. Any failure returns an unusable verdict; the caller then
// applies the in-process rule (never a mass rotation).
func (t *TriggerEngine) ledgerObserve(providerName, secretName string, data map[string]string, sec config.SecretMapping) ledgerVerdict {
	lw := t.ledger
	if !lw.enabled() {
		return ledgerVerdict{}
	}
	ver, err := lw.v.NewVersion(providerName, secretName, data)
	if err != nil {
		t.Logger.Warn("Applied-version ledger cannot identify the value (key material unavailable); no ledger decision",
			zap.String("provider", providerName), zap.String("secret", secretName), zap.Error(err))
		return ledgerVerdict{}
	}
	ctx, cancel := context.WithTimeout(t.ctx, ledgerObserveTimeout)
	live, err := t.ledgerLiveSet(ctx, providerName, secretName)
	cancel()
	if err != nil {
		t.Logger.Warn("Applied-version ledger: live targets could not be inspected; no ledger decision",
			zap.String("provider", providerName), zap.String("secret", secretName), zap.Error(err))
		return ledgerVerdict{}
	}
	if len(live) == 0 {
		return ledgerVerdict{noTargets: true}
	}
	plan := &ledgerPlan{version: ver, fileMode: t.secretFileMode(sec), provider: providerName, secret: secretName}
	rotate, adopted := false, true
	for _, k := range sortedLedgerKeys(live) {
		res, err := lw.v.Observe(k, ver, live[k])
		if err != nil {
			t.Logger.Warn("Applied-version ledger could not record the observation; no ledger decision",
				zap.String("provider", providerName), zap.String("secret", secretName),
				zap.String("service", k.Service), zap.Error(err))
			return ledgerVerdict{}
		}
		if res.Kind != ledger.ObserveAdopted && res.Kind != ledger.ObserveReadopted {
			adopted = false
		}
		d := lw.v.NeedsRotation(k)
		if d.Rotate() {
			rotate = true
		}
		t.Logger.Debug("Applied-version ledger decision", zap.String("secret", secretName), zap.String("service", k.Service),
			zap.String("observe", string(res.Kind)), zap.String("decision", string(d.Kind)), zap.String("reason", string(d.Reason)))
		plan.keys = append(plan.keys, ledgerPlanKey{key: k, seq: res.Entry.DesiredSeq})
	}
	v := ledgerVerdict{usable: true, rotate: rotate, adopted: adopted}
	if rotate {
		v.plan = plan
	}
	return v
}

// ledgerCorrelate writes delivery=requested for every planned entry (design 3
// step 4), after StartRotationWithVersion returned nil and before
// TriggerReload. A refusal (stale desired_seq because a newer value was
// observed meanwhile, persistence) is logged and ignored: that entry is simply
// not advanced by this dispatch.
func (t *TriggerEngine) ledgerCorrelate(p *ledgerPlan, rotationID string) {
	if p == nil || !t.ledger.enabled() {
		return
	}
	for _, pk := range p.keys {
		err := CorrelateAfterJournalStart(t.ledger.v, t.StateTracker, nil, pk.key, rotationID, pk.seq, p.version)
		if err != nil && !errors.Is(err, ledger.ErrNoChange) {
			t.Logger.Warn("Applied-version ledger: delivery request not recorded; this dispatch will not advance the entry",
				zap.String("secret", p.secret), zap.String("service", pk.key.Service), zap.String("rotation_id", rotationID), zap.Error(err))
			continue
		}
		p.correlated = append(p.correlated, pk.key)
	}
}

// ledgerTerminal records the dispatch outcome the journal states (design 3
// step 5), after the journal's own terminal write: verified delivery advances
// applied (signal: adoption unverified), anything else resolves the request
// with the journal's cause. A failure is logged; the startup reconcile
// converges from the journal after a restart (W8).
func (t *TriggerEngine) ledgerTerminal(p *ledgerPlan, rotationID string) {
	if p == nil || len(p.correlated) == 0 || !t.ledger.enabled() || t.StateTracker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(t.ctx, ledgerObserveTimeout)
	live, lerr := t.ledgerLiveSet(ctx, p.provider, p.secret)
	cancel()
	if lerr != nil {
		live = nil // replica identities are refreshed by the next observation
	}
	for _, k := range p.correlated {
		res, err := RecordRotationOutcome(t.ledger.v, t.StateTracker, k, rotationID, &p.version, p.fileMode, live[k])
		if err != nil {
			t.Logger.Warn("Applied-version ledger: rotation outcome not recorded (converges from the journal at the next start)",
				zap.String("secret", p.secret), zap.String("service", k.Service), zap.String("rotation_id", rotationID), zap.Error(err))
			continue
		}
		t.Logger.Info("Applied-version ledger: rotation outcome recorded",
			zap.String("secret", p.secret), zap.String("service", k.Service), zap.String("rotation_id", rotationID),
			zap.String("outcome", string(res.Outcome)), zap.String("adoption", string(res.Adoption)), zap.String("cause", string(res.Cause)))
	}
}

// reconcileLedgerAtStartup converges the ledger with the settled journal
// (design 5 step 5). It is read-only with respect to the journal and Docker
// (live identities are inspected, nothing is mutated).
func (t *TriggerEngine) reconcileLedgerAtStartup() {
	if !t.ledger.enabled() || t.StateTracker == nil {
		return
	}
	in := LedgerReconcileInput{Now: time.Now(), FileMode: map[ledger.Key]bool{}, Live: map[ledger.Key][]ledger.LiveReplica{}}
	fileMode := map[[2]string]bool{}
	if t.Config != nil {
		for pName := range t.Config.Providers {
			for _, sec := range t.Config.Secrets {
				if sec.Provider != "" && sec.Provider != pName {
					continue
				}
				fileMode[[2]string{pName, sec.Name}] = t.secretFileMode(sec)
				ctx, cancel := context.WithTimeout(t.ctx, ledgerObserveTimeout)
				live, err := t.ledgerLiveSet(ctx, pName, sec.Name)
				cancel()
				if err != nil {
					continue // not drift-checked
				}
				for k, reps := range live {
					in.Live[k] = reps
				}
			}
		}
	}
	if snap := t.ledger.v.Snapshot(); snap.Ledger != nil {
		for _, e := range snap.Ledger.Entries {
			in.FileMode[e.Key] = fileMode[[2]string{e.Key.Provider, e.Key.Secret}]
		}
	}
	rep, err := ReconcileLedgerFromTracker(t.ledger.v, t.StateTracker, in)
	switch {
	case err != nil:
		t.Logger.Warn("Applied-version ledger reconcile failed; entries stay as stored (unknown never means changed)", zap.Error(err))
	case rep.Skipped:
		t.Logger.Warn("Applied-version ledger reconcile skipped: the rotation journal is unreadable", zap.String("reason", string(rep.SkipReason)))
	default:
		for _, c := range rep.Changes {
			t.Logger.Info("Applied-version ledger reconciled from the journal",
				zap.String("secret", c.Key.Secret), zap.String("service", c.Key.Service),
				zap.String("rotation_id", c.RotationID), zap.String("rule", c.Rule))
		}
	}
}

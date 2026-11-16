package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
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
	// diag classifies why the ledger is disabled (ledgerDisabled only).
	diag  ledgerDiagnosis
	store *ledger.Store
	v     *ledger.Versions
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
		t.disableLedgerDiag(classifyLedgerError(dir, err, defaultLedgerInspector()),
			fmt.Sprintf("the ledger directory %s is refused: %v", dir, err))
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
	insp := defaultLedgerInspector()
	s, res, err := ledger.Open(dir, cfg)
	if err != nil {
		t.disableLedgerDiag(classifyLedgerError(dir, err, insp), fmt.Sprintf("opening the ledger in %s failed: %v", dir, err))
		return
	}
	if res.Mode != ledger.ModeDurable {
		_ = s.Close()
		// A key reason carries only a text; the typed key error (lost, unsafe,
		// corrupt) is what tells them apart, so ask the key source for it.
		var kerr error
		if res.Reason == ledger.ReasonKeyUnsafe || res.Reason == ledger.ReasonKeyMalformed {
			_, key, e := km.KeyMaterial()
			clear(key)
			kerr = e
		}
		t.disableLedgerDiag(classifyLedgerOpen(dir, res, kerr, insp),
			fmt.Sprintf("the ledger in %s is not durable (%s: %s)", dir, res.Reason, res.Detail))
		return
	}
	_, key, kerr := km.KeyMaterial()
	clear(key)
	if kerr != nil {
		_ = s.Close()
		t.disableLedgerDiag(classifyLedgerError(dir, kerr, insp), fmt.Sprintf("the ledger key in %s is unusable: %v", dir, kerr))
		return
	}
	v, err := ledger.NewVersions(s, km)
	if err != nil {
		_ = s.Close()
		t.disableLedgerDiag(ledgerDiagnosis{code: LedgerCodeUnavailable, detail: "the version identity could not be initialised"}, err.Error())
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

// disableLedger disables the ledger with an unclassified reason (kept for the
// callers that have no typed cause).
func (t *TriggerEngine) disableLedger(reason string) {
	t.disableLedgerDiag(ledgerDiagnosis{code: LedgerCodeUnavailable, detail: reason}, reason)
}

// disableLedgerDiag disables the ledger for this process and emits the ONE
// operator-visible warning (it runs once, at engine start). The warning names
// the cause code, the unsafe path with its actual and expected owner/mode when
// that is the cause, the supported remediation, and the degraded behaviour that
// applies meanwhile. It never contains secret values or key material: only
// paths, modes, uids and static reasons.
func (t *TriggerEngine) disableLedgerDiag(d ledgerDiagnosis, reason string) {
	t.ledger = &ledgerWiring{mode: ledgerDisabled, reason: reason, diag: d}
	fields := []zap.Field{zap.String("code", d.code), zap.String("reason", d.detail)}
	if d.path != "" {
		fields = append(fields, zap.String("path", d.path))
	}
	if d.actual != "" {
		fields = append(fields, zap.String("actual", d.actual))
	}
	if d.expected != "" {
		fields = append(fields, zap.String("expected", d.expected))
	}
	fields = append(fields, zap.String("remediation", d.remediation), zap.String("degraded_behaviour", ledgerDegradedBehaviour))
	t.Logger.Warn("Applied-version ledger DISABLED for this process: durable version tracking is unavailable. "+
		"The first observation of each secret in this process is adopted without rotating (no mass rotation); "+
		"a provider change made while the agent was down may go undetected until the next change of that secret; "+
		"rotation otherwise continues", fields...)
}

// LedgerStatus reports the ledger availability for the existing status RPC
// (and so for `docker dso status` and `docker dso doctor`).
func (t *TriggerEngine) LedgerStatus() api.LedgerStatusInfo {
	if t == nil || t.ledger == nil {
		return api.LedgerStatusInfo{State: api.LedgerNotConfigured, Code: LedgerCodeNotConfigured,
			Message: "the applied-version ledger is not configured in this engine"}
	}
	if t.ledger.enabled() {
		return api.LedgerStatusInfo{State: api.LedgerActive, Code: LedgerCodeActive, Message: "durable version tracking is active"}
	}
	d := t.ledger.diag
	msg := d.detail
	if d.path != "" && d.actual != "" {
		msg = fmt.Sprintf("%s: %s (actual %s, expected %s)", d.path, d.detail, d.actual, d.expected)
	}
	return api.LedgerStatusInfo{State: api.LedgerDisabled, Code: d.code, Message: msg,
		Remediation: d.remediation, Degraded: ledgerDegradedBehaviour}
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

// ============================================================ diagnostics

// Ledger availability cause codes (also carried by api.LedgerStatusInfo.Code).
const (
	LedgerCodeActive            = "active"
	LedgerCodeNotConfigured     = "not_configured"
	LedgerCodeUnsafePermissions = "unsafe_permissions"
	LedgerCodeKeyLost           = "key_lost"
	LedgerCodeCorruptState      = "corrupt_state"
	LedgerCodeUnsupportedSchema = "unsupported_schema"
	LedgerCodeLockHeld          = "lock_held"
	LedgerCodeUnsupportedOS     = "unsupported_platform"
	LedgerCodeInvalidDir        = "invalid_directory"
	LedgerCodeUnavailable       = "unavailable"
)

// ledgerDegradedBehaviour is what the operator gets while the ledger is
// disabled (X3, O-5): the in-process rule of commitObservation.
const ledgerDegradedBehaviour = "durable version tracking is unavailable: the first observation of each secret in this process is " +
	"adopted without rotating; a provider change made while the agent was down may go undetected until the next change of that secret; " +
	"no mass rotation; the rotation journal and operation guard are unaffected"

// ledgerStateDir is the persistent-state directory that contains the ledger.
var ledgerStateDir = filepath.Dir(defaultLedgerDir)

// ledgerDiagnosis classifies one disabled ledger. No field ever holds a secret
// value, key bytes or a version identity.
type ledgerDiagnosis struct {
	code        string
	path        string // the offending path, when identified
	actual      string // actual owner/mode of path
	expected    string // expected owner/mode of path
	detail      string // static cause text
	remediation string
}

// ledgerInspector is the read-only filesystem view the diagnosis uses.
type ledgerInspector struct {
	stat func(string) (os.FileInfo, error)
	euid func() int
}

func defaultLedgerInspector() ledgerInspector {
	return ledgerInspector{stat: os.Stat, euid: os.Geteuid}
}

// classifyLedgerError maps a typed key error (or an open error) to a diagnosis.
func classifyLedgerError(dir string, err error, in ledgerInspector) ledgerDiagnosis {
	detail := keyReason(err)
	switch {
	case errors.Is(err, ledger.ErrKeyLost):
		return diagKeyLost(dir, detail)
	case errors.Is(err, ledger.ErrKeyCorrupt):
		return diagCorrupt(dir, detail)
	case errors.Is(err, ledger.ErrKeyUnsupported):
		return diagUnsupportedOS(detail)
	case errors.Is(err, ledger.ErrKeyDirectory):
		return ledgerDiagnosis{code: LedgerCodeInvalidDir, path: dir, detail: detail,
			remediation: "the ledger directory must be an absolute, persistent path (default " + defaultLedgerDir + "), never under /run/dso; correct the configuration"}
	case errors.Is(err, ledger.ErrKeyUnsafe):
		return diagUnsafe(dir, detail, in)
	}
	return ledgerDiagnosis{code: LedgerCodeUnavailable, path: dir, detail: detail,
		remediation: "see the agent log for the underlying error; durable tracking resumes at the next agent start once the cause is fixed"}
}

// classifyLedgerOpen maps a memory-only ledger.OpenResult (kerr is the typed
// key error for the key reasons, nil otherwise) to a diagnosis.
func classifyLedgerOpen(dir string, res ledger.OpenResult, kerr error, in ledgerInspector) ledgerDiagnosis {
	switch res.Reason {
	case ledger.ReasonUnsafeDir:
		return diagUnsafe(dir, res.Detail, in)
	case ledger.ReasonKeyUnsafe, ledger.ReasonKeyMalformed:
		if kerr != nil {
			return classifyLedgerError(dir, kerr, in)
		}
		if res.Reason == ledger.ReasonKeyMalformed {
			return diagCorrupt(dir, res.Detail)
		}
		return diagUnsafe(dir, res.Detail, in)
	case ledger.ReasonKeyMissing:
		return diagKeyLost(dir, res.Detail)
	case ledger.ReasonNewerSchema:
		return ledgerDiagnosis{code: LedgerCodeUnsupportedSchema, path: filepath.Join(dir, "ledger.json"), detail: "the ledger was written by a newer DSO schema; it is left untouched",
			remediation: "run the dso version that wrote this ledger (upgrade the dso binary); do not delete or edit ledger.json"}
	case ledger.ReasonLocked:
		return ledgerDiagnosis{code: LedgerCodeLockHeld, path: dir, detail: "another process holds the ledger lock",
			remediation: "stop the other dso agent instance that uses " + dir + "; the lock is released when it exits, then restart this agent"}
	case ledger.ReasonUnsupportedPlatform:
		return diagUnsupportedOS(res.Detail)
	case ledger.ReasonCorrupt, ledger.ReasonLedgerUnreadable, ledger.ReasonTooLarge, ledger.ReasonPreserveFailed, ledger.ReasonKeyMismatch:
		return diagCorrupt(dir, res.Detail)
	}
	return ledgerDiagnosis{code: LedgerCodeUnavailable, path: dir, detail: string(res.Reason) + ": " + res.Detail,
		remediation: "see the agent log; durable tracking resumes at the next agent start once the cause is fixed"}
}

func diagKeyLost(dir, detail string) ledgerDiagnosis {
	return ledgerDiagnosis{code: LedgerCodeKeyLost, path: filepath.Join(dir, "ledger.key"), detail: detail,
		remediation: "restore ledger.key (root-owned, mode 0400) into " + dir + " from a backup; the agent never regenerates a lost key. " +
			"If the ledger is unrecoverable, move its files aside deliberately and restart: every secret is then re-adopted with an unknown applied version"}
}

func diagCorrupt(dir, detail string) ledgerDiagnosis {
	return ledgerDiagnosis{code: LedgerCodeCorruptState, path: dir, detail: detail,
		remediation: "inspect the ledger files in " + dir + " (the agent never deletes or rewrites them); restore from a backup or move them aside deliberately and restart"}
}

func diagUnsupportedOS(detail string) ledgerDiagnosis {
	return ledgerDiagnosis{code: LedgerCodeUnsupportedOS, detail: detail,
		remediation: "none: this platform cannot verify directory ownership and modes, so the durable ledger is never enabled here"}
}

// diagUnsafe builds the unsafe_permissions diagnosis: it names the first unsafe
// path of the chain with its actual and expected owner/mode.
func diagUnsafe(dir, detail string, in ledgerInspector) ledgerDiagnosis {
	d := ledgerDiagnosis{code: LedgerCodeUnsafePermissions, path: dir, detail: detail}
	if up, ok := findUnsafeLedgerPath(dir, in); ok {
		d.path, d.actual, d.expected = up.path, up.actual, up.expected
		if d.detail == "" || strings.Contains(d.detail, "chain is unsafe") {
			d.detail = up.problem
		}
		d.remediation = unsafeRemediation(up.path)
		return d
	}
	d.remediation = "make " + dir + " and every directory above it owned by root and not writable by group or others (a non-recursive chmod/chown of those directories only; never -R)"
	return d
}

// unsafeRemediation is the supported fix for one unsafe path. /var/lib/dso is
// re-applied by `bootstrap agent` (its permission step is a non-recursive
// chown/chmod of the four DSO directories); the manual equivalent is the same
// two non-recursive commands. Nothing recursive is ever advised.
func unsafeRemediation(path string) string {
	if path == ledgerStateDir {
		return "re-apply the DSO directory permissions with `sudo docker dso bootstrap agent` (it sets " + ledgerStateDir +
			" to root:dso 0750 non-recursively and rewrites dso.yaml, so review it), or only this directory by hand: " +
			"`sudo chown root:dso " + ledgerStateDir + " && sudo chmod 0750 " + ledgerStateDir + "` (no -R: nothing inside is changed), then restart the agent"
	}
	if filepath.Base(path) == "ledger.key" {
		return "restore the key file's ownership and mode by hand: `sudo chown root " + path + " && sudo chmod 0400 " + path + "` (the key itself is never regenerated), then restart the agent"
	}
	return "fix this directory only, not recursively: `sudo chown root " + path + " && sudo chmod go-w " + path + "` (the ledger directory itself must be 0700), then restart the agent"
}

type unsafeLedgerPath struct {
	path, actual, expected, problem string
}

// findUnsafeLedgerPath walks dir top-down with the key provider's rules and
// returns the first directory that is not owned by root (or the agent user) or
// is group/other-writable (a sticky directory is tolerated as an ancestor
// only), or a ledger file with an unsafe mode. Read-only.
func findUnsafeLedgerPath(dir string, in ledgerInspector) (unsafeLedgerPath, bool) {
	abs, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return unsafeLedgerPath{}, false
	}
	parts := strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator))
	cur := string(filepath.Separator)
	for i, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := in.stat(cur)
		if err != nil {
			return unsafeLedgerPath{}, false
		}
		last := i == len(parts)-1
		uid, gid, known := ownerOf(fi)
		perm := fi.Mode().Perm()
		sticky := fi.Mode()&os.ModeSticky != 0
		expected := expectedDirSpec(cur, abs)
		switch {
		case known && uid != 0 && int(uid) != in.euid():
			return unsafeLedgerPath{cur, ownerModeText(uid, gid, perm), expected, "directory is owned by neither root nor the agent user"}, true
		case perm&0o022 != 0 && (last || !sticky):
			return unsafeLedgerPath{cur, ownerModeText(uid, gid, perm), expected, "directory is writable by group or others"}, true
		}
	}
	for _, name := range []string{"ledger.key", "ledger.json"} {
		p := filepath.Join(abs, name)
		fi, err := in.stat(p)
		if err != nil {
			continue
		}
		perm := fi.Mode().Perm()
		uid, gid, known := ownerOf(fi)
		want, bad := "root, mode 0600 or stricter (private)", perm&^0o600 != 0
		if name == "ledger.key" {
			want, bad = "root, mode 0400 or 0600", perm&^0o600 != 0 || perm&0o400 == 0
		}
		if known && uid != 0 && int(uid) != in.euid() {
			return unsafeLedgerPath{p, ownerModeText(uid, gid, perm), want, "file is owned by neither root nor the agent user"}, true
		}
		if bad {
			return unsafeLedgerPath{p, ownerModeText(uid, gid, perm), want, "file mode is not private"}, true
		}
	}
	return unsafeLedgerPath{}, false
}

// expectedDirSpec states what the contract requires of one directory.
func expectedDirSpec(path, ledgerDir string) string {
	switch path {
	case ledgerStateDir:
		return "root:dso, mode 0750"
	case ledgerDir:
		return "root, mode 0700 (private)"
	}
	return "owned by root (or the agent user), not writable by group or others"
}

func ownerModeText(uid, gid uint32, perm os.FileMode) string {
	return fmt.Sprintf("uid %d gid %d, mode %04o", uid, gid, perm)
}

// ownerOf extracts the numeric owner from a FileInfo without a platform build
// tag: *syscall.Stat_t (linux, darwin) exposes Uid and Gid; anything else
// reports unknown.
func ownerOf(fi os.FileInfo) (uid, gid uint32, ok bool) {
	v := reflect.ValueOf(fi.Sys())
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, 0, false
	}
	u, g := v.FieldByName("Uid"), v.FieldByName("Gid")
	if !u.IsValid() || !g.IsValid() || !u.CanUint() || !g.CanUint() {
		return 0, 0, false
	}
	return uint32(u.Uint()), uint32(g.Uint()), true // #nosec G115 -- uid/gid are 32-bit
}

// keyReason returns the static reason of a typed key error, never key bytes.
func keyReason(err error) string {
	var ke *ledger.KeyError
	if errors.As(err, &ke) && ke.Reason != "" {
		if ke.Err != nil {
			return ke.Reason + ": " + ke.Err.Error()
		}
		return ke.Reason
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

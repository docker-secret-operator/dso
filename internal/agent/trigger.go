package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/faultinject"
	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/proxy"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	pkgprovider "github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
)

type TriggerEngine struct {
	Cache             *SecretCache
	LimitCache        *LimitEnforcingCache // optional; when set, ExecuteRotation writes through it
	Store             *providers.SecretStoreManager
	Reloader          *watcher.ReloaderController
	Logger            *zap.Logger
	rotations         sync.Map
	events            sync.Map
	secretHashes      sync.Map
	lastRotations     sync.Map
	observeLocks      sync.Map // map[string]*sync.Mutex, key provider:secret (PE-31)
	Server            *AgentServer
	Config            *config.Config
	ctx               context.Context
	cancel            context.CancelFunc
	StateTracker      *StateTracker
	LockManager       *rotation.LockManager
	TimeoutController *TimeoutController
	DockerClient      *client.Client // For crash recovery

	// Routing lets startup recovery verify LIVE proxy routing (M0-04 3.1). It is
	// set by AttachProxy to a verifier over this process's own embedded proxy.
	// Nil means routing is unknown and recovery fails closed.
	Routing RoutingVerifier

	proxyMgr *proxy.Manager // embedded proxy (AttachProxy); nil when none
	guard    *journalGuard  // durable competing-mutation guard (M0-04 3.3)

	// Dispatcher is the optional notification sink for rotation/recovery
	// outcome events (internal/notify). Nil when notifications are not
	// configured. Strictly an observer: emitted-to AFTER the authoritative
	// StateTracker transition, via a non-blocking Dispatch that cannot
	// fail, block, or otherwise influence rotation state.
	Dispatcher *notify.Dispatcher

	// ledger is the S6 applied-version ledger wiring (ledger_wiring.go). nil
	// means "not configured": engines built without NewTriggerEngine (unit tests,
	// NewTriggerEngineWithDirs) keep the legacy behaviour in which a first
	// observation dispatches. NewTriggerEngine always configures it, enabled or
	// disabled for the process.
	ledger *ledgerWiring
	// baselined holds the provider:secret keys observed at least once by this
	// process. With a configured but unusable ledger, a key's first observation
	// is adopted without rotating (design 8.2, O-5: no mass rotation).
	baselined sync.Map

	// Test seams; zero means the production defaults
	// (defaultSecretLockTimeout, defaultRotationCooldown, Reloader.TriggerReload).
	secretLockTimeout time.Duration
	rotationCooldown  time.Duration
	reloadFn          func(ctx context.Context, secretName string, onComplete func(error)) error
}

// RotationDecision is what ExecuteRotationDecision did with one observation
// (design 8.3, BQ-3 option b).
type RotationDecision string

const (
	// RotationDispatched: a rotation dispatch was started for the value.
	RotationDispatched RotationDecision = "dispatched"
	// RotationAdopted: first observation without a baseline (ledger adoption,
	// or no live target to record): the value is the baseline, nothing rotated.
	RotationAdopted RotationDecision = "adopted"
	// RotationUnchanged: the value equals the last one this process handled, or
	// the ledger records it as already wanted/applied: nothing rotated.
	RotationUnchanged RotationDecision = "unchanged"
	// RotationDeferred: a change was seen but not dispatched now (cooldown, cache
	// limits); the next observation retries.
	RotationDeferred RotationDecision = "deferred"
	// RotationLedgerDegraded: first observation in this process while the ledger
	// is disabled or unusable: adopted in memory only, nothing rotated.
	RotationLedgerDegraded RotationDecision = "ledger_degraded"
)

// emitEvent records a rotation outcome onto observability.EventStream,
// unconditionally -- this is what makes rotation outcomes visible via
// /api/audit and /api/events, which previously only ever saw SIGHUP signal
// outcomes (see internal/injector/docker.go's LogInjectionEvent). Metadata
// only: secret/provider/container identifiers, status, and a redacted
// error string -- never a secret value. containerIDs is the comma-joined
// form StateTracker uses.
//
// Phase 4.1: this no longer calls t.Dispatcher.Dispatch directly.
// internal/alert.Evaluator now owns the sole decision of whether a
// rotation_failed/rotation_succeeded notification is actually sent
// (deduplicated and cooldown-gated) -- it subscribes to the same
// EventStream this function pushes to, from internal/server.
// StartRESTServer's single consumer loop. t.Dispatcher is still wired at
// startup (internal/cli/agent.go) and handed to that same Evaluator; this
// function keeps the field only because internal/agent/recovery.go's
// crash-recovery events (RecoverySucceeded/RecoveryFailed -- a distinct,
// narrower signal than rotation_failed, out of Phase 4.1's alert scope)
// still dispatch directly.
func (t *TriggerEngine) emitEvent(eventType notify.EventType, providerName, secretName, containerIDs string, duration time.Duration, err error) {
	var containers []string
	if containerIDs != "" {
		containers = strings.Split(containerIDs, ",")
	}
	ev := notify.NewEvent(eventType, providerName, secretName, containers, duration, err)

	status := "success"
	if eventType == notify.RotationFailed || eventType == notify.RecoveryFailed {
		status = "failure"
	}

	// Phase 3 Analytics Overview counters -- process-lifetime-only, reset
	// on restart (see pkg/observability/counters.go). Only rotation
	// outcomes are counted here; recovery outcomes (RecoverySucceeded/
	// RecoveryFailed) are a distinct signal and deliberately left uncounted
	// for this MVP rather than conflated with rotation counts.
	switch eventType {
	case notify.RotationSucceeded:
		observability.RotationSuccessTotal.Add(1)
	case notify.RotationFailed:
		observability.RotationFailureTotal.Add(1)
	}

	streamEvent := map[string]interface{}{
		"timestamp":  ev.Timestamp.Format(time.RFC3339),
		"secret":     ev.SecretName,
		"container":  strings.Join(ev.Containers, ","),
		"event_type": string(ev.Type),
		"status":     status,
	}
	if ev.Provider != "" {
		streamEvent["provider"] = ev.Provider
	}
	if ev.ErrorMessage != "" {
		streamEvent["error"] = ev.ErrorMessage
	}
	select {
	case observability.EventStream <- streamEvent:
	default:
	}
}

// Production state and lock directories. Pinned by TestTriggerEngine_DefaultDirsPinned.
const (
	defaultTriggerStateDir = "/var/lib/dso/state"
	defaultTriggerLockDir  = "/var/lib/dso/locks"
)

func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli *client.Client) (*TriggerEngine, error) {
	stateDir, lockDir := defaultTriggerStateDir, defaultTriggerLockDir
	// Test-only (-tags dsofault): per-test directories for the real-process crash
	// harness. In a normal build Param never overrides anything.
	if v, ok := faultinject.Param("state_dir"); ok {
		stateDir = v
	}
	if v, ok := faultinject.Param("lock_dir"); ok {
		lockDir = v
	}
	ledgerDir := defaultLedgerDir
	if v, ok := faultinject.Param("ledger_dir"); ok {
		ledgerDir = v
	}
	t, err := NewTriggerEngineWithDirs(cache, storeManager, rw, logger, cfg, dockerCli, stateDir, lockDir)
	if err != nil {
		return nil, err
	}
	// Design 5 step 1: open the applied-version ledger. Any failure disables it
	// for this process (operator-visible warning); it is never fatal (X6).
	t.openLedger(ledgerDir)
	return t, nil
}

// NewTriggerEngineWithDirs is NewTriggerEngine with explicit state and lock
// directories. Production uses the /var/lib/dso defaults through
// NewTriggerEngine; out-of-package integration tests (which cannot reach the
// engine's unexported context) pass per-test temporary directories.
func NewTriggerEngineWithDirs(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli *client.Client, stateDir, lockDir string) (*TriggerEngine, error) {
	if rw != nil {
		rw.Cache = cache
		rw.Config = cfg
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize state tracker and lock manager for crash recovery and synchronization
	// Note: StateTracker is best-effort (optional), but LockManager is CRITICAL for rotation safety
	stateTracker, err := NewStateTracker(stateDir, logger)
	if err != nil {
		logger.Warn("Failed to initialize state tracker - rotation recovery disabled",
			zap.Error(err))
		// Continue without state tracker - less safe but functional
		stateTracker = nil
	}

	// CRITICAL: Lock manager must be initialized. Rotation safety depends on it.
	// Fail fast if lock manager initialization fails to prevent data corruption.
	lockManager, err := rotation.NewLockManager(lockDir, logger)
	if err != nil {
		logger.Error("CRITICAL: Failed to initialize rotation lock manager - refusing to start",
			zap.Error(err))
		logger.Error("Lock manager is REQUIRED for rotation safety. Cannot proceed without it.",
			zap.String("path", lockDir))
		cancel()
		return nil, fmt.Errorf("rotation lock manager initialization failed: %w", err)
	}

	timeoutController := NewTimeoutController(logger)

	// The rotation transition journal is the StateTracker. Without it a crash in
	// the middle of a rolling rotation cannot be reconciled safely, so rolling
	// rotation is refused (RequireJournal) rather than run unjournalled.
	if rw != nil {
		rw.RequireJournal = true
		if stateTracker != nil {
			rw.Journal = stateTracker
		}
	}

	guard := newJournalGuard(stateTracker)
	if rw != nil {
		rw.Guard = guard
	}

	return &TriggerEngine{
		guard:             guard,
		Cache:             cache,
		Store:             storeManager,
		Reloader:          rw,
		Logger:            logger,
		Config:            cfg,
		ctx:               ctx,
		cancel:            cancel,
		StateTracker:      stateTracker,
		LockManager:       lockManager,
		TimeoutController: timeoutController,
		DockerClient:      dockerCli,
	}, nil
}

// performAutomaticRecovery automatically recovers from agent crashes by cleaning up
// orphaned containers and restoring the original containers to active state.
//
// This is CRITICAL for preventing orphaned containers and ensuring deterministic
// recovery from agent failures without operator intervention.
func (t *TriggerEngine) performAutomaticRecovery() {
	// Held proxy candidates are re-evaluated whatever the outcome, and rotation
	// dispatch is released (the guard still refuses any service that recovery left
	// unresolved).
	defer t.settleStartup()

	if t.DockerClient == nil {
		t.Logger.Warn("Docker client not available, skipping automatic recovery")
		return
	}

	// Create a recovery handler
	ar := NewAutomaticRecovery(t.DockerClient, t.Logger, t.StateTracker)
	ar.Dispatcher = t.Dispatcher
	ar.Routing = t.Routing
	if t.Config != nil {
		ar.OwnerID = t.Config.Agent.OwnerID
	}

	// Validate state file integrity and cleanup stale state
	ar.ValidateStateOnStartup(t.ctx)

	// Perform automatic recovery from any pending rotations
	if err := ar.RecoverFromCrash(t.ctx); err != nil {
		t.Logger.Error("Automatic recovery encountered errors",
			zap.Error(err))
		// Don't fail startup; recovery is best-effort
	}

	// A restart must not make an uncertain or manual-recovery rotation look healthy:
	// re-flag the affected services from the durable journal.
	if t.Reloader != nil && t.StateTracker != nil {
		for _, rec := range t.StateTracker.snapshotRecords() {
			if rec.Status == "critical_error" && rec.ServiceID != "" {
				t.Reloader.MarkDegradedFromJournal(rec.ServiceID, "interrupted rotation ("+rec.Strategy+", "+rec.Transition+"): "+rec.ErrorMessage)
			}
		}
	}

	// Perform a broad cleanup of any orphaned DSO containers
	// This catches containers that might not be tracked in state
	if err := ar.CleanupOrphanedContainers(t.ctx); err != nil {
		t.Logger.Warn("Failed to cleanup orphaned containers",
			zap.Error(err))
		// Don't fail startup; cleanup is best-effort
	}

	// Design 5 step 5: converge the applied-version ledger with the journal M0
	// recovery has just settled. It is the LAST step before the deferred
	// settleStartup (the guard still refuses every dispatch) and before
	// CleanupOldStates prunes journal records (StartAll). Read-only with respect
	// to Docker and the journal; it cannot unblock anything.
	t.reconcileLedgerAtStartup()
}

// BeginStartup arms startup gating: until StartAll has finished automatic recovery,
// NO dispatch path (first poll, scheduled poll, webhook, IPC reconcile, REST manual
// rotate, watcher) may mutate a workload. The agent calls it right after
// constructing the engine, before any server that can trigger a rotation starts.
// Without it (unit tests that never call StartAll) nothing is gated by startup.
func (t *TriggerEngine) BeginStartup() {
	if t.guard != nil {
		t.guard.armed.Store(true)
	}
}

// settleStartup marks startup recovery as finished and admits settled proxy
// candidates.
func (t *TriggerEngine) settleStartup() {
	t.settleProxy()
	if t.guard != nil {
		t.guard.settled.Store(true)
	}
}

// OPS-M3: the former recoverPendingRotations() method was removed. It only logged
// "MANUAL INTERVENTION REQUIRED" and marked states rollback_required without ever
// executing a rollback, which contradicted the project's automatic-recovery claim.
// The real, executed recovery path is performAutomaticRecovery() ->
// AutomaticRecovery.RecoverFromCrash(), which cleans up orphaned shadow/backup
// containers and restores the original container (see internal/agent/recovery.go).
// That method was the only caller-less duplicate; runtime behavior and the
// documented automatic-recovery contract now match.

// SetLimitCache wires a LimitEnforcingCache into the engine. When set, all
// cache writes in ExecuteRotation go through limit enforcement instead of
// calling SecretCache.Set directly. Call this before StartAll.
func (t *TriggerEngine) SetLimitCache(lc *LimitEnforcingCache) {
	t.LimitCache = lc
}

func (t *TriggerEngine) Stop() {
	t.cancel()
	if t.StateTracker != nil {
		_ = t.StateTracker.Close()
	}
	t.closeLedger()
	t.Logger.Info("Trigger engine stopped")
}

func (t *TriggerEngine) StartAll() error {
	// CRITICAL: Perform automatic recovery from any crashed rotations before starting normal operations
	// This ensures orphaned containers are cleaned up and state is consistent.
	t.performAutomaticRecovery()

	// Clean up old completed/recovered rotations from state file
	if t.StateTracker != nil {
		if err := t.StateTracker.CleanupOldStates(168 * time.Hour); err != nil { // 7 days retention
			t.Logger.Warn("Failed to cleanup old rotation states",
				zap.Error(err))
		}
	}

	for pName, pCfg := range t.Config.Providers {
		t.Logger.Info("Starting trigger engine for provider", zap.String("provider", pName))
		for _, sec := range t.Config.Secrets {
			// If a specific provider is requested for this secret, match it.
			// Otherwise, if only one provider exists, use it.
			if sec.Provider != "" && sec.Provider != pName {
				continue
			}

			// Core polling or event setup based on watch mode
			interval := 5 * time.Minute
			if t.Config.Agent.Watch.PollingInterval != "" {
				if d, err := time.ParseDuration(t.Config.Agent.Watch.PollingInterval); err == nil {
					interval = d
				}
			}

			if sec.Rotation.Enabled {
				if err := t.StartPolling(pName, pCfg, sec, interval); err != nil {
					t.Logger.Error("Failed to start polling", zap.String("provider", pName), zap.String("secret", sec.Name), zap.Error(err))
				}
			}
		}
	}
	return nil
}

// Defaults for the per-secret rotation guards. Overridable per engine (via
// the unexported fields below) only so tests do not have to wait them out.
const (
	defaultSecretLockTimeout = 5 * time.Second
	defaultRotationCooldown  = 30 * time.Second
)

func (t *TriggerEngine) secretLockWait() time.Duration {
	if t.secretLockTimeout > 0 {
		return t.secretLockTimeout
	}
	return defaultSecretLockTimeout
}

func (t *TriggerEngine) cooldown() time.Duration {
	if t.rotationCooldown > 0 {
		return t.rotationCooldown
	}
	return defaultRotationCooldown
}

// keyMutex returns the mutex serialising observe-check-store for one
// provider:secret key (PE-31).
func (t *TriggerEngine) keyMutex(cacheKey string) *sync.Mutex {
	m, _ := t.observeLocks.LoadOrStore(cacheKey, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// rollbackHash forgets newHash as the "last seen" value for cacheKey so the
// next poll (or webhook / IPC reconcile) re-dispatches it. It only removes
// the entry if it still holds newHash: a newer value observed concurrently by
// another caller is never discarded by an older attempt's rollback (PE-31).
func (t *TriggerEngine) rollbackHash(cacheKey, newHash string) {
	t.secretHashes.CompareAndDelete(cacheKey, newHash)
}

// commitObservation is the atomic observe-check-store step of
// ExecuteRotation. Under the per-key mutex it compares the new value against
// the last dispatched one, applies the cooldown, records the new hash and
// writes the cache, so poll, webhook and IPC reconcile callers can neither
// dispatch the same value twice nor leave the stored hash describing a
// different value than the cache (PE-31). It returns the decision and, for a
// dispatch with a usable ledger, the ledger plan the dispatch correlates.
//
// S6 (design 3 and 8.2): when the value differs from the last one handled,
// the ledger observes it under this same mutex, BEFORE the cooldown check, so
// the ledger's desired version sees values in the same order as secretHashes.
// The ledger then decides whether the value must be delivered (NeedsRotation):
// a first observation without a baseline is adopted, never rotated (O-5). When
// the ledger is configured but unusable, a key's first observation in this
// process is adopted in memory and later changes dispatch as before (the
// ledger is advisory, O-3). secretHashes stays the in-process dedupe layer.
func (t *TriggerEngine) commitObservation(cacheKey, providerName, secretName, newHash string, secretData map[string]string, sec config.SecretMapping) (RotationDecision, *ledgerPlan) {
	mu := t.keyMutex(cacheKey)
	mu.Lock()
	defer mu.Unlock()

	// 1. Smart Optimization: Avoid unnecessary restarts
	var oldHash string
	if val, ok := t.secretHashes.Load(cacheKey); ok {
		oldHash = val.(string)
	}

	if oldHash == newHash {
		msg := fmt.Sprintf("No change detected for %s, skipping swap", secretName)
		t.Logger.Debug(msg)
		if t.Server != nil {
			t.Server.Emit(msg)
		}
		return RotationUnchanged, nil
	}

	// 2. Ledger observation and decision (S6). Not configured: legacy dispatch.
	var plan *ledgerPlan
	if t.ledger != nil {
		_, seen := t.baselined.Load(cacheKey)
		lv := t.ledgerObserve(providerName, secretName, secretData, sec)
		hold := RotationDecision("")
		switch {
		case lv.usable && lv.rotate:
			plan = lv.plan
		case lv.usable && lv.adopted:
			hold = RotationAdopted
		case lv.usable:
			hold = RotationUnchanged
		case !seen && lv.noTargets:
			hold = RotationAdopted
		case !seen:
			hold = RotationLedgerDegraded
		}
		if hold != "" {
			// Not a change to deliver: keep the value as the in-process baseline
			// and write the cache exactly as a dispatch would (PE-03: the compose
			// restart path pre-injects from the cache).
			t.secretHashes.Store(cacheKey, newHash)
			t.baselined.Store(cacheKey, true)
			if !t.writeCache(cacheKey, secretName, newHash, secretData) {
				return RotationDeferred, nil
			}
			t.Logger.Info("Secret observed; no rotation dispatched",
				zap.String("provider", providerName), zap.String("secret", secretName), zap.String("decision", string(hold)))
			return hold, nil
		}
	}

	// 3. Add cooldown window. Returns BEFORE storing the hash, so the next
	// poll retries.
	if lastRot, ok := t.lastRotations.Load(cacheKey); ok {
		if time.Since(lastRot.(time.Time)) < t.cooldown() {
			t.Logger.Debug("Cooldown active, skipping rotation", zap.String("secret", secretName))
			return RotationDeferred, nil
		}
	}

	t.secretHashes.Store(cacheKey, newHash)
	t.lastRotations.Store(cacheKey, time.Now())
	t.baselined.Store(cacheKey, true)

	if !t.writeCache(cacheKey, secretName, newHash, secretData) {
		return RotationDeferred, nil
	}
	return RotationDispatched, plan
}

// writeCache stores the observed value in the cache. Writes go through
// LimitEnforcingCache when available so that per-secret size checks and total
// capacity limits are enforced; direct SecretCache.Set is used only when no
// limiter is configured. On a limit refusal the hash is rolled back so the
// next poll retries instead of skipping, and false is returned.
func (t *TriggerEngine) writeCache(cacheKey, secretName, newHash string, secretData map[string]string) bool {
	if t.LimitCache != nil {
		if err := t.LimitCache.SetWithLimits(cacheKey, secretData); err != nil {
			t.Logger.Error("Secret exceeds cache limits, rotation aborted",
				zap.String("secret", secretName),
				zap.Error(err))
			t.rollbackHash(cacheKey, newHash)
			return false
		}
		return true
	}
	t.Cache.Set(cacheKey, secretData)
	return true
}

// triggerReload is Reloader.TriggerReload, or the test seam.
func (t *TriggerEngine) triggerReload(ctx context.Context, secretName string, onComplete func(error)) error {
	if t.reloadFn != nil {
		return t.reloadFn(ctx, secretName, onComplete)
	}
	return t.Reloader.TriggerReload(ctx, secretName, onComplete)
}

// ExecuteRotation observes one provider value and dispatches a rotation when
// it must be delivered. It is ExecuteRotationDecision without the decision,
// kept so existing callers are unchanged.
func (t *TriggerEngine) ExecuteRotation(providerName, secretName string, secretData map[string]string, sec config.SecretMapping) {
	_ = t.ExecuteRotationDecision(providerName, secretName, secretData, sec)
}

// ExecuteRotationDecision is ExecuteRotation returning what it decided (design
// 8.3, BQ-3 option b). RotationDispatched means a dispatch was started (its
// outcome is asynchronous); every other decision means nothing was rotated.
func (t *TriggerEngine) ExecuteRotationDecision(providerName, secretName string, secretData map[string]string, sec config.SecretMapping) RotationDecision {
	cacheKey := fmt.Sprintf("%s:%s", providerName, secretName)
	newHash := ComputeHash(secretData)

	decision, plan := t.commitObservation(cacheKey, providerName, secretName, newHash, secretData, sec)
	if decision != RotationDispatched {
		return decision
	}

	t.Logger.Debug("Diagnostic: Rotation triggered for secret label", zap.String("sec_name", secretName))
	msg := fmt.Sprintf("Secret rotated: %s", secretName)
	t.Logger.Info(msg, zap.String("provider", providerName))
	if t.Server != nil {
		t.Server.Emit(msg)
	}

	// 3. Unified Rotation System (restart | signal | none) with crash recovery and distributed locking
	go func() {
		// Use the agent's long-lived context so rotation goroutines spawned inside
		// TriggerReload are never cancelled prematurely. TriggerReload spawns goroutines
		// and returns immediately; a short-lived derived context would be cancelled
		// the instant TriggerReload returns, killing every in-flight Docker API call.
		// Each rotation strategy (rolling/restart) already enforces its own health-check
		// deadline internally, so no external timeout is needed here.
		ctx := t.ctx

		rotationMode := sec.Rotation.Strategy
		if rotationMode == "" {
			rotationMode = t.Config.Defaults.Rotation.Strategy
		}
		if rotationMode == "" {
			rotationMode = "restart" // Default
		}

		if rotationMode == "none" {
			t.Logger.Debug("Rotation strategy 'none', skipping reload", zap.String("secret", secretName))
			return
		}

		if t.Server != nil {
			t.Server.Emit(fmt.Sprintf("Triggering %s for containers linked to %s", rotationMode, secretName))
		}

		// Use distributed lock to prevent concurrent rotations of same secret
		if t.LockManager != nil {
			if err := t.LockManager.AcquireLock(secretName, t.secretLockWait()); err != nil {
				// PE-01 (D1): nothing was applied. Forget the hash so the next
				// poll retries this value instead of treating it as delivered.
				t.Logger.Warn("Failed to acquire rotation lock; rotation not applied, will retry on next poll",
					zap.String("secret", secretName), zap.Error(err))
				t.rollbackHash(cacheKey, newHash)
				if t.Server != nil {
					t.Server.Emit(fmt.Sprintf("Rotation of %s deferred (secret busy); will retry", secretName))
				}
				return
			}
			defer t.LockManager.ReleaseLock(secretName)
		}

		// Collect the IDs of containers that will be rotated for this secret (H3).
		// The Reloader's Targets map contains one entry per managed container.
		// We join multiple IDs with a comma so a single StateTracker entry covers all
		// containers rotating the same secret simultaneously.
		originalContainerIDs := t.collectContainerIDsForSecret(secretName)

		// One identity for this rotation: the state record created below and the
		// transition journal written by the strategies are the same record.
		rotationID := rotation.NewRotationID()
		ctx = rotation.WithRotationID(ctx, rotationID)

		// Record rotation start for crash recovery, with the target version
		// (O-2) when the ledger supplied one.
		if t.StateTracker != nil {
			if err := t.StateTracker.StartRotationWithVersion(rotationID, providerName, secretName, originalContainerIDs, "", plan.journalVersion()); err != nil {
				t.Logger.Warn("Failed to record rotation state", zap.Error(err))
				if t.ledger != nil {
					// W14 / O-7: with the ledger wired, a dispatch the journal did
					// not record durably is not run (no journal intent, no ledger
					// citation). The value is retried on the next poll. The
					// non-durable record is dropped from memory (F-B3).
					t.rollbackHash(cacheKey, newHash)
					_ = t.StateTracker.DeleteStateID(rotationID)
					t.emitEvent(notify.RotationFailed, providerName, secretName, originalContainerIDs, 0, fmt.Errorf("rotation not started: the journal could not record it: %w", err))
					return
				}
			} else {
				// Design 3 step 4: delivery requested, after the journal start and
				// before TriggerReload. A failure is logged and ignored.
				t.ledgerCorrelate(plan, rotationID)
			}
		}

		// Wall-clock start for the notification event's duration field.
		// Measured here (not at poll time): this is where the reload
		// actually begins.
		rotationStart := time.Now()
		outcome := rotationOutcome{
			providerName: providerName,
			secretName:   secretName,
			cacheKey:     cacheKey,
			newHash:      newHash,
			containerIDs: originalContainerIDs,
			start:        rotationStart,
			rotationID:   rotationID,
			plan:         plan,
		}

		// Note: The Reloader internally handles the strategy logic (restart/signal).
		// Some strategies rotate containers in detached goroutines that outlive
		// TriggerReload's return, so rotation state is only marked complete/failed
		// once onComplete fires with the real, final outcome (REL-1 fix) rather
		// than immediately after TriggerReload returns.
		onComplete := func(asyncErr error) {
			t.finishRotation(outcome, asyncErr)
		}

		if err := t.triggerReload(ctx, secretName, onComplete); err != nil {
			t.Logger.Warn("Reload trigger failed", zap.String("secret", secretName), zap.Error(err))
			// Remove the cached hash so the next poll retries the rotation instead
			// of treating this failed attempt as a successful "no change" baseline.
			t.rollbackHash(cacheKey, newHash)
			if t.StateTracker != nil {
				_ = t.StateTracker.MarkRollbackID(rotationID)
			}
			t.ledgerTerminal(outcome.plan, rotationID)
			t.emitEvent(notify.RotationFailed, providerName, secretName, originalContainerIDs, time.Since(rotationStart), err)
			return
		}
	}()
	return RotationDispatched
}

// rotationOutcome carries what finishRotation needs about one dispatch.
type rotationOutcome struct {
	providerName string
	secretName   string
	cacheKey     string
	newHash      string
	containerIDs string
	start        time.Time
	rotationID   string
	// plan is the ledger plan of the dispatch (nil without a usable ledger).
	plan *ledgerPlan
}

// finishRotation records the final outcome of one TriggerReload dispatch.
//
// PE-01: success is emitted only when the reloader reports that targets were
// actually rotated without error. A deferred outcome (a target's service was
// busy) is "not applied": the hash is rolled back so the value is retried,
// and no success event is emitted. A reload that rotated nothing by
// configuration is likewise never reported as a success.
func (t *TriggerEngine) finishRotation(o rotationOutcome, asyncErr error) {
	switch {
	case errors.Is(asyncErr, watcher.ErrRotationDeferred):
		msg := "Rotation not applied to every target (service busy); will retry on next poll"
		if errors.Is(asyncErr, watcher.ErrRotationBlocked) {
			msg = "Rotation not applied: a target has an unresolved operation (or startup recovery has not settled); it is retried on the next poll once reconciled"
		}
		t.Logger.Warn(msg,
			zap.String("secret", o.secretName), zap.Error(asyncErr))
		t.rollbackHash(o.cacheKey, o.newHash)
		if t.StateTracker != nil {
			// Nothing to recover: the busy targets were never touched and any
			// others finished their own rotation. Leaving "in_progress" would
			// make crash recovery treat this as an interrupted rotation.
			_ = t.StateTracker.DeleteStateID(o.rotationID)
		}
		t.ledgerTerminal(o.plan, o.rotationID)
		if t.Server != nil {
			t.Server.Emit(fmt.Sprintf("Rotation of %s deferred: %v", o.secretName, asyncErr))
		}
		return
	case errors.Is(asyncErr, watcher.ErrNoTargetsRotated):
		// Retrying cannot change a configuration-driven no-op, so the hash
		// is kept; but it is not a success either.
		t.Logger.Warn("No matched container was rotated (no rotating strategy); not reporting success",
			zap.String("secret", o.secretName))
		if t.StateTracker != nil {
			_ = t.StateTracker.DeleteStateID(o.rotationID)
		}
		t.ledgerTerminal(o.plan, o.rotationID)
		return
	case asyncErr != nil:
		t.Logger.Warn("Rotation failed after async completion", zap.String("secret", o.secretName), zap.Error(asyncErr))
		t.rollbackHash(o.cacheKey, o.newHash)
		if t.StateTracker != nil {
			_ = t.StateTracker.MarkRollbackID(o.rotationID)
		}
		t.ledgerTerminal(o.plan, o.rotationID)
		// Notification is emitted AFTER the authoritative state
		// transition above, and via a non-blocking dispatch --
		// delivery outcome can never feed back into rotation state.
		t.emitEvent(notify.RotationFailed, o.providerName, o.secretName, o.containerIDs, time.Since(o.start), asyncErr)
		return
	}
	if t.StateTracker != nil {
		if err := t.StateTracker.CompleteRotationID(o.rotationID); err != nil {
			t.Logger.Warn("Failed to complete rotation state", zap.Error(err))
		}
	}
	// Design 3 step 5: the ledger records the outcome the journal now states.
	t.ledgerTerminal(o.plan, o.rotationID)
	t.emitEvent(notify.RotationSucceeded, o.providerName, o.secretName, o.containerIDs, time.Since(o.start), nil)
}

func (t *TriggerEngine) StartPolling(providerName string, pCfg config.ProviderConfig, sec config.SecretMapping, baseInterval time.Duration) error {
	t.Logger.Info("Initializing secret polling", zap.String("secret", sec.Name), zap.String("provider", providerName), zap.Duration("interval", baseInterval))

	go func() {
		cacheKey := fmt.Sprintf("%s:%s", providerName, sec.Name)

		if _, exists := t.rotations.LoadOrStore(cacheKey, true); exists {
			return
		}
		defer t.rotations.Delete(cacheKey)

		baseBackoff := 5 * time.Second
		if pCfg.Retry.Backoff != "" {
			if d, err := time.ParseDuration(pCfg.Retry.Backoff); err == nil {
				baseBackoff = d
			}
		}

		currentInterval := baseInterval
		maxInterval := baseInterval * 4

		// Fire immediately so the initial hash is established on startup without
		// waiting a full polling interval. This also catches rotations that
		// happened while the agent was down.
		timer := time.NewTimer(0)
		defer timer.Stop()

		for {
			select {
			case <-t.ctx.Done():
				t.Logger.Debug("Polling stopped for secret", zap.String("secret", sec.Name))
				return
			case <-timer.C:
			}

			prov, err := t.Store.GetProvider(providerName, pCfg)
			if err != nil {
				t.Logger.Error("Provider unavailable, retrying", zap.String("provider", providerName), zap.Error(err))
				timer.Reset(baseBackoff)
				continue
			}

			// Use context-aware GetSecret when the provider supports it so that
			// agent shutdown cancels in-flight AWS calls immediately.
			var data map[string]string
			if provCtx, ok := prov.(api.SecretProviderWithContext); ok {
				data, err = provCtx.GetSecretWithContext(t.ctx, sec.Name)
			} else {
				data, err = prov.GetSecret(sec.Name)
			}

			if err != nil {
				// A breaker short-circuit is not evidence the plugin is bad:
				// counting it would hit the store's failure threshold, kill and
				// respawn the plugin, and reset the breaker mid-outage (G013).
				if !errors.Is(err, pkgprovider.ErrCircuitOpen) {
					t.Store.MarkProviderFailure(providerName)
				}
				observability.SecretRequestsTotal.WithLabelValues(providerName, "error").Inc()
				t.Logger.Error("Secret fetch failed, will retry", zap.String("secret", sec.Name), zap.Error(err))
				timer.Reset(baseBackoff)
				continue
			}

			t.Store.MarkProviderHealthy(providerName)
			observability.SecretRequestsTotal.WithLabelValues(providerName, "success").Inc()

			// Adaptive back-off: if data unchanged, slowly increase poll interval
			// (up to 4x base). Reset to base the moment a change is detected.
			// A rolled-back (deferred or failed) rotation leaves no stored
			// hash; keep the base interval so it is retried promptly (PE-01).
			oldData, exists := t.Cache.Get(cacheKey)
			_, dispatched := t.secretHashes.Load(cacheKey)
			if !exists || !dispatched || ComputeHash(oldData) != ComputeHash(data) {
				currentInterval = baseInterval
			} else if currentInterval < maxInterval {
				currentInterval = time.Duration(float64(currentInterval) * 1.5)
			}

			t.Logger.Info("Secret polled from provider",
				zap.String("provider", providerName),
				zap.String("secret", sec.Name),
				zap.Duration("next_poll_in", currentInterval))

			t.ExecuteRotation(providerName, sec.Name, data, sec)

			timer.Reset(currentInterval)
		}
	}()

	return nil
}

func (t *TriggerEngine) HandleWebhook(providerName string, pCfg config.ProviderConfig, sec config.SecretMapping, timestamp string) error {
	cacheKey := fmt.Sprintf("%s:%s", providerName, sec.Name)

	idempKey := cacheKey + ":" + timestamp
	if _, loaded := t.events.LoadOrStore(idempKey, time.Now()); loaded {
		return nil
	}

	// CQ-H7: schedule idempotency-key cleanup with time.AfterFunc rather than a
	// goroutine that blocks on time.Sleep for 5 minutes. Under a webhook storm the
	// old approach accumulated thousands of sleeping goroutines that could not be
	// interrupted by shutdown. AfterFunc holds no blocked goroutine while waiting.
	time.AfterFunc(5*time.Minute, func() {
		t.events.Delete(idempKey)
	})

	prov, err := t.Store.GetProvider(providerName, pCfg)
	if err != nil {
		return err
	}

	// Use context-aware GetSecret if provider supports it; otherwise fall back
	// to the non-context version. Use the agent's root context (t.ctx) so that
	// in-flight webhook fetches are cancelled when the agent shuts down, instead
	// of blocking clean shutdown for up to the provider's own timeout.
	var val map[string]string
	if provCtx, ok := prov.(api.SecretProviderWithContext); ok {
		val, err = provCtx.GetSecretWithContext(t.ctx, sec.Name)
	} else {
		val, err = prov.GetSecret(sec.Name)
	}
	if err != nil {
		return err
	}

	t.ExecuteRotation(providerName, sec.Name, val, sec)
	return nil
}

// collectContainerIDsForSecret returns the IDs of all containers currently
// managed by the Reloader that use secretName. Multiple IDs are joined with a
// comma so a single StateTracker entry can represent a multi-container rotation
// (H3 fix: previously always passed "" to StartRotation/MarkRollback/CompleteRotation).
func (t *TriggerEngine) collectContainerIDsForSecret(secretName string) string {
	if t.Reloader == nil {
		return ""
	}
	var ids []string
	t.Reloader.Targets.Range(func(_, v interface{}) bool {
		tc := v.(*watcher.TargetContainer)
		for _, s := range tc.Secrets {
			if strings.TrimSpace(s) == secretName {
				ids = append(ids, tc.ID)
				break
			}
		}
		return true
	})
	return strings.Join(ids, ",")
}

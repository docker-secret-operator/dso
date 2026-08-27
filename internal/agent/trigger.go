package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
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
	Server            *AgentServer
	Config            *config.Config
	ctx               context.Context
	cancel            context.CancelFunc
	StateTracker      *StateTracker
	LockManager       *rotation.LockManager
	TimeoutController *TimeoutController
	DockerClient      *client.Client // For crash recovery

	// Dispatcher is the optional notification sink for rotation/recovery
	// outcome events (internal/notify). Nil when notifications are not
	// configured. Strictly an observer: emitted-to AFTER the authoritative
	// StateTracker transition, via a non-blocking Dispatch that cannot
	// fail, block, or otherwise influence rotation state.
	Dispatcher *notify.Dispatcher
}

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

func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli *client.Client) (*TriggerEngine, error) {
	if rw != nil {
		rw.Cache = cache
		rw.Config = cfg
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize state tracker and lock manager for crash recovery and synchronization
	// Note: StateTracker is best-effort (optional), but LockManager is CRITICAL for rotation safety
	stateTracker, err := NewStateTracker("/var/lib/dso/state", logger)
	if err != nil {
		logger.Warn("Failed to initialize state tracker - rotation recovery disabled",
			zap.Error(err))
		// Continue without state tracker - less safe but functional
		stateTracker = nil
	}

	// CRITICAL: Lock manager must be initialized. Rotation safety depends on it.
	// Fail fast if lock manager initialization fails to prevent data corruption.
	lockManager, err := rotation.NewLockManager("/var/lib/dso/locks", logger)
	if err != nil {
		logger.Error("CRITICAL: Failed to initialize rotation lock manager - refusing to start",
			zap.Error(err))
		logger.Error("Lock manager is REQUIRED for rotation safety. Cannot proceed without it.",
			zap.String("path", "/var/lib/dso/locks"))
		cancel()
		return nil, fmt.Errorf("rotation lock manager initialization failed: %w", err)
	}

	timeoutController := NewTimeoutController(logger)

	return &TriggerEngine{
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
	if t.DockerClient == nil {
		t.Logger.Warn("Docker client not available, skipping automatic recovery")
		return
	}

	// Create a recovery handler
	ar := NewAutomaticRecovery(t.DockerClient, t.Logger, t.StateTracker)
	ar.Dispatcher = t.Dispatcher

	// Validate state file integrity and cleanup stale state
	ar.ValidateStateOnStartup(t.ctx)

	// Perform automatic recovery from any pending rotations
	if err := ar.RecoverFromCrash(t.ctx); err != nil {
		t.Logger.Error("Automatic recovery encountered errors",
			zap.Error(err))
		// Don't fail startup; recovery is best-effort
	}

	// Perform a broad cleanup of any orphaned DSO containers
	// This catches containers that might not be tracked in state
	if err := ar.CleanupOrphanedContainers(t.ctx); err != nil {
		t.Logger.Warn("Failed to cleanup orphaned containers",
			zap.Error(err))
		// Don't fail startup; cleanup is best-effort
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

func (t *TriggerEngine) ExecuteRotation(providerName, secretName string, secretData map[string]string, sec config.SecretMapping) {
	cacheKey := fmt.Sprintf("%s:%s", providerName, secretName)
	newHash := ComputeHash(secretData)

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
		return
	}

	// 2. Add cooldown window (30s)
	if lastRot, ok := t.lastRotations.Load(cacheKey); ok {
		if time.Since(lastRot.(time.Time)) < 30*time.Second {
			t.Logger.Debug("Cooldown active, skipping rotation", zap.String("secret", secretName))
			return
		}
	}

	t.secretHashes.Store(cacheKey, newHash)
	t.lastRotations.Store(cacheKey, time.Now())

	// Route cache writes through LimitEnforcingCache when available so that
	// per-secret size checks and total capacity limits are enforced. Fall back
	// to direct SecretCache.Set only when no limiter is configured.
	if t.LimitCache != nil {
		if err := t.LimitCache.SetWithLimits(cacheKey, secretData); err != nil {
			t.Logger.Error("Secret exceeds cache limits, rotation aborted",
				zap.String("secret", secretName),
				zap.Error(err))
			// Roll back the hash so the next poll retries instead of skipping.
			t.secretHashes.Delete(cacheKey)
			return
		}
	} else {
		t.Cache.Set(cacheKey, secretData)
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
			if err := t.LockManager.AcquireLock(secretName, 5*time.Second); err != nil {
				t.Logger.Warn("Failed to acquire rotation lock", zap.String("secret", secretName), zap.Error(err))
				return
			}
			defer t.LockManager.ReleaseLock(secretName)
		}

		// Collect the IDs of containers that will be rotated for this secret (H3).
		// The Reloader's Targets map contains one entry per managed container.
		// We join multiple IDs with a comma so a single StateTracker entry covers all
		// containers rotating the same secret simultaneously.
		originalContainerIDs := t.collectContainerIDsForSecret(secretName)

		// Record rotation start for crash recovery
		if t.StateTracker != nil {
			if err := t.StateTracker.StartRotation(providerName, secretName, originalContainerIDs, ""); err != nil {
				t.Logger.Warn("Failed to record rotation state", zap.Error(err))
			}
		}

		// Wall-clock start for the notification event's duration field.
		// Measured here (not at poll time): this is where the reload
		// actually begins.
		rotationStart := time.Now()

		// Note: The Reloader internally handles the strategy logic (restart/signal).
		// Some strategies rotate containers in detached goroutines that outlive
		// TriggerReload's return, so rotation state is only marked complete/failed
		// once onComplete fires with the real, final outcome (REL-1 fix) rather
		// than immediately after TriggerReload returns.
		onComplete := func(asyncErr error) {
			if asyncErr != nil {
				t.Logger.Warn("Rotation failed after async completion", zap.String("secret", secretName), zap.Error(asyncErr))
				t.secretHashes.Delete(cacheKey)
				if t.StateTracker != nil {
					_ = t.StateTracker.MarkRollback(providerName, secretName, originalContainerIDs)
				}
				// Notification is emitted AFTER the authoritative state
				// transition above, and via a non-blocking dispatch --
				// delivery outcome can never feed back into rotation state.
				t.emitEvent(notify.RotationFailed, providerName, secretName, originalContainerIDs, time.Since(rotationStart), asyncErr)
				return
			}
			if t.StateTracker != nil {
				if err := t.StateTracker.CompleteRotation(providerName, secretName, originalContainerIDs); err != nil {
					t.Logger.Warn("Failed to complete rotation state", zap.Error(err))
				}
			}
			t.emitEvent(notify.RotationSucceeded, providerName, secretName, originalContainerIDs, time.Since(rotationStart), nil)
		}

		if err := t.Reloader.TriggerReload(ctx, secretName, onComplete); err != nil {
			t.Logger.Warn("Reload trigger failed", zap.String("secret", secretName), zap.Error(err))
			// Remove the cached hash so the next poll retries the rotation instead
			// of treating this failed attempt as a successful "no change" baseline.
			t.secretHashes.Delete(cacheKey)
			if t.StateTracker != nil {
				_ = t.StateTracker.MarkRollback(providerName, secretName, originalContainerIDs)
			}
			t.emitEvent(notify.RotationFailed, providerName, secretName, originalContainerIDs, time.Since(rotationStart), err)
			return
		}
	}()
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
				t.Store.MarkProviderFailure(providerName)
				observability.SecretRequestsTotal.WithLabelValues(providerName, "error").Inc()
				t.Logger.Error("Secret fetch failed, will retry", zap.String("secret", sec.Name), zap.Error(err))
				timer.Reset(baseBackoff)
				continue
			}

			t.Store.MarkProviderHealthy(providerName)
			observability.SecretRequestsTotal.WithLabelValues(providerName, "success").Inc()

			// Adaptive back-off: if data unchanged, slowly increase poll interval
			// (up to 4x base). Reset to base the moment a change is detected.
			oldData, exists := t.Cache.Get(cacheKey)
			if !exists || ComputeHash(oldData) != ComputeHash(data) {
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

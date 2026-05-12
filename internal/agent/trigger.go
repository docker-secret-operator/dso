package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

type TriggerEngine struct {
	Cache             *SecretCache
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
}

func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config) *TriggerEngine {
	if rw != nil {
		rw.Cache = cache
		rw.Config = cfg
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize state tracker and lock manager for crash recovery and synchronization
	stateTracker, err := NewStateTracker("/var/lib/dso/state", logger)
	if err != nil {
		logger.Warn("Failed to initialize state tracker - rotation recovery disabled",
			zap.Error(err))
		// Continue without state tracker - less safe but functional
		stateTracker = nil
	}

	lockManager, err := rotation.NewLockManager("/var/lib/dso/locks", logger)
	if err != nil {
		logger.Warn("Failed to initialize lock manager - concurrent rotation protection disabled",
			zap.Error(err))
		// Continue without lock manager - unsafe but functional
		lockManager = nil
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
	}
}

// recoverPendingRotations detects and recovers from crashed rotations
// This is CRITICAL for preventing orphaned containers after agent crashes
func (t *TriggerEngine) recoverPendingRotations() {
	if t.StateTracker == nil {
		return
	}

	pending := t.StateTracker.GetPendingRotations()
	if len(pending) == 0 {
		return
	}

	t.Logger.Warn("CRITICAL: Detected crashed rotations, attempting recovery",
		zap.Int("count", len(pending)))

	for _, rotation := range pending {
		t.Logger.Info("Recovering crashed rotation",
			zap.String("provider", rotation.ProviderName),
			zap.String("secret", rotation.SecretName),
			zap.String("status", rotation.Status),
			zap.Time("started", rotation.StartTime),
			zap.Duration("elapsed", time.Since(rotation.StartTime)))

		// For in_progress rotations older than 5 minutes, attempt rollback
		if rotation.Status == "in_progress" && time.Since(rotation.StartTime) > 5*time.Minute {
			t.Logger.Error("Rotation appears to have crashed, marking for manual review",
				zap.String("secret", rotation.SecretName),
				zap.String("original_container", rotation.OriginalContainerID),
				zap.String("new_container", rotation.NewContainerID))

			// Mark as requiring manual intervention
			if err := t.StateTracker.MarkRollback(rotation.ProviderName, rotation.SecretName, rotation.OriginalContainerID); err != nil {
				t.Logger.Error("Failed to mark rotation for rollback", zap.Error(err))
			}
		}

		// For rollback_required, log for operator intervention
		if rotation.Status == "rollback_required" {
			t.Logger.Error("MANUAL INTERVENTION REQUIRED",
				zap.String("secret", rotation.SecretName),
				zap.String("action", "verify container state and run dso-cli recover or manually cleanup"))
		}
	}
}

func (t *TriggerEngine) Stop() {
	t.cancel()
	if t.StateTracker != nil {
		t.StateTracker.Close()
	}
	t.Logger.Info("Trigger engine stopped")
}

func (t *TriggerEngine) StartAll() error {
	// CRITICAL: Recover from any crashed rotations before starting normal operations
	t.recoverPendingRotations()

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
	t.Cache.Set(cacheKey, secretData)

	t.Logger.Debug("Diagnostic: Rotation triggered for secret label", zap.String("sec_name", secretName))
	msg := fmt.Sprintf("Secret rotated: %s", secretName)
	t.Logger.Info(msg, zap.String("provider", providerName))
	if t.Server != nil {
		t.Server.Emit(msg)
	}

	// 3. Unified Rotation System (restart | signal | none) with crash recovery and distributed locking
	go func() {
		// Per-secret timeout isolation (prevents cascading timeouts)
		perSecretTimeout := 30 * time.Second
		ctx, cleanup := t.TimeoutController.CreateSecretContext(t.ctx, secretName, perSecretTimeout)
		defer cleanup()

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

		// Record rotation start for crash recovery
		if t.StateTracker != nil {
			if err := t.StateTracker.StartRotation(providerName, secretName, "", ""); err != nil {
				t.Logger.Warn("Failed to record rotation state", zap.Error(err))
			}
		}

		// Note: The Reloader internally handles the strategy logic (restart/signal)
		if err := t.Reloader.TriggerReload(ctx, secretName); err != nil {
			t.Logger.Warn("Reload trigger failed", zap.String("secret", secretName), zap.Error(err))
			if t.StateTracker != nil {
				t.StateTracker.MarkRollback(providerName, secretName, "")
			}
			return
		}

		// Mark rotation as complete
		if t.StateTracker != nil {
			if err := t.StateTracker.CompleteRotation(providerName, secretName, ""); err != nil {
				t.Logger.Warn("Failed to complete rotation state", zap.Error(err))
			}
		}
	}()
}

func (t *TriggerEngine) StartPolling(providerName string, pCfg config.ProviderConfig, sec config.SecretMapping, baseInterval time.Duration) error {
	t.Logger.Info("Initializing secret polling", zap.String("secret", sec.Name), zap.String("provider", providerName))

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

		for {
			select {
			case <-t.ctx.Done():
				t.Logger.Debug("Polling stopped for secret", zap.String("secret", sec.Name))
				return
			default:
			}

			prov, err := t.Store.GetProvider(providerName, pCfg)
			if err != nil {
				// Retry loop handled inside GetProvider usually, 
				// but here we wait between polling attempts if it fails.
				time.Sleep(baseBackoff) 
				continue
			}

			ch, err := prov.WatchSecret(sec.Name, currentInterval)
			if err != nil {
				// If watch fails, use the backoff
				select {
				case <-t.ctx.Done():
					return
				case <-time.After(baseBackoff):
				}
				continue
			}

			for update := range ch {
				select {
				case <-t.ctx.Done():
					return
				default:
				}

				if update.Error != "" {
					observability.SecretRequestsTotal.WithLabelValues(providerName, "error").Inc()
					t.Logger.Error("Secret update error", zap.Error(fmt.Errorf("%s", update.Error)))
					continue
				}

				oldData, exists := t.Cache.Get(cacheKey)
				if !exists || ComputeHash(oldData) != ComputeHash(update.Data) {
					currentInterval = baseInterval
				} else {
					if currentInterval < maxInterval {
						currentInterval = time.Duration(float64(currentInterval) * 1.5)
					}
				}

				t.ExecuteRotation(providerName, sec.Name, update.Data, sec)
			}
			
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(baseBackoff):
			}
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

	go func() {
		time.Sleep(5 * time.Minute)
		t.events.Delete(idempKey)
	}()

	prov, err := t.Store.GetProvider(providerName, pCfg)
	if err != nil {
		return err
	}

	// Use context-aware GetSecret if provider supports it; otherwise fall back to non-context version
	var val map[string]string
	if provCtx, ok := prov.(api.SecretProviderWithContext); ok {
		val, err = provCtx.GetSecretWithContext(context.Background(), sec.Name)
	} else {
		val, err = prov.GetSecret(sec.Name)
	}
	if err != nil {
		return err
	}

	t.ExecuteRotation(providerName, sec.Name, val, sec)
	return nil
}


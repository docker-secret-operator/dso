package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"go.uber.org/zap"
)

type TriggerEngine struct {
	Cache     *SecretCache
	Store     *providers.SecretStoreManager
	Reloader  *watcher.ReloaderController
	Logger    *zap.Logger
	rotations     sync.Map
	events        sync.Map
	secretHashes  sync.Map
	lastRotations sync.Map
	Server        *AgentServer
	Config        *config.Config
	ctx           context.Context
	cancel        context.CancelFunc
}

func NewTriggerEngine(cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config) *TriggerEngine {
	if rw != nil {
		rw.Cache = cache
		rw.Config = cfg
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &TriggerEngine{
		Cache:    cache,
		Store:    storeManager,
		Reloader: rw,
		Logger:   logger,
		Config:   cfg,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (t *TriggerEngine) Stop() {
	t.cancel()
	t.Logger.Info("Trigger engine stopped")
}

func (t *TriggerEngine) StartAll() error {
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

	// 3. Unified Rotation System (restart | signal | none)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		
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
		
		// Note: The Reloader internally handles the strategy logic (restart/signal)
		if err := t.Reloader.TriggerReload(ctx, secretName); err != nil {
			t.Logger.Warn("Reload trigger failed", zap.String("secret", secretName), zap.Error(err))
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


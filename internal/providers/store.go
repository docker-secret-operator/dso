package providers

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	pkgprovider "github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap"
)

type StoreEntry struct {
	Provider api.SecretProvider
	Client   *plugin.Client
}

type SecretStoreManager struct {
	logger *zap.Logger
	store  sync.Map
}

func NewSecretStoreManager(logger *zap.Logger) *SecretStoreManager {
	return &SecretStoreManager{
		logger: logger,
	}
}

// GetProvider retrieves an active connection or initializes a new one with backoff retry
func (s *SecretStoreManager) GetProvider(providerName string, pCfg config.ProviderConfig) (api.SecretProvider, error) {
	if val, ok := s.store.Load(providerName); ok {
		entry := val.(*StoreEntry)
		return entry.Provider, nil
	}

	providerLogger := s.logger.With(zap.String("provider", providerName), zap.String("type", pCfg.Type))
	providerLogger.Info("Initializing new persistent provider connection")

	var prov api.SecretProvider
	var client *plugin.Client
	var err error

	attempts := 3
	if pCfg.Retry.Attempts > 0 {
		attempts = pCfg.Retry.Attempts
	}

	baseBackoff := 2 * time.Second
	if pCfg.Retry.Backoff != "" {
		if d, err := time.ParseDuration(pCfg.Retry.Backoff); err == nil {
			baseBackoff = d
		}
	}

	success := false
	for attempt := 1; attempt <= attempts; attempt++ {
		// Merge specific config with region/auth for the loader
		loaderConfig := make(map[string]string)
		for k, v := range pCfg.Config {
			loaderConfig[k] = v
		}
		if pCfg.Region != "" {
			loaderConfig["region"] = pCfg.Region
		}
		if pCfg.Auth.Method != "" {
			loaderConfig["auth_method"] = pCfg.Auth.Method
			for mk, mv := range pCfg.Auth.Params {
				loaderConfig["auth_param_"+mk] = mv
			}
		}

		prov, client, err = pkgprovider.LoadProvider(pCfg.Type, loaderConfig)
		if err == nil {
			success = true
			break
		}

		if attempt < attempts {
			// Exponential backoff: delay = base * 2^(attempt-1)
			delay := baseBackoff * time.Duration(1<<(attempt-1))
			// Add jitter: + random(0-100ms)
			jitter := time.Duration(rand.Intn(100)) * time.Millisecond
			totalDelay := delay + jitter

			providerLogger.Warn("Failed to initialize provider, applying backoff",
				zap.Error(err),
				zap.Int("attempt", attempt),
				zap.Duration("retry_in", totalDelay))
			
			time.Sleep(totalDelay)
		}
	}

	if !success {
		return nil, fmt.Errorf("provider %s initialization exhausted %d retries: %w", providerName, attempts, err)
	}

	s.store.Store(providerName, &StoreEntry{
		Provider: prov,
		Client:   client,
	})

	return prov, nil
}

// Shutdown cleanly kills all active provider plugin children
func (s *SecretStoreManager) Shutdown() {
	s.store.Range(func(key, value interface{}) bool {
		entry := value.(*StoreEntry)
		entry.Client.Kill()
		return true
	})
}

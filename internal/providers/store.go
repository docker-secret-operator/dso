package providers

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
	pkgprovider "github.com/docker-secret-operator/dso/pkg/provider"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap"
)

type StoreEntry struct {
	mu          sync.Mutex
	Provider    api.SecretProvider
	Client      *plugin.Client
	LastHealthy time.Time
	ConsecFails int
	MaxFailures int
}

type SecretStoreManager struct {
	logger *zap.Logger
	store  sync.Map
	mu     sync.RWMutex
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
		// Check if connection is stale (no successful use in 10 minutes)
		if time.Since(entry.LastHealthy) > 10*time.Minute {
			s.logger.Warn("Provider connection may be stale, reconnecting",
				zap.String("provider", providerName),
				zap.Duration("lastHealthy", time.Since(entry.LastHealthy)))
			entry.Client.Kill()
			s.store.Delete(providerName)
			// Fall through to reinitialize
		} else if entry.Client.Exited() {
			// Plugin process has crashed; detect and recover automatically
			s.logger.Warn("Provider plugin process crashed, recovering",
				zap.String("provider", providerName))
			entry.Client.Kill()
			s.store.Delete(providerName)
			// Fall through to reinitialize
		} else {
			return entry.Provider, nil
		}
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
			jitter := time.Duration(secureJitterMillis()) * time.Millisecond // #nosec G115 -- secureJitterMillis returns 0-99.
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
		Provider:    prov,
		Client:      client,
		LastHealthy: time.Now(),
		ConsecFails: 0,
		MaxFailures: 5,
	})

	providerLogger.Info("Provider connection established successfully")
	return prov, nil
}

// MarkProviderHealthy updates the last successful use timestamp
func (s *SecretStoreManager) MarkProviderHealthy(providerName string) {
	if val, ok := s.store.Load(providerName); ok {
		entry := val.(*StoreEntry)
		entry.mu.Lock()
		entry.LastHealthy = time.Now()
		entry.ConsecFails = 0
		entry.mu.Unlock()
	}
}

// MarkProviderFailure tracks consecutive failures and removes provider if threshold exceeded
func (s *SecretStoreManager) MarkProviderFailure(providerName string) {
	if val, ok := s.store.Load(providerName); ok {
		entry := val.(*StoreEntry)
		entry.mu.Lock()
		entry.ConsecFails++
		exceeded := entry.ConsecFails >= entry.MaxFailures
		failures := entry.ConsecFails
		entry.mu.Unlock()

		if exceeded {
			s.logger.Error("Provider exhausted failure threshold, removing connection",
				zap.String("provider", providerName),
				zap.Int("failures", failures))
			s.store.Delete(providerName)
		}
	}
}

func secureJitterMillis() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano() % 100)
	}
	return binary.LittleEndian.Uint64(b[:]) % 100
}

// Shutdown cleanly kills all active provider plugin children
func (s *SecretStoreManager) Shutdown() {
	s.store.Range(func(key, value interface{}) bool {
		entry := value.(*StoreEntry)
		entry.Client.Kill()
		return true
	})
}

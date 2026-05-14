package agent

import (
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// TestNewTriggerEngine creates trigger engine with valid config
func TestNewTriggerEngine(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(1 * time.Hour)
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(cache, store, nil, logger, cfg, nil)

	if engine == nil {
		t.Fatal("NewTriggerEngine returned nil")
	}
	if engine.Cache == nil {
		t.Fatal("Engine cache is nil")
	}
	if engine.Config == nil {
		t.Fatal("Engine config is nil")
	}
	if engine.Logger == nil {
		t.Fatal("Engine logger is nil")
	}
	if engine.ctx == nil {
		t.Fatal("Engine context is nil")
	}
	if engine.cancel == nil {
		t.Fatal("Engine cancel is nil")
	}
}

// TestTriggerEngine_Stop cancels context
func TestTriggerEngine_Stop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	// Context should be alive initially
	select {
	case <-engine.ctx.Done():
		t.Fatal("Context should not be cancelled initially")
	default:
		// Expected
	}

	// Stop the engine
	engine.Stop()

	// Context should be cancelled now
	select {
	case <-engine.ctx.Done():
		// Expected - context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after Stop()")
	}
}

// TestTriggerEngine_StartAll_EmptyProviders handles empty provider list
func TestTriggerEngine_StartAll_EmptyProviders(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	// Should not panic with empty providers
	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll should not error with empty providers, got %v", err)
	}
}

// TestTriggerEngine_StartAll_WithProviders starts all configured providers
func TestTriggerEngine_StartAll_WithProviders(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	// Setup config with provider
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
				Config: map[string]string{
					"address": "http://localhost:8200",
				},
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "test-secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{
					Enabled: true,
				},
			},
		},
		Agent: config.AgentConfig{
			Watch: config.WatchConfig{
				PollingInterval: "5m",
			},
		},
	}

	cache := NewSecretCache(1 * time.Hour)
	engine := NewTriggerEngine(cache, store, nil, logger, cfg, nil)

	// StartAll should complete without error
	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll should not error, got %v", err)
	}
}

// TestTriggerEngine_ContextPropagation checks context is usable
func TestTriggerEngine_ContextPropagation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	// Should be able to use context
	select {
	case <-engine.ctx.Done():
		t.Fatal("Context should not be done initially")
	case <-time.After(100 * time.Millisecond):
		// Expected - context is alive
	}
}

// TestTriggerEngine_ConcurrentStop handles concurrent stop calls
func TestTriggerEngine_ConcurrentStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	done := make(chan bool)

	// Multiple concurrent stops
	for i := 0; i < 5; i++ {
		go func() {
			engine.Stop()
			done <- true
		}()
	}

	// Wait for all stops
	for i := 0; i < 5; i++ {
		<-done
	}

	// Should still be in cancelled state
	select {
	case <-engine.ctx.Done():
		// Expected
	default:
		t.Fatal("Context should be cancelled")
	}
}

// TestTriggerEngine_WithReloaderController sets reloader fields
func TestTriggerEngine_WithReloaderController(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}
	cache := NewSecretCache(1 * time.Hour)

	// Create mock reloader
	reloader := &watcher.ReloaderController{}

	engine := NewTriggerEngine(cache, store, reloader, logger, cfg, nil)

	if engine.Reloader != reloader {
		t.Error("Reloader should be set in engine")
	}
	if reloader.Cache != cache {
		t.Error("Cache should be set in reloader")
	}
	if reloader.Config != cfg {
		t.Error("Config should be set in reloader")
	}
}

// TestTriggerEngine_Logger logger is properly set
func TestTriggerEngine_Logger(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	if engine.Logger != logger {
		t.Error("Logger should be set in engine")
	}
}

// TestTriggerEngine_Config config is properly set
func TestTriggerEngine_Config(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	if engine.Config != cfg {
		t.Error("Config should be set in engine")
	}
}

// TestTriggerEngine_MultipleProviders handles multiple providers
func TestTriggerEngine_MultipleProviders(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
			},
			"aws": {
				Type: "aws",
			},
			"azure": {
				Type: "azure",
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "vault-secret",
				Provider: "vault",
			},
			{
				Name:     "aws-secret",
				Provider: "aws",
			},
			{
				Name:     "azure-secret",
				Provider: "azure",
			},
		},
	}

	cache := NewSecretCache(1 * time.Hour)
	engine := NewTriggerEngine(cache, store, nil, logger, cfg, nil)

	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll with multiple providers should not error, got %v", err)
	}
}

// TestTriggerEngine_StartAll_WithCustomPollingInterval uses custom polling interval
func TestTriggerEngine_StartAll_WithCustomPollingInterval(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{Enabled: true},
			},
		},
		Agent: config.AgentConfig{
			Watch: config.WatchConfig{
				PollingInterval: "30s", // Custom interval
			},
		},
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll with custom interval should not error, got %v", err)
	}
}

// TestTriggerEngine_NonRotatingSecrets ignores non-rotating secrets
func TestTriggerEngine_NonRotatingSecrets(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "rotating-secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{Enabled: true},
			},
			{
				Name:     "static-secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{Enabled: false},
			},
		},
		Agent: config.AgentConfig{
			Watch: config.WatchConfig{
				PollingInterval: "5m",
			},
		},
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll should not error with mixed secret types, got %v", err)
	}
}

// TestTriggerEngine_DefaultPollingInterval uses default when not specified
func TestTriggerEngine_DefaultPollingInterval(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{Enabled: true},
			},
		},
		Agent: config.AgentConfig{
			Watch: config.WatchConfig{
				PollingInterval: "", // Empty - should use default
			},
		},
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	err := engine.StartAll()
	if err != nil {
		t.Errorf("StartAll with default interval should not error, got %v", err)
	}
}

// TestTriggerEngine_ConcurrentStartAndStop calls both concurrently
func TestTriggerEngine_ConcurrentStartAndStop(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	store := providers.NewSecretStoreManager(logger)

	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"vault": {
				Type: "vault",
			},
		},
		Secrets: []config.SecretMapping{
			{
				Name:     "secret",
				Provider: "vault",
				Rotation: config.RotationConfigV2{Enabled: true},
			},
		},
		Agent: config.AgentConfig{
			Watch: config.WatchConfig{
				PollingInterval: "5m",
			},
		},
	}

	engine := NewTriggerEngine(nil, store, nil, logger, cfg, nil)

	done := make(chan bool)

	// Start and stop concurrently
	go func() {
		engine.StartAll()
		done <- true
	}()

	go func() {
		time.Sleep(10 * time.Millisecond)
		engine.Stop()
		done <- true
	}()

	// Wait for both to complete
	<-done
	<-done
}

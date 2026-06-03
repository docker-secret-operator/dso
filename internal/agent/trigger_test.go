package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// createTestTempDirs creates temporary directories for lock and state files used in tests
func createTestTempDirs(t *testing.T) (lockDir, stateDir string) {
	var err error
	lockDir, err = os.MkdirTemp("", "dso-test-locks-*")
	if err != nil {
		t.Fatalf("Failed to create temp lock dir: %v", err)
	}

	stateDir, err = os.MkdirTemp("", "dso-test-state-*")
	if err != nil {
		t.Fatalf("Failed to create temp state dir: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(lockDir)
		os.RemoveAll(stateDir)
	})

	return lockDir, stateDir
}

// NewTriggerEngineForTest creates a TriggerEngine with test-appropriate paths
func NewTriggerEngineForTest(t *testing.T, cache *SecretCache, storeManager *providers.SecretStoreManager, rw *watcher.ReloaderController, logger *zap.Logger, cfg *config.Config, dockerCli interface{}) *TriggerEngine {
	lockDir, stateDir := createTestTempDirs(t)

	if rw != nil {
		rw.Cache = cache
		rw.Config = cfg
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize state tracker and lock manager with test directories
	stateTracker, err := NewStateTracker(stateDir, logger)
	if err != nil {
		logger.Warn("Failed to initialize state tracker - rotation recovery disabled",
			zap.Error(err))
		stateTracker = nil
	}

	// CRITICAL: Lock manager must be initialized for rotation safety
	lockManager, err := rotation.NewLockManager(lockDir, logger)
	if err != nil {
		t.Fatalf("Failed to initialize rotation lock manager for test: %v", err)
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
		DockerClient:      nil,
	}
}

// TestNewTriggerEngine creates trigger engine with valid config
func TestNewTriggerEngine(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(1 * time.Hour)
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{
		Providers: make(map[string]config.ProviderConfig),
		Secrets:   make([]config.SecretMapping, 0),
	}

	engine := NewTriggerEngineForTest(t, cache, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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
	engine := NewTriggerEngineForTest(t, cache, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, cache, store, reloader, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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
	engine := NewTriggerEngineForTest(t, cache, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

	engine := NewTriggerEngineForTest(t, nil, store, nil, logger, cfg, nil)

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

// ---- H3 regression tests: collectContainerIDsForSecret ---------------------

// TestCollectContainerIDsForSecret_OneMatch verifies that a single container
// using the target secret returns that container's ID.
func TestCollectContainerIDsForSecret_OneMatch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(time.Hour)
	t.Cleanup(func() { cache.Close() })
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{}

	rc := &watcher.ReloaderController{Logger: logger}
	rc.Targets.Store("ctr-abc", &watcher.TargetContainer{ID: "ctr-abc", Secrets: []string{"db_pass"}})
	rc.Targets.Store("ctr-xyz", &watcher.TargetContainer{ID: "ctr-xyz", Secrets: []string{"api_key"}})

	eng := NewTriggerEngineForTest(t, cache, store, rc, logger, cfg, nil)

	got := eng.collectContainerIDsForSecret("db_pass")
	if got != "ctr-abc" {
		t.Errorf("H3: expected ctr-abc, got %q", got)
	}
}

// TestCollectContainerIDsForSecret_MultipleContainersSameSecret verifies that
// when two containers share a secret both IDs appear in the result (H3 — no
// overwrite).
func TestCollectContainerIDsForSecret_MultipleContainersSameSecret(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(time.Hour)
	t.Cleanup(func() { cache.Close() })
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{}

	rc := &watcher.ReloaderController{Logger: logger}
	rc.Targets.Store("ctr-A", &watcher.TargetContainer{ID: "ctr-A", Secrets: []string{"secret-X"}})
	rc.Targets.Store("ctr-B", &watcher.TargetContainer{ID: "ctr-B", Secrets: []string{"secret-X"}})

	eng := NewTriggerEngineForTest(t, cache, store, rc, logger, cfg, nil)

	got := eng.collectContainerIDsForSecret("secret-X")
	// Both container IDs must appear; order is not guaranteed (sync.Map).
	if !containsID(got, "ctr-A") || !containsID(got, "ctr-B") {
		t.Errorf("H3: expected both ctr-A and ctr-B in result, got %q", got)
	}
}

// TestCollectContainerIDsForSecret_NoMatch verifies that a secret no container
// uses returns an empty string, not a panic.
func TestCollectContainerIDsForSecret_NoMatch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(time.Hour)
	t.Cleanup(func() { cache.Close() })
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{}

	rc := &watcher.ReloaderController{Logger: logger}
	rc.Targets.Store("ctr-A", &watcher.TargetContainer{ID: "ctr-A", Secrets: []string{"other_secret"}})

	eng := NewTriggerEngineForTest(t, cache, store, rc, logger, cfg, nil)

	got := eng.collectContainerIDsForSecret("nonexistent")
	if got != "" {
		t.Errorf("H3: expected empty string for unmatched secret, got %q", got)
	}
}

// TestCollectContainerIDsForSecret_NilReloader verifies the method is safe
// when the engine has no Reloader wired up.
func TestCollectContainerIDsForSecret_NilReloader(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewSecretCache(time.Hour)
	t.Cleanup(func() { cache.Close() })
	store := providers.NewSecretStoreManager(logger)
	cfg := &config.Config{}

	eng := NewTriggerEngineForTest(t, cache, store, nil, logger, cfg, nil)

	// Must not panic.
	got := eng.collectContainerIDsForSecret("some-secret")
	if got != "" {
		t.Errorf("H3: expected empty string with nil Reloader, got %q", got)
	}
}

// containsID returns true if the comma-joined id list contains target.
func containsID(list, target string) bool {
	for _, id := range splitIDs(list) {
		if id == target {
			return true
		}
	}
	return false
}

func splitIDs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

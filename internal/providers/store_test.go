package providers

import (
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/hashicorp/go-plugin"
	"go.uber.org/zap/zaptest"
)

func TestSecretStoreManager_GetProvider_InvalidType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	cfg := config.ProviderConfig{
		Type: "non-existent-provider",
		Retry: config.RetryConfig{
			Attempts: 1, // fail fast
		},
	}

	_, err := manager.GetProvider("test-prov", cfg)
	if err == nil {
		t.Fatal("expected error for non-existent provider type")
	}
}

func TestSecretStoreManager_GetProvider_RetryBackoff(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	cfg := config.ProviderConfig{
		Type: "non-existent-provider",
		Retry: config.RetryConfig{
			Attempts: 2,
			Backoff:  "1ms",
		},
	}

	start := time.Now()
	_, err := manager.GetProvider("test-prov", cfg)
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	if duration < 1*time.Millisecond {
		t.Fatalf("expected delay > 1ms, got %v", duration)
	}
}

func TestSecureJitterMillis(t *testing.T) {
	val := secureJitterMillis()
	if val >= 100 {
		t.Fatalf("jitter should be < 100, got %d", val)
	}
}

func TestShutdown_Empty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	// Should not panic on empty store
	manager.Shutdown()
}

func TestMarkProviderHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	manager.store.Store("prov1", &StoreEntry{
		ConsecFails: 3,
		LastHealthy: time.Time{},
	})

	manager.MarkProviderHealthy("prov1")

	val, _ := manager.store.Load("prov1")
	entry := val.(*StoreEntry)
	if entry.ConsecFails != 0 {
		t.Fatalf("expected ConsecFails=0 after MarkProviderHealthy, got %d", entry.ConsecFails)
	}
	if entry.LastHealthy.IsZero() {
		t.Fatal("expected LastHealthy to be set after MarkProviderHealthy")
	}

	// Should not panic for unknown provider
	manager.MarkProviderHealthy("unknown")
}

// TestGetProvider_ConcurrentWithMarkProviderHealthy is a regression test for
// REL-2: GetProvider used to read entry.LastHealthy directly, racing against
// MarkProviderHealthy/MarkProviderFailure writing it under entry.mu. Under
// `go test -race` this reproduces as a real data race before the fix (both
// read entry.LastHealthy while a concurrent MarkProviderHealthy call writes
// it) and is clean after (GetProvider now takes entry.mu for the read).
func TestGetProvider_ConcurrentWithMarkProviderHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	manager.store.Store("prov1", &StoreEntry{
		Client:      &plugin.Client{}, // zero value: Exited() returns false without a real subprocess
		LastHealthy: time.Now(),
		MaxFailures: 5,
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = manager.GetProvider("prov1", config.ProviderConfig{})
		}()
		go func() {
			defer wg.Done()
			manager.MarkProviderHealthy("prov1")
		}()
	}
	wg.Wait()
}

func TestMarkProviderFailure(t *testing.T) {
	logger := zaptest.NewLogger(t)
	manager := NewSecretStoreManager(logger)

	manager.store.Store("prov1", &StoreEntry{
		ConsecFails: 0,
		MaxFailures: 3,
	})

	// Two failures below threshold — entry should remain
	manager.MarkProviderFailure("prov1")
	manager.MarkProviderFailure("prov1")
	if _, ok := manager.store.Load("prov1"); !ok {
		t.Fatal("provider should still be present below failure threshold")
	}

	// Third failure reaches threshold — entry should be removed
	manager.MarkProviderFailure("prov1")
	if _, ok := manager.store.Load("prov1"); ok {
		t.Fatal("provider should be removed after exceeding failure threshold")
	}

	// Should not panic for unknown provider
	manager.MarkProviderFailure("unknown")
}

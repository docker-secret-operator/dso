package providers

import (
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
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

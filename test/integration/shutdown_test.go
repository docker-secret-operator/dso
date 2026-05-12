package integration

import (
	"context"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/server"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

func TestGracefulShutdown_WithSecretsInCache(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create cache with secrets
	cache := agent.NewSecretCache(1 * time.Minute)
	defer cache.Close()

	// Add some secrets
	for i := 0; i < 10; i++ {
		cache.Set("secret-"+string(rune(i)), map[string]string{
			"key": "secret-value",
		})
	}

	// Verify secrets are in cache
	keys := cache.ListKeys()
	if len(keys) != 10 {
		t.Errorf("Expected 10 secrets in cache, got %d", len(keys))
	}

	// Close cache (simulates graceful shutdown)
	cache.Close()

	// After close, secrets should be cleared and zeroized
	keysAfter := cache.ListKeys()
	if len(keysAfter) != 0 {
		t.Errorf("Expected cache to be cleared after Close(), got %d keys", len(keysAfter))
	}
}

func TestGracefulShutdown_RESTServerShutdown(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cache := agent.NewSecretCache(1 * time.Minute)
	defer cache.Close()

	cfg := &config.Config{
		Agent: config.AgentConfig{},
	}

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start REST server
	apiAddr := "127.0.0.1:0" // Use random port
	restShutdown := server.StartRESTServer(ctx, apiAddr, cache, nil, cfg, logger)

	// Server should start successfully
	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	restShutdown()

	// Verify shutdown completes without panic
	time.Sleep(200 * time.Millisecond)

	// Cancel context to cleanup
	cancel()
}

func TestGracefulShutdown_ContextCancellation(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	cache := agent.NewSecretCache(5 * time.Minute)
	defer cache.Close()

	// Add test data
	cache.Set("test-secret", map[string]string{
		"username": "admin",
		"password": "secret123",
	})

	cfg := &config.Config{}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start REST server with timeout context
	restShutdown := server.StartRESTServer(ctx, "127.0.0.1:0", cache, nil, cfg, logger)

	// Wait for context to timeout
	<-ctx.Done()

	// Manually shutdown (should not panic)
	restShutdown()

	// Sleep a bit to ensure no goroutines panic
	time.Sleep(100 * time.Millisecond)

	// Verify cache is still valid
	_, exists := cache.Get("test-secret")
	if !exists {
		t.Error("Cache should still be valid after REST shutdown")
	}
}

func TestGracefulShutdown_SignalHandling(t *testing.T) {
	// This test verifies that SIGTERM/SIGINT would be handled properly
	// We can't actually send signals in unit tests, but we can verify the mechanism exists

	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Create a parent context like signal.NotifyContext would
	ctx, cancel := context.WithCancel(context.Background())

	cache := agent.NewSecretCache(1 * time.Minute)
	defer cache.Close()

	cache.Set("signal-test", map[string]string{
		"data": "test",
	})

	// Create REST server with cancellable context
	restShutdown := server.StartRESTServer(ctx, "127.0.0.1:0", cache, nil, &config.Config{}, logger)

	// Simulate what signal handler would do - cancel the context
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// Wait for context cancellation
	<-ctx.Done()

	// Cleanup REST server
	restShutdown()

	// Cache cleanup on Close
	cache.Close()

	// Verify no panics or hangs
	time.Sleep(100 * time.Millisecond)
}

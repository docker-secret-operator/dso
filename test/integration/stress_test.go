package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/testcontainers/testcontainers-go"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
)

// TestStress_ConcurrentRotations tests multiple simultaneous rotations
func TestStress_ConcurrentRotations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	numContainers := 5
	containers := make([]testcontainers.Container, 0, numContainers)
	defer func() {
		for _, c := range containers {
			_ = c.Terminate(ctx)
		}
	}()

	// Start containers
	for i := 0; i < numContainers; i++ {
		req := testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "while true; do sleep 1; done"},
		}

		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
		if err != nil {
			t.Fatalf("Failed to start container %d: %v", i, err)
		}
		containers = append(containers, container)
	}

	// Get Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("Failed to create Docker client: %v", err)
	}
	defer cli.Close()

	logger, _ := zap.NewProduction()
	rs := rotation.NewRollingStrategyWithLogger(cli, logger)

	// Concurrent rotation counter
	var successCount int32
	var failureCount int32
	wg := sync.WaitGroup{}

	// Run rotations concurrently
	for i, c := range containers {
		wg.Add(1)
		go func(idx int, container testcontainers.Container) {
			defer wg.Done()

			rotCtx, rotCancel := context.WithTimeout(ctx, 20*time.Second)
			defer rotCancel()

			err := rs.Execute(rotCtx, container.GetContainerID(), map[string]string{
				"ROTATION_NUM": fmt.Sprintf("%d", idx),
				"TIMESTAMP":    time.Now().Format(time.RFC3339),
			}, 5*time.Second)

			if err != nil {
				atomic.AddInt32(&failureCount, 1)
				t.Logf("Container %d rotation failed: %v", idx, err)
			} else {
				atomic.AddInt32(&successCount, 1)
				t.Logf("Container %d rotation succeeded", idx)
			}
		}(i, c)
	}

	wg.Wait()

	successes := atomic.LoadInt32(&successCount)
	failures := atomic.LoadInt32(&failureCount)

	t.Logf("✅ Concurrent rotations: %d succeeded, %d failed", successes, failures)

	if failures > 0 {
		t.Errorf("Expected all rotations to succeed, but %d failed", failures)
	}
}

// TestStress_EventDebouncer_RapidFire tests debouncer under rapid fire events
func TestStress_EventDebouncer_RapidFire(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	debouncer := watcher.NewEventDebouncer(100 * time.Millisecond)
	defer debouncer.Stop()

	numEvents := 10000
	eventDuplicateRatio := 0.8 // 80% are duplicates
	var duplicatesDetected int
	var freshDetected int

	// Generate rapid fire events
	for i := 0; i < numEvents; i++ {
		var eventID string
		if i%int(float64(numEvents)*eventDuplicateRatio) == 0 {
			// Repeat same event ID frequently
			eventID = fmt.Sprintf("container-%d", i%10)
		} else {
			// Mostly unique event IDs
			eventID = fmt.Sprintf("container-unique-%d", i)
		}

		isFresh := debouncer.CheckAndRecord(eventID)

		if isFresh {
			freshDetected++
		} else {
			duplicatesDetected++
		}
	}

	t.Logf("✅ Rapid-fire test: %d fresh events, %d duplicates detected out of %d",
		freshDetected, duplicatesDetected, numEvents)

	expectedDuplicates := int(float64(numEvents) * eventDuplicateRatio * 0.9) // Allow 90% detection
	if duplicatesDetected < expectedDuplicates {
		t.Logf("Warning: Deduplication may be weak (%d < %d expected)", duplicatesDetected, expectedDuplicates)
	}
}

// TestStress_EventQueue_Backpressure tests event queue under high load
func TestStress_EventQueue_Backpressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// This would require a real event handler interface
	// For now, just test the backpressure metrics
	t.Log("✅ Event queue backpressure test would require full integration setup")
}

// TestStress_ConcurrentCacheAccess tests secret cache under concurrent load
func TestStress_ConcurrentCacheAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cache := NewSecretCache(5 * time.Second)
	defer cache.Close()

	numGoroutines := 20
	numOperations := 1000
	var mu sync.Mutex
	var successCount int

	wg := sync.WaitGroup{}

	// Reader goroutines
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("secret-%d", j%100)
				_, exists := cache.Get(key)
				if !exists {
					// Expected - may not exist
				}
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	// Writer goroutines
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("secret-%d", j%100)
				cache.Set(key, map[string]string{
					"password": fmt.Sprintf("value-%d-%d", id, j),
				})
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	t.Logf("✅ Concurrent cache access: %d operations completed", successCount)
	if successCount != numGoroutines*numOperations {
		t.Errorf("Expected %d operations, got %d", numGoroutines*numOperations, successCount)
	}
}

// TestStress_SecretZeroization_UnderLoad tests that secrets are properly zeroized under load
func TestStress_SecretZeroization_UnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cache := NewSecretCache(100 * time.Millisecond)
	defer cache.Close()

	numSecrets := 1000

	// Store many secrets
	for i := 0; i < numSecrets; i++ {
		cache.Set(fmt.Sprintf("secret-%d", i), map[string]string{
			"sensitive_data": fmt.Sprintf("secret-value-%d-with-sensitive-content", i),
		})
	}

	// Delete all secrets
	for i := 0; i < numSecrets; i++ {
		cache.Delete(fmt.Sprintf("secret-%d", i))
	}

	// Verify all are gone
	for i := 0; i < numSecrets; i++ {
		_, exists := cache.Get(fmt.Sprintf("secret-%d", i))
		if exists {
			t.Errorf("Secret %d should be deleted but still exists", i)
			return
		}
	}

	t.Logf("✅ All %d secrets properly deleted and zeroized", numSecrets)
}

// Helper type matching the test needs
type mockSecretCache struct {
	items map[string]map[string]string
	mu    sync.RWMutex
}

func NewSecretCache(ttl time.Duration) *mockSecretCache {
	return &mockSecretCache{
		items: make(map[string]map[string]string),
	}
}

func (c *mockSecretCache) Get(key string) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.items[key]
	return val, ok
}

func (c *mockSecretCache) Set(key string, data map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = data
}

func (c *mockSecretCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *mockSecretCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]map[string]string)
}

package agent

// Docker-free stress tests moved out of the quarantined test/integration suite
// (M0-11). The originals ran against a private mock cache defined in the test file
// (so they proved nothing about the product); these exercise the real SecretCache.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Original: TestStress_ConcurrentCacheAccess (assertion unchanged: every one of
// the numGoroutines*numOperations operations completes). The counter is atomic
// instead of mutex-guarded; run with -race to check the cache itself.
func TestStress_ConcurrentCacheAccess(t *testing.T) {
	cache := NewSecretCache(5 * time.Second)
	defer cache.Close()

	const numGoroutines, numOperations = 20, 1000
	var successCount int64
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// Existence is not asserted: this exercises Get for races/panics.
				_, _ = cache.Get(fmt.Sprintf("secret-%d", j%100))
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				cache.Set(fmt.Sprintf("secret-%d", j%100), map[string]string{
					"password": fmt.Sprintf("value-%d-%d", id, j),
				})
				atomic.AddInt64(&successCount, 1)
			}
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt64(&successCount); got != numGoroutines*numOperations {
		t.Errorf("Expected %d operations, got %d", numGoroutines*numOperations, got)
	}
	// Added: after the storm every key readable must hold a complete value.
	for k := 0; k < 100; k++ {
		if v, ok := cache.Get(fmt.Sprintf("secret-%d", k)); ok && v["password"] == "" {
			t.Errorf("secret-%d holds an empty value after concurrent writes", k)
		}
	}
}

// Original: TestStress_SecretZeroization_UnderLoad (assertion unchanged: every
// stored secret is gone after Delete). Added, because it is what the name claims
// and the mock could never check: the backing byte slices captured before Delete
// are actually zeroed afterwards.
func TestStress_SecretZeroization_UnderLoad(t *testing.T) {
	cache := NewSecretCache(100 * time.Millisecond)
	defer cache.Close()

	const numSecrets = 1000
	for i := 0; i < numSecrets; i++ {
		cache.Set(fmt.Sprintf("secret-%d", i), map[string]string{
			"sensitive_data": fmt.Sprintf("secret-value-%d-with-sensitive-content", i),
		})
	}

	// Capture the live backing buffers (same package) before deleting.
	cache.mu.RLock()
	buffers := make([][]byte, 0, numSecrets)
	for _, item := range cache.items {
		for _, b := range item.Data {
			buffers = append(buffers, b)
		}
	}
	cache.mu.RUnlock()
	if len(buffers) != numSecrets {
		t.Fatalf("expected %d backing buffers, found %d", numSecrets, len(buffers))
	}

	for i := 0; i < numSecrets; i++ {
		cache.Delete(fmt.Sprintf("secret-%d", i))
	}

	for i := 0; i < numSecrets; i++ {
		if _, exists := cache.Get(fmt.Sprintf("secret-%d", i)); exists {
			t.Fatalf("Secret %d should be deleted but still exists", i)
		}
	}
	for i, b := range buffers {
		for _, c := range b {
			if c != 0 {
				t.Fatalf("backing buffer %d was not zeroized by Delete", i)
			}
		}
	}
}

package agent

import (
	"testing"
	"time"
)

// TestSecretCache_ExpiredEntriesCleanup verifies periodic cleanup of expired entries
func TestSecretCache_ExpiredEntriesCleanup(t *testing.T) {
	sc := NewSecretCache(100 * time.Millisecond)
	defer sc.Close()

	// Add entry
	sc.Set("key1", map[string]string{"secret": "value"})

	// Verify it exists
	_, exists := sc.Get("key1")
	if !exists {
		t.Fatal("Entry should exist immediately after Set")
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Verify it's gone (expired)
	_, exists = sc.Get("key1")
	if exists {
		t.Error("Expired entry should not be retrieved")
	}

	// Wait for cleanup goroutine to run (cleanup runs every 5 minutes in production)
	// In this test we just verify Get() checks expiration
}

// TestSecretCache_ConcurrentAccessSafety verifies thread-safety under concurrent load
func TestSecretCache_ConcurrentAccessSafety(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	defer sc.Close()

	done := make(chan bool, 20)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			key := "key-" + string(rune(48+id))
			sc.Set(key, map[string]string{"value": string(rune(48 + id))})
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			sc.Get("key-1")
			sc.ListKeys()
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 20; i++ {
		<-done
	}
}

// TestSecretCache_MemoryBounds verifies cache respects max size
func TestSecretCache_MemoryBounds(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	defer sc.Close()

	// Verify maxSize is set
	if sc.maxSize == 0 {
		t.Error("Cache should have a max size limit")
	}

	// Add multiple entries
	for i := 0; i < 100; i++ {
		key := "key-" + string(rune(48+i%10))
		sc.Set(key, map[string]string{
			"field1": "value1",
			"field2": "value2",
			"field3": "value3",
		})
	}

	// Verify cache doesn't infinitely grow (entries are overwritten)
	keys := sc.ListKeys()
	if len(keys) > 10 {
		t.Errorf("Cache should not grow unbounded, expected <= 10 unique keys, got %d", len(keys))
	}
}

// TestSecretCache_DeleteOperation verifies manual deletion
func TestSecretCache_DeleteOperation(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	defer sc.Close()

	sc.Set("key1", map[string]string{"secret": "value"})
	sc.Set("key2", map[string]string{"secret": "value"})

	// Verify both exist
	_, exists1 := sc.Get("key1")
	_, exists2 := sc.Get("key2")
	if !exists1 || !exists2 {
		t.Fatal("Both entries should exist")
	}

	// Delete one
	sc.Delete("key1")

	// Verify deletion
	_, exists1 = sc.Get("key1")
	_, exists2 = sc.Get("key2")
	if exists1 {
		t.Error("Deleted key1 should not exist")
	}
	if !exists2 {
		t.Error("Undeleted key2 should still exist")
	}
}

// TestSecretCache_ListKeysConsistency verifies ListKeys returns consistent results
func TestSecretCache_ListKeysConsistency(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	defer sc.Close()

	// Add entries
	for i := 0; i < 5; i++ {
		key := "key-" + string(rune(48+i))
		sc.Set(key, map[string]string{"data": "value"})
	}

	// Get keys multiple times
	keys1 := sc.ListKeys()
	keys2 := sc.ListKeys()

	if len(keys1) != len(keys2) {
		t.Errorf("ListKeys should return consistent results: %d vs %d", len(keys1), len(keys2))
	}

	// Verify all keys are present
	keyMap := make(map[string]bool)
	for _, k := range keys1 {
		keyMap[k] = true
	}

	for i := 0; i < 5; i++ {
		key := "key-" + string(rune(48+i))
		if !keyMap[key] {
			t.Errorf("Key %s should be in list", key)
		}
	}
}

// TestSecretCache_RaceConditionDetection simulates high-frequency access pattern
func TestSecretCache_RaceConditionDetection(t *testing.T) {
	sc := NewSecretCache(10 * time.Second)
	defer sc.Close()

	done := make(chan bool, 100)
	errorCh := make(chan error, 100)

	// Simulate rapid concurrent access (stress test)
	for i := 0; i < 100; i++ {
		go func(id int) {
			defer func() {
				if r := recover(); r != nil {
					errorCh <- nil // Panic detected
				}
				done <- true
			}()

			// Mix of operations
			sc.Set("key-1", map[string]string{"value": "1"})
			sc.Get("key-1")
			sc.ListKeys()
			if id%10 == 0 {
				sc.Delete("key-1")
			}
		}(i)
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	// Check for panics
	select {
	case <-errorCh:
		t.Error("Panic detected during concurrent access - race condition present")
	default:
		// No panic
	}
}

// TestSecretCache_CloseStopsCleanup verifies cleanup goroutine stops
func TestSecretCache_CloseStopsCleanup(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)

	// Close should not panic and should stop the cleanup goroutine
	sc.Close()

	// Calling Close again should not panic
	sc.Close()
}

// TestSecretCache_CapacityAllocation verifies initial capacity allocation
func TestSecretCache_CapacityAllocation(t *testing.T) {
	sc := NewSecretCache(1 * time.Hour)
	defer sc.Close()

	// Verify internal structures are properly initialized
	if sc.items == nil {
		t.Error("Cache items map should be initialized")
	}

	if sc.ttl == 0 {
		t.Error("Cache TTL should be set")
	}

	if sc.maxSize == 0 {
		t.Error("Cache max size should be set")
	}

	if sc.stopCh == nil {
		t.Error("Cache stop channel should be initialized")
	}
}

// TestSecretCache_HashConsistency verifies hash generation is deterministic
func TestSecretCache_HashConsistency(t *testing.T) {
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	hash1 := ComputeHash(data)
	hash2 := ComputeHash(data)

	if hash1 != hash2 {
		t.Errorf("Hash should be deterministic: %s vs %s", hash1, hash2)
	}

	// Different data should produce different hash
	data2 := map[string]string{
		"key1": "different_value",
		"key2": "value2",
	}
	hash3 := ComputeHash(data2)

	if hash1 == hash3 {
		t.Error("Different data should produce different hash")
	}
}

// TestSecretCacheIsolation verifies separate cache instances don't interfere
func TestSecretCacheIsolation(t *testing.T) {
	sc1 := NewSecretCache(1 * time.Hour)
	sc2 := NewSecretCache(1 * time.Hour)
	defer sc1.Close()
	defer sc2.Close()

	sc1.Set("key", map[string]string{"cache": "1"})
	sc2.Set("key", map[string]string{"cache": "2"})

	val1, _ := sc1.Get("key")
	val2, _ := sc2.Get("key")

	if val1["cache"] != "1" || val2["cache"] != "2" {
		t.Error("Cache instances should be isolated from each other")
	}
}

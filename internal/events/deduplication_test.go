package events

import (
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/events"
)

func TestComputeFingerprint_Consistency(t *testing.T) {
	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_abc123",
		},
	}

	fp1 := ComputeFingerprint(msg)
	fp2 := ComputeFingerprint(msg)

	if fp1 != fp2 {
		t.Errorf("Fingerprint not consistent: %v != %v", fp1, fp2)
	}
}

func TestComputeFingerprint_DifferentContainers(t *testing.T) {
	msg1 := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_abc123",
		},
	}

	msg2 := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_def456",
		},
	}

	fp1 := ComputeFingerprint(msg1)
	fp2 := ComputeFingerprint(msg2)

	if fp1 == fp2 {
		t.Errorf("Different containers should have different fingerprints")
	}
}

func TestComputeFingerprint_DifferentActions(t *testing.T) {
	msg1 := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_abc123",
		},
	}

	msg2 := events.Message{
		Action: "die",
		Actor: events.Actor{
			ID: "container_abc123",
		},
	}

	fp1 := ComputeFingerprint(msg1)
	fp2 := ComputeFingerprint(msg2)

	if fp1 == fp2 {
		t.Errorf("Different actions should have different fingerprints")
	}
}

func TestDedupCache_NewEventNotDuplicate(t *testing.T) {
	dc := NewDedupCache(1*time.Second, 100)
	defer dc.Clear()

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// First event should not be a duplicate
	if dc.IsDuplicate(msg) {
		t.Error("First event should not be a duplicate")
	}
}

func TestDedupCache_DuplicateEventDetected(t *testing.T) {
	dc := NewDedupCache(1*time.Second, 100)
	defer dc.Clear()

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// First event
	if dc.IsDuplicate(msg) {
		t.Error("First event should not be a duplicate")
	}

	// Second identical event should be duplicate
	if !dc.IsDuplicate(msg) {
		t.Error("Identical event should be detected as duplicate")
	}
}

func TestDedupCache_TTLExpiration(t *testing.T) {
	dc := NewDedupCache(100*time.Millisecond, 100)
	defer dc.Clear()

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// Add event
	if dc.IsDuplicate(msg) {
		t.Error("First event should not be a duplicate")
	}

	// Should be duplicate immediately
	if !dc.IsDuplicate(msg) {
		t.Error("Should be duplicate immediately after first add")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should no longer be considered duplicate
	if dc.IsDuplicate(msg) {
		t.Error("Event should not be duplicate after TTL expiration")
	}
}

func TestDedupCache_MaxSizeEnforcement(t *testing.T) {
	maxSize := 10
	dc := NewDedupCache(30*time.Second, maxSize)
	defer dc.Clear()

	// Add more events than max size
	for i := 0; i < maxSize*2; i++ {
		msg := events.Message{
			Action: "start",
			Actor: events.Actor{
				ID: "container_" + string(rune(i)),
			},
		}
		dc.IsDuplicate(msg)
	}

	stats := dc.GetStats()
	if stats["total_entries"].(int) > maxSize {
		t.Errorf("Cache exceeded max size: %d > %d", stats["total_entries"], maxSize)
	}
}

func TestDedupCache_ClearOperation(t *testing.T) {
	dc := NewDedupCache(1*time.Second, 100)

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// Add event
	dc.IsDuplicate(msg)

	// Should be duplicate before clear
	if !dc.IsDuplicate(msg) {
		t.Error("Should be duplicate before clear")
	}

	// Clear cache
	dc.Clear()

	// Should not be duplicate after clear
	if dc.IsDuplicate(msg) {
		t.Error("Should not be duplicate after clear")
	}
}

func TestDedupCache_ConcurrentAccess(t *testing.T) {
	dc := NewDedupCache(5*time.Second, 1000)
	defer dc.Clear()

	var wg sync.WaitGroup
	dupCount := 0
	mu := sync.Mutex{}

	// Multiple goroutines adding events
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				msg := events.Message{
					Action: "start",
					Actor: events.Actor{
						ID: "container_" + string(rune(id)),
					},
				}

				if dc.IsDuplicate(msg) {
					mu.Lock()
					dupCount++
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have many duplicates due to concurrent adds
	if dupCount < 450 {
		t.Errorf("Expected many duplicates, got %d", dupCount)
	}
}

func TestDedupCache_GetStats(t *testing.T) {
	dc := NewDedupCache(1*time.Second, 100)
	defer dc.Clear()

	// Add some events
	for i := 0; i < 5; i++ {
		msg := events.Message{
			Action: "start",
			Actor: events.Actor{
				ID: "container_" + string(rune(i)),
			},
		}
		dc.IsDuplicate(msg)
	}

	stats := dc.GetStats()

	// Verify stats structure
	expected := []string{"total_entries", "expired", "active", "ttl", "max_size"}
	for _, key := range expected {
		if _, ok := stats[key]; !ok {
			t.Errorf("Missing stat key: %s", key)
		}
	}

	if stats["total_entries"] != 5 {
		t.Errorf("Expected 5 entries, got %d", stats["total_entries"])
	}
}

func TestImmediateDedup_NewEventNotDuplicate(t *testing.T) {
	id := NewImmediateDedup(100)

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	if id.IsDuplicate(msg) {
		t.Error("First event should not be duplicate")
	}
}

func TestImmediateDedup_DuplicateDetected(t *testing.T) {
	id := NewImmediateDedup(100)

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// First add
	id.IsDuplicate(msg)

	// Second should be duplicate
	if !id.IsDuplicate(msg) {
		t.Error("Second identical event should be duplicate")
	}
}

func TestImmediateDedup_MaxSizeAndOverflow(t *testing.T) {
	maxSize := 10
	id := NewImmediateDedup(maxSize)

	// Add up to max size
	for i := 0; i < maxSize; i++ {
		msg := events.Message{
			Action: "start",
			Actor: events.Actor{
				ID: "container_" + string(rune(i)),
			},
		}
		id.IsDuplicate(msg)
	}

	if id.GetSize() != maxSize {
		t.Errorf("Expected size %d, got %d", maxSize, id.GetSize())
	}

	// Add one more - should trigger overflow
	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_overflow",
		},
	}
	id.IsDuplicate(msg)

	if !id.HasOverflowed() {
		t.Error("Should have detected overflow")
	}
}

func TestImmediateDedup_Reset(t *testing.T) {
	id := NewImmediateDedup(100)

	msg := events.Message{
		Action: "start",
		Actor: events.Actor{
			ID: "container_123",
		},
	}

	// Add event
	id.IsDuplicate(msg)

	// Should be tracked
	if id.GetSize() == 0 {
		t.Error("Size should be > 0 before reset")
	}

	// Reset
	id.Reset()

	// Should be cleared
	if id.GetSize() != 0 {
		t.Errorf("Expected size 0 after reset, got %d", id.GetSize())
	}

	// Should not be duplicate
	if id.IsDuplicate(msg) {
		t.Error("Should not be duplicate after reset")
	}
}

func TestDeduplication_ReplayScenario(t *testing.T) {
	// Simulate daemon reconnect with replayed events
	dc := NewDedupCache(2*time.Second, 1000)
	defer dc.Clear()

	// Original events during normal operation
	originalEvents := []events.Message{
		{Action: "create", Actor: events.Actor{ID: "container_a"}},
		{Action: "start", Actor: events.Actor{ID: "container_a"}},
		{Action: "create", Actor: events.Actor{ID: "container_b"}},
		{Action: "start", Actor: events.Actor{ID: "container_b"}},
	}

	// Process original events
	for _, msg := range originalEvents {
		if dc.IsDuplicate(msg) {
			t.Errorf("Original event should not be duplicate: %s", msg.Action)
		}
	}

	// Daemon reconnect - replay same events
	dupCount := 0
	for _, msg := range originalEvents {
		if dc.IsDuplicate(msg) {
			dupCount++
		}
	}

	// All replayed events should be detected as duplicates
	if dupCount != len(originalEvents) {
		t.Errorf("Expected %d duplicates on replay, got %d", len(originalEvents), dupCount)
	}
}

func TestDeduplication_ConcurrentDaemonRestart(t *testing.T) {
	// Simulate concurrent event processing with daemon restart
	dc := NewDedupCache(1*time.Second, 1000)
	defer dc.Clear()

	var wg sync.WaitGroup
	events := []events.Message{
		{Action: "start", Actor: events.Actor{ID: "container_x"}},
		{Action: "start", Actor: events.Actor{ID: "container_y"}},
	}

	// Multiple goroutines processing events (simulating concurrent adds)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for _, msg := range events {
				dc.IsDuplicate(msg)
			}
		}()
	}

	wg.Wait()

	// After concurrent adds, repeating events should be duplicates
	dupCount := 0
	for _, msg := range events {
		if dc.IsDuplicate(msg) {
			dupCount++
		}
	}

	if dupCount != len(events) {
		t.Errorf("Expected duplicates after concurrent adds, got %d", dupCount)
	}
}

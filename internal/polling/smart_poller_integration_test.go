package polling

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestSmartPolling_FullLifecycle tests the complete polling lifecycle
// from initial state through all interval phases
func TestSmartPolling_FullLifecycle(t *testing.T) {
	sp := NewSmartPoller()

	secretName := "test-secret"

	// Phase 1: Initial unknown secret (defaults to 30s)
	interval := sp.GetNextInterval(secretName)
	if interval != 30*time.Second {
		t.Fatalf("phase 1: expected 30s, got %v", interval)
	}

	// Phase 2: Change detected → aggressive (5s)
	sp.RecordChange(secretName)
	interval = sp.GetNextInterval(secretName)
	if interval != 5*time.Second {
		t.Fatalf("phase 2: expected 5s, got %v", interval)
	}

	// Phase 3: Poll recorded → continue aggressive
	sp.RecordPoll(secretName)
	interval = sp.GetNextInterval(secretName)
	if interval != 5*time.Second {
		t.Fatalf("phase 3: expected 5s, got %v", interval)
	}

	// Phase 4: Verify CalculateInterval handles boundary correctly
	// Instead of waiting 2 minutes, directly test the interval calculation
	// when time since change is exactly 2 minutes
	interval = CalculateInterval(2*time.Minute + 10*time.Second)
	if interval != 30*time.Second {
		t.Fatalf("phase 4: expected 30s for 2m+ elapsed, got %v", interval)
	}

	// Phase 5: Verify CalculateInterval for 10+ minute boundary
	interval = CalculateInterval(10*time.Minute + 10*time.Second)
	if interval != 5*time.Minute {
		t.Fatalf("phase 5: expected 5m for 10m+ elapsed, got %v", interval)
	}
}

// TestSmartPolling_LoadTest performs a load test with 1000 operations on 100 secrets
func TestSmartPolling_LoadTest(t *testing.T) {
	sp := NewSmartPoller()

	start := time.Now()

	// 1000 operations on 100 unique secrets
	for i := 0; i < 100; i++ {
		for j := 0; j < 10; j++ {
			secretName := fmt.Sprintf("secret-%d", i)
			sp.GetNextInterval(secretName)
			sp.RecordChange(secretName)
			sp.RecordPoll(secretName)
			sp.GetStats(secretName)
		}
	}

	elapsed := time.Since(start)

	// Should complete in reasonable time (< 1 second for 1000 ops on modern hardware)
	if elapsed > 1*time.Second {
		t.Logf("WARNING: 1000 operations took %v (target < 1s)", elapsed)
		// Don't fail, but log for benchmarking
	}

	// Verify all secrets have state
	for i := 0; i < 100; i++ {
		secretName := fmt.Sprintf("secret-%d", i)
		state := sp.GetStats(secretName)
		if state == nil {
			t.Fatalf("secret-%d: expected non-nil state", i)
		}
		if state.ChangeCount != 10 {
			t.Fatalf("secret-%d: expected 10 changes, got %d", i, state.ChangeCount)
		}
		if state.PollCount != 10 {
			t.Fatalf("secret-%d: expected 10 polls, got %d", i, state.PollCount)
		}
	}
}

// TestSmartPolling_ConcurrentStress performs a comprehensive concurrent stress test
// with 50 goroutines × 200 operations each on 20 unique secrets
func TestSmartPolling_ConcurrentStress(t *testing.T) {
	sp := NewSmartPoller()

	numGoroutines := 50
	opsPerGoroutine := 200
	numSecrets := 20

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)
	panicked := false

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer func() {
				if r := recover(); r != nil {
					errChan <- fmt.Errorf("goroutine %d panicked: %v", goroutineID, r)
					panicked = true
				}
			}()
			defer wg.Done()

			for op := 0; op < opsPerGoroutine; op++ {
				secretID := (goroutineID + op) % numSecrets
				secretName := fmt.Sprintf("secret-%d", secretID)

				switch op % 4 {
				case 0:
					sp.GetNextInterval(secretName)
				case 1:
					sp.RecordChange(secretName)
				case 2:
					sp.RecordPoll(secretName)
				case 3:
					sp.GetStats(secretName)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			t.Fatalf("concurrent stress test failed: %v", err)
		}
	}

	if panicked {
		t.Fatal("concurrent stress test caused panic(s)")
	}

	// Verify data integrity (all secrets have reasonable state)
	for i := 0; i < numSecrets; i++ {
		secretName := fmt.Sprintf("secret-%d", i)
		state := sp.GetStats(secretName)
		if state != nil && (state.ChangeCount < 0 || state.PollCount < 0) {
			t.Fatalf("secret-%d: negative counters detected", i)
		}
	}
}

// TestSmartPolling_IntervalAdaptationTimeline tests interval adaptation
// using direct CalculateInterval calls to simulate time progression
func TestSmartPolling_IntervalAdaptationTimeline(t *testing.T) {
	sp := NewSmartPoller()

	secretName := "adaptive-secret"

	// Timeline of states and expected intervals without real time waits
	timeline := []struct {
		label           string
		action          func()
		timeSinceChange time.Duration
		expectedNext    time.Duration
	}{
		{"start", func() {}, 0, 30 * time.Second}, // Unknown secret
		{"after change", func() { sp.RecordChange(secretName) }, 0, 5 * time.Second},
		{"after poll", func() { sp.RecordPoll(secretName) }, 0, 5 * time.Second},
		{"after 2m10s", func() {}, 2*time.Minute + 10*time.Second, 30 * time.Second},
		{"after 12m10s", func() {}, 12*time.Minute + 10*time.Second, 5 * time.Minute},
	}

	for idx, step := range timeline {
		step.action()

		var actual time.Duration
		if step.timeSinceChange == 0 && idx > 0 {
			// For recent actions, GetNextInterval will calculate based on real time
			// Just verify it returns one of the expected values
			actual = sp.GetNextInterval(secretName)
			if actual != 5*time.Second {
				t.Fatalf("step %d (%s): expected 5s for recent action, got %v",
					idx, step.label, actual)
			}
		} else if step.timeSinceChange > 0 {
			// Test CalculateInterval directly for specific elapsed times
			actual = CalculateInterval(step.timeSinceChange)
			if actual != step.expectedNext {
				t.Fatalf("step %d (%s): expected %v for %v elapsed, got %v",
					idx, step.label, step.expectedNext, step.timeSinceChange, actual)
			}
		} else {
			// Unknown secret
			actual = sp.GetNextInterval(secretName)
			if actual != step.expectedNext {
				t.Fatalf("step %d (%s): expected %v, got %v",
					idx, step.label, step.expectedNext, actual)
			}
		}
	}
}

// TestSmartPolling_HighContentionReadWrite tests high contention between read and write operations
func TestSmartPolling_HighContentionReadWrite(t *testing.T) {
	sp := NewSmartPoller()

	numReaders := 30
	numWriters := 20
	numSecrets := 10
	operationsPerGoroutine := 100

	var wg sync.WaitGroup

	// Start readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for op := 0; op < operationsPerGoroutine; op++ {
				secretID := (readerID + op) % numSecrets
				secretName := fmt.Sprintf("secret-%d", secretID)
				sp.GetNextInterval(secretName)
				sp.GetStats(secretName)
			}
		}(r)
	}

	// Start writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for op := 0; op < operationsPerGoroutine; op++ {
				secretID := (writerID + op) % numSecrets
				secretName := fmt.Sprintf("secret-%d", secretID)
				if op%2 == 0 {
					sp.RecordChange(secretName)
				} else {
					sp.RecordPoll(secretName)
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify all secrets have reasonable state
	for i := 0; i < numSecrets; i++ {
		secretName := fmt.Sprintf("secret-%d", i)
		state := sp.GetStats(secretName)
		// State might be nil if this secret wasn't written to by any writer
		if state != nil {
			if state.ChangeCount < 0 || state.PollCount < 0 {
				t.Fatalf("secret-%d: negative counters", i)
			}
		}
	}
}

// TestSmartPolling_RapidStateChanges tests rapid state changes on the same secret
func TestSmartPolling_RapidStateChanges(t *testing.T) {
	sp := NewSmartPoller()

	secretName := "rapid-secret"
	numRapidChanges := 5000

	// Rapidly record changes and polls
	for i := 0; i < numRapidChanges; i++ {
		sp.RecordChange(secretName)
		sp.RecordPoll(secretName)
	}

	state := sp.GetStats(secretName)
	if state.ChangeCount != numRapidChanges {
		t.Fatalf("expected %d changes, got %d", numRapidChanges, state.ChangeCount)
	}
	if state.PollCount != numRapidChanges {
		t.Fatalf("expected %d polls, got %d", numRapidChanges, state.PollCount)
	}

	// Interval should still be aggressive (recent)
	interval := sp.GetNextInterval(secretName)
	if interval != 5*time.Second {
		t.Fatalf("expected 5s after rapid changes, got %v", interval)
	}
}

// TestSmartPolling_MixedOperationsPatterns tests varied operation patterns
func TestSmartPolling_MixedOperationsPatterns(t *testing.T) {
	sp := NewSmartPoller()

	// Pattern 1: Many changes, few polls
	sp.RecordChange("many-changes")
	for i := 0; i < 100; i++ {
		sp.RecordChange("many-changes")
	}
	sp.RecordPoll("many-changes")

	state := sp.GetStats("many-changes")
	if state.ChangeCount != 101 || state.PollCount != 1 {
		t.Fatalf("pattern 1: expected 101 changes, 1 poll; got %d, %d", state.ChangeCount, state.PollCount)
	}

	// Pattern 2: Few changes, many polls
	sp.RecordChange("few-changes")
	for i := 0; i < 100; i++ {
		sp.RecordPoll("few-changes")
	}

	state = sp.GetStats("few-changes")
	if state.ChangeCount != 1 || state.PollCount != 100 {
		t.Fatalf("pattern 2: expected 1 change, 100 polls; got %d, %d", state.ChangeCount, state.PollCount)
	}

	// Pattern 3: Alternating changes and polls
	for i := 0; i < 50; i++ {
		sp.RecordChange("alternating")
		sp.RecordPoll("alternating")
	}

	state = sp.GetStats("alternating")
	if state.ChangeCount != 50 || state.PollCount != 50 {
		t.Fatalf("pattern 3: expected 50 changes, 50 polls; got %d, %d", state.ChangeCount, state.PollCount)
	}
}

// TestSmartPolling_SnapshotImmutability verifies GetStats returns independent snapshots
func TestSmartPolling_SnapshotImmutability(t *testing.T) {
	sp := NewSmartPoller()

	secretName := "snapshot-test"
	sp.RecordChange(secretName)

	// Get first snapshot
	snapshot1 := sp.GetStats(secretName)
	originalChangeCount := snapshot1.ChangeCount

	// Record more changes
	for i := 0; i < 100; i++ {
		sp.RecordChange(secretName)
	}

	// Get second snapshot
	snapshot2 := sp.GetStats(secretName)

	// First snapshot should be unchanged
	if snapshot1.ChangeCount != originalChangeCount {
		t.Fatalf("first snapshot mutated: expected %d, got %d", originalChangeCount, snapshot1.ChangeCount)
	}

	// Second snapshot should reflect new changes
	if snapshot2.ChangeCount != originalChangeCount+100 {
		t.Fatalf("second snapshot incorrect: expected %d, got %d", originalChangeCount+100, snapshot2.ChangeCount)
	}
}

// BenchmarkSmartPolling_ConcurrentOperations benchmarks mixed concurrent operations
func BenchmarkSmartPolling_ConcurrentOperations(b *testing.B) {
	sp := NewSmartPoller()
	numGoroutines := 20
	opsPerGoroutine := b.N / numGoroutines

	b.ResetTimer()

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				secretName := fmt.Sprintf("secret-%d", (goroutineID+op)%10)
				switch op % 4 {
				case 0:
					sp.GetNextInterval(secretName)
				case 1:
					sp.RecordChange(secretName)
				case 2:
					sp.RecordPoll(secretName)
				case 3:
					sp.GetStats(secretName)
				}
			}
		}(g)
	}
	wg.Wait()
}

// BenchmarkSmartPolling_RecordChangeConflict benchmarks multiple goroutines recording changes
func BenchmarkSmartPolling_RecordChangeConflict(b *testing.B) {
	sp := NewSmartPoller()
	numGoroutines := 10

	b.ResetTimer()

	var wg sync.WaitGroup
	opsPerGoroutine := b.N / numGoroutines

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				sp.RecordChange("contention-secret")
			}
		}(g)
	}
	wg.Wait()
}

package polling

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestCalculateInterval_Aggressive tests aggressive polling (< 2 minutes)
func TestCalculateInterval_Aggressive(t *testing.T) {
	// time.Since returns 30 seconds
	interval := CalculateInterval(30 * time.Second)
	if interval != 5*time.Second {
		t.Fatalf("expected 5s, got %v", interval)
	}
}

// TestCalculateInterval_Baseline tests baseline polling (< 10 minutes)
func TestCalculateInterval_Baseline(t *testing.T) {
	// time.Since returns 5 minutes
	interval := CalculateInterval(5 * time.Minute)
	if interval != 30*time.Second {
		t.Fatalf("expected 30s, got %v", interval)
	}
}

// TestCalculateInterval_Backoff tests backoff polling (>= 10 minutes)
func TestCalculateInterval_Backoff(t *testing.T) {
	// time.Since returns 15 minutes
	interval := CalculateInterval(15 * time.Minute)
	if interval != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", interval)
	}
}

// TestCalculateInterval_Boundary_2Minutes tests the 2-minute boundary
func TestCalculateInterval_Boundary_2Minutes(t *testing.T) {
	// Exactly 2 minutes should be baseline
	interval := CalculateInterval(2 * time.Minute)
	if interval != 30*time.Second {
		t.Fatalf("expected 30s for 2m boundary, got %v", interval)
	}

	// Just under 2 minutes should be aggressive
	interval = CalculateInterval(2*time.Minute - 1*time.Second)
	if interval != 5*time.Second {
		t.Fatalf("expected 5s for just under 2m, got %v", interval)
	}
}

// TestCalculateInterval_Boundary_10Minutes tests the 10-minute boundary
func TestCalculateInterval_Boundary_10Minutes(t *testing.T) {
	// Exactly 10 minutes should be backoff
	interval := CalculateInterval(10 * time.Minute)
	if interval != 5*time.Minute {
		t.Fatalf("expected 5m for 10m boundary, got %v", interval)
	}

	// Just under 10 minutes should be baseline
	interval = CalculateInterval(10*time.Minute - 1*time.Second)
	if interval != 30*time.Second {
		t.Fatalf("expected 30s for just under 10m, got %v", interval)
	}
}

// TestSmartPoller_UnknownSecret_DefaultsTo30s tests unknown secrets default to 30s
func TestSmartPoller_UnknownSecret_DefaultsTo30s(t *testing.T) {
	sp := NewSmartPoller()
	interval := sp.GetNextInterval("unknown-secret")
	if interval != 30*time.Second {
		t.Fatalf("expected 30s default, got %v", interval)
	}
}

// TestSmartPoller_RecordChange tests recording a change and verifying state
func TestSmartPoller_RecordChange(t *testing.T) {
	sp := NewSmartPoller()
	sp.RecordChange("my-secret")

	state := sp.GetStats("my-secret")
	if state == nil {
		t.Fatal("expected state, got nil")
	}
	if state.ChangeCount != 1 {
		t.Fatalf("expected ChangeCount=1, got %d", state.ChangeCount)
	}

	// Interval should now be aggressive (< 2 min)
	interval := sp.GetNextInterval("my-secret")
	if interval != 5*time.Second {
		t.Fatalf("expected 5s after change, got %v", interval)
	}
}

// TestSmartPoller_RecordPoll tests recording a poll and verifying state
func TestSmartPoller_RecordPoll(t *testing.T) {
	sp := NewSmartPoller()
	sp.RecordChange("my-secret")
	sp.RecordPoll("my-secret")

	state := sp.GetStats("my-secret")
	if state.PollCount != 1 {
		t.Fatalf("expected PollCount=1, got %d", state.PollCount)
	}
}

// TestSmartPoller_ConcurrentAccess tests thread-safety with concurrent operations
func TestSmartPoller_ConcurrentAccess(t *testing.T) {
	sp := NewSmartPoller()

	// 50 goroutines, each doing 10 operations
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			secretName := fmt.Sprintf("secret-%d", id%10)

			for j := 0; j < 10; j++ {
				sp.GetNextInterval(secretName)
				sp.RecordChange(secretName)
				sp.RecordPoll(secretName)
				sp.GetStats(secretName)
			}
		}(i)
	}
	wg.Wait()

	// Verify counters are reasonable (non-zero)
	for i := 0; i < 10; i++ {
		secretName := fmt.Sprintf("secret-%d", i)
		state := sp.GetStats(secretName)
		if state == nil || state.ChangeCount == 0 {
			t.Fatalf("secret-%d: expected non-zero counts", i)
		}
	}
}

// TestSmartPoller_StatePreservation tests that state is preserved across calls
func TestSmartPoller_StatePreservation(t *testing.T) {
	sp := NewSmartPoller()

	sp.RecordChange("persistent-secret")
	state1 := sp.GetStats("persistent-secret")
	firstChangeTime := state1.LastChangeTime

	time.Sleep(100 * time.Millisecond)

	sp.RecordPoll("persistent-secret")
	state2 := sp.GetStats("persistent-secret")

	// LastChangeTime should not change on poll
	if !state2.LastChangeTime.Equal(firstChangeTime) {
		t.Fatal("LastChangeTime changed unexpectedly after poll")
	}
	if state2.ChangeCount != 1 {
		t.Fatalf("ChangeCount should still be 1, got %d", state2.ChangeCount)
	}
}

// TestSmartPoller_MultipleSecretsIndependence tests that multiple secrets are tracked independently
func TestSmartPoller_MultipleSecretsIndependence(t *testing.T) {
	sp := NewSmartPoller()

	sp.RecordChange("secret-a")
	sp.RecordPoll("secret-a")

	sp.RecordChange("secret-b")
	sp.RecordPoll("secret-b")

	stateA := sp.GetStats("secret-a")
	stateB := sp.GetStats("secret-b")

	// Both should have ChangeCount = 1 and PollCount = 1
	if stateA.ChangeCount != 1 || stateB.ChangeCount != 1 {
		t.Fatalf("expected both to have ChangeCount=1, got A=%d, B=%d", stateA.ChangeCount, stateB.ChangeCount)
	}
	if stateA.PollCount != 1 || stateB.PollCount != 1 {
		t.Fatalf("expected both to have PollCount=1, got A=%d, B=%d", stateA.PollCount, stateB.PollCount)
	}

	// Both should return aggressive interval (< 2 min)
	intervalA := sp.GetNextInterval("secret-a")
	intervalB := sp.GetNextInterval("secret-b")

	if intervalA != 5*time.Second || intervalB != 5*time.Second {
		t.Fatalf("expected both intervals to be 5s, got A=%v, B=%v", intervalA, intervalB)
	}
}

// TestSmartPoller_IntervalTransition tests interval transitions as time passes
func TestSmartPoller_IntervalTransition(t *testing.T) {
	sp := NewSmartPoller()

	// Create a custom time for testing
	sp.RecordChange("test-secret")

	// Initial interval should be aggressive
	interval := sp.GetNextInterval("test-secret")
	if interval != 5*time.Second {
		t.Fatalf("expected initial interval 5s, got %v", interval)
	}

	// We can't directly test the time-based transitions without mocking time.Now(),
	// but we can verify that CalculateInterval returns correct values at specific points
	testCases := []struct {
		name        string
		duration    time.Duration
		expected    time.Duration
	}{
		{"30 seconds", 30 * time.Second, 5 * time.Second},
		{"1 minute", 1 * time.Minute, 5 * time.Second},
		{"2 minutes", 2 * time.Minute, 30 * time.Second},
		{"5 minutes", 5 * time.Minute, 30 * time.Second},
		{"10 minutes", 10 * time.Minute, 5 * time.Minute},
		{"15 minutes", 15 * time.Minute, 5 * time.Minute},
	}

	for _, tc := range testCases {
		result := CalculateInterval(tc.duration)
		if result != tc.expected {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.expected, result)
		}
	}
}

// TestSmartPoller_GetStats_ReturnsNilForUnknown tests that GetStats returns nil for unknown secrets
func TestSmartPoller_GetStats_ReturnsNilForUnknown(t *testing.T) {
	sp := NewSmartPoller()
	state := sp.GetStats("unknown-secret")
	if state != nil {
		t.Fatalf("expected nil for unknown secret, got %v", state)
	}
}

// TestSmartPoller_ZeroTimeState tests behavior when state has zero time
func TestSmartPoller_ZeroTimeState(t *testing.T) {
	sp := NewSmartPoller()
	sp.RecordPoll("zero-time-secret")

	// RecordPoll creates the state but doesn't set LastChangeTime
	// GetNextInterval should return 30s default because LastChangeTime is zero
	interval := sp.GetNextInterval("zero-time-secret")
	if interval != 30*time.Second {
		t.Fatalf("expected 30s for zero-time state, got %v", interval)
	}
}

package cache

import (
	"sync"
	"testing"
	"time"
)

func TestLatencyTracker_Percentiles(t *testing.T) {
	tr := NewLatencyTracker(10)
	for i := 1; i <= 10; i++ {
		tr.Record(time.Duration(i) * time.Millisecond)
	}

	p50 := tr.P50()
	p95 := tr.P95()

	// With 10 samples [1ms..10ms], p50 ≈ 5ms, p95 = 10ms.
	if p50 < 4*time.Millisecond || p50 > 6*time.Millisecond {
		t.Errorf("P50 out of expected range: %v", p50)
	}
	if p95 < 9*time.Millisecond {
		t.Errorf("P95 too low: %v", p95)
	}
}

func TestLatencyTracker_Empty(t *testing.T) {
	tr := NewLatencyTracker(10)
	if tr.P50() != 0 || tr.P95() != 0 {
		t.Error("expected 0 for empty tracker")
	}
}

func TestLatencyTracker_CircularOverwrite(t *testing.T) {
	tr := NewLatencyTracker(3) // window size 3
	// Fill: [1ms, 2ms, 3ms]
	tr.Record(1 * time.Millisecond)
	tr.Record(2 * time.Millisecond)
	tr.Record(3 * time.Millisecond)
	// Overwrite oldest: window becomes [4ms, 2ms, 3ms]
	tr.Record(4 * time.Millisecond)

	if tr.Count() != 3 {
		t.Errorf("expected count=3 after overflow, got %d", tr.Count())
	}
}

func TestLatencyTracker_ConcurrentRecord(t *testing.T) {
	tr := NewLatencyTracker(200)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(ms int) {
			defer wg.Done()
			tr.Record(time.Duration(ms) * time.Millisecond)
		}(i)
	}
	wg.Wait()
	// Should not panic; count ≤ 200.
	if tr.Count() > 200 {
		t.Errorf("count exceeds window size: %d", tr.Count())
	}
}

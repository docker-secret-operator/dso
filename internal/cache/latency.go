package cache

import (
	"math"
	"sort"
	"sync"
	"time"
)

const defaultWindowSize = 100

// LatencyTracker records a rolling window of durations and computes percentiles.
// It is safe for concurrent use.
type LatencyTracker struct {
	mu      sync.Mutex
	samples []time.Duration
	cap     int
	head    int // circular buffer write pointer
	count   int
}

// NewLatencyTracker creates a tracker that keeps the last windowSize samples.
func NewLatencyTracker(windowSize int) *LatencyTracker {
	if windowSize <= 0 {
		windowSize = defaultWindowSize
	}
	return &LatencyTracker{
		samples: make([]time.Duration, windowSize),
		cap:     windowSize,
	}
}

// Record adds a duration to the rolling window.
func (t *LatencyTracker) Record(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples[t.head] = d
	t.head = (t.head + 1) % t.cap
	if t.count < t.cap {
		t.count++
	}
}

// Percentile returns the p-th percentile (0–100) of the recorded samples.
// Returns 0 if no samples have been recorded.
func (t *LatencyTracker) Percentile(p float64) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.count == 0 {
		return 0
	}
	snapshot := make([]time.Duration, t.count)
	copy(snapshot, t.samples[:t.count])
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i] < snapshot[j] })

	idx := int(math.Ceil(p/100.0*float64(t.count))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(snapshot) {
		idx = len(snapshot) - 1
	}
	return snapshot[idx]
}

// P50 returns the median latency.
func (t *LatencyTracker) P50() time.Duration { return t.Percentile(50) }

// P95 returns the 95th percentile latency.
func (t *LatencyTracker) P95() time.Duration { return t.Percentile(95) }

// Count returns the number of samples recorded (capped at window size).
func (t *LatencyTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// EvalMetrics groups latency trackers for the four P10-tracked operations.
type EvalMetrics struct {
	RecommendationEval *LatencyTracker
	ForecastEval       *LatencyTracker
	DriftScan          *LatencyTracker
	ComplianceEval     *LatencyTracker
}

// NewEvalMetrics creates an EvalMetrics with 100-sample windows.
func NewEvalMetrics() *EvalMetrics {
	return &EvalMetrics{
		RecommendationEval: NewLatencyTracker(100),
		ForecastEval:       NewLatencyTracker(100),
		DriftScan:          NewLatencyTracker(100),
		ComplianceEval:     NewLatencyTracker(100),
	}
}

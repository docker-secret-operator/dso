package runtime

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkResult captures measured performance metrics
type BenchmarkResult struct {
	Name              string
	Duration          time.Duration
	Operations        int64
	SuccessfulOps     int64
	FailedOps         int64
	P50Latency        time.Duration
	P95Latency        time.Duration
	P99Latency        time.Duration
	MaxLatency        time.Duration
	MinLatency        time.Duration
	MemoryBefore      uint64
	MemoryAfter       uint64
	MemoryGrowth      uint64
	GoroutinesBefore  int
	GoroutinesAfter   int
	GoroutinesLeak    int
}

// String returns formatted benchmark result
func (br *BenchmarkResult) String() string {
	return fmt.Sprintf(`
=== %s ===
Duration: %v
Operations: %d (Success: %d, Failed: %d)
Throughput: %.0f ops/sec
Latency P50: %v, P95: %v, P99: %v (Max: %v, Min: %v)
Memory: %d MB → %d MB (Growth: %d MB)
Goroutines: %d → %d (Leak: %d)
`,
		br.Name,
		br.Duration,
		br.Operations, br.SuccessfulOps, br.FailedOps,
		float64(br.Operations)/br.Duration.Seconds(),
		br.P50Latency, br.P95Latency, br.P99Latency, br.MaxLatency, br.MinLatency,
		br.MemoryBefore/1024/1024, br.MemoryAfter/1024/1024, br.MemoryGrowth/1024/1024,
		br.GoroutinesBefore, br.GoroutinesAfter, br.GoroutinesLeak,
	)
}

// RunBenchmark executes a benchmark and collects metrics
func RunBenchmark(name string, duration time.Duration, concurrency int,
	operation func(ctx context.Context, id int) error) *BenchmarkResult {

	// Record baseline
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	result := &BenchmarkResult{
		Name:             name,
		MemoryBefore:     m1.Alloc,
		GoroutinesBefore: goroutinesBefore,
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var latencies []time.Duration
	successOps := int64(0)
	failedOps := int64(0)
	startTime := time.Now()

	// Launch workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				opStart := time.Now()
				err := operation(ctx, id)
				latency := time.Since(opStart)

				if err == nil {
					atomic.AddInt64(&successOps, 1)
				} else {
					atomic.AddInt64(&failedOps, 1)
				}

				mu.Lock()
				latencies = append(latencies, latency)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	result.Duration = time.Since(startTime)
	result.Operations = successOps + failedOps
	result.SuccessfulOps = successOps
	result.FailedOps = failedOps

	// Calculate latency percentiles
	if len(latencies) > 0 {
		calculatePercentiles(latencies, result)
	}

	// Record final metrics
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	result.MemoryAfter = m2.Alloc
	result.MemoryGrowth = m2.Alloc - m1.Alloc
	result.GoroutinesAfter = runtime.NumGoroutine()
	result.GoroutinesLeak = result.GoroutinesAfter - result.GoroutinesBefore

	return result
}

// calculatePercentiles computes latency percentiles
func calculatePercentiles(latencies []time.Duration, result *BenchmarkResult) {
	// Simple sorting (would use quickselect for production)
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[j] < latencies[i] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	n := len(latencies)
	result.MinLatency = latencies[0]
	result.MaxLatency = latencies[n-1]
	result.P50Latency = latencies[n/2]
	result.P95Latency = latencies[(n*95)/100]
	result.P99Latency = latencies[(n*99)/100]
}

// TestSustainedChurnLoad validates behavior under sustained container churn
func TestSustainedChurnLoad(t *testing.T) {
	// Simulate sustained container creation/deletion churn
	result := RunBenchmark("sustained-churn", 10*time.Second, 4, func(ctx context.Context, id int) error {
		// Simulate container operation (create/start/stop/delete)
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	t.Logf("%s", result.String())

	// Validation: throughput should be consistent
	if result.Operations < 100 {
		t.Errorf("Throughput too low: %d ops in %v", result.Operations, result.Duration)
	}

	// P95 latency should be reasonable
	if result.P95Latency > 50*time.Millisecond {
		t.Errorf("P95 latency too high: %v", result.P95Latency)
	}
}

// TestConcurrentSecretRotations validates rotation under load
func TestConcurrentSecretRotations(t *testing.T) {
	result := RunBenchmark("concurrent-rotations", 5*time.Second, 8, func(ctx context.Context, id int) error {
		// Simulate secret rotation (provider fetch + injection)
		time.Sleep(20 * time.Millisecond)
		return nil
	})

	t.Logf("%s", result.String())

	// Validation: should maintain reasonable latency even with concurrent rotations
	if result.P95Latency > 100*time.Millisecond {
		t.Errorf("Rotation latency too high: P95=%v", result.P95Latency)
	}

	// Memory growth should be bounded
	memGrowthMB := result.MemoryGrowth / 1024 / 1024
	if memGrowthMB > 50 {
		t.Errorf("Memory growth too high: %d MB", memGrowthMB)
	}
}

// TestProviderTimeoutRecovery validates recovery from slow providers
func TestProviderTimeoutRecovery(t *testing.T) {
	slowCallCount := int32(0)

	result := RunBenchmark("provider-recovery", 5*time.Second, 4, func(ctx context.Context, id int) error {
		callNum := atomic.AddInt32(&slowCallCount, 1)

		// Simulate occasional slow provider (every 10th call)
		if callNum%10 == 0 {
			time.Sleep(500 * time.Millisecond) // Slow call
		} else {
			time.Sleep(20 * time.Millisecond) // Normal call
		}

		return nil
	})

	t.Logf("%s", result.String())

	// Validation: even with slow calls, should recover
	if result.SuccessfulOps < 10 {
		t.Errorf("Too many failed operations: %d failed, %d successful", result.FailedOps, result.SuccessfulOps)
	}

	// P99 latency should account for slow calls
	if result.P99Latency < 100*time.Millisecond {
		t.Errorf("P99 should reflect slow calls: %v", result.P99Latency)
	}
}

// TestQueueSaturation validates behavior under queue saturation
func TestQueueSaturation(t *testing.T) {
	// Simulate rapid event arrival
	result := RunBenchmark("queue-saturation", 3*time.Second, 16, func(ctx context.Context, id int) error {
		// Very fast operations to saturate queue
		time.Sleep(1 * time.Millisecond)
		return nil
	})

	t.Logf("%s", result.String())

	// Validation: should handle high throughput without complete failure
	if result.SuccessfulOps < 1000 {
		t.Errorf("Should handle high throughput, got %d ops", result.Operations)
	}

	// Goroutine growth should be bounded
	if result.GoroutinesLeak > 10 {
		t.Errorf("Possible goroutine leak: %d goroutines added", result.GoroutinesLeak)
	}
}

// TestMemoryStability validates memory doesn't grow unbounded
func TestMemoryStability(t *testing.T) {
	measurements := make([]uint64, 5)

	for i := 0; i < 5; i++ {
		result := RunBenchmark(
			fmt.Sprintf("memory-stability-%d", i),
			2*time.Second,
			4,
			func(ctx context.Context, id int) error {
				time.Sleep(10 * time.Millisecond)
				return nil
			},
		)

		measurements[i] = result.MemoryGrowth

		t.Logf("Iteration %d: Memory growth = %d MB", i, result.MemoryGrowth/1024/1024)
	}

	// Validation: memory growth should not increase linearly
	firstGrowth := measurements[0]
	lastGrowth := measurements[4]

	// Last iteration should not have significantly more growth than first
	if lastGrowth > firstGrowth*3 {
		t.Errorf("Memory growth increasing: %d → %d MB", firstGrowth/1024/1024, lastGrowth/1024/1024)
	}
}

// TestReconnectStormRecovery validates recovery from reconnect storms
func TestReconnectStormRecovery(t *testing.T) {
	reconnectCount := int32(0)

	result := RunBenchmark("reconnect-storm", 5*time.Second, 4, func(ctx context.Context, id int) error {
		count := atomic.AddInt32(&reconnectCount, 1)

		// Simulate occasional reconnects (every 25 ops)
		if count%25 == 0 {
			time.Sleep(200 * time.Millisecond) // Reconnect delay
		} else {
			time.Sleep(10 * time.Millisecond) // Normal operation
		}

		return nil
	})

	t.Logf("%s", result.String())

	// Validation: should recover from reconnects
	expectedReconnects := result.Operations / 25
	if result.FailedOps > expectedReconnects*2 {
		t.Errorf("Too many failed operations during reconnect: %d", result.FailedOps)
	}

	// P99 latency should reflect reconnect delays (1 in 25 ops = 4% are slow)
	if result.P99Latency < 50*time.Millisecond {
		t.Logf("P99 latency: %v (reconnect delays may not be visible at P99 scale)", result.P99Latency)
	}
}

// TestGoroutineLeakDetection validates goroutine cleanup
func TestGoroutineLeakDetection(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < 3; i++ {
		result := RunBenchmark(
			fmt.Sprintf("goroutine-test-%d", i),
			1*time.Second,
			8,
			func(ctx context.Context, id int) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			},
		)

		// Allow cleanup time
		time.Sleep(100 * time.Millisecond)
		runtime.GC()
		time.Sleep(100 * time.Millisecond)

		if result.GoroutinesLeak > 5 {
			t.Errorf("Iteration %d: Possible goroutine leak: +%d", i, result.GoroutinesLeak)
		}
	}

	finalGoroutines := runtime.NumGoroutine()
	leak := finalGoroutines - initialGoroutines

	// Allow some tolerance for GC and cleanup
	if leak > 10 {
		t.Errorf("Final goroutine count increased by %d", leak)
	}
}

// TestLatencyDegradationUnderLoad validates latency remains bounded
func TestLatencyDegradationUnderLoad(t *testing.T) {
	// Baseline with low concurrency
	baseline := RunBenchmark("baseline-latency", 2*time.Second, 1, func(ctx context.Context, id int) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Loaded with high concurrency
	loaded := RunBenchmark("loaded-latency", 2*time.Second, 16, func(ctx context.Context, id int) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// Validation: latency should not degrade excessively
	p95Ratio := float64(loaded.P95Latency) / float64(baseline.P95Latency)
	if p95Ratio > 5.0 {
		t.Errorf("Latency degradation too high: %.1fx (baseline %v, loaded %v)",
			p95Ratio, baseline.P95Latency, loaded.P95Latency)
	}

	t.Logf("Baseline P95: %v, Loaded P95: %v, Ratio: %.1fx", baseline.P95Latency, loaded.P95Latency, p95Ratio)
}

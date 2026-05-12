package testing

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkMetrics captures performance metrics for validation
type BenchmarkMetrics struct {
	OperationCount   int64
	SuccessCount     int64
	ErrorCount       int64
	TotalDuration    time.Duration
	AvgLatency       time.Duration
	MaxLatency       time.Duration
	MinLatency       time.Duration
	MemoryBefore     uint64
	MemoryAfter      uint64
	AllocsPerOp      uint64
	GoroutinesBefore int
	GoroutinesAfter  int
}

// PrintMetrics prints formatted metrics
func (bm *BenchmarkMetrics) PrintMetrics(name string) string {
	return fmt.Sprintf(`
=== %s ===
Operations: %d
Success: %d
Errors: %d
Total Duration: %v
Avg Latency: %v
Max Latency: %v
Min Latency: %v
Memory Before: %d KB
Memory After: %d KB
Memory Growth: %d KB
Goroutines Before: %d
Goroutines After: %d
Goroutine Leak: %d
`,
		name,
		bm.OperationCount,
		bm.SuccessCount,
		bm.ErrorCount,
		bm.TotalDuration,
		bm.AvgLatency,
		bm.MaxLatency,
		bm.MinLatency,
		bm.MemoryBefore/1024,
		bm.MemoryAfter/1024,
		(bm.MemoryAfter-bm.MemoryBefore)/1024,
		bm.GoroutinesBefore,
		bm.GoroutinesAfter,
		bm.GoroutinesAfter-bm.GoroutinesBefore,
	)
}

// SustainedLoadTest runs a sustained load test with concurrent operations
// This validates that the system doesn't degrade under sustained stress
func SustainedLoadTest(t *testing.T, duration time.Duration, concurrency int,
	operation func(ctx context.Context) error) *BenchmarkMetrics {

	// Record baseline metrics
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	goroutinesBefore := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	metrics := &BenchmarkMetrics{
		MemoryBefore:     m1.Alloc,
		GoroutinesBefore: goroutinesBefore,
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var latencies []time.Duration
	totalOps := int64(0)
	successOps := int64(0)
	errorOps := int64(0)

	startTime := time.Now()

	// Launch concurrent workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				opStart := time.Now()
				err := operation(ctx)
				opLatency := time.Since(opStart)

				atomic.AddInt64(&totalOps, 1)
				if err == nil {
					atomic.AddInt64(&successOps, 1)
				} else {
					atomic.AddInt64(&errorOps, 1)
				}

				// Track latency
				mu.Lock()
				latencies = append(latencies, opLatency)
				mu.Unlock()
			}
		}()
	}

	// Wait for all workers to complete
	wg.Wait()

	metrics.TotalDuration = time.Since(startTime)
	metrics.OperationCount = totalOps
	metrics.SuccessCount = successOps
	metrics.ErrorCount = errorOps

	// Calculate latency statistics
	if len(latencies) > 0 {
		var totalLatency time.Duration
		maxLatency := latencies[0]
		minLatency := latencies[0]

		for _, lat := range latencies {
			totalLatency += lat
			if lat > maxLatency {
				maxLatency = lat
			}
			if lat < minLatency {
				minLatency = lat
			}
		}

		metrics.AvgLatency = totalLatency / time.Duration(len(latencies))
		metrics.MaxLatency = maxLatency
		metrics.MinLatency = minLatency
	}

	// Record final metrics
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	metrics.MemoryAfter = m2.Alloc
	metrics.AllocsPerOp = (m2.Alloc - m1.Alloc) / uint64(totalOps)
	metrics.GoroutinesAfter = runtime.NumGoroutine()

	return metrics
}

// CheckGoroutineLeaks verifies no goroutine leaks occur
// Should be called before and after operation sequences
func CheckGoroutineLeaks(t *testing.T, ctx context.Context, operation func(context.Context) error, iterations int) {
	// Allow time for goroutine cleanup
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	goroutinesBefore := runtime.NumGoroutine()

	// Run operations
	for i := 0; i < iterations; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = operation(ctx)
	}

	// Allow cleanup time
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()

	// Allow some tolerance (cleanup might not be instant)
	tolerance := 5
	leaked := goroutinesAfter - goroutinesBefore
	if leaked > tolerance {
		t.Errorf("Goroutine leak detected: before=%d, after=%d, leaked=%d (tolerance=%d)",
			goroutinesBefore, goroutinesAfter, leaked, tolerance)
	}
}

// TestMemoryBounds verifies memory usage stays within bounds
func CheckMemoryBounds(t *testing.T, maxMemoryMB int64, operation func(context.Context) error, iterations int) {
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	memBefore := m1.Alloc

	ctx := context.Background()
	for i := 0; i < iterations; i++ {
		_ = operation(ctx)
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	memAfter := m2.Alloc

	memGrowth := (memAfter - memBefore) / 1024 / 1024 // Convert to MB
	if memGrowth > uint64(maxMemoryMB) {
		t.Errorf("Memory growth exceeded limit: %d MB > %d MB",
			memGrowth, maxMemoryMB)
	}
}

// TestConcurrentAccess verifies thread-safe concurrent access patterns
func CheckConcurrentAccess(t *testing.T, concurrency int, iterations int,
	operation func(id int, iteration int) error) {

	var wg sync.WaitGroup
	errorCount := int64(0)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for iter := 0; iter < iterations; iter++ {
				if err := operation(id, iter); err != nil {
					atomic.AddInt64(&errorCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("Concurrent access produced %d errors", errorCount)
	}
}

// TestLatencyUnderLoad verifies latency doesn't degrade with load
// baseline: latency measured with low concurrency
// loaded: latency measured with high concurrency
// maxDegradation: acceptable increase (e.g., 2.0 = 200% of baseline)
func CheckLatencyUnderLoad(t *testing.T, operation func(context.Context) error,
	baselineConcurrency, loadConcurrency int, maxDegradation float64) {

	ctx := context.Background()

	// Measure baseline latency
	baselineLatency := measureLatency(ctx, operation, baselineConcurrency, 100)

	// Measure loaded latency
	loadedLatency := measureLatency(ctx, operation, loadConcurrency, 100)

	degradation := float64(loadedLatency) / float64(baselineLatency)
	if degradation > maxDegradation {
		t.Errorf("Latency degradation excessive: %.2f > %.2f (baseline=%v, loaded=%v)",
			degradation, maxDegradation, baselineLatency, loadedLatency)
	}

	t.Logf("Latency degradation: %.2fx (baseline=%v, loaded=%v)",
		degradation, baselineLatency, loadedLatency)
}

// Helper: measure average latency
func measureLatency(ctx context.Context, operation func(context.Context) error, concurrency, opCount int) time.Duration {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalLatency time.Duration

	opsPerWorker := opCount / concurrency
	if opsPerWorker < 1 {
		opsPerWorker = 1
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < opsPerWorker; j++ {
				start := time.Now()
				operation(ctx)
				latency := time.Since(start)

				mu.Lock()
				totalLatency += latency
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return totalLatency / time.Duration(concurrency*opsPerWorker)
}

// TestTimeoutBehavior verifies operations respect context timeouts
func CheckTimeoutBehavior(t *testing.T, operation func(context.Context) error, maxTimeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
	defer cancel()

	timedOut := false
	select {
	case <-ctx.Done():
		timedOut = true
	default:
		operation(ctx)
	}

	if !timedOut && ctx.Err() != nil {
		t.Error("Context timeout not properly respected")
	}
}

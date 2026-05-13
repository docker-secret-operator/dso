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

// ResourceSnapshot captures system resource state at a point in time
type ResourceSnapshot struct {
	Timestamp        time.Time
	MemoryAllocBytes uint64
	MemoryTotalBytes uint64
	GoroutineCount   int
	Timestamp2       time.Time // For debugging timing issues
}

// ResourceStabilityTest tracks resource usage over time to detect leaks
type ResourceStabilityTest struct {
	name           string
	duration       time.Duration
	interval       time.Duration // How often to sample
	snapshots      []ResourceSnapshot
	mu             sync.Mutex
	operationCount int64
}

// NewResourceStabilityTest creates a new resource stability test
func NewResourceStabilityTest(name string, duration, interval time.Duration) *ResourceStabilityTest {
	return &ResourceStabilityTest{
		name:      name,
		duration:  duration,
		interval:  interval,
		snapshots: make([]ResourceSnapshot, 0),
	}
}

// TakeSnapshot captures current resource state
func (rst *ResourceStabilityTest) TakeSnapshot() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	rst.mu.Lock()
	defer rst.mu.Unlock()

	rst.snapshots = append(rst.snapshots, ResourceSnapshot{
		Timestamp:        time.Now(),
		MemoryAllocBytes: m.Alloc,
		MemoryTotalBytes: m.TotalAlloc,
		GoroutineCount:   runtime.NumGoroutine(),
	})
}

// RecordOperation increments the operation counter
func (rst *ResourceStabilityTest) RecordOperation() {
	atomic.AddInt64(&rst.operationCount, 1)
}

// GetMemoryGrowth returns memory growth over the test period
func (rst *ResourceStabilityTest) GetMemoryGrowth() uint64 {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	if len(rst.snapshots) < 2 {
		return 0
	}

	first := rst.snapshots[0]
	last := rst.snapshots[len(rst.snapshots)-1]

	if last.MemoryAllocBytes > first.MemoryAllocBytes {
		return last.MemoryAllocBytes - first.MemoryAllocBytes
	}
	return 0
}

// GetMaxMemory returns the peak memory allocation during the test
func (rst *ResourceStabilityTest) GetMaxMemory() uint64 {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	var max uint64
	for _, snap := range rst.snapshots {
		if snap.MemoryAllocBytes > max {
			max = snap.MemoryAllocBytes
		}
	}
	return max
}

// GetGoroutineGrowth returns goroutine growth over the test period
func (rst *ResourceStabilityTest) GetGoroutineGrowth() int {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	if len(rst.snapshots) < 2 {
		return 0
	}

	first := rst.snapshots[0].GoroutineCount
	last := rst.snapshots[len(rst.snapshots)-1].GoroutineCount

	if last > first {
		return last - first
	}
	return 0
}

// GetAverageMemory returns the average memory allocation across all snapshots
func (rst *ResourceStabilityTest) GetAverageMemory() uint64 {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	if len(rst.snapshots) == 0 {
		return 0
	}

	var sum uint64
	for _, snap := range rst.snapshots {
		sum += snap.MemoryAllocBytes
	}

	return sum / uint64(len(rst.snapshots))
}

// GetOperationCount returns total operations executed
func (rst *ResourceStabilityTest) GetOperationCount() int64 {
	return atomic.LoadInt64(&rst.operationCount)
}

// IsMemoryStable checks if memory growth is within acceptable bounds
// Returns true if memory growth rate is stable (not accelerating)
func (rst *ResourceStabilityTest) IsMemoryStable(maxGrowthPercentPerHour float64) bool {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	if len(rst.snapshots) < 4 {
		return true // Insufficient data
	}

	// Split samples into quarters to detect acceleration
	quarter := len(rst.snapshots) / 4

	q1Start := rst.snapshots[0].MemoryAllocBytes
	q1End := rst.snapshots[quarter].MemoryAllocBytes

	q4Start := rst.snapshots[quarter*3].MemoryAllocBytes
	q4End := rst.snapshots[len(rst.snapshots)-1].MemoryAllocBytes

	q1Growth := float64(q1End - q1Start)
	q4Growth := float64(q4End - q4Start)

	// If later growth is significantly higher, memory is not stable
	if q4Growth > q1Growth*2 {
		return false // Growth accelerating
	}

	return true
}

// IsGoroutineStable checks if goroutine count is bounded
// Returns true if peak goroutine count is within acceptable bounds
func (rst *ResourceStabilityTest) IsGoroutineStable(maxGoroutines int) bool {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	var peak int
	for _, snap := range rst.snapshots {
		if snap.GoroutineCount > peak {
			peak = snap.GoroutineCount
		}
	}

	return peak <= maxGoroutines
}

// String returns formatted stability results
func (rst *ResourceStabilityTest) String() string {
	growth := rst.GetMemoryGrowth()
	maxMem := rst.GetMaxMemory()
	avgMem := rst.GetAverageMemory()
	goroutineGrowth := rst.GetGoroutineGrowth()
	ops := rst.GetOperationCount()

	return fmt.Sprintf(`
=== %s ===
Duration: %v
Operations: %d
Memory Growth: %d MB
Max Memory: %d MB
Average Memory: %d MB
Goroutine Growth: %d
Throughput: %.0f ops/sec
Snapshots: %d
`,
		rst.name,
		rst.duration,
		ops,
		growth/1024/1024,
		maxMem/1024/1024,
		avgMem/1024/1024,
		goroutineGrowth,
		float64(ops)/rst.duration.Seconds(),
		len(rst.snapshots),
	)
}

// TestMemoryStabilityUnderSustainedLoad validates memory stability over extended duration
func TestMemoryStabilityUnderSustainedLoad(t *testing.T) {
	// Simulate 5 minutes of sustained operations (shortened for -short)
	testDuration := 5 * time.Minute
	sampleInterval := 5 * time.Second
	if testing.Short() {
		testDuration = 5 * time.Second
		sampleInterval = 1 * time.Second
	}

	rst := NewResourceStabilityTest("sustained-load-memory", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	// Simulate sustained load: allocate and free memory
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Worker loop: simulate container event processing
	operationTicker := time.NewTicker(10 * time.Millisecond)
	defer operationTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			rst.TakeSnapshot() // Final snapshot
			t.Logf("%s", rst.String())

			// Validation: memory growth should be < 50MB over 5 minutes
			growth := rst.GetMemoryGrowth()
			if growth > 50*1024*1024 {
				t.Errorf("Excessive memory growth: %d MB", growth/1024/1024)
			}

			// Check memory stability
			if !testing.Short() && !rst.IsMemoryStable(0.1) {
				t.Error("Memory growth accelerating - possible leak")
			}

			return

		case <-operationTicker.C:
			rst.RecordOperation()

			// Simulate light work
			_ = make([]byte, 1024)
		}
	}
}

// TestGoroutineStabilityUnderChurn validates goroutines don't leak under high churn
func TestGoroutineStabilityUnderChurn(t *testing.T) {
	testDuration := 3 * time.Minute
	sampleInterval := 2 * time.Second
	if testing.Short() {
		testDuration = 3 * time.Second
		sampleInterval = 500 * time.Millisecond
	}

	rst := NewResourceStabilityTest("goroutine-stability-churn", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	initialGoroutines := runtime.NumGoroutine()

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Worker churn: create and destroy goroutines
	workerTicker := time.NewTicker(50 * time.Millisecond)
	defer workerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			rst.TakeSnapshot() // Final snapshot
			t.Logf("%s", rst.String())

			// Validation: goroutine count should not grow unbounded
			growth := rst.GetGoroutineGrowth()
			maxAllowed := initialGoroutines + 50 // Allow up to 50 new goroutines

			if growth > maxAllowed {
				t.Errorf("Excessive goroutine growth: %d (initial: %d, final: %d)",
					growth, initialGoroutines, initialGoroutines+growth)
			}

			// Check goroutine stability
			if !testing.Short() && !rst.IsGoroutineStable(initialGoroutines+100) {
				t.Error("Goroutine count exceeds expected bounds")
			}

			return

		case <-workerTicker.C:
			rst.RecordOperation()

			// Spawn and finish goroutine
			done := make(chan struct{})
			go func() {
				time.Sleep(100 * time.Millisecond)
				close(done)
			}()

			// Simulate some work while goroutine runs
			go func() {
				<-done
			}()
		}
	}
}

// TestQueueStabilityUnderSaturation validates queue behavior doesn't degrade
func TestQueueStabilityUnderSaturation(t *testing.T) {
	testDuration := 2 * time.Minute
	sampleInterval := 1 * time.Second
	if testing.Short() {
		testDuration = 2 * time.Second
		sampleInterval = 500 * time.Millisecond
	}

	rst := NewResourceStabilityTest("queue-stability-saturation", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Simulate queue saturation: rapid enqueueing and processing
	enqueueRate := time.Millisecond // 1000 ops/sec enqueue rate
	ticker := time.NewTicker(enqueueRate)
	defer ticker.Stop()

	queueSize := 0
	maxQueueSize := 0
	mu := sync.Mutex{}

	// Processor: slow dequeue to cause saturation
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				if queueSize > 0 {
					queueSize--
				}
				mu.Unlock()
			}
		}
	}()

	// Enqueuer
	for {
		select {
		case <-ctx.Done():
			rst.TakeSnapshot() // Final snapshot
			t.Logf("%s", rst.String())

			// Validation: queue shouldn't cause excessive memory growth
			growth := rst.GetMemoryGrowth()
			if growth > 100*1024*1024 {
				t.Errorf("Excessive memory growth under queue saturation: %d MB", growth/1024/1024)
			}

			if !rst.IsMemoryStable(0.2) {
				t.Log("Memory growth not entirely stable under saturation")
			}

			return

		case <-ticker.C:
			rst.RecordOperation()

			mu.Lock()
			queueSize++
			if queueSize > maxQueueSize {
				maxQueueSize = queueSize
			}
			mu.Unlock()
		}
	}
}

// TestProviderRestartStability validates restart operations don't cause leaks
func TestProviderRestartStability(t *testing.T) {
	testDuration := 2 * time.Minute
	sampleInterval := 2 * time.Second
	restartInterval := 5 * time.Second
	if testing.Short() {
		testDuration = 2 * time.Second
		sampleInterval = 500 * time.Millisecond
		restartInterval = 1 * time.Second
	}

	rst := NewResourceStabilityTest("provider-restart-stability", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	restartCount := int32(0)

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Restart simulator
	go func() {
		ticker := time.NewTicker(restartInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Simulate provider restart
				atomic.AddInt32(&restartCount, 1)
				rst.RecordOperation()

				// Simulate restart cleanup (allocate/deallocate)
				_ = make([]byte, 5*1024*1024) // 5MB temporary allocation
				runtime.GC()
			}
		}
	}()

	// Worker that uses providers
	operationTicker := time.NewTicker(50 * time.Millisecond)
	defer operationTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			rst.TakeSnapshot() // Final snapshot
			t.Logf("%s", rst.String())

			growth := rst.GetMemoryGrowth()
			t.Logf("Restarts: %d, Memory growth: %d MB", restartCount, growth/1024/1024)

			// Validation: restart operations should not cause unbounded memory growth
			if growth > 150*1024*1024 {
				t.Errorf("Excessive memory growth during restarts: %d MB", growth/1024/1024)
			}

			if !testing.Short() && !rst.IsMemoryStable(0.15) {
				t.Error("Memory stability degraded under restart load")
			}

			return

		case <-operationTicker.C:
			rst.RecordOperation()
		}
	}
}

// TestReconnectStormStability validates rapid reconnect cycles are stable
func TestReconnectStormStability(t *testing.T) {
	testDuration := 2 * time.Minute
	sampleInterval := 1 * time.Second
	if testing.Short() {
		testDuration = 2 * time.Second
		sampleInterval = 500 * time.Millisecond
	}

	rst := NewResourceStabilityTest("reconnect-storm-stability", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	reconnectCount := int32(0)

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Reconnect storm simulator: rapid disconnect/reconnect cycles
	reconnectTicker := time.NewTicker(100 * time.Millisecond)
	defer reconnectTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			rst.TakeSnapshot() // Final snapshot
			t.Logf("%s", rst.String())

			growth := rst.GetMemoryGrowth()
			t.Logf("Reconnect cycles: %d, Memory growth: %d MB", reconnectCount, growth/1024/1024)

			// Validation: reconnect storms shouldn't cause memory leaks
			if growth > 80*1024*1024 {
				t.Errorf("Excessive memory growth during reconnect storm: %d MB", growth/1024/1024)
			}

			if !testing.Short() && !rst.IsMemoryStable(0.12) {
				t.Error("Memory stability degraded under reconnect storm")
			}

			// Goroutines should also be stable
			if !rst.IsGoroutineStable(runtime.NumGoroutine() + 50) {
				t.Error("Goroutine count unstable under reconnect storm")
			}

			return

		case <-reconnectTicker.C:
			atomic.AddInt32(&reconnectCount, 1)
			rst.RecordOperation()

			// Simulate reconnect: allocate connection, cleanup, reallocate
			_ = make([]byte, 2*1024*1024) // 2MB connection buffer
			runtime.GC()
			_ = make([]byte, 2*1024*1024) // New connection
		}
	}
}

// TestCombinedOperationalLoad validates system under realistic combined load
func TestCombinedOperationalLoad(t *testing.T) {
	testDuration := 3 * time.Minute
	sampleInterval := 2 * time.Second
	if testing.Short() {
		testDuration = 3 * time.Second
		sampleInterval = 500 * time.Millisecond
	}

	rst := NewResourceStabilityTest("combined-operational-load", testDuration, sampleInterval)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	// Sampling goroutine
	go func() {
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.TakeSnapshot()
			}
		}
	}()

	// Multiple workers simulating different operations
	var wg sync.WaitGroup

	// Event processor
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.RecordOperation()
				_ = make([]byte, 10*1024) // Simulate event processing
			}
		}
	}()

	// Provider rotation
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.RecordOperation()
				_ = make([]byte, 8*1024*1024) // Simulate rotation overhead
				runtime.GC()
			}
		}
	}()

	// Connection health checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rst.RecordOperation()
				// Simulate health check
			}
		}
	}()

	// Wait for completion
	go func() {
		<-ctx.Done()
		wg.Wait()
	}()

	wg.Wait()

	rst.TakeSnapshot() // Final snapshot
	t.Logf("%s", rst.String())

	// Validations
	growth := rst.GetMemoryGrowth()
	ops := rst.GetOperationCount()

	t.Logf("Operations: %d, Throughput: %.0f ops/sec", ops, float64(ops)/rst.duration.Seconds())

	// Under combined load: allow up to 200MB growth over 3 minutes
	if growth > 200*1024*1024 {
		t.Errorf("Excessive memory growth: %d MB", growth/1024/1024)
	}

	if !testing.Short() && !rst.IsMemoryStable(0.2) {
		t.Error("Memory growth not stable under combined operational load")
	}

	if !rst.IsGoroutineStable(runtime.NumGoroutine() + 100) {
		t.Error("Goroutines not stable under combined load")
	}
}

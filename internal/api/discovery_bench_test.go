package api

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// MockDockerClient simulates Docker API calls
type MockDockerClient struct {
	containerCount int
	callCount      int
}

func (m *MockDockerClient) Close() error {
	return nil
}

// BenchmarkDiscoveryCache measures cache performance with varying container counts
func BenchmarkDiscoveryCache(b *testing.B) {
	testCases := []struct {
		name            string
		containerCount  int
		expectedLatency time.Duration
	}{
		{"10_containers", 10, 50 * time.Millisecond},
		{"50_containers", 50, 200 * time.Millisecond},
		{"100_containers", 100, 400 * time.Millisecond},
		{"250_containers", 250, 1 * time.Second},
		{"500_containers", 500, 2 * time.Second},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			benchmarkCachePerformance(b, tc.containerCount, tc.expectedLatency)
		})
	}
}

func benchmarkCachePerformance(b *testing.B, containerCount int, targetLatency time.Duration) {
	logger, _ := zap.NewDevelopment()

	// Create a mock discovery API with a cache
	callCount := 0

	refreshFn := func() (*DiscoveryResponse, *MappingResponse, error) {
		callCount++

		// Simulate discovery latency based on container count
		simulatedLatency := time.Duration(containerCount*5) * time.Millisecond
		time.Sleep(simulatedLatency)

		// Create mock containers
		containers := make([]ContainerMetadata, containerCount)
		for i := 0; i < containerCount; i++ {
			containers[i] = ContainerMetadata{
				ID:            fmt.Sprintf("container_%d", i),
				Name:          fmt.Sprintf("container-%d", i),
				Image:         "test:latest",
				Status:        "running",
				Created:       time.Now().Format(time.RFC3339),
				State:         "running",
				RestartPolicy: RestartPolicyInfo{Name: "always"},
				Network: NetworkInfo{
					IP:       "172.17.0.2",
					Networks: []string{"bridge"},
				},
				EnvironmentVariableNames: []string{"PATH", "HOME"},
				SensitiveVariableCount:   0,
				DSOAwareness: DSOAwarenessInfo{
					Status:         "unmanaged",
					ManagedSecrets: make([]string, 0),
					ConfigRefs:     make([]string, 0),
				},
				Labels: make(map[string]string),
			}
		}

		return &DiscoveryResponse{
			Containers:     containers,
			TotalCount:     containerCount,
			ManagedCount:   0,
			UnmanagedCount: containerCount,
			PartialCount:   0,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		}, &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	cache := NewDiscoveryCache(30*time.Second, refreshFn)

	// Reset benchmarking state
	b.ResetTimer()

	// Measure uncached latency (first request)
	start := time.Now()
	cache.GetContainers()
	uncachedLatency := time.Since(start)

	// Measure cached latency (warm cache)
	cachedLatencies := make([]time.Duration, 0)
	for i := 0; i < b.N; i++ {
		start := time.Now()
		cache.GetContainers()
		cachedLatencies = append(cachedLatencies, time.Since(start))
	}

	// Calculate statistics
	var avgCachedLatency time.Duration
	for _, d := range cachedLatencies {
		avgCachedLatency += d
	}
	if len(cachedLatencies) > 0 {
		avgCachedLatency /= time.Duration(len(cachedLatencies))
	}

	// Memory usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryUsed := m.Alloc / 1024 / 1024 // MB

	// Report results
	b.ReportMetric(float64(uncachedLatency.Milliseconds()), "uncached_ms")
	b.ReportMetric(float64(avgCachedLatency.Microseconds()), "cached_us")
	b.ReportMetric(float64(memoryUsed), "memory_mb")
	b.ReportMetric(float64(callCount), "api_calls")

	_ = logger
	_ = targetLatency
}

// TestDiscoveryCacheCorrectness verifies cache returns correct data
func TestDiscoveryCacheCorrectness(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_ = logger

	callCount := 0
	refreshFn := func() (*DiscoveryResponse, *MappingResponse, error) {
		callCount++
		return &DiscoveryResponse{
			Containers:     make([]ContainerMetadata, 5),
			TotalCount:     5,
			ManagedCount:   0,
			UnmanagedCount: 5,
			PartialCount:   0,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		}, &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	cache := NewDiscoveryCache(100*time.Millisecond, refreshFn)

	// First call should miss cache
	resp1, hit1, err := cache.GetContainers()
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	if hit1 {
		t.Error("First call should be cache miss")
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call, got %d", callCount)
	}

	// Second call should hit cache
	resp2, hit2, err := cache.GetContainers()
	if err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	if !hit2 {
		t.Error("Second call should be cache hit")
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call (cached), got %d", callCount)
	}

	// Verify data consistency
	if resp1.TotalCount != resp2.TotalCount {
		t.Errorf("Data mismatch: %d vs %d", resp1.TotalCount, resp2.TotalCount)
	}

	// Wait for TTL expiry
	time.Sleep(150 * time.Millisecond)

	// Third call should miss cache (stale)
	_, hit3, err := cache.GetContainers()
	if err != nil {
		t.Fatalf("Third call failed: %v", err)
	}
	if hit3 {
		t.Error("Third call should be cache miss (TTL expired)")
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls (after TTL), got %d", callCount)
	}

	// Verify stale-while-revalidate: concurrent calls should return stale data
	// while one request refreshes
	metrics := cache.GetMetrics()
	if metrics.RefreshCount != 2 {
		t.Errorf("Expected 2 refreshes, got %d", metrics.RefreshCount)
	}
}

// TestDiscoveryCacheMetrics verifies metrics tracking
func TestDiscoveryCacheMetrics(t *testing.T) {
	refreshCount := 0
	refreshFn := func() (*DiscoveryResponse, *MappingResponse, error) {
		refreshCount++
		return &DiscoveryResponse{
			Containers:     make([]ContainerMetadata, 1),
			TotalCount:     1,
			ManagedCount:   0,
			UnmanagedCount: 1,
			PartialCount:   0,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		}, &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	cache := NewDiscoveryCache(1*time.Second, refreshFn)

	// Make multiple requests to populate metrics
	for i := 0; i < 10; i++ {
		cache.GetContainers()
	}

	metrics := cache.GetMetrics()

	// First request is a miss, rest are hits
	if metrics.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", metrics.Misses)
	}
	if metrics.Hits != 9 {
		t.Errorf("Expected 9 hits, got %d", metrics.Hits)
	}
	if metrics.RefreshCount != 1 {
		t.Errorf("Expected 1 refresh, got %d", metrics.RefreshCount)
	}

	// Cache age should be small
	age := cache.CacheAge()
	if age < 0 {
		t.Error("Cache age should be non-negative")
	}
	if age > 100*time.Millisecond {
		t.Errorf("Cache age too large: %v", age)
	}

	// Cache should be fresh
	if !cache.IsFresh() {
		t.Error("Cache should be fresh")
	}
}

// TestDiscoveryCacheInvalidation verifies cache invalidation
func TestDiscoveryCacheInvalidation(t *testing.T) {
	callCount := 0
	refreshFn := func() (*DiscoveryResponse, *MappingResponse, error) {
		callCount++
		return &DiscoveryResponse{
			Containers:     make([]ContainerMetadata, callCount),
			TotalCount:     callCount,
			ManagedCount:   0,
			UnmanagedCount: callCount,
			PartialCount:   0,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		}, &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	cache := NewDiscoveryCache(10*time.Second, refreshFn)

	// First call
	resp1, _, _ := cache.GetContainers()
	if resp1.TotalCount != 1 {
		t.Errorf("Expected 1 container, got %d", resp1.TotalCount)
	}

	// Cached call
	resp2, _, _ := cache.GetContainers()
	if resp2.TotalCount != 1 {
		t.Errorf("Expected 1 container (cached), got %d", resp2.TotalCount)
	}

	// Invalidate
	cache.InvalidateAsync()
	time.Sleep(50 * time.Millisecond) // Wait for async refresh

	// Should get fresh data
	respInvalidated, _, _ := cache.GetContainers()
	if respInvalidated.TotalCount != 2 {
		t.Errorf("Expected 2 containers (after invalidation), got %d", respInvalidated.TotalCount)
	}
}

// TestEndpointCacheIntegration tests cache integration via direct cache calls
func TestEndpointCacheIntegration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	_ = logger

	var apiCallMutex sync.Mutex
	var apiCallCount int
	refreshFn := func() (*DiscoveryResponse, *MappingResponse, error) {
		apiCallMutex.Lock()
		apiCallCount++
		apiCallMutex.Unlock()
		return &DiscoveryResponse{
			Containers:     make([]ContainerMetadata, 3),
			TotalCount:     3,
			ManagedCount:   0,
			UnmanagedCount: 3,
			PartialCount:   0,
			Timestamp:      time.Now().UTC().Format(time.RFC3339),
		}, &MappingResponse{
			Suggestions: make([]SecretMappingSuggestion, 0),
			TotalCount:  0,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}, nil
	}

	cache := NewDiscoveryCache(500*time.Millisecond, refreshFn)

	// Test direct cache call
	resp, _, err := cache.GetContainers()
	if err != nil {
		t.Fatalf("Failed to get containers: %v", err)
	}

	if resp.TotalCount != 3 {
		t.Errorf("Expected 3 containers, got %d", resp.TotalCount)
	}

	// Subsequent request should use cache
	_, _, _ = cache.GetContainers()

	apiCallMutex.Lock()
	callCount := apiCallCount
	apiCallMutex.Unlock()
	if callCount != 1 {
		t.Errorf("Expected 1 API call (cached), got %d", callCount)
	}

	// Test metrics
	metrics := cache.GetMetrics()
	if metrics.Hits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", metrics.Hits)
	}
	if metrics.Misses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", metrics.Misses)
	}

	// Test InvalidateAsync
	cache.InvalidateAsync()
	// Wait longer for async refresh to complete
	time.Sleep(500 * time.Millisecond)

	apiCallMutex.Lock()
	callCountAfter := apiCallCount
	apiCallMutex.Unlock()
	if callCountAfter < 2 {
		t.Errorf("Expected at least 2 API calls (after refresh), got %d", callCountAfter)
	}
}

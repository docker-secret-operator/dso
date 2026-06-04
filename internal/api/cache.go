package api

import (
	"sync"
	"time"
)

// CacheMetrics tracks cache performance
type CacheMetrics struct {
	Hits          int64
	Misses        int64
	RefreshCount  int64
	LastRefresh   time.Time
	RefreshLatency time.Duration
}

// DiscoveryCacheEntry holds cached data with timestamp
type DiscoveryCacheEntry struct {
	ContainersResponse *DiscoveryResponse
	MappingsResponse   *MappingResponse
	CreatedAt          time.Time
	RefreshInProgress  bool
}

// DiscoveryCache provides thread-safe caching for discovery responses
type DiscoveryCache struct {
	mu              sync.RWMutex
	entry           *DiscoveryCacheEntry
	ttl             time.Duration
	metrics         CacheMetrics
	refreshCallback func() (*DiscoveryResponse, *MappingResponse, error)
}

// NewDiscoveryCache creates a new discovery cache with configurable TTL
func NewDiscoveryCache(ttl time.Duration, refreshCallback func() (*DiscoveryResponse, *MappingResponse, error)) *DiscoveryCache {
	if ttl == 0 {
		ttl = 30 * time.Second // default TTL
	}
	return &DiscoveryCache{
		ttl:             ttl,
		metrics:         CacheMetrics{},
		refreshCallback: refreshCallback,
	}
}

// GetContainers returns cached containers or fetches fresh data
func (dc *DiscoveryCache) GetContainers() (*DiscoveryResponse, bool, error) {
	dc.mu.RLock()
	if dc.entry != nil && !dc.isStale() {
		dc.metrics.Hits++
		defer dc.mu.RUnlock()
		return dc.entry.ContainersResponse, true, nil
	}
	dc.mu.RUnlock()

	// Cache miss or stale - refresh
	containers, _, err := dc.refresh(true)
	return containers, false, err
}

// GetMappings returns cached mappings or fetches fresh data
func (dc *DiscoveryCache) GetMappings() (*MappingResponse, bool, error) {
	dc.mu.RLock()
	if dc.entry != nil && !dc.isStale() {
		dc.metrics.Hits++
		defer dc.mu.RUnlock()
		return dc.entry.MappingsResponse, true, nil
	}
	dc.mu.RUnlock()

	// Cache miss or stale - refresh
	_, mapping, err := dc.refresh(true)
	return mapping, false, err
}

// refresh fetches fresh data and updates cache
func (dc *DiscoveryCache) refresh(recordMetrics bool) (*DiscoveryResponse, *MappingResponse, error) {
	dc.mu.Lock()
	// Check if another goroutine is already refreshing (stale-while-revalidate)
	if dc.entry != nil && dc.entry.RefreshInProgress {
		defer dc.mu.Unlock()
		// Return stale data while refresh is in progress
		if recordMetrics {
			dc.metrics.Hits++
		}
		return dc.entry.ContainersResponse, dc.entry.MappingsResponse, nil
	}
	dc.entry = &DiscoveryCacheEntry{RefreshInProgress: true}
	dc.mu.Unlock()

	// Fetch fresh data (outside lock)
	start := time.Now()
	containers, mappings, err := dc.refreshCallback()
	latency := time.Since(start)

	dc.mu.Lock()
	defer dc.mu.Unlock()

	if recordMetrics {
		dc.metrics.Misses++
		dc.metrics.RefreshCount++
		dc.metrics.LastRefresh = time.Now()
		dc.metrics.RefreshLatency = latency
	}

	if err != nil {
		return nil, nil, err
	}

	dc.entry = &DiscoveryCacheEntry{
		ContainersResponse: containers,
		MappingsResponse:   mappings,
		CreatedAt:          time.Now(),
		RefreshInProgress:  false,
	}

	return containers, mappings, nil
}

// InvalidateAndRefresh invalidates cache and refreshes immediately
func (dc *DiscoveryCache) InvalidateAndRefresh() (*DiscoveryResponse, *MappingResponse, error) {
	dc.mu.Lock()
	dc.entry = nil
	dc.mu.Unlock()

	return dc.refresh(true)
}

// InvalidateAsync invalidates cache and refreshes in background
func (dc *DiscoveryCache) InvalidateAsync() {
	dc.mu.Lock()
	dc.entry = nil
	dc.mu.Unlock()

	go func() {
		dc.refresh(true)
	}()
}

// isStale checks if cached data is older than TTL
func (dc *DiscoveryCache) isStale() bool {
	if dc.entry == nil || dc.entry.CreatedAt.IsZero() {
		return true
	}
	return time.Since(dc.entry.CreatedAt) > dc.ttl
}

// GetMetrics returns cache performance metrics
func (dc *DiscoveryCache) GetMetrics() CacheMetrics {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	metrics := dc.metrics

	// Add age of current cache entry
	if dc.entry != nil && !dc.entry.CreatedAt.IsZero() {
		age := time.Since(dc.entry.CreatedAt)
		if age > dc.ttl {
			// Cache is stale, age is "stale since"
			metrics.RefreshLatency = age - dc.ttl
		}
	}

	return metrics
}

// SetTTL updates the cache TTL
func (dc *DiscoveryCache) SetTTL(ttl time.Duration) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.ttl = ttl
}

// CacheAge returns how long the current cache entry has been cached
func (dc *DiscoveryCache) CacheAge() time.Duration {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	if dc.entry == nil || dc.entry.CreatedAt.IsZero() {
		return -1 // No cache
	}

	return time.Since(dc.entry.CreatedAt)
}

// IsFresh checks if cache is fresh
func (dc *DiscoveryCache) IsFresh() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return !dc.isStale()
}

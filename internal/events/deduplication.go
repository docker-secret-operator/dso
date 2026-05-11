package events

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// EventsDeduped tracks events suppressed by deduplication
	EventsDeduped = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "dso_events_deduped_total",
			Help: "Total number of duplicate events suppressed by deduplication",
		},
	)

	// DedupeCacheSize tracks current size of deduplication cache
	DedupeCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "dso_dedup_cache_size",
			Help: "Current number of entries in deduplication cache",
		},
	)
)

// EventFingerprint uniquely identifies an event for deduplication
type EventFingerprint string

// ComputeFingerprint creates a fingerprint for an event
// Fingerprint includes container ID and action, but NOT timestamp
// This allows deduplication of identical events replayed during reconnect
func ComputeFingerprint(msg events.Message) EventFingerprint {
	// Use MD5 hash of (containerID + action)
	// This captures the semantic identity of the event
	data := fmt.Sprintf("%s:%s", msg.Actor.ID, msg.Action)
	hash := md5.Sum([]byte(data))
	return EventFingerprint(hex.EncodeToString(hash[:]))
}

// DedupCache provides short-lived event deduplication
// Prevents replay amplification during daemon restarts
type DedupCache struct {
	mu      sync.RWMutex
	cache   map[EventFingerprint]time.Time // fingerprint -> expiration time
	ttl     time.Duration
	maxSize int
}

// NewDedupCache creates a new deduplication cache
// ttl: how long to remember events (default: 30 seconds)
// maxSize: maximum entries before cleanup (default: 1000)
func NewDedupCache(ttl time.Duration, maxSize int) *DedupCache {
	if ttl < time.Second {
		ttl = time.Second
	}
	if maxSize < 100 {
		maxSize = 100
	}

	dc := &DedupCache{
		cache:   make(map[EventFingerprint]time.Time),
		ttl:     ttl,
		maxSize: maxSize,
	}

	// Start cleanup goroutine
	go dc.cleanupLoop()

	return dc
}

// IsDuplicate checks if an event is a duplicate
// Returns true if event was recently seen (duplicate)
// Returns false if event is new (not a duplicate)
func (dc *DedupCache) IsDuplicate(msg events.Message) bool {
	fp := ComputeFingerprint(msg)

	dc.mu.Lock()
	defer dc.mu.Unlock()

	now := time.Now()

	// Clean expired entries inline
	for key, expTime := range dc.cache {
		if expTime.Before(now) {
			delete(dc.cache, key)
		}
	}

	// Check for duplicate
	if expTime, exists := dc.cache[fp]; exists && expTime.After(now) {
		EventsDeduped.Inc()
		return true // Duplicate
	}

	// Record new event
	dc.cache[fp] = now.Add(dc.ttl)
	dc.updateMetrics()

	return false // New event
}

// Clear removes all entries from the cache
func (dc *DedupCache) Clear() {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	dc.cache = make(map[EventFingerprint]time.Time)
	DedupeCacheSize.Set(0)
}

// GetStats returns cache statistics
func (dc *DedupCache) GetStats() map[string]interface{} {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	expired := 0
	now := time.Now()
	for _, expTime := range dc.cache {
		if expTime.Before(now) {
			expired++
		}
	}

	return map[string]interface{}{
		"total_entries": len(dc.cache),
		"expired":       expired,
		"active":        len(dc.cache) - expired,
		"ttl":           dc.ttl.String(),
		"max_size":      dc.maxSize,
	}
}

// cleanupLoop periodically removes expired entries
func (dc *DedupCache) cleanupLoop() {
	ticker := time.NewTicker(dc.ttl)
	defer ticker.Stop()

	for range ticker.C {
		dc.mu.Lock()

		now := time.Now()
		expired := 0

		// Remove all expired entries
		for key, expTime := range dc.cache {
			if expTime.Before(now) {
				delete(dc.cache, key)
				expired++
			}
		}

		// If cache exceeds max size, remove oldest entries
		if len(dc.cache) > dc.maxSize {
			// Find oldest entries and remove them
			entriesToRemove := len(dc.cache) - dc.maxSize
			for key, _ := range dc.cache {
				if entriesToRemove <= 0 {
					break
				}
				delete(dc.cache, key)
				entriesToRemove--
			}
		}

		dc.updateMetrics()
		dc.mu.Unlock()
	}
}

// updateMetrics updates prometheus metrics (must be called with lock held)
func (dc *DedupCache) updateMetrics() {
	DedupeCacheSize.Set(float64(len(dc.cache)))
}

// ImmediateDedup provides immediate deduplication without TTL
// Useful for detecting duplicate operations in single stream
type ImmediateDedup struct {
	mu       sync.Mutex
	seen     map[EventFingerprint]bool
	maxSize  int
	overflow bool
}

// NewImmediateDedup creates a new immediate deduplication tracker
func NewImmediateDedup(maxSize int) *ImmediateDedup {
	if maxSize < 100 {
		maxSize = 100
	}

	return &ImmediateDedup{
		seen:    make(map[EventFingerprint]bool),
		maxSize: maxSize,
	}
}

// IsDuplicate checks if event is duplicate in immediate tracker
func (id *ImmediateDedup) IsDuplicate(msg events.Message) bool {
	fp := ComputeFingerprint(msg)

	id.mu.Lock()
	defer id.mu.Unlock()

	if id.seen[fp] {
		return true // Duplicate
	}

	// Add if not at capacity
	if len(id.seen) < id.maxSize {
		id.seen[fp] = true
	} else {
		id.overflow = true
	}

	return false
}

// Reset clears the immediate tracker
func (id *ImmediateDedup) Reset() {
	id.mu.Lock()
	defer id.mu.Unlock()

	id.seen = make(map[EventFingerprint]bool)
	id.overflow = false
}

// GetSize returns current number of tracked events
func (id *ImmediateDedup) GetSize() int {
	id.mu.Lock()
	defer id.mu.Unlock()

	return len(id.seen)
}

// HasOverflowed returns true if tracker exceeded max size
func (id *ImmediateDedup) HasOverflowed() bool {
	id.mu.Lock()
	defer id.mu.Unlock()

	return id.overflow
}

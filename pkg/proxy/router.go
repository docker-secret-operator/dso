package proxy

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Router selects the next backend for a given service using a lock-free
// per-service round-robin counter. The algorithm:
//
//  1. Fetch the current backend snapshot from the registry (one RLock).
//  2. Atomically increment the service counter (no lock needed).
//  3. Use modulo to index into the snapshot.
//
// This guarantees:
//   - No starvation: every backend receives approximately equal traffic.
//   - No blocking: the hot path only hits an atomic add + modulo.
//   - Live adjustments: adding/removing backends takes effect on the next call
//     because the snapshot is fetched fresh each time.
type Router struct {
	registry *Registry

	// mu guards the counters map itself (not the counter values).
	// Counter values are updated atomically without the lock.
	mu       sync.RWMutex
	counters map[string]*uint64 // service → round-robin counter
}

// NewRouter creates a Router that selects backends from the given registry.
func NewRouter(registry *Registry) *Router {
	return &Router{
		registry: registry,
		counters: make(map[string]*uint64),
	}
}

// Next returns the next backend for the given service using round-robin.
//
// Errors:
//   - Returns an error if no backends are registered for the service. Callers
//     should surface this as a 503-equivalent and not drop the connection
//     silently.
func (r *Router) Next(service string) (*Backend, error) {
	backends := r.registry.Backends(service)
	if len(backends) == 0 {
		return nil, fmt.Errorf("router: no backends available for service %q", service)
	}

	// Atomically increment and derive the index. Using uint64 wrap-around is
	// intentional: 2^64 connections per service before reset is acceptable.
	counter := r.getOrCreateCounter(service)
	n := atomic.AddUint64(counter, 1) - 1 // subtract 1 to get zero-based index
	return backends[int(n)%len(backends)], nil
}

// getOrCreateCounter returns the atomic counter for the given service,
// creating it if it does not yet exist. Double-checked locking is used to
// avoid unnecessary write-lock acquisitions on the hot path.
func (r *Router) getOrCreateCounter(service string) *uint64 {
	r.mu.RLock()
	c, ok := r.counters[service]
	r.mu.RUnlock()
	if ok {
		return c
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Re-check after upgrading to write lock (another goroutine may have
	// created the counter between the RUnlock and Lock above).
	if c, ok = r.counters[service]; ok {
		return c
	}
	var zero uint64
	r.counters[service] = &zero
	return &zero
}

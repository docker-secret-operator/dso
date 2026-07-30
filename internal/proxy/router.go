package proxy

import (
	"fmt"
	"sync/atomic"
)

// Router selects the next backend using a lock-free per-registry atomic
// round-robin counter. Draining backends are skipped automatically.
//
// The hot path (Next) acquires no mutex — it only fetches a snapshot from the
// registry (one RLock) and does an atomic add on the counter.
type Router struct {
	registry *Registry
	counter  atomic.Uint64 // all accesses go through atomic.Uint64 methods
}

// NewRouter creates a Router backed by the given registry.
func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
}

// Next returns the next non-draining backend using round-robin selection.
//
// Errors:
//   - Returns an error if no active (non-draining) backends are available.
//     Callers must not silently drop the connection — log and close it.
func (r *Router) Next() (*Backend, error) {
	active := r.registry.Active()
	if len(active) == 0 {
		return nil, fmt.Errorf("router: no active backends available")
	}

	n := r.counter.Add(1) - 1
	// Compute the modulo in uint64 space before narrowing to int for the
	// slice index -- converting the (unbounded, ever-growing) counter value
	// itself to int first could overflow into a negative number on a 64-bit
	// platform, and active[negative] panics. The modulo result is always
	// < len(active), which safely fits in an int.
	idx := int(n % uint64(len(active))) // #nosec G115 -- result is always < len(active), which fits in an int; see comment above
	b := &active[idx]

	// Increment the per-backend request counter in the registry.
	r.registry.incrRequests(b.ID)

	return b, nil
}

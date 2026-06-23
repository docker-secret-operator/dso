// Package cache provides a TTL-based evaluation cache for P8 recommendations
// and P9 forecasts. Both evaluators are O(secrets × drift) per call; caching
// prevents that cost from landing on every HTTP request.
//
// Cache invalidation is explicit:
//   - Rotation event    → InvalidateRecs + InvalidateForecasts
//   - Drift update      → InvalidateRecs + InvalidateForecasts
//   - Policy change     → InvalidateRecs
package cache

import (
	"context"
	"sync"
	"time"
)

const DefaultTTL = 30 * time.Second

// Entry is a typed, TTL-bounded cache slot.
// It is safe for concurrent use.
type Entry[T any] struct {
	mu        sync.RWMutex
	value     T
	updatedAt time.Time
	evalDur   time.Duration // how long the last evaluation took
	ttl       time.Duration
	valid     bool
}

// NewEntry creates an Entry with the given TTL.
func NewEntry[T any](ttl time.Duration) *Entry[T] {
	return &Entry[T]{ttl: ttl}
}

// Get returns the cached value and whether it is still fresh.
func (e *Entry[T]) Get() (T, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.valid || time.Since(e.updatedAt) > e.ttl {
		var zero T
		return zero, false
	}
	return e.value, true
}

// Set stores a new value and records how long the evaluation took.
func (e *Entry[T]) Set(v T, evalDur time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.value = v
	e.updatedAt = time.Now().UTC()
	e.evalDur = evalDur
	e.valid = true
}

// Invalidate marks the entry stale so the next Get triggers a re-evaluation.
func (e *Entry[T]) Invalidate() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.valid = false
}

// LastUpdated returns the time of the last successful Set.
func (e *Entry[T]) LastUpdated() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.updatedAt
}

// LastEvalDuration returns how long the most recent evaluation took.
func (e *Entry[T]) LastEvalDuration() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.evalDur
}

// IsStale returns true if the entry has never been set or its TTL has expired.
func (e *Entry[T]) IsStale() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.valid || time.Since(e.updatedAt) > e.ttl
}

// GetOrCompute returns the cached value if fresh, otherwise calls fn to produce
// a new value, stores it, and returns it. Concurrent callers that arrive while
// fn is running will wait for the result (single-flight via the write lock).
func (e *Entry[T]) GetOrCompute(ctx context.Context, fn func(ctx context.Context) T) T {
	// Fast path: value is fresh.
	if v, ok := e.Get(); ok {
		return v
	}

	// Slow path: recompute under write lock.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock — another goroutine may have
	// computed and stored a fresh value while we were waiting.
	if e.valid && time.Since(e.updatedAt) <= e.ttl {
		return e.value
	}

	start := time.Now()
	v := fn(ctx)
	dur := time.Since(start)

	e.value = v
	e.updatedAt = time.Now().UTC()
	e.evalDur = dur
	e.valid = true

	return v
}

package auth

import (
	"sync"
	"time"
)

const (
	DefaultMaxFailures    = 5
	DefaultWindowDuration = 15 * time.Minute
)

// RateLimiter is an in-memory sliding-window rate limiter for login attempts.
// It tracks failures per key (IP or username) and rejects requests when the
// threshold is exceeded within the window.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string][]time.Time
	maxFails int
	window   time.Duration
}

// NewRateLimiter creates a limiter with the given failure threshold and window.
func NewRateLimiter(maxFails int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string][]time.Time),
		maxFails: maxFails,
		window:   window,
	}
}

// IsLimited returns true when the key has >= maxFails failures within the window.
// It also removes stale entries older than the window.
func (rl *RateLimiter) IsLimited(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.prune(key)
	return len(rl.buckets[key]) >= rl.maxFails
}

// RecordFailure adds a failure timestamp for the given key.
func (rl *RateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.buckets[key] = append(rl.buckets[key], time.Now())
}

// Reset clears all failure records for the given key (on successful login).
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, key)
}

// prune removes entries older than the window. Caller must hold rl.mu.
func (rl *RateLimiter) prune(key string) {
	cutoff := time.Now().Add(-rl.window)
	entries := rl.buckets[key]
	i := 0
	for i < len(entries) && entries[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		rl.buckets[key] = entries[i:]
	}
}

// IPKey returns the rate-limiter key for an IP address.
func IPKey(ip string) string { return "ip:" + ip }

// UsernameKey returns the rate-limiter key for a username.
func UsernameKey(username string) string { return "user:" + username }

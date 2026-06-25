package server

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipRateLimiter holds a per-IP token-bucket limiter and the time it was last
// seen, so the cleanup goroutine can evict stale entries.
type ipRateLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimitMiddleware enforces a per-source-IP request rate limit.
// Each IP gets its own token bucket: ratePerSecond tokens replenished per
// second, with an initial burst of burstSize. Requests that exceed the budget
// receive 429 Too Many Requests immediately.
//
// A background goroutine evicts entries that have been idle for more than
// cleanupInterval to prevent unbounded memory growth.
type rateLimitMiddleware struct {
	mu              sync.Mutex
	limiters        map[string]*ipRateLimiter
	ratePerSecond   rate.Limit
	burstSize       int
	cleanupInterval time.Duration
}

func newRateLimitMiddleware(rps rate.Limit, burst int) *rateLimitMiddleware {
	m := &rateLimitMiddleware{
		limiters:        make(map[string]*ipRateLimiter),
		ratePerSecond:   rps,
		burstSize:       burst,
		cleanupInterval: 5 * time.Minute,
	}
	go m.cleanupLoop()
	return m
}

func (m *rateLimitMiddleware) getLimiter(ip string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.limiters[ip]
	if !ok {
		entry = &ipRateLimiter{
			limiter: rate.NewLimiter(m.ratePerSecond, m.burstSize),
		}
		m.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (m *rateLimitMiddleware) cleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		cutoff := time.Now().Add(-m.cleanupInterval)
		for ip, entry := range m.limiters {
			if entry.lastSeen.Before(cutoff) {
				delete(m.limiters, ip)
			}
		}
		m.mu.Unlock()
	}
}

func (m *rateLimitMiddleware) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrades are long-lived connections — skip per-request limiting.
		if r.URL.Path == "/api/events/ws" {
			next.ServeHTTP(w, r)
			return
		}

		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		if !m.getLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

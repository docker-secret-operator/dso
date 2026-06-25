package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/time/rate"
)

func TestRateLimitMiddleware_AllowsUnderLimit(t *testing.T) {
	rl := newRateLimitMiddleware(rate.Limit(100), 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.wrap(inner)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestRateLimitMiddleware_BlocksWhenExceeded(t *testing.T) {
	// Limit of 1 token, burst of 1 — second request must be blocked.
	rl := newRateLimitMiddleware(rate.Limit(1), 1)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.wrap(inner)

	first := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/events", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusOK {
		t.Fatalf("first request should pass, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/events", nil)
	req2.RemoteAddr = "10.0.0.1:9999"
	handler.ServeHTTP(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should be rate-limited (429), got %d", second.Code)
	}
}

func TestRateLimitMiddleware_PerIPIsolation(t *testing.T) {
	// Burst of 1: IP-A exhausts its budget; IP-B must still be allowed.
	rl := newRateLimitMiddleware(rate.Limit(1), 1)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.wrap(inner)

	send := func(ip string) int {
		req := httptest.NewRequest("GET", "/api/secrets", nil)
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// Exhaust IP-A's budget.
	send("1.2.3.4")
	if got := send("1.2.3.4"); got != http.StatusTooManyRequests {
		t.Errorf("IP-A second request: expected 429, got %d", got)
	}

	// IP-B should still be allowed.
	if got := send("5.6.7.8"); got != http.StatusOK {
		t.Errorf("IP-B first request: expected 200, got %d (rate limiter not per-IP)", got)
	}
}

func TestRateLimitMiddleware_WebSocketExempt(t *testing.T) {
	// WebSocket upgrade path must bypass rate limiting entirely.
	rl := newRateLimitMiddleware(rate.Limit(0), 0) // zero budget — everything blocked
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.wrap(inner)

	req := httptest.NewRequest("GET", "/api/events/ws", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("WebSocket path should bypass rate limiter, got %d", w.Code)
	}
}

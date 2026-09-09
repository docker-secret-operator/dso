package webuiauth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// maxLoginFailures is the number of failed login attempts allowed from a
	// single client IP within loginFailureWindow before further attempts are
	// throttled.
	maxLoginFailures = 5

	// loginFailureWindow is the sliding window during which failures are
	// counted. A key that has gone this long since its last failure is
	// treated as recovered.
	loginFailureWindow = 15 * time.Minute

	// loginLimiterCleanupInterval mirrors rateLimitMiddleware's cleanup
	// cadence (internal/server/ratelimit.go) so stale per-IP entries don't
	// grow the map unboundedly.
	loginLimiterCleanupInterval = 5 * time.Minute
)

// loginAttempts tracks failed login attempts for one client key (IP).
type loginAttempts struct {
	failures    int
	windowStart time.Time
	lastSeen    time.Time
}

// loginLimiter is a bounded, concurrency-safe tracker of failed WebUI login
// attempts, keyed by client IP rather than username -- this is a
// single-operator model with exactly one valid username, so keying by
// username would let an off-path attacker lock out the real operator simply
// by submitting failed logins for their name from anywhere.
//
// It follows the same self-cleaning map + periodic sweep pattern as
// internal/server/ratelimit.go's rateLimitMiddleware, kept here in
// webuiauth because this is auth-domain policy (credential stuffing
// protection), not generic HTTP throttling.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempts

	// now is overridable for tests.
	now func() time.Time
}

func newLoginLimiter() *loginLimiter {
	l := &loginLimiter{
		attempts: make(map[string]*loginAttempts),
		now:      time.Now,
	}
	go l.cleanupLoop()
	return l
}

// allowed reports whether a login attempt from key may proceed (i.e. run
// bcrypt at all). It does not itself record anything -- callers record the
// outcome via recordFailure/recordSuccess once the attempt has been made.
func (l *loginLimiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	a, ok := l.attempts[key]
	if !ok {
		return true
	}
	if l.now().Sub(a.windowStart) > loginFailureWindow {
		// Window has elapsed; treat as recovered.
		delete(l.attempts, key)
		return true
	}
	return a.failures < maxLoginFailures
}

// recordFailure increments the failure count for key, starting a new window
// if none is active or the previous one has expired.
func (l *loginLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	a, ok := l.attempts[key]
	if !ok || now.Sub(a.windowStart) > loginFailureWindow {
		a = &loginAttempts{windowStart: now}
		l.attempts[key] = a
	}
	a.failures++
	a.lastSeen = now
}

// recordSuccess resets the failure counter for key on a successful login.
func (l *loginLimiter) recordSuccess(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func (l *loginLimiter) cleanupLoop() {
	ticker := time.NewTicker(loginLimiterCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		cutoff := time.Now().Add(-loginLimiterCleanupInterval)
		for key, a := range l.attempts {
			if a.lastSeen.Before(cutoff) {
				delete(l.attempts, key)
			}
		}
		l.mu.Unlock()
	}
}

// clientKeyFromRequest extracts the client IP from a request's RemoteAddr,
// matching the extraction logic used by internal/server/ratelimit.go.
func clientKeyFromRequest(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ErrTooManyLoginAttempts is returned by LoginFromRequest when the calling
// IP has exceeded maxLoginFailures within loginFailureWindow. bcrypt is not
// run in this case, so the attack retains its full per-attempt cost.
var ErrTooManyLoginAttempts = &loginRateLimitError{}

// loginRateLimitError is a distinct type (rather than a plain sentinel
// wrapping ErrInvalidCredentials) so callers can distinguish "throttled"
// from "bad credentials" for status-code purposes, while the HTTP response
// body sent to the client stays identical in shape to a normal failed
// login -- it must not reveal whether the throttling was username-specific.
type loginRateLimitError struct{}

func (e *loginRateLimitError) Error() string {
	return "webuiauth: too many failed login attempts"
}

// LoginFromRequest wraps Login with per-client-IP failure throttling. On
// success it resets the IP's failure counter. On failure it records the
// failure and returns ErrInvalidCredentials as before. If the IP has
// already exceeded the failure budget, it returns ErrTooManyLoginAttempts
// without calling bcrypt at all -- so the request has no more compute cost
// than any other rejected request, actually blunting the credential
// stuffing attack rather than just labeling it.
func (m *Manager) LoginFromRequest(r *http.Request, username, password string) (*Session, error) {
	key := clientKeyFromRequest(r)

	if !m.loginLimiter().allowed(key) {
		return nil, ErrTooManyLoginAttempts
	}

	sess, err := m.Login(username, password)
	if err != nil {
		m.loginLimiter().recordFailure(key)
		return nil, err
	}
	m.loginLimiter().recordSuccess(key)
	return sess, nil
}

// loginLimiter lazily initializes m.limiter under m.mu so existing callers
// that construct Manager via NewManager (the only exported constructor)
// don't need any changes -- the limiter is an implementation detail of
// LoginFromRequest, not part of Manager's public construction contract.
func (m *Manager) loginLimiter() *loginLimiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limiter == nil {
		m.limiter = newLoginLimiter()
		// Route through m.now (rather than snapshotting it) so tests that
		// override Manager.now after construction still get a deterministic
		// clock for throttling window math, matching the pattern already
		// used for session expiry.
		m.limiter.now = func() time.Time { return m.now() }
	}
	return m.limiter
}

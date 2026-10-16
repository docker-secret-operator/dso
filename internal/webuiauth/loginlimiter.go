package webuiauth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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

// SetTrustedProxies configures the reverse proxies (IPs or CIDRs) whose
// X-Forwarded-For header is honored for login throttling. Without it every
// client behind a proxy shares the proxy's IP, so five failed logins from
// anyone would lock the real operator out. Entries that do not parse are
// rejected so a typo cannot silently widen or narrow trust.
func (m *Manager) SetTrustedProxies(entries []string) error {
	nets := make([]*net.IPNet, 0, len(entries))
	var ignored []*net.IPNet
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.Contains(e, "/") {
			ip := net.ParseIP(e)
			if ip == nil {
				return fmt.Errorf("invalid trusted proxy %q", e)
			}
			bits := 128
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
				bits = 32
			}
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(e)
		if err != nil {
			return fmt.Errorf("invalid trusted proxy %q: %w", e, err)
		}
		if ones, _ := n.Mask.Size(); ones == 0 {
			// A /0 entry is never trusted. It identifies no proxy, and it is NOT
			// a safe "trust everyone": net.IPNet.Contains never matches an IPv4
			// address against an IPv6 network (or vice versa), so with 0.0.0.0/0
			// a client escapes the trusted set by putting IPv6 hops in
			// X-Forwarded-For (and with ::/0, IPv4 hops) and then picks its own
			// throttle identity. Ignoring the entry keeps throttling keyed on the
			// connection address, which a client cannot choose.
			ignored = append(ignored, n)
			continue
		}
		nets = append(nets, n)
	}
	warnings := trustedProxyRangeWarnings(nets, ignored)
	m.mu.Lock()
	m.trustedProxies = nets
	m.trustedProxyWarnings = warnings
	m.mu.Unlock()
	return nil
}

// Ranges wider than this are reported by TrustedProxyWarnings: more than 256
// IPv4 addresses (shorter than /24) or more than 256 IPv6 addresses (shorter
// than /120).
const (
	broadTrustedIPv4Prefix = 24
	broadTrustedIPv6Prefix = 120
)

// trustedProxyRangeWarnings flags entries that are probably not "just the
// reverse proxy". A trusted peer's X-Forwarded-For is believed, so every host
// inside a listed range can pick its own login-throttle identity; the safe
// configuration lists the exact proxy address(es).
func trustedProxyRangeWarnings(nets, ignored []*net.IPNet) []string {
	var out []string
	for _, n := range ignored {
		out = append(out, fmt.Sprintf(
			"webui.trusted_proxies entry %s matches EVERY address and is IGNORED: it identifies no proxy, and trusting "+
				"every address would let clients choose their own login-throttle identity. X-Forwarded-For is not used "+
				"for it. List your reverse proxy's exact address(es) instead.", n))
	}
	for _, n := range nets {
		ones, bits := n.Mask.Size()
		switch {
		case (bits == 32 && ones < broadTrustedIPv4Prefix) || (bits == 128 && ones < broadTrustedIPv6Prefix):
			out = append(out, fmt.Sprintf(
				"webui.trusted_proxies entry %s is a broad range: any host inside it can choose its own login-throttle "+
					"identity via X-Forwarded-For and so evade the per-client limit. Prefer the exact proxy address(es) "+
					"(e.g. 10.0.0.5) over a network range.", n))
		}
	}
	return out
}

// TrustedProxyWarnings returns the warnings produced by the last
// SetTrustedProxies call (empty when every entry is narrow). Callers log them
// at startup; they are advisory and never change behavior.
func (m *Manager) TrustedProxyWarnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.trustedProxyWarnings...)
}

// isTrustedProxy requires m.mu to be held.
func (m *Manager) isTrustedProxy(ip net.IP) bool {
	for _, n := range m.trustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientKey returns the throttling key for r. The socket peer is used unless
// it is a configured trusted proxy, in which case X-Forwarded-For is walked
// right-to-left and the first address that is not itself a trusted proxy is
// used (the rightmost entries are appended by infrastructure we trust; the
// leftmost are client-controlled and spoofable). Any parse problem falls back
// to the socket peer.
func (m *Manager) clientKey(r *http.Request) string {
	peer := clientKeyFromRequest(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	peerIP := net.ParseIP(peer)
	if len(m.trustedProxies) == 0 || peerIP == nil || !m.isTrustedProxy(peerIP) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			return peer
		}
		if !m.isTrustedProxy(ip) {
			return ip.String()
		}
	}
	return peer
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
	key := m.clientKey(r)

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

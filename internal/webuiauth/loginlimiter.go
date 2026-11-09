package webuiauth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// maxLoginFailures is the number of failed login attempts allowed from a
	// single client key (an IPv4 address or an IPv6 /64) within
	// loginFailureWindow before further attempts are throttled.
	maxLoginFailures = 5

	// loginFailureWindow is the sliding window during which failures are
	// counted. A key that has gone this long since its first counted failure
	// is treated as recovered.
	loginFailureWindow = 15 * time.Minute

	// loginLimiterCleanupInterval mirrors rateLimitMiddleware's cleanup
	// cadence (internal/server/ratelimit.go) so stale per-key entries don't
	// grow the map unboundedly.
	loginLimiterCleanupInterval = 5 * time.Minute

	// loginIPv6PrefixBits is the IPv6 aggregation prefix. A single customer or
	// host is normally delegated a /64 (SLAAC requires it), so an attacker can
	// cheaply rotate through 2^64 addresses inside it; counting per address
	// would give them an unlimited guessing budget. IPv4 is counted per
	// address.
	loginIPv6PrefixBits = 64

	// Global backstop. Per-key accounting cannot stop an attacker who owns many
	// prefixes, so the number of password verifications (bcrypt runs) that may
	// start across ALL clients is also bounded: a sustained rate of one per
	// globalLoginInterval with a burst of globalLoginBurst. That is ~450
	// guesses per 15 minutes in total, versus unbounded without it. A
	// successful login refunds its token. Trade-off: during a sustained
	// distributed attack the real operator may also see HTTP 429 until the
	// bucket refills; their own successful logins cost nothing.
	globalLoginInterval = 2 * time.Second
	globalLoginBurst    = 20
)

// loginAttempts tracks one client key.
type loginAttempts struct {
	failures    int       // counted failures in the current window
	inflight    int       // reserved attempts whose outcome is not yet known
	windowStart time.Time // start of the current failure window
	lastSeen    time.Time
}

// loginLimiter is a bounded, concurrency-safe tracker of login attempts keyed
// by client key rather than username -- this is a single-operator model with
// exactly one valid username, so keying by username would let an off-path
// attacker lock out the real operator.
//
// Accounting is by reservation: reserve() atomically checks AND claims a slot
// (failures + inflight < maxLoginFailures) before any expensive password
// verification starts, and finish() resolves it. Check-then-record would let
// N concurrent requests all pass the check before the first failure is
// recorded, multiplying the budget by the burst size.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempts
	global   *rate.Limiter

	// now is overridable for tests.
	now func() time.Time
}

func newLoginLimiter() *loginLimiter {
	l := &loginLimiter{
		attempts: make(map[string]*loginAttempts),
		global:   rate.NewLimiter(rate.Every(globalLoginInterval), globalLoginBurst),
		now:      time.Now,
	}
	go l.cleanupLoop()
	return l
}

// outcome of a reserved attempt.
type loginOutcome int

const (
	// outcomeNeutral releases the reservation without counting anything: the
	// attempt did not test the credential (internal error, panic).
	outcomeNeutral loginOutcome = iota
	outcomeFailure
	outcomeSuccess
)

// reservation is a claimed slot. Exactly one finish call must follow.
type reservation struct {
	l    *loginLimiter
	key  string
	glob *rate.Reservation
	done bool
}

// reserve claims an attempt for key. It returns nil when the key is over its
// budget (counting attempts still in flight) or the global bucket is empty. No
// state is changed in the denied case, so denied requests cost nothing.
func (l *loginLimiter) reserve(key string) *reservation {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	a, ok := l.attempts[key]
	if ok && a.failures > 0 && now.Sub(a.windowStart) > loginFailureWindow {
		a.failures = 0 // window elapsed: recovered
	}
	if ok && a.failures+a.inflight >= maxLoginFailures {
		return nil
	}
	g := l.global.ReserveN(now, 1)
	if !g.OK() || g.DelayFrom(now) > 0 {
		g.CancelAt(now)
		return nil
	}
	if !ok {
		a = &loginAttempts{}
		l.attempts[key] = a
	}
	if a.failures == 0 {
		a.windowStart = now
	}
	a.inflight++
	a.lastSeen = now
	return &reservation{l: l, key: key, glob: g}
}

// finish resolves the reservation. Safe to call more than once (later calls
// are no-ops), which makes `defer r.finish(outcomeNeutral)` a correct panic
// safety net.
func (r *reservation) finish(o loginOutcome) {
	if r == nil {
		return
	}
	l := r.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	now := l.now()
	a := l.attempts[r.key]
	if a == nil { // swept (cannot happen while inflight > 0, but stay safe)
		a = &loginAttempts{windowStart: now}
		l.attempts[r.key] = a
	}
	if a.inflight > 0 {
		a.inflight--
	}
	a.lastSeen = now
	switch o {
	case outcomeFailure:
		if a.failures == 0 {
			a.windowStart = now
		}
		a.failures++
	case outcomeSuccess:
		a.failures = 0
		r.glob.CancelAt(now) // a correct password is not a guess: refund the global token
	case outcomeNeutral:
		r.glob.CancelAt(now)
	}
	if a.failures == 0 && a.inflight == 0 {
		delete(l.attempts, r.key)
	}
}

func (l *loginLimiter) cleanupLoop() {
	ticker := time.NewTicker(loginLimiterCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		cutoff := l.now().Add(-loginLimiterCleanupInterval)
		for key, a := range l.attempts {
			if a.inflight == 0 && a.lastSeen.Before(cutoff) {
				delete(l.attempts, key)
			}
		}
		l.mu.Unlock()
	}
}

// throttleKey normalizes a client address into the accounting key: IPv4 (and
// IPv4-mapped IPv6) per address, IPv6 per /loginIPv6PrefixBits prefix.
// Anything that is not an IP is used verbatim (it cannot be attacker-chosen
// beyond what the socket layer reports).
func throttleKey(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return addr
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	masked := ip.Mask(net.CIDRMask(loginIPv6PrefixBits, 128))
	return fmt.Sprintf("%s/%d", masked, loginIPv6PrefixBits)
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
		return throttleKey(peer)
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			return throttleKey(peer)
		}
		if !m.isTrustedProxy(ip) {
			return throttleKey(ip.String())
		}
	}
	return throttleKey(peer)
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

// LoginFromRequest wraps Login with failure throttling per client key (IPv4
// address or IPv6 /64) plus a global bound on password verifications.
//
// An attempt is RESERVED before bcrypt runs, so concurrent requests cannot
// all pass a budget check before any failure is recorded. The reservation is
// resolved as follows:
//
//   - wrong credentials: counted as a failure for the key;
//   - correct credentials: the key's failure counter is reset and the global
//     token refunded;
//   - any other outcome (internal error, panic): released without counting,
//     because the credential was not actually tested.
//
// Over budget, it returns ErrTooManyLoginAttempts without running bcrypt, so
// a throttled request costs no more than any other rejected request.
func (m *Manager) LoginFromRequest(r *http.Request, username, password string) (*Session, error) {
	res := m.loginLimiter().reserve(m.clientKey(r))
	if res == nil {
		return nil, ErrTooManyLoginAttempts
	}
	defer res.finish(outcomeNeutral) // panic safety net; no-op after an explicit finish

	sess, err := m.Login(username, password)
	switch {
	case err == nil:
		res.finish(outcomeSuccess)
	case err == ErrInvalidCredentials:
		res.finish(outcomeFailure)
	default:
		res.finish(outcomeNeutral)
	}
	return sess, err
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

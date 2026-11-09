// Package webuiauth implements a minimal, scoped session-auth layer for the
// browser-facing WebUI. It is deliberately NOT a general user-auth system:
// there is exactly one operator identity (configured in dso.yaml under
// webui.username / webui.password_hash), sessions live in an in-memory map
// (no database), and tokens are handed to the browser only via an HttpOnly,
// Secure, SameSite=Strict cookie -- never in a JSON response body and never
// intended for localStorage.
//
// This package is entirely separate from internal/auth, which continues to
// protect the non-browser REST API (--api-addr) via DSO_AUTH_TOKEN. Neither
// mechanism weakens the other.
package webuiauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName is the name of the session cookie set on successful login.
	CookieName = "dso_webui_session"

	// DefaultIdleTimeout is used when no session_idle_timeout is configured.
	DefaultIdleTimeout = 30 * time.Minute

	// DefaultMaxLifetime is the absolute session lifetime applied when no
	// session_max_lifetime is configured. Unlike the idle timeout it is not
	// extended by activity, so a stolen-but-kept-alive session still dies.
	DefaultMaxLifetime = 24 * time.Hour

	// MaxConcurrentSessions caps the number of live sessions kept for the
	// single operator identity. This is a reasonable ceiling for multiple
	// browser tabs/devices, not a multi-user quota. When exceeded, the
	// oldest session (by CreatedAt) is evicted -- never the session that
	// was just created.
	MaxConcurrentSessions = 10
)

var (
	ErrInvalidCredentials = errors.New("webuiauth: invalid username or password")
	ErrNoSession          = errors.New("webuiauth: missing or invalid session")
	ErrSessionExpired     = errors.New("webuiauth: session expired")
)

// Session is one authenticated browser session for the single operator
// identity.
type Session struct {
	// Token is a live bearer credential and must never be serialized to
	// JSON -- it is only ever handed to the browser via the HttpOnly
	// Set-Cookie header (see SessionCookie), never in a response body.
	Token      string `json:"-"`
	Username   string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Manager holds the single operator credential and the in-memory session
// table. It is safe for concurrent use.
type Manager struct {
	username     string
	passwordHash string
	idleTimeout  time.Duration
	// maxLifetime is the absolute lifetime measured from CreatedAt; zero
	// disables the cap.
	maxLifetime time.Duration
	// trustedProxies are the networks whose X-Forwarded-For is honored for
	// login throttling (see SetTrustedProxies). Empty means never trust it.
	trustedProxies []*net.IPNet
	// trustedProxyWarnings describes risky entries in trustedProxies (see
	// TrustedProxyWarnings).
	trustedProxyWarnings []string
	// Secure controls whether issued cookies set the Secure attribute.
	// Should be true in production (TLS); tests over plain HTTP set it
	// false.
	Secure bool

	mu       sync.Mutex
	sessions map[string]*Session

	// limiter tracks failed login attempts per client IP for
	// LoginFromRequest (see loginlimiter.go). Lazily initialized.
	limiter *loginLimiter

	// now is overridable for tests.
	now func() time.Time
}

// NewManager builds a Manager for the single configured operator identity.
// passwordHash must be a bcrypt hash (see HashPassword). If idleTimeout is
// zero, DefaultIdleTimeout is used.
func NewManager(username, passwordHash string, idleTimeout time.Duration) *Manager {
	if idleTimeout <= 0 {
		idleTimeout = DefaultIdleTimeout
	}
	return &Manager{
		username:     username,
		passwordHash: passwordHash,
		idleTimeout:  idleTimeout,
		maxLifetime:  DefaultMaxLifetime,
		Secure:       true,
		sessions:     make(map[string]*Session),
		now:          time.Now,
	}
}

// SetMaxLifetime sets the absolute session lifetime. Zero disables the cap.
// Must be called before the Manager serves requests.
func (m *Manager) SetMaxLifetime(d time.Duration) {
	if d < 0 {
		d = 0
	}
	m.mu.Lock()
	m.maxLifetime = d
	m.mu.Unlock()
}

// compareHash is bcrypt verification; a variable so tests can count how many
// verifications actually start.
var compareHash = bcrypt.CompareHashAndPassword

// HashPassword bcrypt-hashes a plaintext password for storage in
// webui.password_hash.
func HashPassword(plaintext string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Login verifies the submitted credential against the single configured
// operator identity and, on success, creates a new session and returns it.
func (m *Manager) Login(username, password string) (*Session, error) {
	if username == "" || password == "" || m.username == "" || m.passwordHash == "" {
		return nil, ErrInvalidCredentials
	}
	// Constant-time-ish: always run bcrypt compare regardless of username
	// match, to avoid trivially timing out valid usernames.
	userOK := username == m.username
	pwErr := compareHash([]byte(m.passwordHash), []byte(password))
	if !userOK || pwErr != nil {
		return nil, ErrInvalidCredentials
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}

	now := m.now()
	sess := &Session{
		Token:      token,
		Username:   username,
		CreatedAt:  now,
		LastSeenAt: now,
	}

	m.mu.Lock()
	m.sessions[token] = sess
	if len(m.sessions) > MaxConcurrentSessions {
		m.evictOldestLocked(token)
	}
	m.mu.Unlock()

	return sess, nil
}

// evictOldestLocked removes the oldest session (by CreatedAt) other than
// keepToken. Callers must hold m.mu.
func (m *Manager) evictOldestLocked(keepToken string) {
	var oldestToken string
	var oldestAt time.Time
	for token, sess := range m.sessions {
		if token == keepToken {
			continue
		}
		if oldestToken == "" || sess.CreatedAt.Before(oldestAt) {
			oldestToken = token
			oldestAt = sess.CreatedAt
		}
	}
	if oldestToken != "" {
		delete(m.sessions, oldestToken)
	}
}

// Validate checks the session token, refreshing its idle timer on success.
// Expired sessions are removed and rejected.
func (m *Manager) Validate(token string) (*Session, error) {
	if token == "" {
		return nil, ErrNoSession
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[token]
	if !ok {
		return nil, ErrNoSession
	}

	now := m.now()
	if now.Sub(sess.LastSeenAt) > m.idleTimeout {
		delete(m.sessions, token)
		return nil, ErrSessionExpired
	}
	if m.maxLifetime > 0 && now.Sub(sess.CreatedAt) > m.maxLifetime {
		delete(m.sessions, token)
		return nil, ErrSessionExpired
	}

	sess.LastSeenAt = now
	return sess, nil
}

// Logout invalidates the given session token, if present.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// ListSessions returns every currently active session for the single
// operator identity, oldest first. Since there is only one operator, each
// entry typically represents a distinct browser/device login rather than a
// distinct user. Callers must not expose Session.Token to the client -- it
// is a live bearer credential.
func (m *Manager) ListSessions() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]*Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		out = append(out, sess)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// LogoutOthers invalidates every session except the one matching keepToken
// and returns the number of sessions removed. Used by the "sign out other
// sessions" control -- it never signs out the caller's own session.
func (m *Manager) LogoutOthers(keepToken string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0
	for token := range m.sessions {
		if token != keepToken {
			delete(m.sessions, token)
			removed++
		}
	}
	return removed
}

// SessionCookie builds the Set-Cookie header value for a session:
// HttpOnly, SameSite=Strict, and Secure per m.Secure. The cookie lifetime is
// the idle timeout, capped by what remains of the absolute lifetime, so the
// browser never keeps a cookie the server would already reject. It is
// re-issued on each authenticated request (see internal/server) so that
// activity extends the cookie exactly as it extends the server-side idle
// timer.
func (m *Manager) SessionCookie(sess *Session) *http.Cookie {
	m.mu.Lock()
	maxLifetime := m.maxLifetime
	now := m.now()
	m.mu.Unlock()

	life := m.idleTimeout
	if maxLifetime > 0 {
		if remaining := maxLifetime - now.Sub(sess.CreatedAt); remaining < life {
			life = remaining
		}
	}
	maxAge := int(life.Seconds())
	if maxAge < 1 {
		// MaxAge 0 would mean "no Max-Age attribute" (a browser-session
		// cookie); -1 deletes the cookie, which is the right outcome for an
		// already-exhausted lifetime.
		maxAge = -1
	}
	// #nosec G124 -- Secure follows Manager.Secure, which defaults to true; it is false only for plain-HTTP loopback development. HttpOnly and SameSite=Strict are always set.
	return &http.Cookie{
		Name:     CookieName,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
}

// ExpiredCookie builds a Set-Cookie header value that clears the session
// cookie in the browser (used on logout).
func (m *Manager) ExpiredCookie() *http.Cookie {
	// #nosec G124 -- clearing cookie; same Secure policy as the session cookie above.
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

// TokenFromRequest extracts the session token from the request's cookie, if
// present.
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

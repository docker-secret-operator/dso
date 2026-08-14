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
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieName is the name of the session cookie set on successful login.
	CookieName = "dso_webui_session"

	// DefaultIdleTimeout is used when no session_idle_timeout is configured.
	DefaultIdleTimeout = 30 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("webuiauth: invalid username or password")
	ErrNoSession          = errors.New("webuiauth: missing or invalid session")
	ErrSessionExpired     = errors.New("webuiauth: session expired")
)

// Session is one authenticated browser session for the single operator
// identity.
type Session struct {
	Token      string
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
	// Secure controls whether issued cookies set the Secure attribute.
	// Should be true in production (TLS); tests over plain HTTP set it
	// false.
	Secure bool

	mu       sync.Mutex
	sessions map[string]*Session

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
		Secure:       true,
		sessions:     make(map[string]*Session),
		now:          time.Now,
	}
}

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
	pwErr := bcrypt.CompareHashAndPassword([]byte(m.passwordHash), []byte(password))
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
	m.mu.Unlock()

	return sess, nil
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

	sess.LastSeenAt = now
	return sess, nil
}

// Logout invalidates the given session token, if present.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// SessionCookie builds the Set-Cookie header value for a freshly created
// session: HttpOnly, SameSite=Strict, and Secure per m.Secure.
func (m *Manager) SessionCookie(sess *Session) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(m.idleTimeout.Seconds()),
	}
}

// ExpiredCookie builds a Set-Cookie header value that clears the session
// cookie in the browser (used on logout).
func (m *Manager) ExpiredCookie() *http.Cookie {
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

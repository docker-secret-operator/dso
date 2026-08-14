package webuiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testManager(t *testing.T, idle time.Duration) (*Manager, string) {
	t.Helper()
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	m := NewManager("operator", hash, idle)
	m.Secure = false
	return m, hash
}

func TestLogin_Success(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("expected login success, got %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if sess.Username != "operator" {
		t.Fatalf("unexpected username: %q", sess.Username)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	m, _ := testManager(t, 0)
	if _, err := m.Login("operator", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_WrongUsername(t *testing.T) {
	m, _ := testManager(t, 0)
	if _, err := m.Login("nobody", "correct-horse-battery-staple"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestValidate_Success(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	got, err := m.Validate(sess.Token)
	if err != nil {
		t.Fatalf("expected valid session, got %v", err)
	}
	if got.Token != sess.Token {
		t.Fatal("returned wrong session")
	}
}

func TestValidate_MissingToken(t *testing.T) {
	m, _ := testManager(t, 0)
	if _, err := m.Validate(""); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession, got %v", err)
	}
}

func TestValidate_UnknownToken(t *testing.T) {
	m, _ := testManager(t, 0)
	if _, err := m.Validate("deadbeef"); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession, got %v", err)
	}
}

func TestValidate_Expiry(t *testing.T) {
	m, _ := testManager(t, 10*time.Millisecond)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	// Force the clock forward past the idle timeout.
	frozen := m.now().Add(time.Hour)
	m.now = func() time.Time { return frozen }

	if _, err := m.Validate(sess.Token); err != ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
	// Second call must see it already evicted.
	if _, err := m.Validate(sess.Token); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession after eviction, got %v", err)
	}
}

func TestLogout_InvalidatesSession(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	m.Logout(sess.Token)
	if _, err := m.Validate(sess.Token); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession after logout, got %v", err)
	}
}

func TestRequireSession_MissingCookie(t *testing.T) {
	m, _ := testManager(t, 0)
	handler := m.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/config/raw", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireSession_ValidCookie(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	handler := m.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/config/raw", nil)
	req.AddCookie(m.SessionCookie(sess))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequireSession_InvalidCookie(t *testing.T) {
	m, _ := testManager(t, 0)
	handler := m.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/config/raw", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "garbage"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestCheckHandshake_MissingCookie(t *testing.T) {
	m, _ := testManager(t, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/events/ws", nil)
	if _, err := m.CheckHandshake(req); err != ErrNoSession {
		t.Fatalf("expected ErrNoSession, got %v", err)
	}
}

func TestCheckHandshake_ValidCookie(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, err := m.Login("operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/events/ws", nil)
	req.AddCookie(m.SessionCookie(sess))
	if _, err := m.CheckHandshake(req); err != nil {
		t.Fatalf("expected valid handshake, got %v", err)
	}
}

func TestSessionCookie_Attributes(t *testing.T) {
	m, _ := testManager(t, 0)
	sess, _ := m.Login("operator", "correct-horse-battery-staple")
	c := m.SessionCookie(sess)
	if !c.HttpOnly {
		t.Fatal("expected HttpOnly cookie")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Fatal("expected SameSite=Strict")
	}
}

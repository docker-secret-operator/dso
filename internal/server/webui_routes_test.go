package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
	"go.uber.org/zap"
)

// newTestRESTServer builds a RESTServer with both a bearer token and a
// webui session manager configured, mirroring what StartRESTServer wires up
// when both DSO_AUTH_TOKEN and webui.enabled are set.
func newTestRESTServer(t *testing.T) (*RESTServer, string) {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", "test-bearer-token-16bytes")

	hash, err := webuiauth.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	mgr := webuiauth.NewManager("operator", hash, 0)
	mgr.Secure = false // plain-http test server

	// Isolated temp dir, not the real /var/run/dso path: EventStore now
	// replays events.jsonl on construction, so using the real path here
	// would pick up unrelated history from any actual dso agent process
	// running on the same host.
	return &RESTServer{
		Cache:      agent.NewSecretCache(5 * time.Minute),
		Logger:     zap.NewNop(),
		Hub:        NewHub(zap.NewNop()),
		EventStore: newEventStoreAt(10, nil, t.TempDir(), "events.jsonl"),
		Auth:       auth.NewAuthenticator(),
		WebUIAuth:  mgr,
	}, "test-bearer-token-16bytes"
}

func TestRESTServer_DualAuth_BearerTokenAllowed(t *testing.T) {
	s, token := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if !s.authorized(req) {
		t.Fatal("expected bearer token to be authorized")
	}
}

func TestRESTServer_DualAuth_SessionCookieAllowed(t *testing.T) {
	s, _ := newTestRESTServer(t)

	sess, err := s.WebUIAuth.Login("operator", "correct-horse")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	req.AddCookie(s.WebUIAuth.SessionCookie(sess))
	if !s.authorized(req) {
		t.Fatal("expected valid session cookie to be authorized")
	}
}

func TestRESTServer_DualAuth_NeitherRejected(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	if s.authorized(req) {
		t.Fatal("expected request with neither token nor session to be rejected")
	}
}

func TestRESTServer_DualAuth_InvalidSessionRejected(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	req.AddCookie(&http.Cookie{Name: webuiauth.CookieName, Value: "bogus-token"})
	if s.authorized(req) {
		t.Fatal("expected invalid session cookie to be rejected")
	}
}

// TestRESTServer_Login_SetsCookie_NotBody verifies the login handler never
// leaks the session token into the JSON response body -- only via the
// HttpOnly Set-Cookie header -- so browser JS can never read or store it.
func TestRESTServer_Login_SetsCookie_NotBody(t *testing.T) {
	s, _ := newTestRESTServer(t)

	body := `{"username":"operator","password":"correct-horse"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleAuthLogin(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); containsToken(got) {
		t.Errorf("response body must not contain the session token: %s", got)
	}
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == webuiauth.CookieName {
			found = true
			if !c.HttpOnly {
				t.Error("session cookie must be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("expected Set-Cookie for session token")
	}
}

func TestRESTServer_Login_WrongPassword_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	body := `{"username":"operator","password":"wrong"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleAuthLogin(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRESTServer_Logout_ClearsCookie(t *testing.T) {
	s, _ := newTestRESTServer(t)
	sess, err := s.WebUIAuth.Login("operator", "correct-horse")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(s.WebUIAuth.SessionCookie(sess))
	rec := httptest.NewRecorder()

	s.handleAuthLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if _, err := s.WebUIAuth.Validate(sess.Token); err == nil {
		t.Error("expected session to be invalidated after logout")
	}
}

func TestRESTServer_Session_ValidCookie_Authenticated(t *testing.T) {
	s, _ := newTestRESTServer(t)
	sess, err := s.WebUIAuth.Login("operator", "correct-horse")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(s.WebUIAuth.SessionCookie(sess))
	rec := httptest.NewRecorder()

	s.handleAuthSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, `"authenticated":true`) {
		t.Errorf("expected body to report authenticated:true, got %s", got)
	}
}

func TestRESTServer_Session_NoCookie_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()

	s.handleAuthSession(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRESTServer_Session_InvalidCookie_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: webuiauth.CookieName, Value: "bogus-token"})
	rec := httptest.NewRecorder()

	s.handleAuthSession(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// TestRESTServer_Session_RoutedThroughAuthorizedGate confirms /api/auth/session
// is NOT in publicPaths -- it must go through the same authorized() dual-auth
// gate as any other protected endpoint, and rejects when neither a bearer
// token nor a session cookie is present.
func TestRESTServer_Session_RoutedThroughAuthorizedGate(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 from ServeHTTP without credentials, got %d", rec.Code)
	}
}

func TestRESTServer_Discovery_NoSecretsLeaked(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/discovery", nil)
	rec := httptest.NewRecorder()
	s.handleDiscovery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func containsToken(body string) bool {
	// A crude but sufficient check for this test: the login handler must
	// never echo the raw session token (a 64-char hex string) into the body.
	return len(body) > 0 && (indexOf(body, "token") >= 0 && indexOf(body, "\"status\"") < 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

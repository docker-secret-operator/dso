package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
)

const (
	testOperator = "operator"
	testPassword = "correct horse battery staple"
)

// newWebUIServer builds a RESTServer with WebUI authentication enabled and,
// unless token is "", DSO_AUTH_TOKEN set. These tests drive ServeHTTP -- the
// real route table and gate -- not helper functions.
func newWebUIServer(t *testing.T, token string) *RESTServer {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", token) // "" == unset for auth.NewAuthenticator
	hash, err := webuiauth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	mgr := webuiauth.NewManager(testOperator, hash, time.Hour)
	mgr.Secure = false
	s.WebUIAuth = mgr
	return s
}

func do(s *RESTServer, method, path string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func login(t *testing.T, s *RESTServer) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"`+testOperator+`","password":"`+testPassword+`"}`))
	req.RemoteAddr = "192.0.2.10:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == webuiauth.CookieName {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

// protectedRoutes enumerates every route in ServeHTTP's switch (method + path)
// that must reject anonymous callers. TestRouteTableCoverage keeps this list
// honest against the source so a newly added route cannot silently skip it.
var protectedRoutes = []struct{ method, path string }{
	{"GET", "/api/auth/session"},
	{"GET", "/api/discovery"},
	{"GET", "/api/config/raw"},
	{"GET", "/api/secrets"},
	{"GET", "/api/secrets/db/rotate"},
	{"POST", "/api/secrets/db/rotate"},
	{"GET", "/api/events"},
	{"GET", "/api/events/ws"},
	{"GET", "/api/logs"},
	{"GET", "/api/containers"},
	{"GET", "/api/audit"},
	{"GET", "/api/sessions"},
	{"POST", "/api/sessions/revoke-others"},
	{"GET", "/api/integrations"},
	{"POST", "/api/integrations/notifications/test"},
	{"GET", "/api/drift"},
	{"GET", "/api/analytics"},
	{"GET", "/api/alerts"},
	{"GET", "/api/does-not-exist"},
	{"DELETE", "/api/secrets"},
}

// publicRoutes are the only paths served without the global credential.
var publicRoutes = map[string]bool{
	"/health": true, "/api/auth/login": true, "/api/auth/logout": true, webhookPath: true,
}

var routeLiteral = regexp.MustCompile(`r\.URL\.Path == "(/[^"]*)"|HasPrefix\(r\.URL\.Path, "(/[^"]*)"\)`)

// TestRouteTableCoverage parses rest.go and fails if ServeHTTP registers a path
// that is neither in protectedRoutes nor an explicitly public route. This is
// the guard against "added a route, forgot to test it is gated".
func TestRouteTableCoverage(t *testing.T) {
	src, err := os.ReadFile("rest.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func (s *RESTServer) ServeHTTP")
	end := strings.Index(body[start:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("could not locate ServeHTTP")
	}
	known := map[string]bool{}
	for _, r := range protectedRoutes {
		known[r.path] = true
	}
	var missing []string
	for _, m := range routeLiteral.FindAllStringSubmatch(body[start:start+end], -1) {
		path := m[1] + m[2]
		if publicRoutes[path] || path == "/api" {
			continue
		}
		covered := known[path]
		for k := range known { // prefix routes are covered by any request under them
			if strings.HasPrefix(k, path) {
				covered = true
			}
		}
		if !covered {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("routes registered in ServeHTTP but absent from protectedRoutes: %v", missing)
	}
}

// F1 regression: WebUI enabled + DSO_AUTH_TOKEN unset must NOT be open.
func TestWebUIEnabled_NoToken_AllProtectedRoutesReturn401(t *testing.T) {
	s := newWebUIServer(t, "")
	for _, r := range protectedRoutes {
		w := do(s, r.method, r.path, nil)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without credentials = %d, want 401", r.method, r.path, w.Code)
		}
	}
}

func TestWebUIEnabled_WithToken_AllProtectedRoutesReturn401Anonymous(t *testing.T) {
	s := newWebUIServer(t, "0123456789abcdef-token")
	for _, r := range protectedRoutes {
		if w := do(s, r.method, r.path, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without credentials = %d, want 401", r.method, r.path, w.Code)
		}
	}
}

func TestWebUIEnabled_NoToken_AuthorizedIsFalse(t *testing.T) {
	s := newWebUIServer(t, "")
	req := httptest.NewRequest("GET", "/api/audit", nil)
	if s.authorized(req) {
		t.Fatal("authorized() must be false for an anonymous request when the WebUI is enabled")
	}
}

func TestPublicEndpointsStayPublic(t *testing.T) {
	s := newWebUIServer(t, "")
	if w := do(s, "GET", "/health", nil); w.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200", w.Code)
	}
	// Login is public (it is the way in): wrong creds are 401 from the
	// handler, not from the gate -- distinguishable by a correct login working.
	_ = login(t, s)
	if w := do(s, "POST", "/api/auth/logout", nil); w.Code != http.StatusOK {
		t.Errorf("logout = %d, want 200", w.Code)
	}
}

func TestNonAPIPathsServeWebUIShellWithoutAuth(t *testing.T) {
	s := newWebUIServer(t, "")
	s.WebUI = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("shell")) })
	if w := do(s, "GET", "/dashboard", nil); w.Code != http.StatusOK || w.Body.String() != "shell" {
		t.Errorf("static shell = %d %q", w.Code, w.Body.String())
	}
	// But a path that merely starts with /api never reaches the shell handler.
	if w := do(s, "GET", "/apix", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("/apix anonymous = %d, want 401", w.Code)
	}
}

func TestLegacyOpenMode_NoToken_NoWebUI_StillWorks(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "")
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	if w := do(s, "GET", "/api/secrets", nil); w.Code != http.StatusOK {
		t.Errorf("legacy open mode GET /api/secrets = %d, want 200 (documented compatibility)", w.Code)
	}
}

func TestTokenOnly_NoWebUI_RequiresBearer(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "0123456789abcdef-token")
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	if w := do(s, "GET", "/api/secrets", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", w.Code)
	}
	if w := do(s, "GET", "/api/secrets", map[string]string{"Authorization": "Bearer wrong-token-value-xx"}); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer = %d, want 401", w.Code)
	}
	if w := do(s, "GET", "/api/secrets", map[string]string{"Authorization": "Bearer 0123456789abcdef-token"}); w.Code != http.StatusOK {
		t.Errorf("valid bearer = %d, want 200", w.Code)
	}
}

func TestSession_ValidSessionWorks_AndCookieIsReissued(t *testing.T) {
	s := newWebUIServer(t, "")
	c := login(t, s)
	w := do(s, "GET", "/api/secrets", nil, c)
	if w.Code != http.StatusOK {
		t.Fatalf("valid session = %d, want 200", w.Code)
	}
	var reissued *http.Cookie
	for _, rc := range w.Result().Cookies() {
		if rc.Name == webuiauth.CookieName {
			reissued = rc
		}
	}
	if reissued == nil || reissued.Value != c.Value || reissued.MaxAge <= 0 {
		t.Fatalf("authenticated request must re-issue the session cookie with a positive Max-Age, got %+v", reissued)
	}
	if !reissued.HttpOnly || reissued.SameSite != http.SameSiteStrictMode {
		t.Error("re-issued cookie lost HttpOnly/SameSite=Strict")
	}
}

func TestSession_InvalidExpiredRevokedFail(t *testing.T) {
	s := newWebUIServer(t, "")
	// invalid
	if w := do(s, "GET", "/api/secrets", nil, &http.Cookie{Name: webuiauth.CookieName, Value: "forged"}); w.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie = %d, want 401", w.Code)
	}
	// logout revokes
	c := login(t, s)
	if w := do(s, "GET", "/api/secrets", nil, c); w.Code != http.StatusOK {
		t.Fatalf("pre-logout = %d", w.Code)
	}
	if w := do(s, "POST", "/api/auth/logout", nil, c); w.Code != http.StatusOK {
		t.Fatalf("logout = %d", w.Code)
	}
	if w := do(s, "GET", "/api/secrets", nil, c); w.Code != http.StatusUnauthorized {
		t.Errorf("session after logout = %d, want 401", w.Code)
	}
	// idle expiry
	hash, _ := webuiauth.HashPassword(testPassword)
	short := webuiauth.NewManager(testOperator, hash, 60*time.Millisecond)
	short.Secure = false
	s.WebUIAuth = short
	c2 := login(t, s)
	time.Sleep(120 * time.Millisecond)
	if w := do(s, "GET", "/api/secrets", nil, c2); w.Code != http.StatusUnauthorized {
		t.Errorf("idle-expired session = %d, want 401", w.Code)
	}
	// Absolute expiry, even with activity. End-to-end through ServeHTTP; the
	// deterministic (injected-clock) version of this rule lives in
	// internal/webuiauth.TestAbsoluteLifetime_ExpiresDespiteActivity -- the
	// Manager clock is unexported, so this test uses real time with margins
	// chosen so scheduler jitter cannot flip either assertion:
	//
	//   - "still alive" checks happen at ~0.2s and ~0.4s into a 1s cap, leaving
	//     ~0.6s of slack for the only jitter-sensitive direction (a delay makes
	//     the session OLDER, never younger);
	//   - the final check happens after sleeping 1.2s total past the last
	//     request, so the session is at least ~1.6s old. time.Sleep never
	//     returns early, so this direction cannot be affected by jitter.
	//
	// The idle timeout is an hour, so the only thing that can end this session
	// is the absolute lifetime -- proving activity does not extend it.
	const (
		absoluteCap = 1 * time.Second
		activityGap = 200 * time.Millisecond
		pastCapWait = 1200 * time.Millisecond
	)
	capped := webuiauth.NewManager(testOperator, hash, time.Hour)
	capped.Secure = false
	capped.SetMaxLifetime(absoluteCap)
	s.WebUIAuth = capped
	c3 := login(t, s)
	for i := 0; i < 2; i++ {
		time.Sleep(activityGap)
		if w := do(s, "GET", "/api/secrets", nil, c3); w.Code != http.StatusOK {
			t.Fatalf("active session died early at step %d: %d", i, w.Code)
		}
	}
	time.Sleep(pastCapWait)
	if w := do(s, "GET", "/api/secrets", nil, c3); w.Code != http.StatusUnauthorized {
		t.Errorf("session past absolute lifetime = %d, want 401 (activity must not extend the cap)", w.Code)
	}
}

func TestSession_RevokeOthersInvalidatesOtherSessionsOnly(t *testing.T) {
	s := newWebUIServer(t, "")
	a, b := login(t, s), login(t, s)
	if w := do(s, "POST", "/api/sessions/revoke-others", nil, a); w.Code != http.StatusOK {
		t.Fatalf("revoke-others = %d", w.Code)
	}
	if w := do(s, "GET", "/api/secrets", nil, b); w.Code != http.StatusUnauthorized {
		t.Errorf("other session after revoke = %d, want 401", w.Code)
	}
	if w := do(s, "GET", "/api/secrets", nil, a); w.Code != http.StatusOK {
		t.Errorf("calling session after revoke = %d, want 200", w.Code)
	}
}

func TestBearerStillWorksWithWebUIEnabled(t *testing.T) {
	s := newWebUIServer(t, "0123456789abcdef-token")
	if w := do(s, "GET", "/api/secrets", map[string]string{"Authorization": "Bearer 0123456789abcdef-token"}); w.Code != http.StatusOK {
		t.Errorf("valid bearer = %d, want 200", w.Code)
	}
}

// ---- CSRF / Origin ---------------------------------------------------------

func TestCSRF_CookieAuthenticatedStateChangeRequiresAllowedOrigin(t *testing.T) {
	s := newWebUIServer(t, "")
	c := login(t, s)
	const target = "/api/sessions/revoke-others"

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"cross-site origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"same host other port", map[string]string{"Origin": "http://example.com:3000"}, http.StatusForbidden},
		{"null origin", map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"malformed origin", map[string]string{"Origin": "::::"}, http.StatusForbidden},
		{"cross-site no origin (Fetch Metadata)", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-site no origin (Fetch Metadata)", map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"same origin", map[string]string{"Origin": "http://example.com"}, http.StatusOK},
		{"same origin via Fetch Metadata", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"non-browser client", nil, http.StatusOK},
	}
	for _, tc := range cases {
		// Re-login each time: revoke-others succeeding is irrelevant, but a
		// 200 must not depend on earlier cases.
		cc := login(t, s)
		if w := do(s, "POST", target, tc.hdr, cc); w.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, w.Code, tc.want)
		}
	}
	_ = c

	// Safe methods are not origin-restricted (cookies are SameSite=Strict).
	cc := login(t, s)
	if w := do(s, "GET", "/api/secrets", map[string]string{"Origin": "https://evil.example"}, cc); w.Code != http.StatusOK {
		t.Errorf("GET with foreign origin = %d, want 200", w.Code)
	}
}

func TestCSRF_AllowedOriginsConfig(t *testing.T) {
	s := newWebUIServer(t, "")
	s.AllowedOrigins = []string{"https://dso.example.com/"}
	c := login(t, s)
	if w := do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "https://dso.example.com"}, c); w.Code != http.StatusOK {
		t.Errorf("configured origin = %d, want 200", w.Code)
	}
	c = login(t, s)
	if w := do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "https://dso.example.org"}, c); w.Code != http.StatusForbidden {
		t.Errorf("unlisted origin = %d, want 403", w.Code)
	}
}

func TestCSRF_BearerRequestsAreNotOriginRestricted(t *testing.T) {
	s := newWebUIServer(t, "0123456789abcdef-token")
	h := map[string]string{"Authorization": "Bearer 0123456789abcdef-token", "Origin": "https://evil.example"}
	if w := do(s, "POST", "/api/sessions/revoke-others", h); w.Code != http.StatusOK {
		t.Errorf("bearer + foreign Origin = %d, want 200 (bearer is not an ambient credential)", w.Code)
	}
}

func TestCSRF_LoginAndLogoutRejectForeignOrigin(t *testing.T) {
	s := newWebUIServer(t, "")
	for _, p := range []string{"/api/auth/login", "/api/auth/logout"} {
		if w := do(s, "POST", p, map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
			t.Errorf("%s with foreign origin = %d, want 403", p, w.Code)
		}
	}
}

func TestCSRF_WebSocketUpgradeFromForeignOriginRejected(t *testing.T) {
	s := newWebUIServer(t, "")
	c := login(t, s)
	w := do(s, "GET", "/api/events/ws", map[string]string{"Origin": "http://example.com:3000"}, c)
	if w.Code != http.StatusForbidden {
		t.Errorf("ws upgrade from other port = %d, want 403", w.Code)
	}
}

func TestCSRF_OpenModeAlsoChecksOrigin(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "")
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	if w := do(s, "POST", "/api/sessions/revoke-others", map[string]string{"Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
		t.Errorf("open-mode POST from foreign origin = %d, want 403", w.Code)
	}
}

// ---- Host header (DNS rebinding) ------------------------------------------

func TestHostGuard(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "")
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	s.HostGuard = newHostGuard([]string{"Dso.Internal"}, []string{"https://proxy.example.com"})

	ok := []string{"localhost:8471", "127.0.0.1:8471", "[::1]:8471", "LOCALHOST", "app.localhost", "dso.internal:8471", "proxy.example.com"}
	for _, h := range ok {
		req := httptest.NewRequest("GET", "/api/secrets", nil)
		req.Host = h
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", h, w.Code)
		}
	}
	for _, h := range []string{"attacker.example", "attacker.example:8471", "127.0.0.1.attacker.example", "10.0.0.5:8471", ""} {
		req := httptest.NewRequest("GET", "/api/secrets", nil)
		req.Host = h
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", h, w.Code)
		}
	}
	// /health is exempt so health checks addressing the service by name work.
	req := httptest.NewRequest("GET", "/health", nil)
	req.Host = "dso-agent:8471"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/health with service-name Host = %d, want 200", w.Code)
	}
}

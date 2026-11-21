package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// F-E1 structural default: the zero value of RESTServer must fail closed.
// Before this change the struct field was denyAnonymous (zero value = open),
// so any construction path that skipped enforceCredentialPolicy silently
// served protected routes anonymously. These tests pin the inverted polarity.

// closedDefaultRoutes are representative data-bearing / state-changing routes.
var closedDefaultRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/secrets"},
	{http.MethodGet, "/api/config/raw"},
	{http.MethodGet, "/api/events"},
	{http.MethodGet, "/api/audit"},
	{http.MethodGet, "/api/containers"},
	{http.MethodGet, "/api/auth/session"},
	{http.MethodPost, "/api/secrets/db/rotate"},
}

// bareServer is a RESTServer literal that sets no access fields at all --
// only what the handlers need not to dereference nil. In particular it never
// mentions allowAnonymous.
func bareServer(t *testing.T) *RESTServer {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", "")
	return &RESTServer{Logger: zap.NewNop(), Config: &config.Config{}}
}

func serveReq(s *RESTServer, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestRESTServerZeroValue_RejectsAnonymousOnProtectedRoutes(t *testing.T) {
	s := bareServer(t)
	for _, rt := range closedDefaultRoutes {
		if rec := serveReq(s, rt.method, rt.path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("bare RESTServer{} %s %s = %d, want 401", rt.method, rt.path, rec.Code)
		}
	}
	if m, _ := s.authenticate(httptest.NewRequest(http.MethodGet, "/api/secrets", nil)); m != authNone {
		t.Errorf("bare RESTServer{} authenticate(anonymous) = %v, want authNone", m)
	}
	// Also with a non-nil Authenticator that has no token (what production
	// constructs): still closed.
	s.Auth = auth.NewAuthenticator()
	if rec := serveReq(s, http.MethodGet, "/api/secrets", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-token Authenticator, no opt-in = %d, want 401", rec.Code)
	}
}

func TestRESTServer_ExplicitAllowAnonymous_PermitsLegacyLoopbackMode(t *testing.T) {
	s := bareServer(t)
	s.allowAnonymous = true
	if m, _ := s.authenticate(httptest.NewRequest(http.MethodGet, "/api/secrets", nil)); m != authOpen {
		t.Fatalf("allowAnonymous=true authenticate(anonymous) = %v, want authOpen", m)
	}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", nil); rec.Code == http.StatusUnauthorized {
		t.Errorf("allowAnonymous=true /api/config/raw = 401, want it past the auth gate")
	}
}

// allowAnonymous never weakens a configured credential: with a token set, an
// anonymous request is rejected even if the field were somehow true.
func TestRESTServer_AllowAnonymousIgnoredWhenTokenConfigured(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", wiredToken)
	s := &RESTServer{Logger: zap.NewNop(), Config: &config.Config{}, Auth: auth.NewAuthenticator(), allowAnonymous: true}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("token set + allowAnonymous, anonymous = %d, want 401", rec.Code)
	}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", map[string]string{"Authorization": "Bearer wrong-token-0123456789"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer = %d, want 401", rec.Code)
	}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", map[string]string{"Authorization": "Bearer " + wiredToken}); rec.Code != http.StatusOK {
		t.Errorf("valid bearer = %d, want 200", rec.Code)
	}
}

// Bearer auth on a zero-value server (no opt-in) works exactly as before.
func TestRESTServerZeroValue_BearerTokenStillAuthenticates(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", wiredToken)
	s := &RESTServer{Logger: zap.NewNop(), Config: &config.Config{}, Auth: auth.NewAuthenticator()}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", map[string]string{"Authorization": "Bearer " + wiredToken}); rec.Code != http.StatusOK {
		t.Errorf("valid bearer on zero-value server = %d, want 200", rec.Code)
	}
	if rec := serveReq(s, http.MethodGet, "/api/config/raw", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous on token server = %d, want 401", rec.Code)
	}
}

// Public routes are never gated, so the closed default must not affect them.
func TestRESTServerZeroValue_PublicRoutesUnaffected(t *testing.T) {
	s := bareServer(t)
	if rec := serveReq(s, http.MethodGet, "/health", nil); rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200", rec.Code)
	}
	// With the WebUI disabled, login/logout reach their handlers (which
	// report the WebUI as disabled) instead of being stopped by the gate.
	for _, p := range []string{"/api/auth/login", "/api/auth/logout"} {
		rec := serveReq(s, http.MethodPost, p, nil)
		if rec.Code == http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "WebUI is disabled") {
			t.Errorf("POST %s = %d %q, want it to reach the handler (not the auth gate)", p, rec.Code, rec.Body.String())
		}
	}
}

// With the WebUI enabled and the production policy applied (allowAnonymous
// reset to false), login and logout still work and the session authenticates.
func TestRESTServerClosedDefault_WebUILoginLogoutUnaffected(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "")
	s := policyWired(t, "", webUICfg(t, nil))
	if s.allowAnonymous {
		t.Fatal("enforceCredentialPolicy left allowAnonymous=true on a WebUI wiring")
	}
	c := loginWired(t, s)
	if rec := req(s, http.MethodGet, "/api/secrets", "", nil, c); rec.Code != http.StatusOK {
		t.Errorf("session GET = %d, want 200", rec.Code)
	}
	if rec := req(s, http.MethodGet, "/api/secrets", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET with WebUI = %d, want 401", rec.Code)
	}
	if rec := req(s, http.MethodPost, "/api/auth/logout", "", nil, c); rec.Code != http.StatusOK {
		t.Errorf("logout = %d, want 200", rec.Code)
	}
	if rec := req(s, http.MethodGet, "/api/secrets", "", nil, c); rec.Code != http.StatusUnauthorized {
		t.Errorf("session after logout = %d, want 401", rec.Code)
	}
}

// The HMAC-signed webhook authenticates itself and must work on a server that
// denies anonymous access, while an unsigned webhook is still rejected.
func TestRESTServerClosedDefault_SignedWebhookUnaffected(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	s.allowAnonymous = false // production default for no-token mode
	if w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("closed-1"))); w.Code != authenticated {
		t.Errorf("signed webhook on closed server = %d (%s), want %d (authenticated)", w.Code, w.Body.String(), authenticated)
	}
	unsigned := httptest.NewRequest(http.MethodPost, webhookPath, strings.NewReader(string(webhookBody("closed-2"))))
	if w := serve(s, unsigned); w.Code == authenticated || w.Code == http.StatusOK {
		t.Errorf("unsigned webhook on closed server = %d, want rejection", w.Code)
	}
	// Non-POST on the webhook path is a protected route (events list).
	if w := serve(s, httptest.NewRequest(http.MethodGet, webhookPath, nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("GET %s on closed server = %d, want 401", webhookPath, w.Code)
	}
}

// enforceCredentialPolicy assigns the decision rather than only ever opening:
// a server that arrives already opened is closed again without the opt-in,
// and token/WebUI wirings never end up with allowAnonymous=true.
func TestEnforceCredentialPolicy_AssignsDecision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
		cfg   *config.Config
		optIn string
		want  bool
	}{
		{"legacy, no opt-in", "", &config.Config{}, "", false},
		{"legacy, opt-in", "", &config.Config{}, "1", true},
		{"legacy, opt-in=false", "", &config.Config{}, "false", false},
		{"token, opt-in", wiredToken, &config.Config{}, "1", false},
		{"webui, opt-in", "", webUICfg(t, nil), "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envAllowUnauthenticatedLoopback, tc.optIn)
			s, w, _ := wired(t, tc.token, tc.cfg, "127.0.0.1:8471")
			s.allowAnonymous = true // simulate a pre-opened server
			s.enforceCredentialPolicy(w, zap.NewNop())
			if s.allowAnonymous != tc.want {
				t.Errorf("allowAnonymous = %v, want %v", s.allowAnonymous, tc.want)
			}
		})
	}
}

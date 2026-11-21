package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// policyWired is wired() plus the F-E1 deployment policy step that
// StartRESTServerSupervised applies after applyAccessWiring.
func policyWired(t *testing.T, token string, cfg *config.Config) *RESTServer {
	t.Helper()
	s, w, _ := wired(t, token, cfg, "127.0.0.1:8471")
	s.enforceCredentialPolicy(w, zap.NewNop())
	return s
}

// F-E1 regression: with no DSO_AUTH_TOKEN and the WebUI disabled, the REST API
// on loopback used to serve every protected route (secret listing, manual
// rotation, config, events) to any local UID. It must now fail closed.
func TestCredentialPolicy_NoToken_NoWebUI_FailsClosedByDefault(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "")
	s := policyWired(t, "", &config.Config{})

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/secrets"},
		{"GET", "/api/config/raw"},
		{"GET", "/api/events"},
		{"POST", "/api/secrets/db/rotate"},
	} {
		if rec := req(s, tc.method, tc.path, "127.0.0.1:8471", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401 without the explicit opt-in", tc.method, tc.path, rec.Code)
		}
	}
	if rec := req(s, "GET", "/health", "127.0.0.1:8471", nil); rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200 (public route)", rec.Code)
	}
}

func TestCredentialPolicy_ExplicitOptInRestoresLegacyLoopbackMode(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "1")
	s := policyWired(t, "", &config.Config{})
	if rec := req(s, "GET", "/api/secrets", "127.0.0.1:8471", nil); rec.Code != http.StatusOK {
		t.Errorf("opted-in legacy GET = %d, want 200", rec.Code)
	}
	// The opt-in never disables the DNS-rebinding Host guard.
	if rec := req(s, "GET", "/api/secrets", "attacker.example", nil); rec.Code != http.StatusForbidden {
		t.Errorf("foreign Host with opt-in = %d, want 403", rec.Code)
	}
}

func TestCredentialPolicy_OptInValueParsing(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "false": false, "yes": false, "1": true, "true": true, "TRUE": true, " 1 ": true} {
		t.Setenv(envAllowUnauthenticatedLoopback, v)
		if got := unauthenticatedLoopbackAllowed(); got != want {
			t.Errorf("%q => %v, want %v", v, got, want)
		}
	}
}

// The policy only affects legacy open mode; token mode is unchanged.
func TestCredentialPolicy_TokenModeUnaffected(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "")
	s := policyWired(t, wiredToken, &config.Config{})
	if rec := req(s, "GET", "/api/secrets", "127.0.0.1:8471", map[string]string{"Authorization": "Bearer " + wiredToken}); rec.Code != http.StatusOK {
		t.Errorf("bearer GET = %d, want 200", rec.Code)
	}
	if rec := req(s, "GET", "/api/secrets", "127.0.0.1:8471", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous GET in token mode = %d, want 401", rec.Code)
	}
}

// End to end through StartRESTServerSupervised (the production entry point):
// a real loopback listener with no token and no opt-in rejects anonymous API
// calls while /health stays reachable.
func TestStartRESTServer_NoTokenLoopback_RejectsAnonymousAPI(t *testing.T) {
	t.Setenv(envAllowUnauthenticatedLoopback, "")
	lnp := injectListener(t, func(l net.Listener) *captureListener { return &captureListener{Listener: l} })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdown, _, err := startForTest(t, ctx, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	base := "http://" + (*lnp).Addr().String()

	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	get := func(path string) int {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/api/secrets"); code != http.StatusUnauthorized {
		t.Errorf("anonymous /api/secrets = %d, want 401", code)
	}
	if code := get("/health"); code == http.StatusUnauthorized {
		t.Errorf("/health = %d, want it to stay public", code)
	}
}

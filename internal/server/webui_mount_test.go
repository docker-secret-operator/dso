package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/docker-secret-operator/dso/internal/webui"
)

// newTestRESTServerWithWebUI builds a RESTServer identical to
// newTestRESTServer but with a WebUI handler mounted, mirroring what
// StartRESTServer wires up when cfg.WebUI.Enabled is true.
func newTestRESTServerWithWebUI(t *testing.T) *RESTServer {
	t.Helper()
	s, _ := newTestRESTServer(t)

	fsys := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>spa shell</html>")},
	}
	s.WebUI = webui.NewHandler(fsys)
	return s
}

// TestServeHTTP_WebUI_RootServesStaticAssets verifies "/" is delegated to the
// WebUI handler and served without needing auth (SPA shells are public; the
// app enforces auth client-side after login).
func TestServeHTTP_WebUI_RootServesStaticAssets(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "spa shell") {
		t.Errorf("expected SPA shell content, got: %q", rec.Body.String())
	}
}

// TestServeHTTP_WebUI_UnknownRouteFallsBackToIndex verifies an arbitrary
// client-side route (e.g. /dashboard, with no matching embedded file) is
// served the SPA shell rather than a 404.
func TestServeHTTP_WebUI_UnknownRouteFallsBackToIndex(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/some/deep/route", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (SPA fallback), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa shell") {
		t.Errorf("expected SPA shell fallback content, got: %q", rec.Body.String())
	}
}

// TestServeHTTP_WebUI_APIPathsNeverReachSPAHandler is the core regression
// this integration must never break: any /api-prefixed path -- known or
// unknown -- must be handled by the API layer (auth + switch), never by the
// SPA static handler, even when a WebUI handler is mounted.
func TestServeHTTP_WebUI_APIPathsNeverReachSPAHandler(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	// Unknown API path: must 404 from the API layer's default case, not
	// fall through to the SPA handler's 200 index.html fallback.
	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	req.Header.Set("Authorization", "Bearer test-bearer-token-16bytes")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from API layer for unknown /api path, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "spa shell") {
		t.Fatal("unknown /api path leaked into the SPA fallback handler")
	}
}

// TestServeHTTP_WebUI_APIPathsStillRequireAuth verifies mounting a WebUI
// handler does not weaken the auth gate on real /api routes.
func TestServeHTTP_WebUI_APIPathsStillRequireAuth(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/secrets, got %d", rec.Code)
	}
}

// TestServeHTTP_WebUI_HealthStaysPublicAndUnproxied verifies /health is
// never captured by the SPA handler and stays served by the real handler.
func TestServeHTTP_WebUI_HealthStaysPublicAndUnproxied(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "spa shell") {
		t.Fatal("/health was served by the SPA handler instead of the real health handler")
	}
	if !strings.Contains(rec.Body.String(), `"status":"up"`) {
		t.Errorf("expected real health payload, got: %q", rec.Body.String())
	}
}

// TestServeHTTP_WebUI_WebSocketRouteUnaffected verifies /api/events/ws is
// still routed to the WebSocket upgrade handler (not the SPA handler) when a
// WebUI handler is mounted. It doesn't attempt a full upgrade handshake --
// existing hub tests cover that -- it only checks the request isn't diverted
// to the SPA fallback and that auth is still enforced.
func TestServeHTTP_WebUI_WebSocketRouteUnaffected(t *testing.T) {
	s := newTestRESTServerWithWebUI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/events/ws", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	// No auth provided: must be rejected by the auth gate (401), same as
	// without WebUI mounted -- not silently served SPA content (200).
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated websocket route, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestServeHTTP_WebUIDisabled_RegressionCheck verifies that when no WebUI
// handler is mounted (WebUI disabled in config, the existing default), "/"
// behaves exactly as before this integration: a plain 404 from the API
// layer's default case, and all existing API behavior is untouched.
func TestServeHTTP_WebUIDisabled_RegressionCheck(t *testing.T) {
	s, token := newTestRESTServer(t)
	s.WebUI = nil // explicit: WebUI disabled

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for \"/\" with WebUI disabled, got %d", rec.Code)
	}

	// Existing API surface still works identically.
	req2 := httptest.NewRequest(http.MethodGet, "/api/discovery", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/discovery with WebUI disabled, got %d", rec2.Code)
	}
}

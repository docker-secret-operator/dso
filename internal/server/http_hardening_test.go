package server

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/webui"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const hardeningToken = "hardening-bearer-token-0123456789"

// chain returns the real middleware chain StartRESTServer builds (minus the
// rate limiter), with the embedded WebUI mounted.
func hardeningChain(t *testing.T, withWebUI bool) (*RESTServer, http.Handler) {
	t.Helper()
	s := newWebUIServer(t, hardeningToken)
	if withWebUI {
		fsys, err := webui.Assets()
		if err != nil {
			t.Fatal(err)
		}
		s.WebUI = webui.NewHandler(fsys)
	}
	return s, secureHeadersFor(s.requestIsHTTPS, s)
}

func hardenServe(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSecurityHeaders_PresentOnAPIHTMLAndStatic(t *testing.T) {
	_, h := hardeningChain(t, true)
	cases := []struct {
		name, path string
		wantCode   int
		htmlCSP    bool
	}{
		{"health", "/health", 200, false},
		{"api 401", "/api/discovery", 401, false},
		{"api 404", "/api/nope", 401, false}, // gate first; still carries headers
		{"html root", "/", 200, true},
		{"html route", "/dashboard", 200, true},
		{"spa fallback", "/some/deep/route", 200, true},
		{"missing asset", "/_next/static/missing.js", 404, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := hardenServe(h, httptest.NewRequest(http.MethodGet, c.path, nil))
			if rec.Code != c.wantCode {
				t.Fatalf("code %d want %d", rec.Code, c.wantCode)
			}
			hd := rec.Header()
			want := map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "no-referrer",
				"Cache-Control":          "no-store",
			}
			for k, v := range want {
				if hd.Get(k) != v {
					t.Errorf("%s = %q want %q", k, hd.Get(k), v)
				}
			}
			if pp := hd.Get("Permissions-Policy"); !strings.Contains(pp, "camera=()") || !strings.Contains(pp, "geolocation=()") {
				t.Errorf("Permissions-Policy = %q", pp)
			}
			csp := hd.Get("Content-Security-Policy")
			if !strings.Contains(csp, "frame-ancestors 'none'") {
				t.Errorf("CSP lacks frame-ancestors: %q", csp)
			}
			if c.htmlCSP != strings.Contains(csp, "sha256-") {
				t.Errorf("html document policy expected=%v, got %q", c.htmlCSP, csp)
			}
			if !c.htmlCSP && !strings.HasPrefix(csp, "default-src 'none'") {
				t.Errorf("non-document CSP should be baseline deny: %q", csp)
			}
			if hd.Get("Strict-Transport-Security") != "" {
				t.Errorf("HSTS must be absent over plain HTTP")
			}
		})
	}
	// Static asset keeps its real content type and 200 with the headers.
	rec := hardenServe(h, httptest.NewRequest(http.MethodGet, "/icon.svg", nil))
	if rec.Code == 200 && rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("static asset missing nosniff")
	}
}

func TestHSTS_OnlyOnVerifiedHTTPS(t *testing.T) {
	s, h := hardeningChain(t, false)
	if err := s.WebUIAuth.SetTrustedProxies([]string{"192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	get := func(mod func(*http.Request)) string {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.RemoteAddr = "198.51.100.7:4000"
		mod(req)
		return hardenServe(h, req).Header().Get("Strict-Transport-Security")
	}
	if v := get(func(r *http.Request) {}); v != "" {
		t.Errorf("plain HTTP: HSTS %q", v)
	}
	if v := get(func(r *http.Request) { r.TLS = &tls.ConnectionState{} }); v != hstsValue {
		t.Errorf("direct TLS: HSTS %q want %q", v, hstsValue)
	}
	if v := get(func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }); v != "" {
		t.Errorf("untrusted peer X-Forwarded-Proto must be ignored, got %q", v)
	}
	trusted := func(r *http.Request) { r.RemoteAddr = "192.0.2.1:4000" }
	if v := get(func(r *http.Request) { trusted(r); r.Header.Set("X-Forwarded-Proto", "https") }); v != hstsValue {
		t.Errorf("trusted proxy https: HSTS %q", v)
	}
	if v := get(func(r *http.Request) { trusted(r); r.Header.Set("X-Forwarded-Proto", "http") }); v != "" {
		t.Errorf("trusted proxy http: HSTS %q", v)
	}
	// Client-supplied "https" earlier in the chain, proxy appended "http".
	if v := get(func(r *http.Request) { trusted(r); r.Header.Set("X-Forwarded-Proto", "https, http") }); v != "" {
		t.Errorf("last (proxy-appended) value must win, got HSTS %q", v)
	}
}

func TestMethodEnforcement(t *testing.T) {
	s, _ := hardeningChain(t, false)
	bearer := map[string]string{"Authorization": "Bearer " + hardeningToken}
	rows := []struct {
		method, path string
		auth         bool
		code         int
		allow        string
	}{
		{"DELETE", "/api/secrets", true, 405, "GET, HEAD"},
		{"PUT", "/api/logs", true, 405, "GET, HEAD"},
		{"PATCH", "/api/config/raw", true, 405, "GET, HEAD"},
		{"POST", "/api/events", true, 405, "GET, HEAD"},
		{"POST", "/api/containers", true, 405, "GET, HEAD"},
		{"DELETE", "/api/audit", true, 405, "GET, HEAD"},
		{"PUT", "/api/discovery", true, 405, "GET, HEAD"},
		{"DELETE", "/api/secrets/x/rotate", true, 405, "GET, HEAD, POST"},
		{"PUT", "/api/events/secret-update", true, 405, "GET, HEAD, POST"},
		{"DELETE", "/api/sessions/revoke-others", true, 405, "GET, HEAD, POST"},
		{"PUT", "/api/integrations/notifications/test", true, 405, "GET, HEAD, POST"},
		{"POST", "/api/events/ws", true, 405, "GET"},
		{"POST", "/health", false, 405, "GET, HEAD"},
		{"GET", "/api/auth/login", false, 405, "POST"},
		{"PUT", "/api/auth/logout", false, 405, "POST"},
		{"POST", "/api/auth/session", true, 405, "GET, HEAD"},
		// Unknown route keeps its 404; not-authenticated callers get 401, never 405.
		{"DELETE", "/api/nope", true, 404, ""},
		{"DELETE", "/api/secrets", false, 401, ""},
	}
	for _, r := range rows {
		hdr := map[string]string{}
		if r.auth {
			hdr = bearer
		}
		w := do(s, r.method, r.path, hdr)
		if w.Code != r.code {
			t.Errorf("%s %s: %d want %d", r.method, r.path, w.Code, r.code)
			continue
		}
		if got := w.Header().Get("Allow"); got != r.allow {
			t.Errorf("%s %s: Allow %q want %q", r.method, r.path, got, r.allow)
		}
	}
	// Permitted methods are not blocked.
	for _, m := range []string{"GET", "HEAD"} {
		if w := do(s, m, "/health", nil); w.Code != 200 {
			t.Errorf("%s /health: %d", m, w.Code)
		}
	}
	if w := do(s, "GET", "/api/discovery", bearer); w.Code == 405 {
		t.Error("GET /api/discovery must not be 405")
	}
}

func TestBodyLimit_Oversized413(t *testing.T) {
	s, h := hardeningChain(t, false)
	body := strings.NewReader(`{"username":"` + strings.Repeat("a", maxRequestBodyBytes+10) + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	req.RemoteAddr = "192.0.2.50:1"
	if rec := hardenServe(h, req); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized login body: %d want 413", rec.Code)
	}
	// Malformed-but-small stays 400.
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("{"))
	req.RemoteAddr = "192.0.2.51:1"
	if rec := hardenServe(h, req); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed login body: %d want 400", rec.Code)
	}
	_ = s
}

func TestHTTPServerBoundaries_BuiltBySupervisedStart(t *testing.T) {
	capCh := make(chan *http.Server, 1)
	old := serveFunc
	serveFunc = func(srv *http.Server, ln net.Listener) error {
		capCh <- srv
		<-make(chan struct{}) // hold; shutdown() closes the listener
		return nil
	}
	t.Cleanup(func() { serveFunc = old })
	t.Setenv("DSO_AUTH_TOKEN", "")
	shutdown, _, err := StartRESTServerSupervised(context.Background(), "127.0.0.1:0", nil, nil, nil, zap.NewNop(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown()
	var captured *http.Server
	select {
	case captured = <-capCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server never handed to serveFunc")
	}
	if captured.ReadHeaderTimeout <= 0 || captured.ReadHeaderTimeout > 15*time.Second {
		t.Errorf("ReadHeaderTimeout = %v", captured.ReadHeaderTimeout)
	}
	if captured.ReadTimeout <= 0 || captured.ReadTimeout > 30*time.Second {
		t.Errorf("ReadTimeout = %v", captured.ReadTimeout)
	}
	if captured.WriteTimeout <= 0 || captured.IdleTimeout <= 0 || captured.IdleTimeout > 2*time.Minute {
		t.Errorf("Write/Idle = %v/%v", captured.WriteTimeout, captured.IdleTimeout)
	}
	if captured.MaxHeaderBytes <= 0 || captured.MaxHeaderBytes > 128<<10 {
		t.Errorf("MaxHeaderBytes = %d", captured.MaxHeaderBytes)
	}
}

// ---- WebSocket ----

func wsURL(ts *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/events/ws" + query
}

func dialWS(ts *httptest.Server, query string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	return d.Dial(wsURL(ts, query), hdr)
}

func wsStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func cookieHdr(c *http.Cookie) http.Header {
	h := http.Header{}
	h.Set("Cookie", c.Name+"="+c.Value)
	return h
}

func TestWebSocket_UpgradeAuthAndOrigin(t *testing.T) {
	s, h := hardeningChain(t, false)
	ts := httptest.NewServer(h)
	defer ts.Close()
	sess := login(t, s)
	host := strings.TrimPrefix(ts.URL, "http://")

	// Unauthenticated.
	if _, resp, err := dialWS(ts, "", nil); err == nil || wsStatus(resp) != 401 {
		t.Errorf("unauthenticated upgrade: err=%v status=%d want 401", err, wsStatus(resp))
	}
	// Credentials in the query string are never accepted.
	for _, q := range []string{"?token=" + hardeningToken, "?access_token=" + hardeningToken,
		"?authorization=Bearer%20" + hardeningToken, "?" + webuiauth.CookieName + "=" + sess.Value, "?session=" + sess.Value} {
		if c, resp, err := dialWS(ts, q, nil); err == nil || wsStatus(resp) != 401 {
			if c != nil {
				_ = c.Close()
			}
			t.Errorf("query credential %q: err=%v status=%d want 401", q, err, wsStatus(resp))
		}
	}
	// Valid cookie + hostile / null / cross-site Origin: rejected (CSWSH).
	for _, origin := range []string{"https://evil.example", "null", "http://localhost:1"} {
		hd := cookieHdr(sess)
		hd.Set("Origin", origin)
		if c, resp, err := dialWS(ts, "", hd); err == nil || wsStatus(resp) != 403 {
			if c != nil {
				_ = c.Close()
			}
			t.Errorf("origin %q: err=%v status=%d want 403", origin, err, wsStatus(resp))
		}
	}
	// Bearer with a hostile Origin is also rejected by the upgrader's origin check.
	hd := http.Header{}
	hd.Set("Authorization", "Bearer "+hardeningToken)
	hd.Set("Origin", "https://evil.example")
	if c, resp, err := dialWS(ts, "", hd); err == nil || wsStatus(resp) != 403 {
		if c != nil {
			_ = c.Close()
		}
		t.Errorf("bearer + evil origin: err=%v status=%d want 403", err, wsStatus(resp))
	}
	// Same-origin cookie upgrade succeeds.
	hd = cookieHdr(sess)
	hd.Set("Origin", "http://"+host)
	c, _, err := dialWS(ts, "", hd)
	if err != nil {
		t.Fatalf("same-origin cookie upgrade: %v", err)
	}
	_ = c.Close()
	// Bearer without Origin (non-browser client) succeeds.
	hd = http.Header{}
	hd.Set("Authorization", "Bearer "+hardeningToken)
	c, _, err = dialWS(ts, "", hd)
	if err != nil {
		t.Fatalf("bearer upgrade: %v", err)
	}
	_ = c.Close()
	// A configured allowed origin is honoured by the SAME predicate the gate uses.
	s.AllowedOrigins = []string{"https://dso.example.com"}
	hd = cookieHdr(sess)
	hd.Set("Origin", "https://dso.example.com")
	c, _, err = dialWS(ts, "", hd)
	if err != nil {
		t.Fatalf("allowed_origins upgrade: %v", err)
	}
	_ = c.Close()
}

func setRecheck(t *testing.T, d time.Duration) {
	t.Helper()
	old := wsRecheckNanos.Swap(int64(d))
	t.Cleanup(func() { wsRecheckNanos.Store(old) })
}

func expectPolicyClose(t *testing.T, c *websocket.Conn, within time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(within))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			ce, ok := err.(*websocket.CloseError)
			if !ok || ce.Code != websocket.ClosePolicyViolation {
				t.Fatalf("expected 1008 close, got %T %v", err, err)
			}
			return
		}
	}
}

func TestWebSocket_ClosedWhenSessionRevoked(t *testing.T) {
	setRecheck(t, 40*time.Millisecond)
	s, h := hardeningChain(t, false)
	ts := httptest.NewServer(h)
	defer ts.Close()
	sess := login(t, s)
	c, _, err := dialWS(ts, "", cookieHdr(sess))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s.WebUIAuth.Logout(sess.Value) // logout / revoke-others / eviction all delete the session
	expectPolicyClose(t, c, 3*time.Second)
}

func TestWebSocket_ClosedWhenSessionIdleExpires(t *testing.T) {
	setRecheck(t, 40*time.Millisecond)
	s, h := hardeningChain(t, false)
	hash, _ := webuiauth.HashPassword(testPassword)
	mgr := webuiauth.NewManager(testOperator, hash, 300*time.Millisecond)
	mgr.Secure = false
	s.WebUIAuth = mgr
	ts := httptest.NewServer(h)
	defer ts.Close()
	sess := login(t, s)
	c, _, err := dialWS(ts, "", cookieHdr(sess))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The open socket must NOT keep the idle timer alive.
	expectPolicyClose(t, c, 3*time.Second)
}

func TestWebSocket_ReadLimitEnforced(t *testing.T) {
	s, h := hardeningChain(t, false)
	ts := httptest.NewServer(h)
	defer ts.Close()
	hd := http.Header{}
	hd.Set("Authorization", "Bearer "+hardeningToken)
	c, _, err := dialWS(ts, "", hd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxClientMessageBytes*4))); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			ce, ok := err.(*websocket.CloseError)
			if !ok || ce.Code != websocket.CloseMessageTooBig {
				t.Fatalf("expected 1009 close for oversized frame, got %T %v", err, err)
			}
			break
		}
	}
	_ = s
}

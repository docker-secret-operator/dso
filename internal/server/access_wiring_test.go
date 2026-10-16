package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/webuiauth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const wiredToken = "wired-dso-auth-token-0123456789"

// wired builds a RESTServer through the same functions StartRESTServer uses to
// derive its access configuration (Authenticator from the environment ->
// buildAccessWiring -> applyAccessWiring), without opening a listener or
// touching /var/run/dso. token == "" leaves DSO_AUTH_TOKEN unset.
func wired(t *testing.T, token string, cfg *config.Config, addr string) (*RESTServer, *accessWiring, *observer.ObservedLogs) {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", token)
	authn := auth.NewAuthenticator()
	core, logs := observer.New(zapcore.DebugLevel)
	w, err := buildAccessWiring(cfg, authn, addr, zap.New(core))
	if err != nil {
		t.Fatalf("buildAccessWiring: %v", err)
	}
	s := createTestRESTServer()
	s.Config = cfg
	if cfg == nil {
		s.Config = &config.Config{}
	}
	s.Auth = authn
	s.applyAccessWiring(w)
	return s, w, logs
}

func webUICfg(t *testing.T, mutate func(*config.WebUIConfig)) *config.Config {
	t.Helper()
	hash, err := webuiauth.HashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.WebUI = config.WebUIConfig{Enabled: true, Username: testOperator, PasswordHash: hash, CookieSecure: "never"}
	if mutate != nil {
		mutate(&cfg.WebUI)
	}
	return cfg
}

func req(s *RESTServer, method, path, host string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader("{}"))
	r.RemoteAddr = "192.0.2.50:4000"
	if host != "" {
		r.Host = host
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func loginWired(t *testing.T, s *RESTServer) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"`+testOperator+`","password":"`+testPassword+`"}`))
	r.RemoteAddr = "192.0.2.51:4000"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == webuiauth.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// ---- the four auth modes, through production wiring -------------------------

func TestWiring_NoToken_WebUIDisabled_LegacyLoopbackOnlyMode(t *testing.T) {
	s, w, logs := wired(t, "", &config.Config{}, "127.0.0.1:8471")
	if !w.LegacyOpen || w.HostGuard == nil || w.WebUIAuth != nil || w.WebUI != nil {
		t.Fatalf("wiring = %+v", w)
	}
	if rec := req(s, "GET", "/api/secrets", "127.0.0.1:8471", nil); rec.Code != http.StatusOK {
		t.Errorf("legacy anonymous GET = %d, want 200 (documented compatibility)", rec.Code)
	}
	if rec := req(s, "GET", "/api/secrets", "attacker.example", nil); rec.Code != http.StatusForbidden {
		t.Errorf("legacy request with foreign Host = %d, want 403 (DNS rebinding)", rec.Code)
	}
	if rec := req(s, "POST", "/api/sessions/revoke-others", "localhost:8471", map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Errorf("legacy cross-origin POST = %d, want 403", rec.Code)
	}
	warned := false
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "without authentication") {
			warned = true
		}
	}
	if !warned {
		t.Error("legacy open mode must log a warning")
	}
}

func TestWiring_NoToken_WebUIEnabled_AuthenticationRequired(t *testing.T) {
	s, w, _ := wired(t, "", webUICfg(t, nil), "127.0.0.1:8471")
	if w.LegacyOpen || w.WebUIAuth == nil || w.HostGuard != nil {
		t.Fatalf("wiring = %+v", w)
	}
	for _, r := range protectedRoutes {
		if rec := req(s, r.method, r.path, "127.0.0.1:8471", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", r.method, r.path, rec.Code)
		}
	}
	c := loginWired(t, s)
	if rec := req(s, "GET", "/api/secrets", "127.0.0.1:8471", nil, c); rec.Code != http.StatusOK {
		t.Errorf("valid session = %d, want 200", rec.Code)
	}
}

func TestWiring_Token_WebUIDisabled_BearerRequired(t *testing.T) {
	s, w, _ := wired(t, wiredToken, &config.Config{}, "127.0.0.1:8471")
	if w.LegacyOpen || w.WebUIAuth != nil || w.HostGuard != nil {
		t.Fatalf("wiring = %+v", w)
	}
	for _, r := range protectedRoutes {
		if rec := req(s, r.method, r.path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", r.method, r.path, rec.Code)
		}
	}
	if rec := req(s, "GET", "/api/secrets", "", map[string]string{"Authorization": "Bearer " + wiredToken}); rec.Code != http.StatusOK {
		t.Errorf("valid bearer = %d, want 200", rec.Code)
	}
	if rec := req(s, "GET", "/api/secrets", "", map[string]string{"Authorization": "Bearer nope-nope-nope-nope"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer = %d, want 401", rec.Code)
	}
}

func TestWiring_Token_WebUIEnabled_BearerOrSession(t *testing.T) {
	s, w, _ := wired(t, wiredToken, webUICfg(t, nil), "127.0.0.1:8471")
	if w.LegacyOpen || w.WebUIAuth == nil {
		t.Fatalf("wiring = %+v", w)
	}
	if rec := req(s, "GET", "/api/secrets", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", rec.Code)
	}
	if rec := req(s, "GET", "/api/secrets", "", map[string]string{"Authorization": "Bearer " + wiredToken}); rec.Code != http.StatusOK {
		t.Errorf("bearer = %d, want 200", rec.Code)
	}
	c := loginWired(t, s)
	if rec := req(s, "GET", "/api/secrets", "", nil, c); rec.Code != http.StatusOK {
		t.Errorf("session = %d, want 200", rec.Code)
	}
}

// legacyOpen and the Authenticator must never disagree: LegacyOpen is exactly
// "an anonymous request is authenticated as authOpen".
func TestWiring_LegacyOpenMatchesAuthenticatorForEveryCombination(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		webui  bool
		legacy bool
	}{
		{"no token, no webui", "", false, true},
		{"no token, webui", "", true, false},
		{"token, no webui", wiredToken, false, false},
		{"token, webui", wiredToken, true, false},
	} {
		cfg := &config.Config{}
		if tc.webui {
			cfg = webUICfg(t, nil)
		}
		s, w, _ := wired(t, tc.token, cfg, "127.0.0.1:8471")
		if w.LegacyOpen != tc.legacy {
			t.Errorf("%s: LegacyOpen = %v, want %v", tc.name, w.LegacyOpen, tc.legacy)
		}
		m, _ := s.authenticate(httptest.NewRequest("GET", "/api/secrets", nil))
		if (m == authOpen) != w.LegacyOpen {
			t.Errorf("%s: authenticate(anonymous)=%v disagrees with LegacyOpen=%v", tc.name, m, w.LegacyOpen)
		}
		if (w.HostGuard != nil) != tc.legacy {
			t.Errorf("%s: HostGuard presence %v, want %v (only legacy mode enforces Host without allowed_hosts)", tc.name, w.HostGuard != nil, tc.legacy)
		}
	}
	// A whitespace-only token is a configured (if useless) token for the
	// Authenticator; the wiring must follow it, not re-read the environment.
	_, w, _ := wired(t, "   ", &config.Config{}, "127.0.0.1:8471")
	if w.LegacyOpen {
		t.Error("whitespace token treated as unset by wiring but as set by the Authenticator")
	}
}

// ---- Host / Origin independence and propagation -----------------------------

func TestWiring_AllowedHostsAndOriginsPropagate(t *testing.T) {
	cfg := webUICfg(t, func(u *config.WebUIConfig) {
		u.AllowedHosts = []string{"Dso.Internal"}
		u.AllowedOrigins = []string{"https://dso.example.com"}
	})
	s, w, _ := wired(t, "", cfg, "127.0.0.1:8471")
	if len(s.AllowedOrigins) != 1 || s.AllowedOrigins[0] != "https://dso.example.com" {
		t.Errorf("AllowedOrigins = %v", s.AllowedOrigins)
	}
	if w.HostGuard == nil {
		t.Fatal("allowed_hosts must enable the Host check")
	}
	if got := strings.Join(w.HostGuard.hosts(), ","); got != "dso.example.com,dso.internal" {
		t.Errorf("effective Host allowlist = %q (allowed_hosts + hosts of allowed_origins)", got)
	}
	for host, want := range map[string]int{
		"dso.internal:8471": http.StatusUnauthorized, // passes Host check, then needs auth
		"dso.example.com":   http.StatusUnauthorized,
		"localhost:8471":    http.StatusUnauthorized,
		"10.0.0.5:8471":     http.StatusForbidden,
		"evil.example":      http.StatusForbidden,
	} {
		if rec := req(s, "GET", "/api/secrets", host, nil); rec.Code != want {
			t.Errorf("Host %q = %d, want %d", host, rec.Code, want)
		}
	}
}

// Review finding L3: allowed_origins must not, by itself, start enforcing Host.
func TestWiring_AllowedOriginsAloneDoesNotEnableHostEnforcement_ReverseProxy(t *testing.T) {
	cfg := webUICfg(t, func(u *config.WebUIConfig) { u.AllowedOrigins = []string{"https://dso.example.com"} })
	s, w, _ := wired(t, "", cfg, "127.0.0.1:8471")
	if w.HostGuard != nil {
		t.Fatalf("allowed_origins alone installed a Host guard (hosts %v)", w.HostGuard.hosts())
	}
	c := loginWired(t, s)

	// Reverse proxy rewrote Host to the internal service name; the browser's
	// Origin is the public one. CSRF check uses allowed_origins; no Host check.
	proxied := func(origin string) int {
		return req(s, "POST", "/api/sessions/revoke-others", "dso-agent:8471", map[string]string{"Origin": origin}, c).Code
	}
	if code := proxied("https://dso.example.com"); code != http.StatusOK {
		t.Errorf("proxied same-site POST = %d, want 200", code)
	}
	c = loginWired(t, s)
	if code := proxied("https://evil.example"); code != http.StatusForbidden {
		t.Errorf("proxied foreign-origin POST = %d, want 403", code)
	}
	// Direct-by-IP and Docker service-name access keep working.
	for _, host := range []string{"10.0.0.5:8471", "dso-agent:8471"} {
		c = loginWired(t, s)
		if rec := req(s, "GET", "/api/secrets", host, nil, c); rec.Code != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, rec.Code)
		}
	}
}

func TestWiring_TokenMode_AllowedOriginsAloneLeavesWebhookReachableByAnyHostname(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebUI.AllowedOrigins = []string{"https://dso.example.com"}
	cfg.Agent.Watch.Webhook = config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret}
	s, w, _ := wired(t, wiredToken, cfg, "127.0.0.1:8471")
	if w.HostGuard != nil {
		t.Fatal("unexpected Host guard")
	}
	hreq := signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("proxy"))
	hreq.Host = "ci-runner.corp.example"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, hreq)
	if rec.Code != authenticated {
		t.Errorf("webhook via other hostname = %d, want %d (authenticated)", rec.Code, authenticated)
	}
}

func TestWiring_LegacyMode_AllowedOriginsHostIsAcceptedButNothingElse(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebUI.AllowedOrigins = []string{"https://dso.example.com"}
	s, w, _ := wired(t, "", cfg, "127.0.0.1:8471")
	if w.HostGuard == nil {
		t.Fatal("legacy mode must enforce Host")
	}
	if rec := req(s, "GET", "/api/secrets", "dso.example.com", nil); rec.Code != http.StatusOK {
		t.Errorf("proxy hostname named by allowed_origins = %d, want 200", rec.Code)
	}
	if rec := req(s, "GET", "/api/secrets", "10.0.0.5:8471", nil); rec.Code != http.StatusForbidden {
		t.Errorf("unlisted host in legacy mode = %d, want 403", rec.Code)
	}
}

// ---- session / proxy propagation -------------------------------------------

func loginMaxAge(t *testing.T, s *RESTServer) int {
	t.Helper()
	return loginWired(t, s).MaxAge
}

func TestWiring_SessionTimersPropagate(t *testing.T) {
	cases := []struct {
		name     string
		idle     string
		maxLife  string
		wantLow  int
		wantHigh int
		warn     bool
	}{
		{"defaults", "", "", 1799, 1800, false},                   // idle default 30m < 24h cap
		{"idle 10m", "10m", "", 599, 600, false},                  // SessionIdleTimeout propagates
		{"cap shorter than idle", "1h", "20m", 1199, 1200, false}, // SessionMaxLifetime propagates
		{"cap disabled", "1h", "0", 3599, 3600, false},            // "0" disables the cap
		{"invalid cap -> default", "1h", "banana", 3599, 3600, true},
		{"invalid idle -> default", "bogus", "", 1799, 1800, true},
	}
	for _, tc := range cases {
		cfg := webUICfg(t, func(u *config.WebUIConfig) { u.SessionIdleTimeout = tc.idle; u.SessionMaxLifetime = tc.maxLife })
		s, _, logs := wired(t, "", cfg, "127.0.0.1:8471")
		got := loginMaxAge(t, s)
		if got < tc.wantLow || got > tc.wantHigh {
			t.Errorf("%s: cookie Max-Age = %d, want %d..%d", tc.name, got, tc.wantLow, tc.wantHigh)
		}
		warned := false
		for _, e := range logs.All() {
			if strings.Contains(e.Message, "Invalid webui.") {
				warned = true
			}
		}
		if warned != tc.warn {
			t.Errorf("%s: invalid-value warning = %v, want %v", tc.name, warned, tc.warn)
		}
	}
}

func TestWiring_SessionMaxLifetimeEnforcedEndToEnd(t *testing.T) {
	// 1s cap: the "fresh session" request has ~1s of slack against scheduler
	// jitter (the only jitter-sensitive direction); the final check sleeps 1.3s
	// and time.Sleep never returns early.
	cfg := webUICfg(t, func(u *config.WebUIConfig) { u.SessionMaxLifetime = "1s" })
	s, _, _ := wired(t, "", cfg, "127.0.0.1:8471")
	c := loginWired(t, s)
	if rec := req(s, "GET", "/api/secrets", "", nil, c); rec.Code != http.StatusOK {
		t.Fatalf("fresh session = %d", rec.Code)
	}
	time.Sleep(1300 * time.Millisecond)
	if rec := req(s, "GET", "/api/secrets", "", nil, c); rec.Code != http.StatusUnauthorized {
		t.Errorf("session past configured webui.session_max_lifetime = %d, want 401", rec.Code)
	}
}

func TestWiring_TrustedProxiesPropagate(t *testing.T) {
	attempt := func(s *RESTServer, forwarded string, password string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"username":"`+testOperator+`","password":"`+password+`"}`))
		r.RemoteAddr = "10.0.0.1:5000"
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}

	// Trusted: attacker behind the proxy is throttled; the operator is not.
	cfg := webUICfg(t, func(u *config.WebUIConfig) { u.TrustedProxies = []string{"10.0.0.1"} })
	s, _, _ := wired(t, "", cfg, "127.0.0.1:8471")
	for i := 0; i < 5; i++ {
		attempt(s, "198.51.100.66", "wrong")
	}
	if code := attempt(s, "198.51.100.66", "wrong"); code != http.StatusTooManyRequests {
		t.Errorf("attacker behind trusted proxy = %d, want 429", code)
	}
	if code := attempt(s, "198.51.100.7", testPassword); code != http.StatusOK {
		t.Errorf("operator behind same proxy = %d, want 200 (per-client throttling)", code)
	}

	// Not configured: forwarded headers are ignored, so the peer is the key.
	s2, _, _ := wired(t, "", webUICfg(t, nil), "127.0.0.1:8471")
	for i := 0; i < 5; i++ {
		attempt(s2, "198.51.100."+strconv.Itoa(i+1), "wrong")
	}
	if code := attempt(s2, "198.51.100.200", testPassword); code != http.StatusTooManyRequests {
		t.Errorf("rotating X-Forwarded-For without trusted_proxies = %d, want 429 (shared peer bucket)", code)
	}
}

func TestWiring_InvalidTrustedProxiesFailStartup(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", "")
	cfg := webUICfg(t, func(u *config.WebUIConfig) { u.TrustedProxies = []string{"not-an-ip"} })
	if _, err := buildAccessWiring(cfg, auth.NewAuthenticator(), "127.0.0.1:8471", zap.NewNop()); err == nil || !strings.Contains(err.Error(), "trusted_proxies") {
		t.Fatalf("expected a trusted_proxies error, got %v", err)
	}
}

func TestWiring_CookieSecureFollowsConfig(t *testing.T) {
	s, _, _ := wired(t, "", webUICfg(t, func(u *config.WebUIConfig) { u.CookieSecure = "always" }), "127.0.0.1:8471")
	if !loginWired(t, s).Secure {
		t.Error("cookie_secure=always did not produce a Secure cookie")
	}
}

// ---- StartRESTServer's own early failures (no listener, no filesystem) ------

func TestStartRESTServer_RefusesBadSecurityConfigBeforeAllocatingAnything(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	start := func(addr string, cfg *config.Config) error {
		_, err := StartRESTServer(ctx, addr, nil, nil, cfg, zap.NewNop(), nil)
		return err
	}

	t.Setenv("DSO_AUTH_TOKEN", "")
	if err := start("0.0.0.0:0", &config.Config{}); err == nil || !strings.Contains(err.Error(), "without authentication") {
		t.Errorf("public bind without token: %v", err)
	}
	if err := start("localhost:0", &config.Config{}); err == nil {
		t.Error("a hostname bind is not provably loopback and needs a token")
	}

	t.Setenv("DSO_AUTH_TOKEN", "short")
	if err := start("127.0.0.1:0", &config.Config{}); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("short token: %v", err)
	}
	t.Setenv("DSO_AUTH_TOKEN", strings.Repeat("x", maxAuthTokenBytes+1))
	if err := start("127.0.0.1:0", &config.Config{}); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Errorf("overlong token: %v", err)
	}

	t.Setenv("DSO_AUTH_TOKEN", wiredToken)
	bad := &config.Config{}
	bad.Agent.Watch.Webhook = config.WebhookConfig{Enabled: true, HMACSecret: "short"}
	if err := start("127.0.0.1:0", bad); err == nil || !strings.Contains(err.Error(), "hmac_secret") {
		t.Errorf("short hmac_secret: %v", err)
	}
	badProxy := webUICfg(t, func(u *config.WebUIConfig) { u.TrustedProxies = []string{"nonsense"} })
	if err := start("127.0.0.1:0", badProxy); err == nil || !strings.Contains(err.Error(), "trusted_proxies") {
		t.Errorf("bad trusted_proxies: %v", err)
	}
}

// ---- webhook guard lifecycle ------------------------------------------------

// webhookServerAt returns a server whose guard was initialized at start via the
// same method StartRESTServer uses, with a controllable clock.
func webhookServerAt(t *testing.T, start time.Time, now *time.Time, mutate func(*config.WebhookConfig)) *RESTServer {
	t.Helper()
	s, _ := newWebhookServer(t, mutate)
	s.initWebhookGuard(start)
	s.webhookGuard.now = func() time.Time { return *now }
	return s
}

func TestWebhookGuard_FloorIsEstablishedAtInitializationNotByFirstRequest(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	now := start
	s := webhookServerAt(t, start, &now, nil)

	// Before any request exists, the guard and its floor are already in place.
	if s.webhookGuard == nil || s.webhookGuard.floor != start.Unix() {
		t.Fatalf("guard not initialized at start: %+v", s.webhookGuard)
	}

	// The first webhook arrives 4 minutes after start. A request signed at
	// start+10s is legitimate (after start) and must be accepted. A lazily
	// created guard would have floor = start+240s and reject it.
	now = start.Add(240 * time.Second)
	if w := serve(s, signedRequest(testHMACSecret, start.Add(10*time.Second).Unix(), webhookBody("early"))); w.Code != authenticated {
		t.Fatalf("request signed after start but before the first webhook = %d, want %d (floor must not move to first request)", w.Code, authenticated)
	}
	if s.webhookGuard.floor != start.Unix() {
		t.Errorf("floor moved to %d after a request", s.webhookGuard.floor)
	}
	// A request signed before start is rejected even though it is inside tolerance.
	if w := serve(s, signedRequest(testHMACSecret, start.Add(-1*time.Second).Unix(), webhookBody("pre-start"))); w.Code != http.StatusUnauthorized {
		t.Errorf("request signed before start = %d, want 401", w.Code)
	}
}

func TestWebhookGuard_FloorBoundary(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	now := start.Add(30 * time.Second)
	s := webhookServerAt(t, start, &now, nil)
	for _, tc := range []struct {
		name string
		ts   int64
		want int
	}{
		{"exactly at start", start.Unix(), authenticated},
		{"one second before start", start.Unix() - 1, http.StatusUnauthorized},
		{"one second after start", start.Unix() + 1, authenticated},
	} {
		if w := serve(s, signedRequest(testHMACSecret, tc.ts, webhookBody(tc.name))); w.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestWebhookGuard_ToleranceBoundaryWithInjectedClock(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	now := start.Add(1000 * time.Second)
	s := webhookServerAt(t, start, &now, func(w *config.WebhookConfig) { w.TimestampTolerance = "60s" })
	for _, tc := range []struct {
		name  string
		delta int64
		want  int
	}{
		{"now-60s (limit)", -60, authenticated},
		{"now-61s", -61, http.StatusUnauthorized},
		{"now+60s (limit)", 60, authenticated},
		{"now+61s", 61, http.StatusUnauthorized},
	} {
		if w := serve(s, signedRequest(testHMACSecret, now.Unix()+tc.delta, webhookBody(tc.name))); w.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

// Simulated restart: a captured request cannot be replayed into a new server
// instance even though that instance's replay table is empty.
func TestWebhookGuard_RestartRejectsRequestsSignedBeforeTheNewStart(t *testing.T) {
	t0 := time.Unix(1_800_000_000, 0)
	nowA := t0.Add(60 * time.Second)
	a := webhookServerAt(t, t0, &nowA, nil)
	captured := signedRequestBytes(testHMACSecret, t0.Add(50*time.Second).Unix(), webhookBody("captured"))
	if w := serve(a, captured.build()); w.Code != authenticated {
		t.Fatalf("original delivery = %d", w.Code)
	}
	if w := serve(a, captured.build()); w.Code != http.StatusConflict {
		t.Fatalf("same-instance replay = %d, want 409", w.Code)
	}

	// "Restart" 50s later: new guard, empty table, later floor.
	restart := t0.Add(100 * time.Second)
	nowB := restart.Add(10 * time.Second)
	b := webhookServerAt(t, restart, &nowB, nil)
	if len(b.webhookGuard.seen) != 0 {
		t.Fatal("new instance should start with an empty replay table")
	}
	if w := serve(b, captured.build()); w.Code != http.StatusUnauthorized {
		t.Errorf("replay of pre-restart request into restarted server = %d, want 401", w.Code)
	}
	if w := serve(b, signedRequest(testHMACSecret, restart.Add(5*time.Second).Unix(), webhookBody("fresh"))); w.Code != authenticated {
		t.Errorf("fresh post-restart delivery = %d, want %d", w.Code, authenticated)
	}
}

func TestWebhookGuard_InitIsIdempotentAndNeverResetsState(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	now := start.Add(10 * time.Second)
	s := webhookServerAt(t, start, &now, nil)
	first := s.webhookGuard
	ts := start.Add(5 * time.Second).Unix()
	if w := serve(s, signedRequest(testHMACSecret, ts, webhookBody("once"))); w.Code != authenticated {
		t.Fatalf("delivery = %d", w.Code)
	}

	s.initWebhookGuard(start.Add(3600 * time.Second)) // must be a no-op
	if s.webhookGuard != first || s.webhookGuard.floor != start.Unix() {
		t.Fatal("second init replaced the guard or moved the floor")
	}
	if w := serve(s, signedRequest(testHMACSecret, ts, webhookBody("once"))); w.Code != http.StatusConflict {
		t.Errorf("replay after redundant init = %d, want 409 (table must survive)", w.Code)
	}
	// Request handling never swaps the guard either.
	for i := 0; i < 5; i++ {
		serve(s, signedRequest(testHMACSecret, ts, webhookBody("again"+strconv.Itoa(i))))
	}
	if s.replayGuard() != first {
		t.Error("request handling replaced the guard")
	}
}

// signedRequestBytes lets one signed request be delivered several times.
type reusable struct {
	ts   int64
	body []byte
	sig  string
}

func signedRequestBytes(secret string, ts int64, body []byte) reusable {
	return reusable{ts: ts, body: body, sig: WebhookSignature(secret, ts, body)}
}
func (r reusable) build() *http.Request { return requestWithSignature(r.ts, r.body, r.sig) }

// ---- integrations status ----------------------------------------------------

func integrationAuthConfigured(t *testing.T, wh config.WebhookConfig) bool {
	t.Helper()
	s, _, _ := wired(t, "", &config.Config{}, "127.0.0.1:8471")
	s.Config.Agent.Watch.Webhook = wh
	rec := req(s, "GET", "/api/integrations", "127.0.0.1:8471", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/integrations = %d", rec.Code)
	}
	var out struct {
		Integrations []struct {
			Name           string `json:"name"`
			AuthConfigured bool   `json:"auth_configured"`
		} `json:"integrations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, i := range out.Integrations {
		if i.Name == "secret-update-webhook" {
			return i.AuthConfigured
		}
	}
	t.Fatal("webhook integration missing")
	return false
}

func TestIntegrations_WebhookAuthStatusRecognizesHMAC(t *testing.T) {
	if !integrationAuthConfigured(t, config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret}) {
		t.Error("HMAC-only webhook reported as unauthenticated")
	}
	if !integrationAuthConfigured(t, config.WebhookConfig{Enabled: true, AuthToken: testLegacyToken}) {
		t.Error("legacy-token webhook reported as unauthenticated")
	}
	if !integrationAuthConfigured(t, config.WebhookConfig{Enabled: true, AuthToken: testLegacyToken, HMACSecret: testHMACSecret}) {
		t.Error("webhook with both credentials reported as unauthenticated")
	}
	if integrationAuthConfigured(t, config.WebhookConfig{Enabled: true}) {
		t.Error("webhook with no credential reported as authenticated")
	}
}

// There must be one source of truth for "is a bearer token configured": the
// Authenticator instance the server verifies requests with. If the environment
// changes after the Authenticator was built (or the wiring re-read it), the two
// would disagree and the API could be left open while requests are denied, or
// vice versa. This pins the wiring to the instance.
func TestWiring_LegacyOpenFollowsTheAuthenticatorNotTheEnvironment(t *testing.T) {
	t.Setenv("DSO_AUTH_TOKEN", wiredToken)
	authn := auth.NewAuthenticator() // token captured here

	t.Setenv("DSO_AUTH_TOKEN", "") // environment now says "unset"
	w, err := buildAccessWiring(&config.Config{}, authn, "127.0.0.1:8471", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if w.LegacyOpen || w.HostGuard != nil {
		t.Errorf("wiring re-read the environment: LegacyOpen=%v HostGuard=%v, but the Authenticator has a token", w.LegacyOpen, w.HostGuard != nil)
	}

	// And the reverse: Authenticator without a token, environment later set.
	t.Setenv("DSO_AUTH_TOKEN", "")
	open := auth.NewAuthenticator()
	t.Setenv("DSO_AUTH_TOKEN", wiredToken)
	w, err = buildAccessWiring(&config.Config{}, open, "127.0.0.1:8471", zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if !w.LegacyOpen {
		t.Error("wiring followed the environment instead of the Authenticator (which has no token)")
	}
}

// trusted_proxies advisories reach the startup log (not just the Manager).
func TestWiring_TrustedProxyAdvisoriesAreLoggedAtStartup(t *testing.T) {
	collect := func(entries ...string) []string {
		cfg := webUICfg(t, func(u *config.WebUIConfig) { u.TrustedProxies = entries })
		_, _, logs := wired(t, "", cfg, "127.0.0.1:8471")
		var msgs []string
		for _, e := range logs.All() {
			if strings.Contains(e.Message, "trusted_proxies") {
				msgs = append(msgs, e.Message)
			}
		}
		return msgs
	}
	if m := collect("10.0.0.5"); len(m) != 0 {
		t.Errorf("an exact proxy address must not warn: %v", m)
	}
	if m := collect("10.0.0.0/8"); len(m) != 1 || !strings.Contains(m[0], "broad range") {
		t.Errorf("broad range: want one 'broad range' warning, got %v", m)
	}
	for _, all := range []string{"0.0.0.0/0", "::/0"} {
		if m := collect(all); len(m) != 1 || !strings.Contains(m[0], "IGNORED") {
			t.Errorf("%s: want one IGNORED warning, got %v", all, m)
		}
	}
}

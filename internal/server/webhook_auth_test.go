package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	testHMACSecret   = "hmac-secret-0123456789abcdef"
	testLegacyToken  = "legacy-webhook-token-123456"
	testGlobalToken  = "global-dso-auth-token-123456"
	webhookSecretMsg = "S3cr3tBodyMarker" // must never appear in logs
)

// newWebhookServer returns a server whose webhook is enabled. A request that
// passes authentication reaches "Secret not configured" (404) because no
// secrets are mapped: 404 therefore means "authenticated", while 401/403/409/
// 413 mean "rejected by webhook authentication". No rotation is ever triggered.
func newWebhookServer(t *testing.T, mutate func(*config.WebhookConfig)) (*RESTServer, *observer.ObservedLogs) {
	t.Helper()
	t.Setenv("DSO_AUTH_TOKEN", "")
	s := createTestRESTServer()
	s.Auth = auth.NewAuthenticator()
	core, logs := observer.New(zapcore.DebugLevel)
	s.Logger = zap.New(core)
	wh := config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret}
	if mutate != nil {
		mutate(&wh)
	}
	s.Config.Agent.Watch.Webhook = wh
	return s, logs
}

func webhookBody(extra string) []byte {
	b, _ := json.Marshal(WebhookPayload{
		Provider: "vault", SecretName: "unknown_secret", EventType: "secret_updated",
		Timestamp: extra,
	})
	return b
}

func signedRequest(secret string, ts int64, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(webhookTimestampHeader, strconv.FormatInt(ts, 10))
	req.Header.Set(webhookSignatureHeader, WebhookSignature(secret, ts, body))
	return req
}

func serve(s *RESTServer, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

const authenticated = http.StatusNotFound // see newWebhookServer

func TestWebhookHMAC_ValidSignatureAuthenticates_WithoutGlobalToken(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	if w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("1"))); w.Code != authenticated {
		t.Fatalf("valid signed webhook = %d (%s), want %d (authenticated)", w.Code, w.Body.String(), authenticated)
	}
}

// F2 regression: the webhook credential must not have to equal, or be
// accompanied by, DSO_AUTH_TOKEN.
func TestWebhookHMAC_WorksWhenGlobalTokenIsSetAndDifferent(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	t.Setenv("DSO_AUTH_TOKEN", testGlobalToken)
	s.Auth = auth.NewAuthenticator()
	if w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("1"))); w.Code != authenticated {
		t.Fatalf("signed webhook with a different global token configured = %d, want %d", w.Code, authenticated)
	}
	// And the global token alone is NOT a webhook credential.
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(webhookBody("2")))
	req.Header.Set("Authorization", "Bearer "+testGlobalToken)
	if w := serve(s, req); w.Code != http.StatusUnauthorized {
		t.Fatalf("global token presented to webhook = %d, want 401", w.Code)
	}
}

func TestWebhookHMAC_RejectsBadCredentials(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("1")

	tamper := signedRequest(testHMACSecret, now, body)
	tamper.Body = http.NoBody
	tamper = httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(webhookBody("tampered")))
	tamper.Header.Set(webhookTimestampHeader, strconv.FormatInt(now, 10))
	tamper.Header.Set(webhookSignatureHeader, WebhookSignature(testHMACSecret, now, body))

	reTimestamped := signedRequest(testHMACSecret, now, body) // sign for ts, send a different ts
	reTimestamped.Header.Set(webhookTimestampHeader, strconv.FormatInt(now+1, 10))

	noSig := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	noSig.Header.Set(webhookTimestampHeader, strconv.FormatInt(now, 10))

	noTS := signedRequest(testHMACSecret, now, body)
	noTS.Header.Del(webhookTimestampHeader)

	good := WebhookSignature(testHMACSecret, now, body)
	cases := map[string]*http.Request{
		"wrong secret":        signedRequest("some-other-secret-0123456", now, body),
		"tampered body":       tamper,
		"timestamp swapped":   reTimestamped,
		"no signature":        noSig,
		"no timestamp":        noTS,
		"non-numeric ts":      withHeaders(signedRequest(testHMACSecret, now, body), webhookTimestampHeader, "yesterday"),
		"negative ts":         withHeaders(signedRequest(testHMACSecret, now, body), webhookTimestampHeader, "-5"),
		"huge ts":             withHeaders(signedRequest(testHMACSecret, now, body), webhookTimestampHeader, "99999999999999999999"),
		"sig without prefix":  withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, strings.TrimPrefix(good, "v1=")),
		"sig wrong version":   withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, "v2="+strings.TrimPrefix(good, "v1=")),
		"sig truncated":       withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, good[:len(good)-2]),
		"sig non-hex":         withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, "v1="+strings.Repeat("z", 64)),
		"sig with extra data": withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, good+",v1="+strings.TrimPrefix(good, "v1=")),
	}
	for name, req := range cases {
		if w := serve(s, req); w.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", name, w.Code)
		}
	}
}

func withHeaders(r *http.Request, k, v string) *http.Request { r.Header.Set(k, v); return r }

func TestWebhookHMAC_TimestampTolerance(t *testing.T) {
	s, _ := newWebhookServer(t, nil) // default tolerance 5m
	now := time.Now().Unix()
	s.replayGuard().floor = now - 3600 // isolate tolerance from the restart floor
	for _, tc := range []struct {
		name  string
		delta int64
		want  int
	}{
		{"fresh", 0, authenticated},
		{"4m old", -240, authenticated},
		{"4m in future", 240, authenticated},
		{"stale 10m", -600, http.StatusUnauthorized},
		{"future 10m", 600, http.StatusUnauthorized},
	} {
		w := serve(s, signedRequest(testHMACSecret, now+tc.delta, webhookBody(tc.name)))
		if w.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, w.Code, tc.want)
		}
	}

	custom, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.TimestampTolerance = "30s" })
	custom.replayGuard().floor = now - 3600
	if w := serve(custom, signedRequest(testHMACSecret, now-60, webhookBody("a"))); w.Code != http.StatusUnauthorized {
		t.Errorf("60s old with 30s tolerance = %d, want 401", w.Code)
	}
	if w := serve(custom, signedRequest(testHMACSecret, now-10, webhookBody("b"))); w.Code != authenticated {
		t.Errorf("10s old with 30s tolerance = %d, want %d", w.Code, authenticated)
	}
}

func TestWebhookHMAC_ReplayIsRejected(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("replay")
	if w := serve(s, signedRequest(testHMACSecret, now, body)); w.Code != authenticated {
		t.Fatalf("first delivery = %d, want %d", w.Code, authenticated)
	}
	if w := serve(s, signedRequest(testHMACSecret, now, body)); w.Code != http.StatusConflict {
		t.Fatalf("replayed delivery = %d, want 409", w.Code)
	}
	// A genuinely new delivery (different body) still works.
	if w := serve(s, signedRequest(testHMACSecret, now, webhookBody("next"))); w.Code != authenticated {
		t.Fatalf("new delivery = %d, want %d", w.Code, authenticated)
	}
}

func TestWebhookHMAC_ConcurrentReplayOnlyOneWins(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("race")
	const n = 40
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- serve(s, signedRequest(testHMACSecret, now, body)).Code
		}()
	}
	wg.Wait()
	close(codes)
	won, replays := 0, 0
	for c := range codes {
		switch c {
		case authenticated:
			won++
		case http.StatusConflict:
			replays++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if won != 1 || replays != n-1 {
		t.Fatalf("concurrent identical deliveries: %d accepted, %d replays; want exactly 1 and %d", won, replays, n-1)
	}
}

func TestWebhookHMAC_UnauthenticatedTrafficDoesNotFillReplayTable(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	for i := 0; i < 50; i++ {
		serve(s, signedRequest("wrong-secret-0123456789", now, webhookBody(fmt.Sprint(i))))
	}
	if n := len(s.replayGuard().seen); n != 0 {
		t.Fatalf("replay table holds %d entries after only unauthenticated traffic", n)
	}
}

func TestWebhookHMAC_RequestPredatingProcessStartIsRejected(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	s.replayGuard().floor = now + 100 // pretend the process started "after" this request was signed
	if w := serve(s, signedRequest(testHMACSecret, now, webhookBody("old"))); w.Code != http.StatusUnauthorized {
		t.Fatalf("request signed before process start = %d, want 401 (restart replay gap)", w.Code)
	}
}

func TestReplayGuard_ExpiryAndBound(t *testing.T) {
	clock := time.Unix(1_000_000, 0)
	g := newReplayGuard()
	g.now = func() time.Time { return clock }
	g.max = 2

	if err := g.remember("a", clock.Unix()+10); err != nil {
		t.Fatal(err)
	}
	if err := g.remember("a", clock.Unix()+10); err != errWebhookReplay {
		t.Fatalf("duplicate = %v, want replay", err)
	}
	if err := g.remember("b", clock.Unix()+100); err != nil {
		t.Fatal(err)
	}
	// Table full of live entries: fail closed rather than evict.
	if err := g.remember("c", clock.Unix()+100); err != errReplayFull {
		t.Fatalf("full table = %v, want errReplayFull", err)
	}
	// After "a" expires its slot is reclaimed and "a" itself is acceptable again.
	clock = clock.Add(11 * time.Second)
	if err := g.remember("c", clock.Unix()+100); err != nil {
		t.Fatalf("after expiry purge = %v, want nil", err)
	}
	if _, ok := g.seen["a"]; ok {
		t.Error("expired entry was not purged")
	}
}

func TestWebhookHMAC_BodySizeLimit(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	big := bytes.Repeat([]byte("x"), maxWebhookBodyBytes+1)

	// Declared length: rejected before any body is read.
	req := signedRequest(testHMACSecret, now, big)
	if w := serve(s, req); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized Content-Length = %d, want 413", w.Code)
	}
	// Undeclared length (chunked): rejected while reading, never fully buffered.
	req = signedRequest(testHMACSecret, now, big)
	req.ContentLength = -1
	if w := serve(s, req); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized chunked body = %d, want 413", w.Code)
	}
	// Exactly at the limit is fine for size purposes (and authenticates).
	ok := bytes.Repeat([]byte("y"), maxWebhookBodyBytes)
	if w := serve(s, signedRequest(testHMACSecret, now, ok)); w.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("body at the limit was rejected as too large")
	}
}

func TestWebhook_ConfigurationGates(t *testing.T) {
	now := time.Now().Unix()
	body := webhookBody("1")

	disabled, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.Enabled = false })
	if w := serve(disabled, signedRequest(testHMACSecret, now, body)); w.Code != http.StatusForbidden {
		t.Errorf("disabled = %d, want 403", w.Code)
	}
	none, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.HMACSecret = "" })
	if w := serve(none, signedRequest(testHMACSecret, now, body)); w.Code != http.StatusForbidden {
		t.Errorf("enabled with no credential = %d, want 403", w.Code)
	}
	// Anonymous request to a fully configured webhook is never accepted: the
	// route's exemption from the global gate depends on this.
	configured, _ := newWebhookServer(t, nil)
	if w := serve(configured, httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous POST to configured webhook = %d, want 401", w.Code)
	}
	// Only POST is exempt: GET on the same path still needs the global credential.
	withWebUI := newWebUIServer(t, "")
	withWebUI.Config = configured.Config
	if w := serve(withWebUI, httptest.NewRequest(http.MethodGet, webhookPath, nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("GET %s anonymous = %d, want 401", webhookPath, w.Code)
	}
}

func TestWebhook_SignatureHeaderWithoutHMACConfiguredIsRejected(t *testing.T) {
	s, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.HMACSecret = ""; w.AuthToken = testLegacyToken })
	if w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("1"))); w.Code != http.StatusUnauthorized {
		t.Errorf("signed request to legacy-only webhook = %d, want 401", w.Code)
	}
}

// ---- legacy bearer mode ----------------------------------------------------

func legacyReq(token string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestWebhookLegacy_BearerStillAcceptedWithDeprecationWarning(t *testing.T) {
	s, logs := newWebhookServer(t, func(w *config.WebhookConfig) { w.HMACSecret = ""; w.AuthToken = testLegacyToken })
	// Different global token set: the legacy webhook token alone suffices now.
	t.Setenv("DSO_AUTH_TOKEN", testGlobalToken)
	s.Auth = auth.NewAuthenticator()

	if w := serve(s, legacyReq(testLegacyToken, webhookBody("1"))); w.Code != authenticated {
		t.Fatalf("legacy webhook = %d, want %d", w.Code, authenticated)
	}
	if w := serve(s, legacyReq(testLegacyToken, webhookBody("2"))); w.Code != authenticated {
		t.Fatalf("second legacy webhook = %d, want %d", w.Code, authenticated)
	}
	warned := 0
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "DEPRECATED") {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("deprecation warning logged %d times, want exactly once per server", warned)
	}
	if w := serve(s, legacyReq("wrong-legacy-token-123456", webhookBody("3"))); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong legacy token = %d, want 401", w.Code)
	}
}

func TestWebhookLegacy_CanCoexistWithHMAC_AndBeDisabled(t *testing.T) {
	both, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.AuthToken = testLegacyToken })
	if w := serve(both, legacyReq(testLegacyToken, webhookBody("1"))); w.Code != authenticated {
		t.Errorf("legacy while HMAC also configured = %d, want %d", w.Code, authenticated)
	}
	strict, _ := newWebhookServer(t, func(w *config.WebhookConfig) { w.AuthToken = testLegacyToken; w.DisableLegacyAuth = true })
	if w := serve(strict, legacyReq(testLegacyToken, webhookBody("2"))); w.Code != http.StatusUnauthorized {
		t.Errorf("legacy with disable_legacy_auth = %d, want 401", w.Code)
	}
	if w := serve(strict, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("3"))); w.Code != authenticated {
		t.Errorf("HMAC with legacy disabled = %d, want %d", w.Code, authenticated)
	}
	// Legacy token with nothing else usable configured at all -> 403 config error.
	onlyLegacyDisabled, _ := newWebhookServer(t, func(w *config.WebhookConfig) {
		w.HMACSecret = ""
		w.AuthToken = testLegacyToken
		w.DisableLegacyAuth = true
	})
	if w := serve(onlyLegacyDisabled, legacyReq(testLegacyToken, webhookBody("4"))); w.Code != http.StatusForbidden {
		t.Errorf("no usable credential = %d, want 403", w.Code)
	}
}

// ---- no credential/body leakage in logs -----------------------------------

func TestWebhook_LogsNeverContainCredentialsSignaturesOrBodies(t *testing.T) {
	s, logs := newWebhookServer(t, func(w *config.WebhookConfig) { w.AuthToken = testLegacyToken })
	now := time.Now().Unix()
	body := webhookBody(webhookSecretMsg)
	sig := WebhookSignature(testHMACSecret, now, body)

	serve(s, signedRequest("wrong-secret-0123456789", now, body)) // bad signature
	serve(s, signedRequest(testHMACSecret, now-9999, body))       // stale
	serve(s, signedRequest(testHMACSecret, now, body))            // accepted
	serve(s, signedRequest(testHMACSecret, now, body))            // replay
	serve(s, legacyReq(testLegacyToken, body))                    // legacy accepted
	serve(s, legacyReq("not-the-token-0123456789", body))         // legacy rejected
	serve(s, withHeaders(signedRequest(testHMACSecret, now, body), webhookSignatureHeader, "v1=garbage"))

	forbidden := []string{testHMACSecret, testLegacyToken, "not-the-token-0123456789", "wrong-secret-0123456789",
		webhookSecretMsg, strings.TrimPrefix(sig, "v1="), "v1=garbage"}
	for _, e := range logs.All() {
		blob := e.Message
		for _, f := range e.Context {
			blob += " " + f.Key + "=" + f.String + fmt.Sprint(f.Interface)
		}
		for _, bad := range forbidden {
			if strings.Contains(blob, bad) {
				t.Errorf("log entry leaks %q: %s", bad, blob)
			}
		}
	}
	if len(logs.All()) == 0 {
		t.Fatal("expected rejection/deprecation log entries (test is vacuous otherwise)")
	}
}

// ---- startup validation ----------------------------------------------------

func TestValidateWebhookConfig(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	mk := func(w config.WebhookConfig) *config.Config {
		c := &config.Config{}
		c.Agent.Watch.Webhook = w
		return c
	}
	if err := validateWebhookConfig(mk(config.WebhookConfig{Enabled: true, HMACSecret: "short"}), logger); err == nil {
		t.Error("short hmac_secret must be rejected")
	}
	if err := validateWebhookConfig(mk(config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret, TimestampTolerance: "forever"}), logger); err == nil {
		t.Error("unparsable tolerance must be rejected")
	}
	for _, bad := range []string{"0s", "-1m", "2h"} {
		if err := validateWebhookConfig(mk(config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret, TimestampTolerance: bad}), logger); err == nil {
			t.Errorf("tolerance %q must be rejected", bad)
		}
	}
	if err := validateWebhookConfig(mk(config.WebhookConfig{Enabled: true, HMACSecret: testHMACSecret, TimestampTolerance: "10m"}), logger); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	if err := validateWebhookConfig(mk(config.WebhookConfig{Enabled: false, HMACSecret: "short"}), logger); err != nil {
		t.Errorf("disabled webhook must not be validated: %v", err)
	}
	logs.TakeAll()
	_ = validateWebhookConfig(mk(config.WebhookConfig{Enabled: true, AuthToken: testLegacyToken}), logger)
	found := false
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "DEPRECATED") {
			found = true
		}
		if strings.Contains(e.Message, testLegacyToken) {
			t.Error("startup warning leaked the token")
		}
	}
	if !found {
		t.Error("expected a startup deprecation warning for legacy bearer auth")
	}
}

// ---- replay key canonicalization (review finding H1) -----------------------

// caseVariant returns hex with its letters re-cased per pattern: 0 = as-is,
// 1 = upper, 2 = alternating, 3 = lower. Digits are unaffected.
func caseVariant(sigHeader string, pattern int) string {
	hexPart := strings.TrimPrefix(sigHeader, webhookSigPrefix)
	switch pattern {
	case 1:
		hexPart = strings.ToUpper(hexPart)
	case 2:
		b := []byte(hexPart)
		for i := range b {
			if i%2 == 0 && b[i] >= 'a' && b[i] <= 'f' {
				b[i] -= 'a' - 'A'
			}
		}
		hexPart = string(b)
	case 3:
		hexPart = strings.ToLower(hexPart)
	}
	return webhookSigPrefix + hexPart
}

func requestWithSignature(ts int64, body []byte, sigHeader string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, webhookPath, bytes.NewReader(body))
	req.Header.Set(webhookTimestampHeader, strconv.FormatInt(ts, 10))
	req.Header.Set(webhookSignatureHeader, sigHeader)
	return req
}

// Finding H1: hex.DecodeString accepts upper and mixed case, so the replay
// table must be keyed on the decoded MAC, not on the header text.
func TestWebhookHMAC_ReplayWithSignatureCaseVariantsIsRejected(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("case-replay")
	sig := WebhookSignature(testHMACSecret, now, body)
	if strings.ToUpper(sig) == sig || !strings.ContainsAny(strings.TrimPrefix(sig, "v1="), "abcdef") {
		t.Skip("signature has no letters to re-case (astronomically unlikely)")
	}

	if w := serve(s, requestWithSignature(now, body, sig)); w.Code != authenticated {
		t.Fatalf("first delivery = %d, want %d", w.Code, authenticated)
	}
	for name, pattern := range map[string]int{"upper": 1, "mixed": 2, "lower again": 3, "as sent": 0} {
		if w := serve(s, requestWithSignature(now, body, caseVariant(sig, pattern))); w.Code != http.StatusConflict {
			t.Errorf("%s-case replay = %d, want 409", name, w.Code)
		}
	}
	if n := len(s.replayGuard().seen); n != 1 {
		t.Errorf("replay table has %d entries after 1 distinct delivery, want 1 (case variants must not add entries)", n)
	}
}

func TestWebhookHMAC_ConcurrentCaseVariantReplaysOnlyOneWins(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("case-race")
	sig := WebhookSignature(testHMACSecret, now, body)
	const n = 48
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes <- serve(s, requestWithSignature(now, body, caseVariant(sig, i%4))).Code
		}(i)
	}
	wg.Wait()
	close(codes)
	won, replays := 0, 0
	for c := range codes {
		switch c {
		case authenticated:
			won++
		case http.StatusConflict:
			replays++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if won != 1 || replays != n-1 {
		t.Fatalf("%d accepted / %d replays across case variants, want exactly 1 / %d", won, replays, n-1)
	}
}

// Invalid signatures must never be remembered, whatever their casing.
func TestWebhookHMAC_InvalidSignaturesAreNotStored(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	now := time.Now().Unix()
	body := webhookBody("never-stored")
	bad := WebhookSignature("not-the-secret-0123456789", now, body)
	for pattern := 0; pattern < 4; pattern++ {
		if w := serve(s, requestWithSignature(now, body, caseVariant(bad, pattern))); w.Code != http.StatusUnauthorized {
			t.Errorf("invalid signature (pattern %d) = %d, want 401", pattern, w.Code)
		}
	}
	if n := len(s.replayGuard().seen); n != 0 {
		t.Errorf("replay table holds %d entries after only invalid signatures", n)
	}
}

// The attack cannot be used to exhaust the table either: N case variants of one
// accepted request consume one slot, not N.
func TestWebhookHMAC_CaseVariantsCannotFillReplayTable(t *testing.T) {
	s, _ := newWebhookServer(t, nil)
	s.replayGuard().max = 3
	now := time.Now().Unix()
	body := webhookBody("fill")
	sig := WebhookSignature(testHMACSecret, now, body)
	serve(s, requestWithSignature(now, body, sig))
	for i := 0; i < 20; i++ {
		serve(s, requestWithSignature(now, body, caseVariant(sig, i%4)))
	}
	if w := serve(s, signedRequest(testHMACSecret, now, webhookBody("legit-other"))); w.Code != authenticated {
		t.Fatalf("legitimate delivery after variant flood = %d, want %d (table must not be exhausted)", w.Code, authenticated)
	}
}

// ---- HMAC key strength (non-breaking warning; minimum unchanged) -----------------

func TestValidateWebhookConfig_WarnsOnShortButAcceptedHMACSecret(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	mk := func(secret string) *config.Config {
		c := &config.Config{}
		c.Agent.Watch.Webhook = config.WebhookConfig{Enabled: true, HMACSecret: secret}
		return c
	}
	count := func() int {
		n := 0
		for _, e := range logs.TakeAll() {
			if strings.Contains(e.Message, "hmac_secret is only") {
				n++
				if strings.Contains(e.Message, "0123456789abcdef") {
					t.Errorf("warning leaked secret material: %s", e.Message)
				}
			}
		}
		return n
	}

	// 16..31 bytes: accepted (backward compatible) but warned.
	for _, secret := range []string{"0123456789abcdef", "0123456789abcdef0123456789abcde"} {
		if err := validateWebhookConfig(mk(secret), logger); err != nil {
			t.Errorf("%d-byte secret must still be accepted: %v", len(secret), err)
		}
		if count() != 1 {
			t.Errorf("%d-byte secret: expected exactly one strength warning", len(secret))
		}
	}
	// 32+ bytes: no warning. Under 16: still a hard error (unchanged).
	if err := validateWebhookConfig(mk("0123456789abcdef0123456789abcdef"), logger); err != nil || count() != 0 {
		t.Errorf("32-byte secret must be accepted silently (err=%v)", err)
	}
	if err := validateWebhookConfig(mk("0123456789abcde"), logger); err == nil {
		t.Error("15-byte secret must still be rejected")
	}
}

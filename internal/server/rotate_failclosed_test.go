package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// fakeRotationEngine is the test double of RotationEngine. It is not a rotation
// implementation: it only records calls and returns a canned answer.
type fakeRotationEngine struct {
	mu         sync.Mutex
	manualErr  error
	webhookErr error
	manual     []string // "secret|actor"
	webhooks   []string
}

func (f *fakeRotationEngine) RotateSecretManually(_ context.Context, secret, actor string, onComplete func(error)) error {
	f.mu.Lock()
	f.manual = append(f.manual, secret+"|"+actor)
	f.mu.Unlock()
	if f.manualErr != nil {
		return f.manualErr
	}
	if onComplete != nil {
		onComplete(nil)
	}
	return nil
}

func (f *fakeRotationEngine) HandleWebhook(_ string, _ config.ProviderConfig, sec config.SecretMapping, _ string) error {
	f.mu.Lock()
	f.webhooks = append(f.webhooks, sec.Name)
	f.mu.Unlock()
	return f.webhookErr
}

func (f *fakeRotationEngine) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.manual), len(f.webhooks)
}

func assertUnavailable503(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type %q, want JSON", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != "rotation_engine_unavailable" || body["message"] == "" {
		t.Fatalf("body %q (%v)", w.Body.String(), err)
	}
}

func postJSON(s *RESTServer, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

// Manual rotation without an engine fails closed: 503, a stable body, no panic,
// nothing started.
func TestManualRotate_NoEngine_FailsClosed503(t *testing.T) {
	s := restServerWithSecret("db_password")
	s.TriggerEngine = nil
	core, logs := observer.New(zapcore.WarnLevel)
	s.Logger = zap.New(core)
	assertUnavailable503(t, postJSON(s, "/api/secrets/db_password/rotate", ""))
	if n := logs.FilterMessageSnippet("engine is unavailable").Len(); n != 1 {
		t.Fatalf("logged %d times, want once", n)
	}
}

// A typed-nil *agent.TriggerEngine stored in the interface is also "unavailable".
func TestManualRotate_TypedNilEngine_FailsClosed503(t *testing.T) {
	s := restServerWithSecret("db_password")
	var typedNil *agent.TriggerEngine
	s.TriggerEngine = typedNil
	if s.TriggerEngine == nil {
		t.Fatal("test setup: expected a non-nil interface holding a nil pointer")
	}
	assertUnavailable503(t, postJSON(s, "/api/secrets/db_password/rotate", ""))
}

// Without an engine nothing reaches the reloader or the journal, even with a fully
// wired reloader and managed container (the old fallback would have rotated).
func TestManualRotate_NoEngine_NoDockerCallNoJournal(t *testing.T) {
	s, ts, calls := rotationFixture(t, "ok")
	eng := wireEngine(t, s)
	s.TriggerEngine = nil
	resp, err := http.Post(ts.URL+"/api/secrets/db/rotate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", resp.StatusCode)
	}
	if n := len(eng.StateTracker.GetPendingRotations()); n != 0 {
		t.Fatalf("%d journal records", n)
	}
	if c := calls.count("/containers/"); c != 0 {
		t.Fatalf("%d docker calls without an engine", c)
	}
}

// A configured engine is called with the derived actor and the 202 contract holds.
func TestManualRotate_FakeEngine_Accepted(t *testing.T) {
	s := restServerWithSecret("db_password")
	f := &fakeRotationEngine{}
	s.TriggerEngine = f
	w := postJSON(s, "/api/secrets/db_password/rotate", "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if m, _ := f.calls(); m != 1 || !strings.HasPrefix(f.manual[0], "db_password|") {
		t.Fatalf("engine calls %v", f.manual)
	}
}

// Engine refusals keep their status codes.
func TestManualRotate_FakeEngine_ErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{agent.ErrManualBusy, http.StatusConflict},
		{agent.ErrManualNoTargets, http.StatusNotFound},
		{agent.ErrManualUnknownSecret, http.StatusNotFound},
		{context.DeadlineExceeded, http.StatusServiceUnavailable},
	} {
		s := restServerWithSecret("db_password")
		s.TriggerEngine = &fakeRotationEngine{manualErr: tc.err}
		if w := postJSON(s, "/api/secrets/db_password/rotate", ""); w.Code != tc.want {
			t.Errorf("%v -> %d, want %d", tc.err, w.Code, tc.want)
		}
	}
}

// Auth/CSRF/Origin gates run before the engine check: unauthenticated is 401 and
// a cross-site origin 403, never 503, with no engine configured.
func TestManualRotate_NoEngine_GatesRunFirst(t *testing.T) {
	s := newWebUIServer(t, "")
	s.TriggerEngine = nil
	s.Config.Secrets = restServerWithSecret("db_password").Config.Secrets
	if w := do(s, "POST", "/api/secrets/db_password/rotate", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", w.Code)
	}
	c := login(t, s)
	if w := do(s, "POST", "/api/secrets/db_password/rotate", map[string]string{"Origin": "https://evil.example"}, c); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site = %d, want 403", w.Code)
	}
	if w := do(s, "POST", "/api/secrets/db_password/rotate", map[string]string{"Origin": "http://example.com"}, c); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("authorised, no engine = %d, want 503", w.Code)
	}
}

// Input validation (bad name, unknown secret) still answers before the engine check.
func TestManualRotate_NoEngine_ValidationUnchanged(t *testing.T) {
	s := restServerWithSecret("db_password")
	s.TriggerEngine = nil
	if w := postJSON(s, "/api/secrets/does-not-exist/rotate", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown secret = %d, want 404", w.Code)
	}
	if w := postJSON(s, "/api/secrets/bad%20name%21/rotate", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed name = %d, want 400", w.Code)
	}
}

// Read-only endpoints do not need the engine.
func TestReadOnlyEndpoints_WithoutEngineUnchanged(t *testing.T) {
	s := restServerWithSecret("db_password")
	s.TriggerEngine = nil
	for _, p := range []string{"/api/secrets", "/api/events", "/api/containers", "/health"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		if w.Code == http.StatusServiceUnavailable || w.Code >= 500 {
			t.Errorf("GET %s = %d without an engine", p, w.Code)
		}
	}
}

func webhookRotateServer(t *testing.T) *RESTServer {
	t.Helper()
	s, _ := newWebhookServer(t, nil)
	s.Config.Providers = map[string]config.ProviderConfig{"vault": {}}
	s.Config.Secrets = []config.SecretMapping{{Name: "unknown_secret", Provider: "vault"}}
	return s
}

// The webhook route is the second rotation entry point: authenticated and valid,
// but with no engine it fails closed (503, no panic); bad credentials are still 401.
func TestWebhookRotate_NoEngine_FailsClosed503(t *testing.T) {
	s := webhookRotateServer(t)
	s.TriggerEngine = nil
	if w := serve(s, signedRequest("wrong-secret-0123456789abcdef", time.Now().Unix(), webhookBody("1"))); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d, want 401", w.Code)
	}
	w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("2")))
	assertUnavailable503(t, w)

	var typedNil *agent.TriggerEngine
	s.TriggerEngine = typedNil
	assertUnavailable503(t, serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("3"))))
}

func TestWebhookRotate_FakeEngine_Accepted(t *testing.T) {
	s := webhookRotateServer(t)
	f := &fakeRotationEngine{}
	s.TriggerEngine = f
	w := serve(s, signedRequest(testHMACSecret, time.Now().Unix(), webhookBody("1")))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if _, wh := f.calls(); wh != 1 {
		t.Fatalf("engine webhook calls = %d", wh)
	}
}

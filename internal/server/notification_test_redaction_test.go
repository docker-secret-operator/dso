package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const restLeakToken = "TOKEN-rest-4c2d9e-SECRET"

// The complete POST /api/integrations/notifications/test response -- status,
// headers and body -- and the server's logs must not contain any part of a
// configured webhook URL, whatever way the destination fails. The endpoint's
// authentication and Origin gates stay in force with these webhooks configured.
func TestNotificationsTest_ResponseAndLogsNeverLeakWebhookURL(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer ok.Close()
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadAddr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close()

	s := newWebUIServer(t, "")
	core, logs := observer.New(zapcore.DebugLevel)
	s.Logger = zap.New(core)
	s.Config = &config.Config{Notifications: config.NotificationsConfig{Enabled: true, Webhooks: []config.NotificationWebhook{
		{URL: "https://hooks.example.com/services/" + restLeakToken + "/%zz", MaxRetries: -1},                                                        // parser error
		{URL: "https://hooks.example.com:badport/p?key=" + restLeakToken, MaxRetries: -1},                                                            // parser error, query
		{URL: "http://u:" + restLeakToken + "@hooks.example.com/" + restLeakToken + "?k=1", MaxRetries: -1},                                          // plain HTTP rejected
		{URL: "ftp://hooks.example.com/" + restLeakToken, MaxRetries: -1},                                                                            // unsupported scheme
		{URL: "http://u:" + restLeakToken + "@" + deadAddr + "/h/" + restLeakToken + "?k=" + restLeakToken, AllowInsecureHTTP: true, MaxRetries: -1}, // transport failure
		{URL: ok.URL + "/" + restLeakToken, AllowInsecureHTTP: true},                                                                                 // valid
	}}}
	const target = "/api/integrations/notifications/test"

	// Gates: anonymous -> 401, foreign Origin -> 403, even with these webhooks configured.
	if w := do(s, "POST", target, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", w.Code)
	}
	c := login(t, s)
	if w := do(s, "POST", target, map[string]string{"Origin": "https://evil.example"}, c); w.Code != http.StatusForbidden {
		t.Errorf("cross-site origin = %d, want 403", w.Code)
	}

	w := do(s, "POST", target, map[string]string{"Origin": "http://example.com"}, c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	whole := w.Result().Status + "\n" + strings.Join(w.Result().Header.Values("Set-Cookie"), "\n") + "\n" + w.Body.String()
	if strings.Contains(strings.ToLower(whole), strings.ToLower(restLeakToken)) {
		t.Fatalf("response leaks the webhook token:\n%s", whole)
	}
	// Legitimate behaviour preserved: the valid destination succeeded, the rest failed with diagnostics.
	body := w.Body.String()
	if !strings.Contains(body, `"total_count":6`) || !strings.Contains(body, `"failed":5`) {
		t.Errorf("unexpected result counts: %s", body)
	}
	if !strings.Contains(body, ok.URL) {
		t.Errorf("the valid destination should be identified by scheme://host: %s", body)
	}
	for _, e := range logs.All() {
		if strings.Contains(strings.ToLower(e.Message), strings.ToLower(restLeakToken)) {
			t.Errorf("log message leaks the token: %s", e.Message)
		}
		for _, f := range e.Context {
			if strings.Contains(strings.ToLower(f.String), strings.ToLower(restLeakToken)) {
				t.Errorf("log field %s leaks the token", f.Key)
			}
			if er, isErr := f.Interface.(error); isErr && strings.Contains(strings.ToLower(er.Error()), strings.ToLower(restLeakToken)) {
				t.Errorf("log error field %s leaks the token: %v", f.Key, er)
			}
		}
	}
}

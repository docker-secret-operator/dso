package notify

// A webhook URL is a credential (Slack/Teams style URLs carry the token in the
// path or query). None of it may reach the notification-test results, error
// text, or the delivery logs.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const leakToken = "TOKEN-9f3a7c1e-SECRET"

func noLeak(t *testing.T, what, s string) {
	t.Helper()
	if strings.Contains(strings.ToLower(s), strings.ToLower(leakToken)) {
		t.Errorf("%s leaks the webhook token: %s", what, s)
	}
}

// closedServerURL returns a URL (with the token in path, query and user-info)
// whose server is shut down, so every delivery attempt fails with a transport
// error that, unredacted, prints the whole URL.
func closedServerURL() string {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()
	return "http://user:" + leakToken + "@" + addr + "/hooks/" + leakToken + "?key=" + leakToken
}

func TestRunTest_NeverLeaksWebhookURL(t *testing.T) {
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer rejecting.Close()

	cases := []struct {
		name          string
		hook          config.NotificationWebhook
		wantDest      string // "" = just must not leak
		wantErrSubstr string
	}{
		{"token in path, bad escape (parser error)", config.NotificationWebhook{URL: "https://hooks.example.com/services/" + leakToken + "/%zz"}, "invalid destination", "could not be parsed"},
		{"token in query, bad port (parser error)", config.NotificationWebhook{URL: "https://hooks.example.com:badport/p?key=" + leakToken}, "invalid destination", "could not be parsed"},
		{"control character (parser error)", config.NotificationWebhook{URL: "https://hooks.example.com/" + leakToken + "\x7f"}, "invalid destination", "could not be parsed"},
		{"whole URL is only a token", config.NotificationWebhook{URL: leakToken}, "invalid destination", "http or https"},
		{"token-looking scheme", config.NotificationWebhook{URL: leakToken + ":rest/" + leakToken}, "invalid destination", "http or https"},
		{"unsupported scheme", config.NotificationWebhook{URL: "ftp://hooks.example.com/" + leakToken + "?k=" + leakToken}, "invalid destination", "http or https"},
		{"plain HTTP rejected by policy", config.NotificationWebhook{URL: "http://user:" + leakToken + "@hooks.example.com/" + leakToken + "?k=" + leakToken}, "http://hooks.example.com", "plain HTTP"},
		{"no host", config.NotificationWebhook{URL: "https:///" + leakToken}, "invalid destination", ""},
		{"delivery failure (transport error carries the URL)", config.NotificationWebhook{URL: closedServerURL(), AllowInsecureHTTP: true, MaxRetries: -1}, "", ""},
		{"destination rejects the event (HTTP 4xx)", config.NotificationWebhook{URL: rejecting.URL + "/" + leakToken + "?k=" + leakToken, AllowInsecureHTTP: true, MaxRetries: -1}, "", "HTTP 403"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Notifications: config.NotificationsConfig{Enabled: true, Webhooks: []config.NotificationWebhook{tc.hook}}}
			results, err := RunTest(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].Success {
				t.Fatalf("want exactly one failed result, got %+v", results)
			}
			raw, _ := json.Marshal(results) // what the REST layer serialises
			noLeak(t, "serialized results", string(raw))
			noLeak(t, "Destination", results[0].Destination)
			noLeak(t, "Error", results[0].Error)
			if tc.wantDest != "" && results[0].Destination != tc.wantDest {
				t.Errorf("Destination = %q, want %q", results[0].Destination, tc.wantDest)
			}
			if tc.wantErrSubstr != "" && !strings.Contains(results[0].Error, tc.wantErrSubstr) {
				t.Errorf("Error = %q, want it to mention %q", results[0].Error, tc.wantErrSubstr)
			}
			if results[0].Error == "" {
				t.Error("a failure must still carry a diagnostic")
			}
		})
	}
}

func TestRunTest_ValidDestinationStillWorks(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer ok.Close()
	cfg := &config.Config{Notifications: config.NotificationsConfig{Enabled: true, Webhooks: []config.NotificationWebhook{
		{URL: ok.URL + "/" + leakToken + "?k=" + leakToken, AllowInsecureHTTP: true},
	}}}
	results, err := RunTest(context.Background(), cfg)
	if err != nil || len(results) != 1 || !results[0].Success {
		t.Fatalf("valid destination must succeed: %+v err=%v", results, err)
	}
	if results[0].Destination != ok.URL {
		t.Errorf("Destination = %q, want scheme://host %q", results[0].Destination, ok.URL)
	}
	raw, _ := json.Marshal(results)
	noLeak(t, "serialized results", string(raw))
}

// Constructor errors are logged by the agent at startup (logger.Fatal) and
// printed by the CLI, so they must be safe too.
func TestNewWebhookNotifier_ErrorsNeverContainURL(t *testing.T) {
	for _, raw := range []string{
		"https://h.example.com/" + leakToken + "/%zz",
		"https://h.example.com:bad/" + leakToken,
		leakToken + ":x",
		"ftp://h.example.com/" + leakToken,
		"http://u:" + leakToken + "@h.example.com/" + leakToken,
		"https:///" + leakToken,
	} {
		_, err := NewWebhookNotifier(WebhookOptions{URL: raw}, nil)
		if err == nil {
			t.Fatalf("%q: expected a validation error", raw)
		}
		noLeak(t, "constructor error for "+raw, err.Error())
	}
}

// The production dispatcher logs failed deliveries; the log line must not carry
// the URL either.
func TestDispatcher_FailedDeliveryLogDoesNotLeakURL(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	n, err := NewWebhookNotifier(WebhookOptions{URL: closedServerURL(), AllowInsecureHTTP: true, MaxRetries: -1}, zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher([]*WebhookNotifier{n}, zap.New(core))
	d.Dispatch(NewEvent(RotationSucceeded, "p", "s", nil, 0, nil))
	d.Stop(5 * time.Second)

	if logs.FilterMessage("notification delivery failed").Len() == 0 {
		t.Fatal("expected a delivery failure to be logged")
	}
	for _, e := range logs.All() {
		noLeak(t, "log message", e.Message)
		for _, f := range e.Context {
			noLeak(t, "log field "+f.Key, f.String)
			if f.Interface != nil {
				if er, ok := f.Interface.(error); ok {
					noLeak(t, "log error field", er.Error())
				}
			}
		}
	}
}

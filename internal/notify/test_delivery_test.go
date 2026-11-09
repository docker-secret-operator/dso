package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
)

func TestRunTest_NilConfig(t *testing.T) {
	results, err := RunTest(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results for nil config, got %d", len(results))
	}
}

func TestRunTest_DisabledOrEmpty(t *testing.T) {
	cfg := &config.Config{Notifications: config.NotificationsConfig{Enabled: false}}
	results, err := RunTest(context.Background(), cfg)
	if err != nil || len(results) != 0 {
		t.Fatalf("expected no results when disabled, got %d results, err=%v", len(results), err)
	}
}

func TestRunTest_SuccessAndFailure(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Enabled: true,
			Webhooks: []config.NotificationWebhook{
				{URL: ok.URL, AllowInsecureHTTP: true},
				{URL: "not-a-valid-url", MaxRetries: -1},
			},
		},
	}

	results, err := RunTest(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if !results[0].Success {
		t.Errorf("expected first destination to succeed, got error: %s", results[0].Error)
	}
	if results[1].Success {
		t.Errorf("expected second destination to fail (invalid URL)")
	}
}

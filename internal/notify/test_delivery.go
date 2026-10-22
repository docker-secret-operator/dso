package notify

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/pkg/config"
)

// TestResult reports the outcome of delivering the synthetic test event to
// one configured notification destination.
type TestResult struct {
	Destination string `json:"destination"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
}

// RunTest sends ONE synthetic test event to every notification destination
// configured in cfg.Notifications.Webhooks, synchronously, and reports a
// per-destination result. This is the single implementation behind both
// `dso notifications test` (internal/cli/notifications.go) and the WebUI's
// POST /api/integrations/notifications/test (internal/server/rest.go) --
// neither caller duplicates delivery logic; they only decide how to present
// the returned results (CLI prints them, REST returns them as JSON).
//
// The event's secret_name is the literal string "test-secret" and carries
// no real project data -- this never touches a provider or the vault. A nil
// or unconfigured cfg is not an error: it simply yields zero results, which
// callers report as "nothing configured" rather than a failure.
func RunTest(ctx context.Context, cfg *config.Config) ([]TestResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil || !cfg.Notifications.Enabled || len(cfg.Notifications.Webhooks) == 0 {
		return nil, nil
	}

	event := NewEvent(RotationSucceeded, "test-provider", "test-secret", []string{"test-container"}, 0, nil)

	results := make([]TestResult, 0, len(cfg.Notifications.Webhooks))
	for _, w := range cfg.Notifications.Webhooks {
		n, err := NewWebhookNotifier(WebhookOptions{
			URL:               w.URL,
			MaxRetries:        w.MaxRetries,
			AllowInsecureHTTP: w.AllowInsecureHTTP,
		}, nil) // nil logger: caller decides how/whether to surface per-destination errors
		if err != nil {
			results = append(results, TestResult{Destination: w.URL, Success: false, Error: err.Error()})
			continue
		}

		testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		notifyErr := n.Notify(testCtx, event)
		cancel()
		if notifyErr != nil {
			results = append(results, TestResult{Destination: n.SafeName(), Success: false, Error: notifyErr.Error()})
			continue
		}
		results = append(results, TestResult{Destination: n.SafeName(), Success: true})
	}

	return results, nil
}

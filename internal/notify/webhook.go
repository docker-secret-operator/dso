package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// Notifier delivers one event to one destination. Implementations must be
// safe for use from a single dispatcher goroutine and must bound their own
// work (timeouts, retry counts) -- the dispatcher trusts Notify to return.
type Notifier interface {
	Notify(ctx context.Context, event RotationEvent) error
}

const (
	defaultWebhookTimeout = 10 * time.Second
	defaultMaxRetries     = 2
	// maxResponseBytes bounds how much of a webhook response is read (and
	// then discarded). Responses are never parsed or acted on; reading a
	// bounded amount just lets the connection be reused.
	maxResponseBytes = 4 * 1024
)

// WebhookOptions configures one webhook destination. Constructed from the
// operator's dso.yaml (see pkg/config.NotificationWebhook) -- endpoint URLs
// are admin-controlled configuration at the same trust level as provider
// endpoints, not untrusted user input.
type WebhookOptions struct {
	URL        string
	Timeout    time.Duration // 0 -> defaultWebhookTimeout
	MaxRetries int           // <0 -> 0 retries; 0 -> defaultMaxRetries
	// Events filters which event types this destination receives; empty
	// means all.
	Events []EventType
	// AllowInsecureHTTP permits an http:// (non-TLS) endpoint. Off by
	// default: notification payloads carry secret *names* and error text,
	// which shouldn't transit plaintext HTTP without an explicit opt-in.
	AllowInsecureHTTP bool
}

// WebhookNotifier POSTs events as JSON to a single endpoint.
type WebhookNotifier struct {
	opts     WebhookOptions
	client   *http.Client
	logger   *zap.Logger
	safeName string // scheme://host only -- never the full URL, which may embed tokens
	events   map[EventType]bool
}

// NewWebhookNotifier validates the destination and returns a notifier.
// Validation failures are configuration errors and should fail agent
// startup loudly rather than being silently skipped.
func NewWebhookNotifier(opts WebhookOptions, logger *zap.Logger) (*WebhookNotifier, error) {
	u, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook URL: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !opts.AllowInsecureHTTP {
			return nil, fmt.Errorf("webhook endpoint %s://%s uses plain HTTP; set allow_insecure_http: true to permit this explicitly", u.Scheme, u.Host)
		}
	default:
		return nil, fmt.Errorf("webhook URL must be http(s), got scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("webhook URL has no host")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultMaxRetries
	} else if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}

	var events map[EventType]bool
	if len(opts.Events) > 0 {
		events = make(map[EventType]bool, len(opts.Events))
		for _, e := range opts.Events {
			events[e] = true
		}
	}

	return &WebhookNotifier{
		opts: opts,
		client: &http.Client{
			Timeout: timeout,
			// Never follow redirects: a redirect could re-point the
			// admin-configured destination somewhere the admin never
			// approved (and could downgrade https -> http).
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger:   logger,
		safeName: u.Scheme + "://" + u.Host,
		events:   events,
	}, nil
}

// SafeName identifies this destination in logs and metrics without
// exposing the path/query, which may contain embedded credentials.
func (w *WebhookNotifier) SafeName() string { return w.safeName }

// Notify delivers one event, retrying transient failures (network errors,
// HTTP 5xx) up to MaxRetries with linear backoff. HTTP 4xx is treated as
// permanent (the destination rejected the payload; retrying identical
// bytes cannot succeed). Total work is bounded by (retries+1) x timeout.
func (w *WebhookNotifier) Notify(ctx context.Context, event RotationEvent) error {
	if w.events != nil && !w.events[event.Type] {
		return nil // filtered out for this destination -- not a failure
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= w.opts.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		permanent, err := w.deliver(ctx, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if permanent {
			return lastErr
		}
	}
	return lastErr
}

// deliver performs a single POST. The bool result reports whether the
// failure is permanent (retrying cannot help).
func (w *WebhookNotifier) deliver(ctx context.Context, body []byte) (permanent bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.opts.URL, bytes.NewReader(body))
	if err != nil {
		return true, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dso-notify")

	resp, err := w.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("webhook %s: %w", w.safeName, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return false, nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return true, fmt.Errorf("webhook %s: destination rejected event (HTTP %d)", w.safeName, resp.StatusCode)
	default:
		return false, fmt.Errorf("webhook %s: HTTP %d", w.safeName, resp.StatusCode)
	}
}

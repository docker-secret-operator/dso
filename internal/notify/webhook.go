package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
		// url.Parse errors embed the complete URL (path, query and user-info
		// may carry the token), so the parser's message is never propagated.
		return nil, errors.New("invalid webhook URL: it could not be parsed (check the configured URL)")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !opts.AllowInsecureHTTP {
			return nil, fmt.Errorf("webhook endpoint %s://%s uses plain HTTP; set allow_insecure_http: true to permit this explicitly", u.Scheme, u.Host)
		}
	default:
		// The scheme text is attacker/operator-typed and, for a malformed
		// URL, can be a fragment of a token ("tok:en/..."), so it is not echoed.
		return nil, errors.New("webhook URL must use http or https")
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

// invalidDestination labels a configured URL that cannot be identified safely.
const invalidDestination = "invalid destination"

// SafeDestination returns a label for a configured webhook URL that is safe to
// show in API responses and logs: scheme://host for a well-formed http(s) URL,
// otherwise "invalid destination". The raw string is never echoed and no
// partially-sanitized form is returned, so a malformed URL (which may be
// nothing but a token) cannot leak through the fallback.
func SafeDestination(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return invalidDestination
	}
	return u.Scheme + "://" + u.Host
}

// deliveryError is a delivery failure whose text has had the configured URL
// removed. It keeps the underlying cause for errors.Is / errors.As (for
// example context.DeadlineExceeded) without ever printing it.
type deliveryError struct {
	msg   string
	cause error
}

func (e *deliveryError) Error() string { return e.msg }
func (e *deliveryError) Unwrap() error { return e.cause }

// unwrapURLError drops the *url.Error wrapper (whose text is the request URL).
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

// RedactURLInError returns err's text with the configured URL's sensitive
// parts removed: it drops the *url.Error wrapper (which prints the full request
// URL) and replaces any remaining occurrence of the URL, its path, query or
// user-info with "[redacted]". It is the single place delivery errors are made
// safe for logs and API responses.
func RedactURLInError(err error, rawURL string) string {
	if err == nil {
		return ""
	}
	msg := unwrapURLError(err).Error()
	for _, secret := range sensitiveURLParts(rawURL) {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

// sensitiveURLParts lists substrings of rawURL that may carry credentials,
// longest first so a full URL is replaced before its components.
func sensitiveURLParts(rawURL string) []string {
	parts := []string{rawURL}
	if u, err := url.Parse(rawURL); err == nil {
		if u.User != nil {
			parts = append(parts, u.User.String())
			if pw, ok := u.User.Password(); ok {
				parts = append(parts, pw)
			}
			parts = append(parts, u.User.Username())
		}
		parts = append(parts, u.RawQuery, strings.Trim(u.EscapedPath(), "/"), strings.Trim(u.Path, "/"), u.Fragment, u.EscapedFragment())
	}
	out := parts[:0]
	for _, p := range parts {
		if len(p) >= 3 { // too-short fragments would mangle ordinary words
			out = append(out, p)
		}
	}
	return out
}

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
		return true, &deliveryError{msg: "webhook " + w.safeName + ": cannot build request: " + RedactURLInError(err, w.opts.URL)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "dso-notify")

	resp, err := w.client.Do(req)
	if err != nil {
		// The HTTP client's error is a *url.Error whose text is the full
		// request URL (path + query); keep only the underlying cause.
		return false, &deliveryError{msg: "webhook " + w.safeName + ": " + RedactURLInError(err, w.opts.URL), cause: unwrapURLError(err)}
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

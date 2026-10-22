package notify

// Residual-risk coverage for webhook URL redaction (follow-up to the M4 fix):
// URL fragments, redirects, error wrapping, the destination label policy and
// the Prometheus labels. All tokens are synthetic.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

func lacks(t *testing.T, what, s string, tokens ...string) {
	t.Helper()
	for _, tok := range tokens {
		if strings.Contains(strings.ToLower(s), strings.ToLower(tok)) {
			t.Errorf("%s contains %q: %s", what, tok, s)
		}
	}
}

func TestRedactURLInError_Fragment(t *testing.T) {
	const frag = "FRAGTOK-5b1e90-SYNTH"
	raw := "https://hooks.example.com/p#" + frag
	got := RedactURLInError(errors.New("upstream said: bad request near "+frag+" and "+raw), raw)
	lacks(t, "redacted message", got, frag)

	// Percent-encoded fragment: both the decoded and the raw form are covered.
	enc := "FRAG%20TOK-SYNTH"
	got = RedactURLInError(errors.New("saw "+enc+" / FRAG TOK-SYNTH"), "https://h.example.com/p#"+enc)
	lacks(t, "redacted message (encoded fragment)", got, "TOK-SYNTH")

	// An empty fragment and a malformed URL must not break or mangle anything.
	if got := RedactURLInError(errors.New("connection refused"), "https://h.example.com/p#"); got != "connection refused" {
		t.Errorf("empty fragment changed the message: %q", got)
	}
	bad := "https://h.example.com:badport/p?q=1#" + frag
	got = RedactURLInError(errors.New("failed for "+bad), bad)
	lacks(t, "redacted message (malformed URL)", got, frag)
	if RedactURLInError(nil, bad) != "" {
		t.Error("nil error must render as the empty string")
	}
}

// The delivery error keeps its cause for errors.Is / errors.As while its text
// carries no URL, and the *url.Error wrapper (whose text IS the URL) is gone.
func TestDeliveryError_WrappedDeadlinePreservedAndURLFree(t *testing.T) {
	const tok = "DEADLINETOK-3c7f-SYNTH"
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice the client giving up
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	target := slow.URL + "/" + tok + "?k=" + tok + "#" + tok

	n, err := NewWebhookNotifier(WebhookOptions{URL: target, AllowInsecureHTTP: true, MaxRetries: -1}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	nerr := n.Notify(ctx, NewEvent(RotationSucceeded, "p", "s", nil, 0, nil))
	if nerr == nil {
		t.Fatal("expected a timeout error")
	}
	if !errors.Is(nerr, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false for %T: %v", nerr, nerr)
	}
	var ue *url.Error
	if errors.As(nerr, &ue) {
		t.Errorf("the *url.Error wrapper (its text is the full URL) is still reachable: %v", ue)
	}
	lacks(t, "error text", nerr.Error(), tok)
	lacks(t, "RunTest-style redaction", RedactURLInError(nerr, target), tok)

	// A direct *url.Error wrapping a deadline: Is still works after redaction's unwrap.
	wrapped := &deliveryError{msg: "x", cause: unwrapURLError(&url.Error{Op: "Post", URL: target, Err: context.DeadlineExceeded})}
	if !errors.Is(wrapped, context.DeadlineExceeded) {
		t.Error("deliveryError must keep the cause for errors.Is")
	}
}

// Redirects are NOT followed (CheckRedirect returns http.ErrUseLastResponse):
// a 3xx is reported as an HTTP status, and the Location -- which may carry a
// token -- is never part of the error. This pins the behaviour that exists.
func TestNotify_RedirectIsNotFollowedAndLocationNotExposed(t *testing.T) {
	const locTok = "LOCTOK-8a2d-SYNTH"
	var hits int32
	var target *httptest.Server
	target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Redirect(w, r, "https://elsewhere.example.com/"+locTok+"?k="+locTok, http.StatusFound)
	}))
	defer target.Close()

	n, err := NewWebhookNotifier(WebhookOptions{URL: target.URL + "/pathtok-SYNTH?q=qtok-SYNTH", AllowInsecureHTTP: true, MaxRetries: -1}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	nerr := n.Notify(context.Background(), NewEvent(RotationSucceeded, "p", "s", nil, 0, nil))
	if nerr == nil || !strings.Contains(nerr.Error(), "HTTP 302") {
		t.Fatalf("a redirect must surface as an HTTP status error, got %v", nerr)
	}
	lacks(t, "redirect error", nerr.Error(), locTok, "elsewhere.example.com", "pathtok-SYNTH", "qtok-SYNTH")
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("the redirect was followed: %d requests reached the server, want 1", h)
	}
}

// Documented policy, not a secrecy guarantee: the label is exactly
// scheme://host (host and port as configured, same as `config show`). The
// path, query, fragment and user-info never appear. A token placed IN the
// hostname is visible by design.
func TestSafeDestination_Policy(t *testing.T) {
	const tok = "URLTOK-6d0c-SYNTH"
	hidden := []string{"user-SYNTH", "pw-" + tok, "path-" + tok, "query-" + tok, "frag-" + tok}
	cases := map[string]string{
		"https://user-SYNTH:pw-" + tok + "@hooks.example.com/path-" + tok + "?k=query-" + tok + "#frag-" + tok: "https://hooks.example.com",
		"http://hooks.example.com:8080/path-" + tok:                                                            "http://hooks.example.com:8080",
		"https://[2001:db8::1]:8443/path-" + tok + "?k=query-" + tok:                                           "https://[2001:db8::1]:8443",
	}
	for raw, want := range cases {
		got := SafeDestination(raw)
		if got != want {
			t.Errorf("SafeDestination = %q, want exactly %q", got, want)
		}
		lacks(t, "destination", got, hidden...)
	}
	// Documented limitation: the host (a tenant sub-domain, say) is NOT hidden.
	host := "tenant-" + tok + ".hooks.example.com"
	if got := SafeDestination("https://" + host + "/x"); got != "https://"+host {
		t.Errorf("hostname policy changed: %q", got)
	}
	// Anything that is not a well-formed http(s) URL is never echoed.
	for _, raw := range []string{tok, tok + ":rest", "ftp://h.example.com/" + tok, "https:///" + tok, "https://h.example.com:bad/" + tok} {
		if got := SafeDestination(raw); got != "invalid destination" {
			t.Errorf("SafeDestination(malformed) = %q", got)
		}
	}
}

// The Prometheus destination label is the same scheme://host: no path, query,
// fragment or user-info reaches /metrics, and the label set is unchanged.
func TestNotificationMetrics_DestinationLabelHasNoURLComponents(t *testing.T) {
	const tok = "METRICTOK-91ee-SYNTH"
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close() // every delivery fails

	n, err := NewWebhookNotifier(WebhookOptions{
		URL: "http://user-" + tok + ":pw-" + tok + "@" + addr + "/path-" + tok + "?k=query-" + tok + "#frag-" + tok, AllowInsecureHTTP: true, MaxRetries: -1,
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher([]*WebhookNotifier{n}, zap.NewNop())
	d.Dispatch(NewEvent(RotationFailed, "p", "s", nil, 0, errors.New("boom")))
	d.Stop(5 * time.Second)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, mf := range families {
		if mf.GetName() != "dso_notification_attempts_total" && mf.GetName() != "dso_notification_failures_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			names := map[string]string{}
			for _, l := range m.GetLabel() {
				names[l.GetName()] = l.GetValue()
			}
			if len(names) != 2 || names["destination"] == "" || names["event_type"] == "" {
				t.Errorf("label set changed: %v", names)
			}
			if names["destination"] == "http://"+addr {
				seen++
			}
			for k, v := range names {
				lacks(t, "metric label "+k, v, tok)
			}
		}
	}
	if seen < 2 {
		t.Errorf("expected attempts and failures series for destination http://%s, found %d", addr, seen)
	}
}

package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// The sentinel below stands in for a real secret value in tests. The
// structural guarantee under test is that no code path can place it in an
// event: NewEvent has no secret-value parameter at all, and error text is
// force-redacted.
const sentinel = "ROTATION_NOTIFICATION_SECRET_9f82b7"

// ── event contract ─────────────────────────────────────────────────────────

func TestNewEvent_Metadata(t *testing.T) {
	e := NewEvent(RotationSucceeded, "vault", "db_password", []string{"abc123"}, 1500*time.Millisecond, nil)

	if e.EventID == "" {
		t.Error("expected a non-empty event ID")
	}
	if e.Type != RotationSucceeded {
		t.Errorf("expected rotation_succeeded, got %s", e.Type)
	}
	if e.Provider != "vault" || e.SecretName != "db_password" {
		t.Errorf("metadata mismatch: %+v", e)
	}
	if e.DurationSeconds != 1.5 {
		t.Errorf("expected 1.5s duration, got %v", e.DurationSeconds)
	}
	if e.ErrorMessage != "" {
		t.Error("success events must carry no error message")
	}
	if e.Timestamp.IsZero() {
		t.Error("expected a timestamp")
	}
}

func TestNewEvent_UniqueIDs(t *testing.T) {
	a := NewEvent(RotationFailed, "p", "s", nil, 0, nil)
	b := NewEvent(RotationFailed, "p", "s", nil, 0, nil)
	if a.EventID == b.EventID {
		t.Fatal("expected distinct event IDs for distinct events")
	}
}

func TestNewEvent_ErrorRedaction(t *testing.T) {
	// A provider error embedding a credential in a recognizable pattern
	// must come out redacted -- error text is treated as untrusted data.
	err := errors.New("vault request failed: token=hvs.SECRETTOKENVALUE1234 rejected")
	e := NewEvent(RotationFailed, "vault", "db_password", nil, 0, err)

	if strings.Contains(e.ErrorMessage, "hvs.SECRETTOKENVALUE1234") {
		t.Fatalf("credential pattern survived redaction: %q", e.ErrorMessage)
	}
	if e.ErrorMessage == "" {
		t.Fatal("expected a (redacted) error message to be present on failure events")
	}
}

func TestNewEvent_NoSecretValueField(t *testing.T) {
	// Marshal a fully-populated event and assert the sentinel cannot
	// appear: there is no API surface through which a secret VALUE can
	// enter an event -- only names/metadata.
	e := NewEvent(RotationFailed, "vault", "db_password", []string{"c1", "c2"}, time.Second,
		errors.New("some failure"))
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Fatal("sentinel appeared in event payload")
	}
}

// ── webhook notifier ───────────────────────────────────────────────────────

func newTestNotifier(t *testing.T, url string, opts WebhookOptions) *WebhookNotifier {
	t.Helper()
	opts.URL = url
	opts.AllowInsecureHTTP = true // httptest servers are http://
	n, err := NewWebhookNotifier(opts, zaptest.NewLogger(t))
	if err != nil {
		t.Fatalf("NewWebhookNotifier: %v", err)
	}
	return n
}

func TestWebhook_SuccessfulDelivery(t *testing.T) {
	var got RotationEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("payload was not valid JSON: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{})
	e := NewEvent(RotationSucceeded, "vault", "db_password", nil, time.Second, nil)
	if err := n.Notify(context.Background(), e); err != nil {
		t.Fatalf("expected successful delivery, got %v", err)
	}
	if got.EventID != e.EventID || got.Type != RotationSucceeded {
		t.Fatalf("delivered payload mismatch: %+v", got)
	}
}

func TestWebhook_4xxIsPermanent_NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{MaxRetries: 3})
	err := n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	if err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("4xx must not be retried; got %d calls", c)
	}
}

func TestWebhook_5xxRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{MaxRetries: 2})
	err := n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	if err != nil {
		t.Fatalf("expected eventual success after retries, got %v", err)
	}
	if c := atomic.LoadInt32(&calls); c != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", c)
	}
}

func TestWebhook_ConnectionFailure_BoundedAndReported(t *testing.T) {
	// A port with nothing listening: connection refused immediately.
	n := newTestNotifier(t, "http://127.0.0.1:1", WebhookOptions{MaxRetries: 1})
	start := time.Now()
	err := n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("delivery attempt was not bounded: took %v", elapsed)
	}
}

func TestWebhook_Timeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // hold the request open past the client timeout
	}))
	defer srv.Close()
	defer close(release)

	n := newTestNotifier(t, srv.URL, WebhookOptions{Timeout: 200 * time.Millisecond, MaxRetries: -1})
	err := n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}

func TestWebhook_EventFilter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{Events: []EventType{RotationFailed}})

	if err := n.Notify(context.Background(), NewEvent(RotationSucceeded, "p", "s", nil, 0, nil)); err != nil {
		t.Fatalf("filtered-out event must not be an error: %v", err)
	}
	if err := n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil)); err != nil {
		t.Fatalf("matching event failed: %v", err)
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("expected exactly 1 delivered event (the matching one), got %d", c)
	}
}

func TestWebhook_RejectsPlainHTTPWithoutOptIn(t *testing.T) {
	_, err := NewWebhookNotifier(WebhookOptions{URL: "http://example.internal/hook"}, zaptest.NewLogger(t))
	if err == nil {
		t.Fatal("expected plain-HTTP endpoint to be rejected without allow_insecure_http")
	}
}

func TestWebhook_RejectsNonHTTPSchemes(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "gopher://x", "ftp://x/y"} {
		if _, err := NewWebhookNotifier(WebhookOptions{URL: u, AllowInsecureHTTP: true}, zaptest.NewLogger(t)); err == nil {
			t.Errorf("expected scheme rejection for %q", u)
		}
	}
}

func TestWebhook_SafeNameOmitsPathAndQuery(t *testing.T) {
	n := newTestNotifier(t, "http://hooks.example.com/services/T000/B000/tokenXYZ?auth=abc", WebhookOptions{})
	if strings.Contains(n.SafeName(), "token") || strings.Contains(n.SafeName(), "auth=") {
		t.Fatalf("SafeName leaked path/query material: %q", n.SafeName())
	}
	if n.SafeName() != "http://hooks.example.com" {
		t.Fatalf("unexpected SafeName: %q", n.SafeName())
	}
}

func TestWebhook_DoesNotFollowRedirects(t *testing.T) {
	var followed int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&followed, 1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	n := newTestNotifier(t, redirector.URL, WebhookOptions{MaxRetries: -1})
	// 302 is outside 2xx -- treated as a (non-permanent) failure, but the
	// critical assertion is that the redirect target was never contacted.
	_ = n.Notify(context.Background(), NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	if atomic.LoadInt32(&followed) != 0 {
		t.Fatal("redirect was followed -- the admin-approved destination must be the only endpoint contacted")
	}
}

// ── dispatcher ─────────────────────────────────────────────────────────────

func TestDispatcher_DeliversAsynchronously(t *testing.T) {
	received := make(chan RotationEvent, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e RotationEvent
		_ = json.NewDecoder(r.Body).Decode(&e)
		received <- e
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{})
	d := NewDispatcher([]*WebhookNotifier{n}, zaptest.NewLogger(t))
	defer d.Stop(5 * time.Second)

	e := NewEvent(RotationSucceeded, "vault", "db_password", nil, 0, nil)
	d.Dispatch(e)

	select {
	case got := <-received:
		if got.EventID != e.EventID {
			t.Fatalf("delivered wrong event: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event was never delivered")
	}
}

func TestDispatcher_DispatchNeverBlocks_QueueFullDrops(t *testing.T) {
	// No notifier and a stopped worker: fill the buffer completely, then
	// keep dispatching. Every call must return promptly (drop, not block).
	d := NewDispatcher(nil, zaptest.NewLogger(t))
	d.Stop(time.Second) // worker exited; nothing consumes the channel

	done := make(chan struct{})
	go func() {
		for i := 0; i < queueCapacity*2; i++ {
			d.Dispatch(NewEvent(RotationFailed, "p", "s", nil, 0, nil))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Dispatch blocked -- it must always be non-blocking")
	}
}

func TestDispatcher_DispatchAfterStop_NoPanic(t *testing.T) {
	// Detached rotation goroutines can fire onComplete after shutdown has
	// begun -- Dispatch after Stop must be completely safe.
	d := NewDispatcher(nil, zaptest.NewLogger(t))
	d.Stop(time.Second)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispatch after Stop panicked: %v", r)
		}
	}()
	for i := 0; i < queueCapacity*2; i++ {
		d.Dispatch(NewEvent(RotationSucceeded, "p", "s", nil, 0, nil))
	}
}

func TestDispatcher_StopDrainsQueuedEvents(t *testing.T) {
	var delivered int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&delivered, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{})
	d := NewDispatcher([]*WebhookNotifier{n}, zaptest.NewLogger(t))

	const events = 5
	for i := 0; i < events; i++ {
		d.Dispatch(NewEvent(RotationSucceeded, "p", "s", nil, 0, nil))
	}
	d.Stop(10 * time.Second)

	if got := atomic.LoadInt32(&delivered); got != events {
		t.Fatalf("expected all %d queued events drained on Stop, delivered %d", events, got)
	}
}

func TestDispatcher_NotificationFailureIsIsolated(t *testing.T) {
	// Every destination hard-fails; Dispatch must still be callable and
	// the dispatcher must keep functioning. The rotation-isolation half of
	// this invariant is structural (rotation code never sees a delivery
	// result -- Dispatch returns nothing), so what's provable here is that
	// failures don't wedge or kill the delivery loop.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := newTestNotifier(t, srv.URL, WebhookOptions{MaxRetries: -1})
	d := NewDispatcher([]*WebhookNotifier{n}, zaptest.NewLogger(t))

	for i := 0; i < 3; i++ {
		d.Dispatch(NewEvent(RotationFailed, "p", "s", nil, 0, nil))
	}
	d.Stop(10 * time.Second) // must return despite every delivery failing
}

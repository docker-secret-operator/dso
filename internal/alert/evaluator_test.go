package alert

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/notify"
	"go.uber.org/zap"
)

// fakeDispatcher records every RotationEvent handed to it. "fail" models
// a completely unreachable notification destination: the real
// notify.Dispatcher already isolates delivery outcomes from callers
// (Dispatch has no error return; delivery runs on its own goroutine and
// only ever logs failures) -- this fake reproduces that same
// never-surfaces-to-the-caller shape.
type fakeDispatcher struct {
	mu      sync.Mutex
	events  []notify.RotationEvent
	fail    bool
	failLog []string
}

func (f *fakeDispatcher) Dispatch(ev notify.RotationEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		f.failLog = append(f.failLog, ev.EventID)
		return
	}
	f.events = append(f.events, ev)
}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

func (f *fakeDispatcher) last() notify.RotationEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events[len(f.events)-1]
}

func testLogger() *zap.Logger {
	return zap.NewNop()
}

func rotationFailedEvent(secret string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     secret,
		"container":  "c1",
		"event_type": "rotation_failed",
		"status":     "failure",
		"error":      "redacted provider error",
	}
}

func rotationSucceededEvent(secret string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     secret,
		"container":  "c1",
		"event_type": "rotation_succeeded",
		"status":     "success",
	}
}

func signalFailedEvent(container string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     "s1",
		"container":  container,
		"event_type": "signal_failed",
		"status":     "failure",
		"error":      "signal failed",
	}
}

func signalSuccessEvent(container string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"secret":     "s1",
		"container":  container,
		"event_type": "signal_success",
		"status":     "success",
	}
}

func degradedEvent(container string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"container":  container,
		"event_type": "service_degraded",
		"status":     "failure",
		"error":      "rollback failed after 3 attempts",
	}
}

func recoveredEvent(container string) map[string]interface{} {
	return map[string]interface{}{
		"timestamp":  time.Now().Format(time.RFC3339),
		"container":  container,
		"event_type": "service_recovered",
		"status":     "success",
	}
}

// 1. rotation_failed creates alert
func TestEvaluator_RotationFailed_CreatesAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(rotationFailedEvent("db-password"))

	alerts := e.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	a := alerts[0]
	if a.Type != "rotation_failed" || a.Status != StatusFiring || a.Resource != "db-password" {
		t.Fatalf("unexpected alert: %+v", a)
	}
	if d.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", d.count())
	}
}

// 2. injection_failed creates alert
func TestEvaluator_InjectionFailed_CreatesAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(signalFailedEvent("api-container"))

	alerts := e.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Type != "injection_failed" || alerts[0].Resource != "api-container" {
		t.Fatalf("unexpected alert: %+v", alerts[0])
	}
}

// 3. service_degraded creates alert
func TestEvaluator_ServiceDegraded_CreatesAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(degradedEvent("backend"))

	alerts := e.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Type != "service_degraded" || alerts[0].Status != StatusFiring {
		t.Fatalf("unexpected alert: %+v", alerts[0])
	}
}

// 4. unrelated event ignored
func TestEvaluator_UnrelatedEvent_Ignored(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(map[string]interface{}{"event_type": "drift_detected", "container": "x"})
	e.Evaluate(map[string]interface{}{})

	if len(e.Alerts()) != 0 {
		t.Fatalf("expected no alerts, got %+v", e.Alerts())
	}
	if d.count() != 0 {
		t.Fatalf("expected no notifications, got %d", d.count())
	}
}

// 5. duplicate event deduplicated
func TestEvaluator_DuplicateEvent_Deduplicated(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(rotationFailedEvent("db-password"))
	e.Evaluate(rotationFailedEvent("db-password"))
	e.Evaluate(rotationFailedEvent("db-password"))

	if len(e.Alerts()) != 1 {
		t.Fatalf("expected exactly 1 alert record, got %d", len(e.Alerts()))
	}
	if d.count() != 1 {
		t.Fatalf("expected exactly 1 notification despite 3 identical events, got %d", d.count())
	}
}

// 6. cooldown suppresses notification
func TestEvaluator_Cooldown_SuppressesNotification(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Hour, testLogger()) // long cooldown

	e.Evaluate(degradedEvent("backend"))
	e.Evaluate(degradedEvent("backend")) // still within cooldown

	if d.count() != 1 {
		t.Fatalf("expected 1 notification (second suppressed by cooldown), got %d", d.count())
	}
	alerts := e.Alerts()
	if len(alerts) != 1 || alerts[0].Status != StatusFiring {
		t.Fatalf("unexpected alert state: %+v", alerts)
	}
}

// 7. cooldown expiration allows notification
func TestEvaluator_CooldownExpiration_AllowsNotification(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, 50*time.Millisecond, testLogger())

	e.Evaluate(degradedEvent("backend"))
	time.Sleep(80 * time.Millisecond)
	e.Evaluate(degradedEvent("backend"))

	if d.count() != 2 {
		t.Fatalf("expected 2 notifications after cooldown expired, got %d", d.count())
	}
}

// 8. service_recovered resolves degraded alert
func TestEvaluator_ServiceRecovered_ResolvesDegradedAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(degradedEvent("backend"))
	e.Evaluate(recoveredEvent("backend"))

	alerts := e.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert record, got %d", len(alerts))
	}
	if alerts[0].Status != StatusResolved || alerts[0].ResolvedAt == nil {
		t.Fatalf("expected resolved alert, got %+v", alerts[0])
	}
	if d.count() != 2 {
		t.Fatalf("expected 2 notifications (degraded + recovered), got %d", d.count())
	}
	if d.last().Type != notify.ServiceRecovered {
		t.Fatalf("expected last notification to be ServiceRecovered, got %v", d.last().Type)
	}
}

// 9. duplicate recovery suppressed
func TestEvaluator_DuplicateRecovery_Suppressed(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(degradedEvent("backend"))
	e.Evaluate(recoveredEvent("backend"))
	e.Evaluate(recoveredEvent("backend")) // no active alert to resolve anymore

	if d.count() != 2 {
		t.Fatalf("expected exactly 2 notifications (degraded + one recovery), got %d", d.count())
	}
}

// A healthy-only rotation_succeeded (no prior failure) must not create or
// notify anything -- mirrors "repeated recovered -> suppressed" for the
// rotation_failed/rotation_succeeded pair too.
func TestEvaluator_RotationSucceeded_WithNoPriorFailure_IsNoop(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(rotationSucceededEvent("db-password"))

	if len(e.Alerts()) != 0 || d.count() != 0 {
		t.Fatalf("expected no alert and no notification, got alerts=%+v notifications=%d", e.Alerts(), d.count())
	}
}

// rotation_succeeded resolves a matching firing rotation_failed alert.
func TestEvaluator_RotationSucceeded_ResolvesRotationFailedAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(rotationFailedEvent("db-password"))
	e.Evaluate(rotationSucceededEvent("db-password"))

	alerts := e.Alerts()
	if len(alerts) != 1 || alerts[0].Status != StatusResolved {
		t.Fatalf("expected resolved alert, got %+v", alerts)
	}
	if d.last().Type != notify.RotationSucceeded {
		t.Fatalf("expected resolution notification type RotationSucceeded, got %v", d.last().Type)
	}
}

// signal_success resolves a matching firing injection_failed alert.
func TestEvaluator_SignalSuccess_ResolvesInjectionFailedAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(signalFailedEvent("api-container"))
	e.Evaluate(signalSuccessEvent("api-container"))

	alerts := e.Alerts()
	if len(alerts) != 1 || alerts[0].Status != StatusResolved {
		t.Fatalf("expected resolved alert, got %+v", alerts)
	}
}

// 10. no secret plaintext in alert
func TestEvaluator_NoSecretPlaintextInAlert(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	ev := rotationFailedEvent("db-password")
	ev["error"] = "provider returned value sk-live-ABCDEF123456"
	e.Evaluate(ev)

	a := e.Alerts()[0]
	if strings.Contains(a.Message, "sk-live-ABCDEF123456") {
		t.Fatalf("alert message leaked raw error text: %q", a.Message)
	}
	if a.Message != "Secret rotation failed for db-password." {
		t.Fatalf("expected the fixed template message, got %q", a.Message)
	}
	nd := d.last()
	if strings.Contains(nd.ErrorMessage, "sk-live-ABCDEF123456") {
		t.Fatalf("dispatched notification leaked raw error text: %q", nd.ErrorMessage)
	}
}

// 11. deterministic dedup keys
func TestEvaluator_DeterministicDedupKeys(t *testing.T) {
	d := &fakeDispatcher{}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(rotationFailedEvent("secret-a"))
	e.Evaluate(signalFailedEvent("container-b"))
	e.Evaluate(degradedEvent("container-c"))

	byKey := map[string]Alert{}
	for _, a := range e.Alerts() {
		byKey[a.DedupKey] = a
	}
	for _, want := range []string{"rotation_failed:secret-a", "injection_failed:container-b", "degraded:container-c"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("expected dedup key %q present, got keys %v", want, byKey)
		}
	}
}

// 12. concurrent identical events create one alert/notification
func TestEvaluator_ConcurrentIdenticalEvents_OneAlertOneNotification(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   map[string]interface{}
	}{
		{"service_degraded", degradedEvent("backend")},
		{"rotation_failed", rotationFailedEvent("db-password")},
		{"injection_failed", signalFailedEvent("api-container")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDispatcher{}
			e := NewEvaluator(d, time.Minute, testLogger())

			const n = 20
			var wg sync.WaitGroup
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func() {
					defer wg.Done()
					e.Evaluate(tc.ev)
				}()
			}
			wg.Wait()

			if len(e.Alerts()) != 1 {
				t.Fatalf("expected exactly 1 alert from %d concurrent identical events, got %d", n, len(e.Alerts()))
			}
			if d.count() != 1 {
				t.Fatalf("expected exactly 1 notification from %d concurrent identical events, got %d", n, d.count())
			}
		})
	}
}

// Notification failure must never affect evaluator state or panic.
func TestEvaluator_NotificationFailure_EvaluatorRemainsHealthy(t *testing.T) {
	d := &fakeDispatcher{fail: true}
	e := NewEvaluator(d, time.Minute, testLogger())

	e.Evaluate(degradedEvent("backend")) // dispatch "fails" (dropped by fake)

	alerts := e.Alerts()
	if len(alerts) != 1 || alerts[0].Status != StatusFiring {
		t.Fatalf("expected alert state to still be correct despite notification failure, got %+v", alerts)
	}

	// The evaluator must remain fully operational for subsequent events.
	e.Evaluate(recoveredEvent("backend"))
	alerts = e.Alerts()
	if alerts[0].Status != StatusResolved {
		t.Fatalf("expected evaluator to keep working after a notification failure, got %+v", alerts)
	}
}

// A nil Dispatcher (notifications not configured) must never panic --
// alert state is still tracked.
func TestEvaluator_NilDispatcher_TracksStateWithoutPanicking(t *testing.T) {
	e := NewEvaluator(nil, time.Minute, testLogger())

	e.Evaluate(degradedEvent("backend"))
	e.Evaluate(recoveredEvent("backend"))

	alerts := e.Alerts()
	if len(alerts) != 1 || alerts[0].Status != StatusResolved {
		t.Fatalf("expected tracked+resolved alert with nil dispatcher, got %+v", alerts)
	}
}

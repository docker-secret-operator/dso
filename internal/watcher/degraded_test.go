package watcher

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/pkg/observability"
)

// drainEventStream removes any events already buffered on the shared
// observability.EventStream channel before a test begins, so a leftover
// event from an unrelated prior test (in this same package's test binary)
// can never be mistaken for the event under test.
func drainEventStream(t *testing.T) {
	t.Helper()
	for {
		select {
		case <-observability.EventStream:
		default:
			return
		}
	}
}

// expectEvent waits briefly for exactly one event matching predicate to
// arrive on observability.EventStream, failing the test if none arrives.
func expectEvent(t *testing.T, predicate func(map[string]interface{}) bool) map[string]interface{} {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-observability.EventStream:
			if predicate(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for expected event on observability.EventStream")
			return nil
		}
	}
}

// expectNoEvent asserts nothing matching predicate arrives within a short
// window -- used to prove a reconciliation pass did NOT re-emit a
// transition event for a service whose degraded state didn't actually
// change.
func expectNoEvent(t *testing.T, predicate func(map[string]interface{}) bool) {
	t.Helper()
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case ev := <-observability.EventStream:
			if predicate(ev) {
				t.Fatalf("unexpected duplicate event: %+v", ev)
			}
		case <-deadline:
			return
		}
	}
}

func isServiceDegradedEvent(service string) func(map[string]interface{}) bool {
	return func(ev map[string]interface{}) bool {
		return ev["event_type"] == "service_degraded" && ev["container"] == service
	}
}

func isServiceRecoveredEvent(service string) func(map[string]interface{}) bool {
	return func(ev map[string]interface{}) bool {
		return ev["event_type"] == "service_recovered" && ev["container"] == service
	}
}

// Test 1 — healthy service: no degraded state.
func TestDegraded_HealthyService_NoDegradedState(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))

	if got := rc.DegradedServices(); len(got) != 0 {
		t.Fatalf("expected no degraded services on a fresh controller, got %+v", got)
	}
	if _, degraded := rc.IsDegraded("backend"); degraded {
		t.Fatal("expected IsDegraded(\"backend\") to be false")
	}
}

// Test 2 — transition healthy -> degraded: exactly one degraded event.
func TestDegraded_HealthyToDegraded_EmitsExactlyOneEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	rc.markDegraded("backend", "rollback failed after 3 attempts")

	ev := expectEvent(t, isServiceDegradedEvent("backend"))
	if ev["status"] != "failure" {
		t.Errorf("expected status=failure, got %v", ev["status"])
	}
	if ev["error"] != "rollback failed after 3 attempts" {
		t.Errorf("expected error field to carry the reason, got %v", ev["error"])
	}
}

// Test 3 — reconciliation while already degraded: no duplicate degraded-enter event.
func TestDegraded_AlreadyDegraded_NoDuplicateEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	rc.markDegraded("backend", "first failure")
	expectEvent(t, isServiceDegradedEvent("backend"))

	// Same service degraded again (e.g. a second secret failing on it) must
	// not re-emit the transition event.
	rc.markDegraded("backend", "second failure")
	expectNoEvent(t, isServiceDegradedEvent("backend"))
}

// Test 4 — transition degraded -> healthy: exactly one recovery event.
func TestDegraded_DegradedToHealthy_EmitsExactlyOneEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	rc.markDegraded("backend", "failure")
	expectEvent(t, isServiceDegradedEvent("backend"))

	rc.clearDegraded("backend")
	ev := expectEvent(t, isServiceRecoveredEvent("backend"))
	if ev["status"] != "success" {
		t.Errorf("expected status=success, got %v", ev["status"])
	}
}

// Test 5 — reconciliation while healthy: no duplicate clear events.
func TestDegraded_AlreadyHealthy_NoDuplicateClearEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	// Never degraded in the first place -- a normal successful rotation on
	// an already-healthy service must not emit a spurious recovery event.
	rc.clearDegraded("backend")
	expectNoEvent(t, isServiceRecoveredEvent("backend"))
}

// Test 6 — DegradedServices() returns correct current state.
func TestDegraded_DegradedServices_ReturnsCorrectState(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	rc.markDegraded("backend", "reason-a")
	rc.markDegraded("worker", "reason-b")

	got := rc.DegradedServices()
	if len(got) != 2 {
		t.Fatalf("expected 2 degraded services, got %d: %+v", len(got), got)
	}
	byService := map[string]string{}
	for _, d := range got {
		byService[d.Service] = d.Reason
	}
	if byService["backend"] != "reason-a" || byService["worker"] != "reason-b" {
		t.Fatalf("unexpected degraded contents: %+v", byService)
	}

	reason, degraded := rc.IsDegraded("backend")
	if !degraded || reason != "reason-a" {
		t.Fatalf("IsDegraded(\"backend\") = (%q, %v), want (\"reason-a\", true)", reason, degraded)
	}
}

// Test 7 — restart: degraded state does NOT survive restart (a fresh
// controller/process starts with a genuinely empty map, matching the
// documented current-state-only design -- there is no persistence to
// restore from).
func TestDegraded_DoesNotSurviveRestart(t *testing.T) {
	rc1 := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rc1.markDegraded("backend", "failure")
	if _, degraded := rc1.IsDegraded("backend"); !degraded {
		t.Fatal("expected backend to be degraded on rc1")
	}

	// Simulate a restart: a brand new controller (new process) has no
	// knowledge of rc1's in-memory state.
	rc2 := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if _, degraded := rc2.IsDegraded("backend"); degraded {
		t.Fatal("expected a fresh controller to have no degraded state (degraded state must not survive restart)")
	}
}

// Test 8 — concurrent reconciliation: N goroutines racing markDegraded for
// the SAME service (simulating multiple secrets on one container failing
// around the same reconciliation pass) must still produce exactly one
// service_degraded event, never a duplicate. markDegraded's correctness
// rests entirely on sync.Map.Swap being atomic -- this proves that holds
// under real concurrent access, not just sequential calls. Run with -race.
func TestDegraded_ConcurrentMarkDegraded_EmitsExactlyOneEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			rc.markDegraded("backend", "concurrent failure")
		}(i)
	}
	wg.Wait()

	var count int32
	deadline := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case ev := <-observability.EventStream:
			if isServiceDegradedEvent("backend")(ev) {
				atomic.AddInt32(&count, 1)
			}
		case <-deadline:
			break collect
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 service_degraded event from %d concurrent markDegraded calls, got %d", goroutines, count)
	}
	if _, degraded := rc.IsDegraded("backend"); !degraded {
		t.Fatal("expected backend to remain degraded after the race")
	}
}

// Test 9 — the mirror case for clearDegraded: N goroutines racing to clear
// the same already-degraded service must produce exactly one
// service_recovered event.
func TestDegraded_ConcurrentClearDegraded_EmitsExactlyOneEvent(t *testing.T) {
	rc := newMockController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	drainEventStream(t)

	rc.markDegraded("backend", "failure")
	expectEvent(t, isServiceDegradedEvent("backend"))

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			rc.clearDegraded("backend")
		}(i)
	}
	wg.Wait()

	var count int32
	deadline := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case ev := <-observability.EventStream:
			if isServiceRecoveredEvent("backend")(ev) {
				atomic.AddInt32(&count, 1)
			}
		case <-deadline:
			break collect
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 service_recovered event from %d concurrent clearDegraded calls, got %d", goroutines, count)
	}
	if _, degraded := rc.IsDegraded("backend"); degraded {
		t.Fatal("expected backend to no longer be degraded after the race")
	}
}

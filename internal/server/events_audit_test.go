package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// Phase 5: /api/events + /api/audit -- shared-store behavior, Actor field,
// and secret-sentinel non-leakage.
// ============================================================================

// TestRESTServer_Audit_Unauthorized verifies /api/audit is gated by the same
// authorized() check as every other protected endpoint -- no separate,
// weaker auth path.
func TestRESTServer_Audit_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

// TestRESTServer_EventsAndAudit_ShareTheSameStore documents and locks in the
// current, real backend behavior: /api/events and /api/audit read the exact
// same EventStore -- an event added once is visible from both endpoints.
// This is not a bug to hide from the frontend; the Phase 5 report calls it
// out explicitly, and this test exists so a future change to that coupling
// is a deliberate, reviewed decision rather than an accidental regression.
func TestRESTServer_EventsAndAudit_ShareTheSameStore(t *testing.T) {
	server := createTestRESTServer()
	server.EventStore.Add(Event{
		"timestamp":  "2026-01-01T00:00:00Z",
		"event_type": "secret_fetch",
		"secret":     "db-password",
		"status":     "success",
	})

	for _, path := range []string{"/api/events", "/api/audit"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		var events []Event
		if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
			t.Fatalf("%s: failed to decode response: %v", path, err)
		}
		if len(events) != 1 || events[0]["secret"] != "db-password" {
			t.Errorf("%s: expected the same event to be visible, got %+v", path, events)
		}
	}
}

// TestRESTServer_Audit_ActorFieldPassesThroughWhenPresent verifies the
// Phase 5 "user" field (added in internal/audit/audit.go for secret_fetch
// events) survives the round trip through /api/audit unmodified -- this is
// the field the Audit page's Actor column reads.
func TestRESTServer_Audit_ActorFieldPassesThroughWhenPresent(t *testing.T) {
	server := createTestRESTServer()
	server.EventStore.Add(Event{
		"timestamp":  "2026-01-01T00:00:00Z",
		"event_type": "secret_fetch",
		"secret":     "db-password",
		"status":     "success",
		"user":       "alice",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	var events []Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(events) != 1 || events[0]["user"] != "alice" {
		t.Errorf("expected user=alice to pass through, got %+v", events)
	}
}

// TestRESTServer_Audit_MissingActorFieldIsHonest verifies an event with no
// "user" key (the vast majority of real events today -- only secret_fetch
// ever sets it) omits the key entirely rather than a zero value, so the
// frontend's "Not available" fallback is honest instead of masking absence.
func TestRESTServer_Audit_MissingActorFieldIsHonest(t *testing.T) {
	server := createTestRESTServer()
	server.EventStore.Add(Event{
		"timestamp":  "2026-01-01T00:00:00Z",
		"event_type": "rotation_succeeded",
		"secret":     "db-password",
		"status":     "success",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	var events []Event
	if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %+v", events)
	}
	if _, present := events[0]["user"]; present {
		t.Errorf("expected no user key for an event that never set one, got %+v", events[0])
	}
}

// TestRESTServer_EventsAndAudit_NeverFabricateStatus verifies handleEvents/
// handleAudit never invent a "status" for an event that didn't report one --
// EventStore is a plain map, so a missing key must stay missing through the
// full round trip rather than being coerced into a fabricated "unknown" or
// "success". The frontend's honest-rendering guarantees (see
// events-content.tsx / audit-content.tsx) depend on this holding at the API
// layer, not just in the UI.
func TestRESTServer_EventsAndAudit_NeverFabricateStatus(t *testing.T) {
	server := createTestRESTServer()
	server.EventStore.Add(Event{
		"timestamp":  "2026-01-01T00:00:00Z",
		"event_type": "service_degraded",
		"container":  "payments-api",
		// deliberately no "status" key
	})

	for _, path := range []string{"/api/events", "/api/audit"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		var events []Event
		if err := json.Unmarshal(rec.Body.Bytes(), &events); err != nil {
			t.Fatalf("%s: failed to decode response: %v", path, err)
		}
		if len(events) != 1 {
			t.Fatalf("%s: expected 1 event, got %+v", path, events)
		}
		if _, present := events[0]["status"]; present {
			t.Errorf("%s: expected no fabricated status key, got %+v", path, events[0])
		}
	}
}

// TestRESTServer_Events_SecretSentinelPassesThroughUnfiltered documents a
// real characteristic of this design, not a vulnerability to hide: Event is
// a plain map[string]interface{} (see eventstore.go), so handleEvents and
// handleAudit serialize whatever a producer added, verbatim -- there is no
// field-allowlisting DTO at this layer (unlike, e.g., ContainerSummary for
// /api/containers). The actual safety boundary is upstream, at each
// producer (internal/notify/event.go's NewEvent redacts ErrorMessage before
// it ever reaches EventStream; internal/injector and internal/audit only
// ever construct maps from safe, known-shape fields). This test proves that
// boundary, not the REST layer, and is why the sentinel-non-leakage
// assertions for Events/Audit live in the frontend component tests
// (events.test.tsx / audit.test.tsx), matching the existing Secrets/
// Containers convention of testing "the UI never renders an unsafe field"
// rather than "the backend strips one" when no allowlisting DTO exists.
func TestRESTServer_Events_SecretSentinelPassesThroughUnfiltered(t *testing.T) {
	const sentinel = "DSO_EVENT_SECRET_SENTINEL_7d21a4"
	server := createTestRESTServer()
	server.EventStore.Add(Event{
		"timestamp":  "2026-01-01T00:00:00Z",
		"event_type": "rotation_succeeded",
		"secret":     "db-password",
		"status":     "success",
		// A real producer would never set this key -- it stands in for a
		// hypothetical misbehaving/future producer, proving the REST layer
		// itself applies no filtering either way.
		"value": sentinel,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), sentinel) {
		t.Fatalf("expected the REST layer to pass the map through verbatim (no allowlisting DTO exists here); got %s", rec.Body.String())
	}
}

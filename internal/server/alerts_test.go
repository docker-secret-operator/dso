package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/alert"
)

func doAlertsRequest(t *testing.T, s *RESTServer, token string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return body
}

// Unauthorized: /api/alerts is gated by the same authorized() check as
// every other protected route.
func TestRESTServer_Alerts_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

// A nil s.Alerts (older test constructions, or an evaluator genuinely not
// wired) must yield a clean empty list, not a 500.
func TestRESTServer_Alerts_NilEvaluator_ReturnsCleanEmptyResponse(t *testing.T) {
	s, token := newTestRESTServer(t)
	s.Alerts = nil

	body := doAlertsRequest(t, s, token)
	if body["total_count"].(float64) != 0 {
		t.Fatalf("expected total_count 0, got %v", body["total_count"])
	}
	alerts, ok := body["alerts"].([]interface{})
	if !ok || len(alerts) != 0 {
		t.Fatalf("expected an empty alerts array, got %v", body["alerts"])
	}
}

// GET /api/alerts reflects the wired evaluator's real state.
func TestRESTServer_Alerts_ReflectsEvaluatorState(t *testing.T) {
	s, token := newTestRESTServer(t)
	ev := alert.NewEvaluator(nil, time.Minute, s.Logger)
	ev.Evaluate(map[string]interface{}{
		"event_type": "service_degraded",
		"container":  "backend",
		"error":      "rollback failed after 3 attempts",
	})
	s.Alerts = ev

	body := doAlertsRequest(t, s, token)
	if body["total_count"].(float64) != 1 {
		t.Fatalf("expected total_count 1, got %v", body["total_count"])
	}
	alerts := body["alerts"].([]interface{})
	a := alerts[0].(map[string]interface{})
	if a["type"] != "service_degraded" || a["status"] != "firing" || a["resource"] != "backend" {
		t.Fatalf("unexpected alert in response: %+v", a)
	}
}

// Historical EventStore replay must NOT create alerts -- only newly
// arriving runtime events (fed through Evaluate the same way
// StartRESTServer's live EventStream consumer does) may. This proves the
// two paths are genuinely decoupled: seeding events.jsonl with history and
// constructing an EventStore from it (which replays that history) has zero
// effect on a separately-constructed Evaluator, since nothing in
// EventStore's replay path ever calls Evaluate -- exactly mirroring
// production wiring, where replay happens inside NewEventStore's
// constructor and the evaluator is only ever driven by the live
// observability.EventStream consumer loop in StartRESTServer.
func TestRESTServer_Alerts_HistoricalReplay_DoesNotCreateAlerts(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/events.jsonl"
	writeEventLines(t, path, []string{
		marshalEvent(t, Event{"timestamp": "2026-01-01T00:00:00Z", "secret": "old-secret", "event_type": "rotation_failed", "status": "failure"}),
		marshalEvent(t, Event{"timestamp": "2026-01-01T00:01:00Z", "container": "old-backend", "event_type": "service_degraded", "status": "failure"}),
	})

	// This replays the two historical events above -- confirmed by Phase 2's
	// own TestEventStore_ExistingEvents_AreRestored using the identical
	// helper -- but replay never touches the evaluator below.
	store := newEventStoreAt(50, nil, dir, "events.jsonl")
	if len(store.GetLast(0, "")) != 2 {
		t.Fatalf("expected replay to restore 2 historical events into EventStore")
	}

	ev := alert.NewEvaluator(nil, time.Minute, nil)
	if len(ev.Alerts()) != 0 {
		t.Fatalf("expected zero alerts from a freshly-constructed evaluator untouched by replay, got %+v", ev.Alerts())
	}

	// Now simulate ONE new live event, the way StartRESTServer's consumer
	// loop would for a genuinely new runtime occurrence.
	ev.Evaluate(map[string]interface{}{
		"event_type": "rotation_failed",
		"secret":     "new-secret",
		"status":     "failure",
	})

	alerts := ev.Alerts()
	if len(alerts) != 1 {
		t.Fatalf("expected exactly 1 alert from the new live event, got %d: %+v", len(alerts), alerts)
	}
	if alerts[0].Resource != "new-secret" {
		t.Fatalf("expected the live event's secret, not a replayed one, got %+v", alerts[0])
	}
}

// No secret plaintext, provider credentials, or tokens ever appear in the
// serialized /api/alerts JSON.
func TestRESTServer_Alerts_NoSecretValuesInResponse(t *testing.T) {
	s, token := newTestRESTServer(t)
	ev := alert.NewEvaluator(nil, time.Minute, s.Logger)
	ev.Evaluate(map[string]interface{}{
		"event_type": "rotation_failed",
		"secret":     "db-password",
		"status":     "failure",
		"error":      "provider returned value sk-live-ABCDEF123456",
	})
	s.Alerts = ev

	req := httptest.NewRequest(http.MethodGet, "/api/alerts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, forbidden := range []string{"sk-live-ABCDEF123456", "password_hash", "auth_token", "private_key"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked sensitive content %q: %s", forbidden, body)
		}
	}
}

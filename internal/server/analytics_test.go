package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/internal/notify"
	"github.com/docker-secret-operator/dso/pkg/observability"
)

func doAnalyticsRequest(t *testing.T, s *RESTServer, token string) map[string]interface{} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
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

// Unauthorized: /api/analytics is gated by the same authorized() check as
// every other protected route -- no separate/weaker auth path.
func TestRESTServer_Analytics_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

// Test 1/13 — zero resources / unavailable: nil Cache/Reloader must yield
// explicit "unavailable" (JSON null), never a fabricated 0.
func TestRESTServer_Analytics_DistinguishesZeroFromUnavailable(t *testing.T) {
	s, token := newTestRESTServer(t)
	// newTestRESTServer wires a real (empty) Cache by default -- explicitly
	// nil it out here so this test exercises the true "source unavailable"
	// path, not "Cache present but genuinely has zero secrets" (a
	// different, legitimate real-0 case covered by
	// TestRESTServer_Analytics_ManagedSecretsFromRealCache).
	s.Cache = nil
	s.Reloader = nil

	body := doAnalyticsRequest(t, s, token)
	current := body["current"].(map[string]interface{})

	for _, field := range []string{"managed_secrets", "containers_targeted", "drifted", "degraded"} {
		if v, ok := current[field]; !ok || v != nil {
			t.Errorf("expected current.%s to be JSON null (unavailable) with nil Cache/Reloader, got %v", field, v)
		}
	}
}

// Test 2 — normal healthy system: real Cache present -> a real, non-null
// managed_secrets count (0 is a legitimate value here, not "unavailable").
func TestRESTServer_Analytics_ManagedSecretsFromRealCache(t *testing.T) {
	s, token := newTestRESTServer(t)
	s.Cache = agentCacheWithKeys(t, "local:my-secret", "local:other-secret")

	body := doAnalyticsRequest(t, s, token)
	current := body["current"].(map[string]interface{})

	got, ok := current["managed_secrets"].(float64)
	if !ok {
		t.Fatalf("expected managed_secrets to be a real number (Cache is non-nil), got %v", current["managed_secrets"])
	}
	if int(got) != 2 {
		t.Fatalf("expected managed_secrets=2, got %v", got)
	}
}

func agentCacheWithKeys(t *testing.T, keys ...string) *agent.SecretCache {
	t.Helper()
	c := agent.NewSecretCache(0)
	t.Cleanup(c.Close)
	for _, k := range keys {
		c.Set(k, map[string]string{"k": "v"})
	}
	return c
}

// Test 5/6/7 — rotation success/failure and injection failure counters
// reflect the real atomic values from pkg/observability, labeled as
// "since last restart", never as an all-time total.
func TestRESTServer_Analytics_CountersReflectRealValues(t *testing.T) {
	resetAnalyticsCounters(t)
	observability.RotationSuccessTotal.Add(3)
	observability.RotationFailureTotal.Add(1)
	observability.InjectionSuccessTotal.Add(5)
	observability.InjectionFailureTotal.Add(2)

	s, token := newTestRESTServer(t)
	body := doAnalyticsRequest(t, s, token)
	since := body["since_restart"].(map[string]interface{})

	if since["rotation_success_total"].(float64) != 3 {
		t.Errorf("rotation_success_total = %v, want 3", since["rotation_success_total"])
	}
	if since["rotation_failure_total"].(float64) != 1 {
		t.Errorf("rotation_failure_total = %v, want 1", since["rotation_failure_total"])
	}
	if since["injection_success_total"].(float64) != 5 {
		t.Errorf("injection_success_total = %v, want 5", since["injection_success_total"])
	}
	if since["injection_failure_total"].(float64) != 2 {
		t.Errorf("injection_failure_total = %v, want 2", since["injection_failure_total"])
	}
	note, _ := since["note"].(string)
	if note == "" {
		t.Fatal("expected a non-empty note distinguishing this from an all-time total")
	}
}

// resetAnalyticsCounters zeroes the shared package-level counters before a
// test that asserts on their exact value, so an earlier test's increments
// (this file's own tests run in the same process/binary) can't leak in.
func resetAnalyticsCounters(t *testing.T) {
	t.Helper()
	observability.RotationSuccessTotal.Store(0)
	observability.RotationFailureTotal.Store(0)
	observability.InjectionSuccessTotal.Store(0)
	observability.InjectionFailureTotal.Store(0)
}

// Test 8 — multiple events / recent activity buckets by event_type and
// status, reusing the same EventStore every other page reads.
func TestRESTServer_Analytics_RecentActivityBucketing(t *testing.T) {
	s, token := newTestRESTServer(t)

	s.EventStore.Add(Event{"event_type": string(notify.RotationSucceeded), "status": "success", "secret": "a"})
	s.EventStore.Add(Event{"event_type": string(notify.RotationFailed), "status": "failure", "secret": "b"})
	s.EventStore.Add(Event{"event_type": "signal_failed", "status": "failure", "secret": "c"})
	s.EventStore.Add(Event{"event_type": "secret_fetch", "status": "failure", "secret": "d"})

	body := doAnalyticsRequest(t, s, token)
	activity := body["recent_activity"].(map[string]interface{})

	successful := activity["successful_rotations"].([]interface{})
	failed := activity["failed_rotations"].([]interface{})
	injectionFailures := activity["injection_failures"].([]interface{})
	recentFailures := activity["recent_failures"].([]interface{})

	if len(successful) != 1 {
		t.Errorf("expected 1 successful rotation, got %d", len(successful))
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 failed rotation, got %d", len(failed))
	}
	if len(injectionFailures) != 1 {
		t.Errorf("expected 1 injection failure, got %d", len(injectionFailures))
	}
	// recent_failures should include every failure-status event: rotation
	// failure, signal failure, and secret_fetch failure = 3.
	if len(recentFailures) != 3 {
		t.Errorf("expected 3 recent failures, got %d: %+v", len(recentFailures), recentFailures)
	}
}

// Test 11/12 — EventStore-sourced analytics data contains only metadata,
// never a secret value; the full response never leaks any value-shaped
// field.
func TestRESTServer_Analytics_NoSecretValuesInResponse(t *testing.T) {
	s, token := newTestRESTServer(t)
	s.Cache = agentCacheWithKeys(t, "local:database-password")
	s.EventStore.Add(Event{"event_type": string(notify.RotationFailed), "status": "failure", "secret": "database-password", "error": "connection refused"})

	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, forbidden := range []string{`"value"`, `"secret_value"`, `"plaintext"`, `"credential"`, `"access_token"`, `"private_key"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("analytics response unexpectedly contains %q: %s", forbidden, body)
		}
	}
}

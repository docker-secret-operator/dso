package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/agent"
	"github.com/docker-secret-operator/dso/pkg/config"
	"go.uber.org/zap"
)

// Helper to create test RESTServer
func createTestRESTServer() *RESTServer {
	logger, _ := zap.NewDevelopment()
	hub := NewHub(logger)
	go hub.Run()
	return &RESTServer{
		Cache:      agent.NewSecretCache(1 * time.Hour),
		Config:     &config.Config{},
		Logger:     logger,
		Hub:        hub,
		EventStore: NewEventStore(100, hub),
	}
}

// ============================================================================
// Health Endpoint Tests
// ============================================================================

// TestRESTServer_HandleHealth returns healthy status
func TestRESTServer_HandleHealth(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "up") {
		t.Errorf("Expected response to contain 'up', got: %s", body)
	}
}

// TestRESTServer_HealthNoAuth health endpoint requires no auth
func TestRESTServer_HealthNoAuth(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/health", nil)
	// No Authorization header - should still work

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Health endpoint should not require auth, got status %d", w.Code)
	}
}

// ============================================================================
// Authorization Tests
// ============================================================================

// TestRESTServer_Authorized_NoAuth returns true when no auth configured
func TestRESTServer_Authorized_NoAuth(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/api/secrets", nil)

	if !server.authorized(req) {
		t.Error("Should be authorized when no auth is configured")
	}
}

// TestRESTServer_Authorized_NoAuthConfigured accepts all when no auth configured
func TestRESTServer_Authorized_NoAuthConfigured(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/api/secrets", nil)

	if !server.authorized(req) {
		t.Error("Should be authorized when no auth is configured")
	}
}

// ============================================================================
// Protected Endpoints Tests
// ============================================================================

// TestRESTServer_ProtectedEndpointAccessible protected endpoints are accessible
func TestRESTServer_ProtectedEndpointAccessible(t *testing.T) {
	server := createTestRESTServer()

	req := httptest.NewRequest("GET", "/api/secrets", nil)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)

	// Should return 200 when no auth is configured
	if w.Code != http.StatusOK {
		t.Errorf("Endpoint should be accessible, got %d", w.Code)
	}
}

// ============================================================================
// Events Endpoint Tests
// ============================================================================

// TestRESTServer_HandleEvents returns event list
func TestRESTServer_HandleEvents(t *testing.T) {
	server := createTestRESTServer()

	// Add some events
	server.EventStore.Add(Event{
		"Type":      "secret_updated",
		"Severity":  "info",
		"Message":   "Secret updated",
		"Timestamp": time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse response
	var events []Event
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Errorf("Failed to decode response: %v", err)
	}

	if len(events) < 1 {
		t.Error("Expected at least 1 event in response")
	}
}

// TestRESTServer_HandleEvents_WithLimit respects limit parameter
func TestRESTServer_HandleEvents_WithLimit(t *testing.T) {
	server := createTestRESTServer()

	// Add multiple events
	for i := 0; i < 10; i++ {
		server.EventStore.Add(Event{
			"Type":      "test",
			"Severity":  "info",
			"Message":   "Test event",
			"Timestamp": time.Now(),
		})
	}

	req := httptest.NewRequest("GET", "/api/events?limit=3", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var events []Event
	json.NewDecoder(w.Body).Decode(&events)

	if len(events) > 3 {
		t.Errorf("Expected at most 3 events, got %d", len(events))
	}
}

// TestRESTServer_HandleEvents_WithSeverity filters by severity
func TestRESTServer_HandleEvents_WithSeverity(t *testing.T) {
	server := createTestRESTServer()

	server.EventStore.Add(Event{
		"Type":      "error",
		"status":    "error",
		"Message":   "Error event",
		"Timestamp": time.Now(),
	})
	server.EventStore.Add(Event{
		"Type":      "info",
		"status":    "info",
		"Message":   "Info event",
		"Timestamp": time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/events?severity=error", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	var events []Event
	json.NewDecoder(w.Body).Decode(&events)

	for _, ev := range events {
		if ev["status"] != "error" {
			t.Errorf("Expected status 'error', got %q", ev["status"])
		}
	}
}

// TestRESTServer_HandleEvents_EmptyResponse returns empty array when no events
func TestRESTServer_HandleEvents_EmptyResponse(t *testing.T) {
	server := createTestRESTServer()

	req := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	body := w.Body.String()
	if body != "[]" {
		t.Errorf("Expected empty array, got: %s", body)
	}
}

// ============================================================================
// Routing Tests
// ============================================================================

// TestRESTServer_NotFound returns 404 for unknown path
func TestRESTServer_NotFound(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/unknown/path", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

// TestRESTServer_Routing routes requests correctly
func TestRESTServer_Routing(t *testing.T) {
	tests := []struct {
		path    string
		method  string
		handler string
	}{
		{"/health", "GET", "health"},
		{"/api/events", "GET", "events"},
		{"/api/secrets", "GET", "secrets"},
		{"/api/logs", "GET", "logs"},
	}

	for _, tt := range tests {
		server := createTestRESTServer()
		req := httptest.NewRequest(tt.method, tt.path, nil)
		w := httptest.NewRecorder()

		server.ServeHTTP(w, req)

		// All these should return 200 (not 404 or 401 without auth)
		if w.Code == http.StatusNotFound {
			t.Errorf("Path %s should be routed, got 404", tt.path)
		}
	}
}

// ============================================================================
// Content-Type Tests
// ============================================================================

// TestRESTServer_ResponseContentType returns correct content type
func TestRESTServer_ResponseContentType(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/api/events", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected application/json, got %s", contentType)
	}
}

// ============================================================================
// Webhook Tests
// ============================================================================

// TestRESTServer_HandleSecretUpdate_DisabledWebhook rejects when disabled
func TestRESTServer_HandleSecretUpdate_DisabledWebhook(t *testing.T) {
	server := createTestRESTServer()
	server.Config.Agent = config.AgentConfig{
		Watch: config.WatchConfig{
			Webhook: config.WebhookConfig{
				Enabled: false,
			},
		},
	}

	payload := WebhookPayload{
		Provider:   "vault",
		SecretName: "db_password",
		EventType:  "secret_updated",
		Timestamp:  time.Now().String(),
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/events/secret-update", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 when webhooks disabled, got %d", w.Code)
	}
}

// TestRESTServer_HandleSecretUpdate_NoAuthToken rejects when token not set
func TestRESTServer_HandleSecretUpdate_NoAuthToken(t *testing.T) {
	server := createTestRESTServer()
	server.Config.Agent = config.AgentConfig{
		Watch: config.WatchConfig{
			Webhook: config.WebhookConfig{
				Enabled:   true,
				AuthToken: "", // No token
			},
		},
	}

	payload := WebhookPayload{
		Provider:   "vault",
		SecretName: "db_password",
		EventType:  "secret_updated",
		Timestamp:  time.Now().String(),
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/events/secret-update", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 when auth token not set, got %d", w.Code)
	}
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestRESTServer_InvalidQueryParameter handles bad query params
func TestRESTServer_InvalidQueryParameter(t *testing.T) {
	server := createTestRESTServer()
	req := httptest.NewRequest("GET", "/api/events?limit=not-a-number", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should use default limit and not error
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with invalid limit param, got %d", w.Code)
	}
}

// TestRESTServer_NegativeLimit uses default when limit is negative
func TestRESTServer_NegativeLimit(t *testing.T) {
	server := createTestRESTServer()

	// Add an event
	server.EventStore.Add(Event{
		"Type":      "test",
		"Message":   "Test",
		"Timestamp": time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/events?limit=-5", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with negative limit, got %d", w.Code)
	}
}

type UnauthorizedError struct{}

func (e *UnauthorizedError) Error() string {
	return "unauthorized"
}

// ============================================================================
// Integration Tests
// ============================================================================

// TestRESTServer_MultipleRequests handles multiple requests correctly
func TestRESTServer_MultipleRequests(t *testing.T) {
	server := createTestRESTServer()

	// First request
	req1 := httptest.NewRequest("GET", "/health", nil)
	w1 := httptest.NewRecorder()
	server.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Error("First health request failed")
	}

	// Second request
	req2 := httptest.NewRequest("GET", "/health", nil)
	w2 := httptest.NewRecorder()
	server.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Error("Second health request failed")
	}
}

// TestRESTServer_ConcurrentRequests handles concurrent requests safely
func TestRESTServer_ConcurrentRequests(t *testing.T) {
	server := createTestRESTServer()
	done := make(chan bool)

	// Multiple concurrent requests
	for i := 0; i < 10; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Error("Concurrent request failed")
			}
			done <- true
		}()
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestRESTServer_LargeResponseBody handles large responses
func TestRESTServer_LargeResponseBody(t *testing.T) {
	server := createTestRESTServer()

	// Add many events
	for i := 0; i < 1000; i++ {
		server.EventStore.Add(Event{
			"type":      "test",
			"message":   "Test event with some content",
			"timestamp": time.Now(),
		})
	}

	req := httptest.NewRequest("GET", "/api/events?limit=1000", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	// Verify large body was encoded correctly
	_, err := io.ReadAll(w.Body)
	if err != nil {
		t.Errorf("Failed to read large response body: %v", err)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker-secret-operator/dso/internal/watcher"
)

// TestRESTServer_Containers_Unauthorized verifies /api/containers is gated by
// the same authorized() check as /api/secrets and /api/discovery -- no
// separate/weaker auth path for the new endpoint.
func TestRESTServer_Containers_Unauthorized(t *testing.T) {
	s, _ := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

// TestRESTServer_Containers_EmptyState verifies a nil Reloader (or one with
// no targets) returns an empty list rather than an error or fabricated data.
func TestRESTServer_Containers_EmptyState(t *testing.T) {
	s, token := newTestRESTServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Containers []ContainerSummary `json:"containers"`
		TotalCount int                `json:"total_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.TotalCount != 0 || len(body.Containers) != 0 {
		t.Fatalf("expected empty containers list, got %+v", body)
	}
}

// TestRESTServer_Containers_ReturnsRealData verifies containers actually
// present in ReloaderController.Targets are returned with their real
// strategy and secret names.
func TestRESTServer_Containers_ReturnsRealData(t *testing.T) {
	s, token := newTestRESTServer(t)

	reloader := &watcher.ReloaderController{}
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:          "container-abc",
		Strategy:    "restart",
		ComposePath: "/opt/stack/docker-compose.yml",
		Secrets:     []string{"db-password", "api-key"},
	})
	s.Reloader = reloader

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Containers []ContainerSummary `json:"containers"`
		TotalCount int                `json:"total_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body.TotalCount != 1 || len(body.Containers) != 1 {
		t.Fatalf("expected exactly one container, got %+v", body)
	}
	got := body.Containers[0]
	if got.ID != "container-abc" || got.Strategy != "restart" || got.ComposePath != "/opt/stack/docker-compose.yml" {
		t.Errorf("unexpected container summary: %+v", got)
	}
	if len(got.Secrets) != 2 || got.Secrets[0] != "db-password" || got.Secrets[1] != "api-key" {
		t.Errorf("unexpected secrets list: %+v", got.Secrets)
	}
}

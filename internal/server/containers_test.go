package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker/docker/client"
)

// newMockDockerReloader builds a ReloaderController backed by a fake Docker
// daemon (an httptest server serving a fixed "/containers/json" list), for
// tests that need Phase 4's Docker-metadata enrichment without a real
// Docker socket.
func newMockDockerReloader(t *testing.T, containers []map[string]interface{}) *watcher.ReloaderController {
	t.Helper()
	body, err := json.Marshal(containers)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("failed to create mock Docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	reloader := &watcher.ReloaderController{}
	reloader.SetDockerClient(cli)
	return reloader
}

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

// TestRESTServer_Containers_DegradedFieldsPresent verifies the Phase 3
// additive degraded/degraded_reason fields serialize correctly for a
// healthy (non-degraded) container -- degraded=false, no
// degraded_reason key at all (omitempty), never a fabricated true.
func TestRESTServer_Containers_DegradedFieldsPresent(t *testing.T) {
	s, token := newTestRESTServer(t)

	reloader := &watcher.ReloaderController{}
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:       "container-abc",
		Strategy: "restart",
		Secrets:  []string{"db-password"},
	})
	s.Reloader = reloader

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var raw struct {
		Containers []map[string]interface{} `json:"containers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(raw.Containers) != 1 {
		t.Fatalf("expected 1 container, got %+v", raw.Containers)
	}
	c := raw.Containers[0]
	if degraded, ok := c["degraded"].(bool); !ok || degraded {
		t.Errorf("expected degraded=false for a healthy container, got %v", c["degraded"])
	}
	if _, present := c["degraded_reason"]; present {
		t.Errorf("expected degraded_reason to be omitted (omitempty) for a healthy container, got %v", c["degraded_reason"])
	}
}

// TestRESTServer_Containers_DockerMetadataMerged verifies Phase 4's
// name/image/status/health/ports fields are populated by joining the
// tracked target against live Docker metadata on container ID.
func TestRESTServer_Containers_DockerMetadataMerged(t *testing.T) {
	s, token := newTestRESTServer(t)

	reloader := newMockDockerReloader(t, []map[string]interface{}{
		{
			"Id": "container-abc", "Names": []string{"/payments-api"}, "Image": "myapp/backend:2026.09",
			"State": "running", "Status": "Up 5 minutes (healthy)",
			"Ports": []map[string]interface{}{{"PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"}},
		},
	})
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:       "container-abc",
		Strategy: "restart",
		Secrets:  []string{"db-password"},
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
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Containers) != 1 {
		t.Fatalf("expected 1 container, got %+v", body.Containers)
	}
	got := body.Containers[0]
	if got.Name != "payments-api" || got.Image != "myapp/backend:2026.09" || got.Status != "running" || got.Health != "healthy" {
		t.Errorf("unexpected merged Docker metadata: %+v", got)
	}
	if len(got.Ports) != 1 || got.Ports[0] != "8080->80/tcp" {
		t.Errorf("unexpected ports: %+v", got.Ports)
	}
}

// TestRESTServer_Containers_MissingDockerMetadataIsHonest verifies a
// tracked target that Docker no longer reports (e.g. a stale/removed
// container) yields empty enrichment fields -- via JSON omitempty, "Not
// available" territory for the frontend -- rather than a fabricated value.
func TestRESTServer_Containers_MissingDockerMetadataIsHonest(t *testing.T) {
	s, token := newTestRESTServer(t)

	reloader := newMockDockerReloader(t, []map[string]interface{}{}) // Docker reports nothing
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:       "container-abc",
		Strategy: "restart",
		Secrets:  []string{"db-password"},
	})
	s.Reloader = reloader

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var raw struct {
		Containers []map[string]interface{} `json:"containers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	c := raw.Containers[0]
	for _, field := range []string{"name", "image", "status", "health", "ports"} {
		if _, present := c[field]; present {
			t.Errorf("expected %q to be omitted (omitempty) when Docker has no matching container, got %v", field, c[field])
		}
	}
}

// TestRESTServer_Containers_DockerFailureDegradesGracefully verifies a
// Docker-side failure while enriching containers does not fail the whole
// endpoint -- the tracked-target list (the real source of truth for "which
// containers does DSO manage") still returns 200 with honest empty
// enrichment fields, not a 500.
func TestRESTServer_Containers_DockerFailureDegradesGracefully(t *testing.T) {
	s, token := newTestRESTServer(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "docker daemon unavailable", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("failed to create mock Docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	reloader := &watcher.ReloaderController{}
	reloader.SetDockerClient(cli)
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:       "container-abc",
		Strategy: "restart",
		Secrets:  []string{"db-password"},
	})
	s.Reloader = reloader

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even when Docker enrichment fails, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Containers []ContainerSummary `json:"containers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(body.Containers) != 1 || body.Containers[0].ID != "container-abc" {
		t.Fatalf("expected the tracked target to still be listed, got %+v", body.Containers)
	}
}

// TestRESTServer_Containers_SecretSentinelNeverLeaks is the mandatory Phase
// 4 security regression test: a sentinel placed in a fake Docker
// environment/label must never appear in the /api/containers JSON response.
func TestRESTServer_Containers_SecretSentinelNeverLeaks(t *testing.T) {
	const sentinel = "DSO_CONTAINER_SECRET_SENTINEL_4a71e2"
	s, token := newTestRESTServer(t)

	reloader := newMockDockerReloader(t, []map[string]interface{}{
		{
			"Id": "container-abc", "Names": []string{"/app"}, "Image": "app:1",
			"State": "running", "Status": "Up 1 minute",
			"Labels":  map[string]string{"dso.secret.value": sentinel, "com.example.token": sentinel},
			"Command": "run --token=" + sentinel,
		},
	})
	reloader.Targets.Store("container-abc", &watcher.TargetContainer{
		ID:       "container-abc",
		Strategy: "restart",
		Secrets:  []string{"db-password"},
	})
	s.Reloader = reloader

	req := httptest.NewRequest(http.MethodGet, "/api/containers", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), sentinel) {
		t.Fatalf("sentinel leaked into /api/containers response: %s", rec.Body.String())
	}
}

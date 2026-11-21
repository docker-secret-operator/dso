package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// dockerListFixture builds a fake "/containers/json" response body from raw
// container.Summary-shaped maps, so each test can supply exactly the fields
// it cares about without depending on the full Summary struct shape.
func dockerListFixture(t *testing.T, containers []map[string]interface{}) http.Handler {
	t.Helper()
	body, err := json.Marshal(containers)
	if err != nil {
		t.Fatalf("failed to marshal fixture: %v", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

func TestListDockerContainerInfo_RunningAndStopped(t *testing.T) {
	c := newMockController(t, dockerListFixture(t, []map[string]interface{}{
		{"Id": "c1", "Names": []string{"/payments-api"}, "Image": "myapp/backend:2026.09", "State": "running", "Status": "Up 5 minutes"},
		{"Id": "c2", "Names": []string{"/old-worker"}, "Image": "myapp/worker:1.0", "State": "exited", "Status": "Exited (0) 2 hours ago"},
	}))

	info, err := c.ListDockerContainerInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info["c1"].Status != "running" || info["c1"].Name != "payments-api" || info["c1"].Image != "myapp/backend:2026.09" {
		t.Errorf("unexpected running container info: %+v", info["c1"])
	}
	if info["c2"].Status != "exited" || info["c2"].Name != "old-worker" {
		t.Errorf("unexpected stopped container info: %+v", info["c2"])
	}
}

func TestListDockerContainerInfo_HealthStates(t *testing.T) {
	c := newMockController(t, dockerListFixture(t, []map[string]interface{}{
		{"Id": "healthy", "Names": []string{"/svc-a"}, "Image": "a:1", "State": "running", "Status": "Up 3 minutes (healthy)"},
		{"Id": "unhealthy", "Names": []string{"/svc-b"}, "Image": "b:1", "State": "running", "Status": "Up 3 minutes (unhealthy)"},
		{"Id": "starting", "Names": []string{"/svc-c"}, "Image": "c:1", "State": "running", "Status": "Up 5 seconds (health: starting)"},
		{"Id": "nohealth", "Names": []string{"/svc-d"}, "Image": "d:1", "State": "running", "Status": "Up 10 minutes"},
	}))

	info, err := c.ListDockerContainerInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info["healthy"].Health != "healthy" {
		t.Errorf("expected healthy, got %q", info["healthy"].Health)
	}
	if info["unhealthy"].Health != "unhealthy" {
		t.Errorf("expected unhealthy, got %q", info["unhealthy"].Health)
	}
	if info["starting"].Health != "starting" {
		t.Errorf("expected starting, got %q", info["starting"].Health)
	}
	// No healthcheck configured -- must not fabricate "healthy" just
	// because the container is running.
	if info["nohealth"].Health != "" {
		t.Errorf("expected no health state for a container without a healthcheck, got %q", info["nohealth"].Health)
	}
}

func TestListDockerContainerInfo_PublishedPortsOnly(t *testing.T) {
	c := newMockController(t, dockerListFixture(t, []map[string]interface{}{
		{
			"Id": "c1", "Names": []string{"/web"}, "Image": "nginx:1.27", "State": "running", "Status": "Up 1 minute",
			"Ports": []map[string]interface{}{
				{"PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"},
				{"PrivatePort": 443, "Type": "tcp"}, // internal-only, no PublicPort -- must be excluded
			},
		},
	}))

	info, err := c.ListDockerContainerInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ports := info["c1"].Ports
	if len(ports) != 1 || ports[0] != "8080->80/tcp" {
		t.Errorf("expected exactly one published port formatted as 8080->80/tcp, got %+v", ports)
	}
}

func TestListDockerContainerInfo_NilClientDegradesGracefully(t *testing.T) {
	c := &ReloaderController{}

	info, err := c.ListDockerContainerInfo(context.Background())
	if err != nil {
		t.Fatalf("expected no error with a nil Docker client, got %v", err)
	}
	if len(info) != 0 {
		t.Errorf("expected an empty map with a nil Docker client, got %+v", info)
	}
}

// TestListDockerContainerInfo_SentinelNeverLeaks proves the safe DTO never
// carries arbitrary Docker metadata (labels, env-shaped fields) through to
// its output, even when the raw Docker API response contains a sentinel
// value in fields this code deliberately never reads.
func TestListDockerContainerInfo_SentinelNeverLeaks(t *testing.T) {
	const sentinel = "DSO_CONTAINER_SECRET_SENTINEL_4a71e2"

	c := newMockController(t, dockerListFixture(t, []map[string]interface{}{
		{
			"Id": "c1", "Names": []string{"/app"}, "Image": "app:1", "State": "running", "Status": "Up 1 minute",
			"Labels":  map[string]string{"dso.secret.value": sentinel, "com.example.token": sentinel},
			"Command": "run --token=" + sentinel,
		},
	}))

	info, err := c.ListDockerContainerInfo(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	if strings.Contains(string(encoded), sentinel) {
		t.Fatalf("sentinel leaked into DockerContainerInfo output: %s", encoded)
	}
}

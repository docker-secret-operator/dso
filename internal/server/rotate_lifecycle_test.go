package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"go.uber.org/zap"
)

// These tests drive POST /api/secrets/{name}/rotate through a REAL net/http
// server (httptest.NewServer), because net/http cancels the request context
// the moment the handler returns -- httptest.NewRecorder never does, which is
// how the original defect (rotation goroutines running on the request
// context) escaped the handler tests.

type dockerCalls struct {
	mu    sync.Mutex
	calls []string
	abort int // create requests aborted by the client (ctx cancelled)
}

func (d *dockerCalls) add(s string) { d.mu.Lock(); d.calls = append(d.calls, s); d.mu.Unlock() }
func (d *dockerCalls) count(sub string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// countReq counts requests with the given method whose path contains sub (the
// Docker client prefixes paths with /v<api-version>).
func (d *dockerCalls) countReq(method, sub string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if strings.HasPrefix(c, method+" ") && strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

// countEnds counts requests with the given method whose request line ends with suffix.
func (d *dockerCalls) countEnds(method, suffix string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if strings.HasPrefix(c, method+" ") && strings.HasSuffix(c, suffix) {
			n++
		}
	}
	return n
}
func (d *dockerCalls) aborted() int { d.mu.Lock(); defer d.mu.Unlock(); return d.abort }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// rotationFixture starts a mock Docker daemon (createMode: "ok", "fail" or
// "hang"), a ReloaderController pointed at it with one restart-strategy
// target, and a REST server wrapped in a real HTTP server.
func rotationFixture(t *testing.T, createMode string) (*RESTServer, *httptest.Server, *dockerCalls) {
	t.Helper()
	const origID, newID, name = "orig-1", "new-1", "myapp"

	inspect := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: origID, Name: "/" + name, HostConfig: &container.HostConfig{}},
		Config:            &container.Config{Env: []string{"FOO=bar"}, Labels: map[string]string{}},
		NetworkSettings:   &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{}},
	}
	inspectJSON, _ := json.Marshal(inspect)
	healthy := inspect
	healthy.ContainerJSONBase = &container.ContainerJSONBase{
		ID: newID, Name: "/" + name, HostConfig: &container.HostConfig{},
		State: &container.State{Running: true, Health: &container.Health{Status: "healthy"}},
	}
	healthyJSON, _ := json.Marshal(healthy)
	createJSON, _ := json.Marshal(container.CreateResponse{ID: newID})

	calls := &dockerCalls{}
	release := make(chan struct{})
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// A call made on an already-cancelled context may still reach the
		// server but the client has abandoned it; only record calls whose
		// client is still waiting after a short delay (the hanging ones are
		// recorded immediately, they are what the test waits on).
		hangs := strings.Contains(p, "/containers/create") || strings.Contains(p, "/containers/"+newID+"/json")
		if !hangs {
			time.Sleep(15 * time.Millisecond)
			if r.Context().Err() != nil {
				return
			}
		}
		calls.add(r.Method + " " + p + "?" + r.URL.RawQuery)
		switch {
		case r.Method == http.MethodGet && strings.Contains(p, "/containers/"+origID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(inspectJSON)
		case r.Method == http.MethodGet && strings.Contains(p, "/containers/"+newID+"/json"):
			if createMode == "health-hang" {
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done(): // the client gave up waiting for health
					calls.mu.Lock()
					calls.abort++
					calls.mu.Unlock()
				case <-release:
				}
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(healthyJSON)
		case strings.Contains(p, "/containers/create"):
			switch createMode {
			case "fail":
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
			case "hang":
				_, _ = io.Copy(io.Discard, r.Body) // lets net/http notice the client closing the connection
				select {
				case <-r.Context().Done(): // the client gave up
				case <-release:
					return
				}
				calls.mu.Lock()
				calls.abort++
				calls.mu.Unlock()
			default:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write(createJSON)
			}
		case strings.Contains(p, "/rename"), strings.Contains(p, "/stop"), strings.Contains(p, "/start"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(docker.Close)
	t.Cleanup(func() { close(release) }) // runs before docker.Close: never leave a handler blocked
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(docker.URL, "http://"))

	rc, err := watcher.NewReloaderController(zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{"vault": {}},
		Secrets: []config.SecretMapping{{
			Name: "db", Provider: "vault",
			Inject:   config.InjectionConfig{Type: "env"},
			Mappings: map[string]string{"value": "DB_PASSWORD"},
		}},
	}
	rc.Config = cfg
	rc.Targets.Store(origID, &watcher.TargetContainer{ID: origID, Strategy: "restart", Secrets: []string{"db"}})

	s := createTestRESTServer()
	s.Config = cfg
	s.Reloader = rc
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return s, ts, calls
}

func postRotate(t *testing.T, ts *httptest.Server) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/secrets/db/rotate", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("rotate = %d, want 202", resp.StatusCode)
	}
}

// Success: the rotation must run to completion AFTER the 202 has been sent.
func TestManualRotate_RunsToCompletionAfterResponse(t *testing.T) {
	_, ts, calls := rotationFixture(t, "ok")
	postRotate(t, ts)

	waitFor(t, "replacement container created", func() bool { return calls.count("/containers/create") > 0 })
	waitFor(t, "replacement started", func() bool { return calls.count("/containers/new-1/start") > 0 })
	waitFor(t, "original removed", func() bool { return calls.countReq("DELETE", "/containers/orig-1") > 0 })
}

// Failure: a create error rolls back (the original is renamed back) while the
// context is still live, and the original is never removed.
func TestManualRotate_FailureRollsBackOriginalName(t *testing.T) {
	_, ts, calls := rotationFixture(t, "fail")
	postRotate(t, ts)

	waitFor(t, "create attempted", func() bool { return calls.count("/containers/create") > 0 })
	waitFor(t, "original renamed away and back", func() bool { return calls.count("/containers/orig-1/rename") >= 2 })
	if calls.countReq("DELETE", "/containers/orig-1") != 0 {
		t.Fatal("original container must not be removed when the replacement could not be created")
	}
}

// Timeout: a hung daemon call is abandoned when manualRotationTimeout expires
// (the work is bounded and its resources are released), not left running.
func TestManualRotate_TimeoutAbandonsHungDockerCall(t *testing.T) {
	old := manualRotationTimeout
	manualRotationTimeout = 300 * time.Millisecond
	t.Cleanup(func() { manualRotationTimeout = old })

	_, ts, calls := rotationFixture(t, "hang")
	start := time.Now()
	postRotate(t, ts)

	waitFor(t, "hung create call to be abandoned by the client", func() bool { return calls.aborted() > 0 })
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("abandoned after %v, want roughly the 300ms timeout", d)
	}
	// The abandoned rotation must still undo itself: the original was renamed
	// away before the create, so it must be renamed BACK, even though the
	// rotation context has expired (bounded cleanup context).
	waitFor(t, "original renamed back after the timeout", func() bool { return calls.countEnds("POST", "/containers/orig-1/rename?name=myapp") > 0 })
	if calls.countReq("DELETE", "/containers/orig-1") != 0 {
		t.Fatal("original must not be removed")
	}
}

// Shutdown: cancelling the daemon context cancels in-flight manual rotations.
func TestManualRotate_DaemonShutdownCancelsRotation(t *testing.T) {
	s, ts, calls := rotationFixture(t, "hang")
	base, cancel := context.WithCancel(context.Background())
	s.rotationBaseCtx = base
	postRotate(t, ts)

	waitFor(t, "rotation reached the hung create call", func() bool { return calls.count("/containers/create") > 0 })
	cancel()
	waitFor(t, "in-flight create to be cancelled on daemon shutdown", func() bool { return calls.aborted() > 0 })
}

// Health check cut short by the timeout: the replacement is removed, the
// original gets its name back and is running again, and it is never deleted.
func TestManualRotate_TimeoutDuringHealthCheck_RollsBackCleanly(t *testing.T) {
	old := manualRotationTimeout
	manualRotationTimeout = 400 * time.Millisecond
	t.Cleanup(func() { manualRotationTimeout = old })

	_, ts, calls := rotationFixture(t, "health-hang")
	postRotate(t, ts)

	waitFor(t, "health check abandoned", func() bool { return calls.aborted() > 0 })
	waitFor(t, "replacement removed", func() bool { return calls.countReq("DELETE", "/containers/new-1") > 0 })
	waitFor(t, "original name restored", func() bool { return calls.countEnds("POST", "/containers/orig-1/rename?name=myapp") > 0 })
	waitFor(t, "original restarted", func() bool { return calls.countReq("POST", "/containers/orig-1/start") > 0 })
	if calls.countReq("DELETE", "/containers/orig-1") != 0 {
		t.Fatal("original must not be deleted when the replacement never became healthy")
	}
}

// Daemon shutdown during the health check: same clean rollback.
func TestManualRotate_ShutdownDuringHealthCheck_RollsBackCleanly(t *testing.T) {
	s, ts, calls := rotationFixture(t, "health-hang")
	base, cancel := context.WithCancel(context.Background())
	s.rotationBaseCtx = base
	postRotate(t, ts)

	waitFor(t, "rotation reached the health check", func() bool { return calls.countReq("POST", "/containers/new-1/start") > 0 })
	time.Sleep(50 * time.Millisecond)
	cancel()
	waitFor(t, "replacement removed", func() bool { return calls.countReq("DELETE", "/containers/new-1") > 0 })
	waitFor(t, "original name restored", func() bool { return calls.countEnds("POST", "/containers/orig-1/rename?name=myapp") > 0 })
	waitFor(t, "original restarted", func() bool { return calls.countReq("POST", "/containers/orig-1/start") > 0 })
	if calls.countReq("DELETE", "/containers/orig-1") != 0 {
		t.Fatal("original must not be deleted")
	}
}

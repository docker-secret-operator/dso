package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

// newCountingListener returns a ContainerListener wired to a stub Docker API
// that counts ContainerInspect calls, so tests can assert on how many API
// round-trips handleEvent costs.
func newCountingListener(t *testing.T, inspectCalls *int32) *ContainerListener {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inspect is /containers/<id>/json; the list endpoint is /containers/json.
		if strings.HasSuffix(r.URL.Path, "/json") && !strings.HasSuffix(r.URL.Path, "/containers/json") {
			atomic.AddInt32(inspectCalls, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Id":"c1","Config":{"Labels":{"secret":"db"}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	cl := NewContainerListener(cli)
	cl.ctx = context.Background()
	return cl
}

// TestHandleEvent_SkipsInspectForIrrelevantContainer is the PERF-4 regression
// test. handleEvent used to call ContainerInspect as its very first action,
// before checking whether the container was relevant at all -- so every
// exec_create/exec_die/health_status event on any container on the host cost
// an API round-trip. The daemon already ships labels in Actor.Attributes, so
// an irrelevant container must be rejected without any inspect.
func TestHandleEvent_SkipsInspectForIrrelevantContainer(t *testing.T) {
	var inspects int32
	cl := newCountingListener(t, &inspects)

	cl.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{
			ID: "unmanaged-container",
			// Real attributes for a container DSO does not manage: name/image
			// plus some unrelated labels. None are DSO-relevant.
			Attributes: map[string]string{
				"name":            "some-other-app",
				"image":           "nginx:latest",
				"com.example.env": "prod",
			},
		},
	})

	if got := atomic.LoadInt32(&inspects); got != 0 {
		t.Errorf("handleEvent made %d inspect call(s) for an irrelevant container; expected 0", got)
	}
}

// TestHandleEvent_StillInspectsRelevantContainer confirms the optimization did
// not break the path that matters: a container carrying DSO labels must still
// be inspected and processed.
func TestHandleEvent_StillInspectsRelevantContainer(t *testing.T) {
	var inspects int32
	cl := newCountingListener(t, &inspects)

	cl.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor: events.Actor{
			ID: "c1",
			Attributes: map[string]string{
				"name":   "app",
				"secret": "db_password", // DSO-relevant
			},
		},
	})

	if got := atomic.LoadInt32(&inspects); got != 1 {
		t.Errorf("handleEvent made %d inspect call(s) for a relevant container; expected exactly 1", got)
	}
}

// TestHandleEvent_EmptyAttributesFallsThroughToInspect pins the deliberate
// conservative fallback: if an event arrives with no attributes, we must NOT
// treat that as "no relevant labels" (which would silently skip a container we
// should watch) -- we fall through to the inspect instead.
func TestHandleEvent_EmptyAttributesFallsThroughToInspect(t *testing.T) {
	var inspects int32
	cl := newCountingListener(t, &inspects)

	cl.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: "start",
		Actor:  events.Actor{ID: "c1"}, // no Attributes at all
	})

	if got := atomic.LoadInt32(&inspects); got != 1 {
		t.Errorf("handleEvent made %d inspect call(s) with empty attributes; expected 1 (conservative fallback)", got)
	}
}

// TestHandleEvent_IrrelevantContainerStillClearsTracking confirms the cheap
// early-return preserves the cleanup semantics of the old post-inspect
// irrelevance path: a container that loses its DSO labels must be dropped from
// lastLabels, and that must not require an API call.
func TestHandleEvent_IrrelevantContainerStillClearsTracking(t *testing.T) {
	var inspects int32
	cl := newCountingListener(t, &inspects)

	cl.mu.Lock()
	cl.lastLabels["was-tracked"] = map[string]string{"secret": "db"}
	cl.mu.Unlock()

	cl.handleEvent(events.Message{
		Type:   events.ContainerEventType,
		Action: "update",
		Actor: events.Actor{
			ID:         "was-tracked",
			Attributes: map[string]string{"name": "app"}, // labels now gone
		},
	})

	cl.mu.RLock()
	_, stillTracked := cl.lastLabels["was-tracked"]
	cl.mu.RUnlock()

	if stillTracked {
		t.Error("a container that lost its DSO labels was not dropped from lastLabels")
	}
	if got := atomic.LoadInt32(&inspects); got != 0 {
		t.Errorf("cleanup cost %d inspect call(s); expected 0", got)
	}
}

// TestWatchEvents_SubscribesWithActionFilters asserts the other half of PERF-4:
// the event subscription itself must narrow by action, so the noisy events
// (exec_*, health_status, attach, top, resize, oom) are never delivered in the
// first place rather than being received and discarded.
func TestWatchEvents_SubscribesWithActionFilters(t *testing.T) {
	gotFilters := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events") {
			select {
			case gotFilters <- r.URL.Query().Get("filters"):
			default:
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	cli, err := client.NewClientWithOpts(
		client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer func() { _ = cli.Close() }()

	cl := NewContainerListener(cli)
	if err := cl.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = cl.Stop() }()

	raw := <-gotFilters
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		decoded = raw
	}

	if !strings.Contains(decoded, "container") {
		t.Errorf("subscription did not filter by type=container: %s", decoded)
	}
	// The action filter is the PERF-4 fix. Without it the daemon streams every
	// container event on the host.
	for _, action := range []string{"start", "stop", "die", "update"} {
		if !strings.Contains(decoded, action) {
			t.Errorf("subscription is missing the %q action filter: %s", action, decoded)
		}
	}
	// And the noisy ones must NOT be subscribed to.
	for _, noisy := range []string{"exec_create", "exec_start", "health_status", "attach", "resize"} {
		if strings.Contains(decoded, noisy) {
			t.Errorf("subscription should not include noisy action %q: %s", noisy, decoded)
		}
	}
}

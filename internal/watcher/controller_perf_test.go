package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/docker/docker/api/types/container"
)

// TestReconcileRuntimeState_UsesSingleBatchedList is the PERF-5 regression
// test: reconciliation previously issued one ContainerInspect per tracked
// target, on a 10-minute ticker AND on every Docker daemon reconnect. With
// N tracked containers that was N round-trips per cycle. It must now use a
// single batched list call regardless of how many targets are tracked.
func TestReconcileRuntimeState_UsesSingleBatchedList(t *testing.T) {
	var inspectCalls, listCalls int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// Order matters: the LIST endpoint is /containers/json while an
		// INSPECT is /containers/<id>/json, so the list must be matched first
		// or it is miscounted as an inspect.
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			atomic.AddInt32(&listCalls, 1)
			w.WriteHeader(http.StatusOK)
			// Report all three tracked containers as still existing.
			_, _ = w.Write([]byte(`[{"Id":"container-1"},{"Id":"container-2"},{"Id":"container-3"}]`))
		case strings.HasSuffix(r.URL.Path, "/json") && strings.Contains(r.URL.Path, "/containers/"):
			// A per-container inspect. Any of these is a PERF-5 regression.
			atomic.AddInt32(&inspectCalls, 1)
			w.WriteHeader(http.StatusOK)
			b, _ := json.Marshal(minimalInspect("c", "c"))
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}
	})

	rc := newMockController(t, handler)
	for _, id := range []string{"container-1", "container-2", "container-3"} {
		rc.Targets.Store(id, &TargetContainer{ID: id, Secrets: []string{"s"}})
	}

	rc.reconcileRuntimeState(context.Background())

	if got := atomic.LoadInt32(&inspectCalls); got != 0 {
		t.Errorf("reconciliation made %d per-container inspect calls; expected 0 (PERF-5: must use a batched list)", got)
	}
	// Pass 1 (existence) + pass 2 (label re-registration) each list once.
	if got := atomic.LoadInt32(&listCalls); got > 2 {
		t.Errorf("reconciliation made %d list calls; expected at most 2 regardless of target count", got)
	}

	// All three were reported alive, so none should have been dropped.
	for _, id := range []string{"container-1", "container-2", "container-3"} {
		if _, ok := rc.Targets.Load(id); !ok {
			t.Errorf("container %s was wrongly treated as orphaned", id)
		}
	}
}

// TestReconcileRuntimeState_RemovesOnlyMissingContainers confirms the batched
// implementation still detects genuinely-gone containers.
func TestReconcileRuntimeState_RemovesOnlyMissingContainers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.WriteHeader(http.StatusOK)
			// "gone" is deliberately absent from the list.
			_, _ = w.Write([]byte(`[{"Id":"alive"}]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	rc := newMockController(t, handler)
	rc.Targets.Store("alive", &TargetContainer{ID: "alive"})
	rc.Targets.Store("gone", &TargetContainer{ID: "gone"})

	rc.reconcileRuntimeState(context.Background())

	if _, ok := rc.Targets.Load("alive"); !ok {
		t.Error("existing container was wrongly removed")
	}
	if _, ok := rc.Targets.Load("gone"); ok {
		t.Error("missing container was not cleaned up")
	}
}

// TestReconcileRuntimeState_ListFailureDoesNotWipeTracking guards a hazard the
// batched rewrite introduces: if the single list call fails, treating the
// (empty) result as authoritative would mark EVERY tracked container orphaned
// and drop the whole tracking set on one transient API error. The old
// per-inspect code could not fail this way, so this is a new invariant.
func TestReconcileRuntimeState_ListFailureDoesNotWipeTracking(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"daemon unavailable"}`))
	})

	rc := newMockController(t, handler)
	for _, id := range []string{"container-1", "container-2"} {
		rc.Targets.Store(id, &TargetContainer{ID: id})
	}

	rc.reconcileRuntimeState(context.Background())

	for _, id := range []string{"container-1", "container-2"} {
		if _, ok := rc.Targets.Load(id); !ok {
			t.Errorf("container %s was dropped because the list call failed; "+
				"a transient API error must not wipe the tracking set", id)
		}
	}
}

// TestReconcileRuntimeState_StoppedContainerIsNotOrphaned pins the semantics
// that made `All: true` mandatory rather than incidental. ContainerInspect
// succeeded for stopped containers, so the pre-PERF-5 code treated
// "stopped but present" as existing. A default running-only list would
// reclassify every stopped container as orphaned and untrack it.
func TestReconcileRuntimeState_StoppedContainerIsNotOrphaned(t *testing.T) {
	var sawAllFlag bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			// Docker encodes ListOptions{All:true} as ?all=1.
			if r.URL.Query().Get("all") == "1" || r.URL.Query().Get("all") == "true" {
				sawAllFlag = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[{"Id":"stopped-but-present","State":"exited"}]`))
				return
			}
			// Running-only view: the stopped container is invisible.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})

	rc := newMockController(t, handler)
	rc.Targets.Store("stopped-but-present", &TargetContainer{ID: "stopped-but-present"})

	rc.reconcileRuntimeState(context.Background())

	if !sawAllFlag {
		t.Error("reconciliation did not request all containers; stopped containers would be wrongly orphaned")
	}
	if _, ok := rc.Targets.Load("stopped-but-present"); !ok {
		t.Error("a stopped-but-existing container was wrongly treated as orphaned")
	}
}

// interfaceGuard keeps the unused-import checker honest if the container
// package is only referenced from minimalInspect in another file.
var _ = container.ListOptions{}

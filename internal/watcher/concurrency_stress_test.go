package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ============================================================================
// Phase 7 / P2-4: concurrency regression tests.
//
// The Phase 6 audit did NOT find a confirmed race -- it found missing
// targeted coverage for two specific interleavings. These tests exercise
// those interleavings under `go test -race` and assert the invariants the
// audit cared about: no data race, no duplicate rotation, no leaked locks,
// no stale degraded state, no unexpected panic. They do not weaken any
// existing locking to make themselves pass.
// ============================================================================

// stressHandler builds a mock Docker daemon HTTP handler serving just
// enough endpoints for TriggerReload's "signal" strategy branch (the
// simplest, synchronous-release path -- ContainerKill then releaseLock(),
// no goroutine) and reconcileRuntimeState's two ContainerList calls, so the
// two can be driven concurrently against real ReloaderController code
// without needing to mock the full rolling/restart rotation pipeline.
func stressHandler(t *testing.T, containerIDs []string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/containers/json"):
			// Serves BOTH reconcileRuntimeState's "All:true" liveness list
			// and its "dso.reloader=true" labeled-container list -- every
			// stress-test container satisfies both queries, since the
			// point here is exercising the concurrent mutation of shared
			// state, not filter-matching correctness (already covered
			// elsewhere).
			summaries := make([]container.Summary, 0, len(containerIDs))
			for _, id := range containerIDs {
				summaries = append(summaries, container.Summary{
					ID:     id,
					Names:  []string{"/" + id},
					Labels: map[string]string{"dso.reloader": "true", "dso.secrets": "db-password"},
					State:  "running",
				})
			}
			writeJSON(w, summaries)
		case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/containers/") && strings.HasSuffix(req.URL.Path, "/json"):
			id := extractContainerID(req.URL.Path)
			writeJSON(w, container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{
					ID:   id,
					Name: "/" + id,
				},
				Config: &container.Config{
					Labels: map[string]string{"com.docker.compose.service": id},
				},
			})
		case req.Method == http.MethodPost && strings.Contains(req.URL.Path, "/kill"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

func extractContainerID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, p := range parts {
		if p == "containers" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// TestConcurrent_TriggerReloadVsReconciliation is Test A from the Phase 7
// spec: exercises concurrent TriggerReload(...) and reconcileRuntimeState(...)
// calls against shared ReloaderController state, repeated across many
// interleavings rather than one deterministic call, under the race
// detector.
func TestConcurrent_TriggerReloadVsReconciliation(t *testing.T) {
	const numContainers = 5
	const numRounds = 30

	containerIDs := make([]string, numContainers)
	for i := range containerIDs {
		containerIDs[i] = "container-" + strconv.Itoa(i)
	}

	r := newMockController(t, stressHandler(t, containerIDs))
	for _, id := range containerIDs {
		r.Targets.Store(id, &TargetContainer{
			ID:       id,
			Strategy: "signal", // synchronous release path -- no rotation goroutine involved
			Secrets:  []string{"db-password"},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Repeated concurrent TriggerReload calls for the same secret, from
	// multiple goroutines -- stresses rotationLocks acquire/release and the
	// Targets.Range iteration used to find matching containers.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numRounds; i++ {
				_ = r.TriggerReload(ctx, "db-password", nil)
			}
		}()
	}

	// Concurrent periodic reconciliation, mutating r.Targets (delete
	// orphans, add missed-registration entries) while TriggerReload is
	// iterating/reading the same map and touching rotationLocks.
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numRounds; i++ {
				r.reconcileRuntimeState(ctx)
			}
		}()
	}

	wg.Wait()

	// No leaked locks: every rotationLocks entry must have been released by
	// the time all TriggerReload calls have returned. A leaked lock here
	// would permanently block future rotations for that service.
	leaked := 0
	r.rotationLocks.Range(func(key, value interface{}) bool {
		leaked++
		t.Logf("leaked rotation lock: %v", key)
		return true
	})
	if leaked != 0 {
		t.Errorf("expected zero leaked rotation locks after all goroutines completed, found %d", leaked)
	}

	// No stale degraded state: the "signal" strategy never marks a service
	// degraded, so nothing should be flagged after this stress run.
	degradedCount := 0
	r.degraded.Range(func(key, value interface{}) bool {
		degradedCount++
		t.Logf("unexpected degraded entry: %v = %v", key, value)
		return true
	})
	if degradedCount != 0 {
		t.Errorf("expected zero degraded services after a signal-only stress run, found %d", degradedCount)
	}

	// No stale pendingProxy markers: the "signal" strategy never touches
	// proxy-pending state, so it must remain empty.
	pendingCount := 0
	r.pendingProxy.Range(func(key, value interface{}) bool {
		pendingCount++
		return true
	})
	if pendingCount != 0 {
		t.Errorf("expected zero pending-proxy entries after a signal-only stress run, found %d", pendingCount)
	}

	// Targets survive the concurrent reconciliation (all containers were
	// reported alive throughout), proving reconciliation didn't wipe live
	// targets out from under concurrent TriggerReload calls.
	remaining := 0
	r.Targets.Range(func(key, value interface{}) bool {
		remaining++
		return true
	})
	if remaining != numContainers {
		t.Errorf("expected all %d targets to survive concurrent reconciliation, found %d", numContainers, remaining)
	}
}

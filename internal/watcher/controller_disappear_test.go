package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// TestTriggerReload_Restart_ContainerDisappearsDuringDoubleFailure is a
// regression/coverage test for Scenario E's one previously-untested
// sub-case: the "restart" strategy successfully renames the original
// container out of the way, but ContainerCreate then fails (e.g. the target
// disappeared, or any other Docker API failure), and the subsequent
// best-effort rollback rename (moving the original back to its real name)
// also fails.
//
// Traced by inspection in the DSO lifecycle audit as bounded and
// non-corrupting -- no panic, no cross-contamination of unrelated
// containers -- but not previously asserted by any test. This test closes
// that gap: it verifies (a) no panic, (b) the rollback rename is still
// attempted (best-effort, even though its own error is discarded by the
// production code), and (c) the aggregate rotation error surfaced via
// onComplete reflects the real (ContainerCreate) failure rather than being
// silently swallowed.
func TestTriggerReload_Restart_ContainerDisappearsDuringDoubleFailure(t *testing.T) {
	var renameCalls int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/container-1/json"):
			resp := minimalInspect("myapp", "container-1")
			b, _ := json.Marshal(resp)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/container-1/rename"):
			n := atomic.AddInt32(&renameCalls, 1)
			if n == 1 {
				// Step 3 in the production code: rename original -> tempOldName.
				// Succeeds -- the original container still exists at this point.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// Step 5's rollback attempt: rename tempOldName -> originalName,
			// issued only after ContainerCreate has already failed below.
			// Returning 404 here simulates the original container having
			// disappeared (e.g. removed by something else) during that
			// window -- the exact race this test targets.
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such container: container-1"}`))

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"container create failed"}`))

		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{{Name: "db_password", Provider: "vault"}},
	}
	rc.Targets.Store("container-1", &TargetContainer{
		ID:       "container-1",
		Strategy: "restart",
		Secrets:  []string{"db_password"},
	})

	var (
		mu     sync.Mutex
		called bool
		gotErr error
	)
	onComplete := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		called = true
		gotErr = err
	}

	// Must not panic -- this call alone is the regression check for the
	// "no crash" half of the original NOT PROVEN finding.
	if err := rc.TriggerReload(context.Background(), "db_password", onComplete); err != nil {
		t.Fatalf("TriggerReload returned an unexpected synchronous error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := called
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("onComplete was never called")
	}
	if gotErr == nil {
		t.Error("expected the aggregate rotation error to reflect ContainerCreate's failure, got nil")
	}
	if got := atomic.LoadInt32(&renameCalls); got != 2 {
		t.Errorf("expected exactly 2 rename calls (rename-away, then best-effort rollback rename-back), got %d", got)
	}
}

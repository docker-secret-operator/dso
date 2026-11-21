package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
	"github.com/docker-secret-operator/dso/pkg/observability"
	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
)

// Synthetic placeholder values only.
var (
	syntheticV2 = map[string]string{"value": "synthetic-v2"}
	syntheticV3 = map[string]string{"value": "synthetic-v3"}
)

func newOutcomeTestEngine(t *testing.T) *TriggerEngine {
	t.Helper()
	logger := zap.NewNop()
	lm, err := rotation.NewLockManager("", logger)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &TriggerEngine{
		Cache:             NewSecretCache(time.Hour),
		Logger:            logger,
		Config:            &config.Config{},
		LockManager:       lm,
		ctx:               ctx,
		cancel:            cancel,
		secretLockTimeout: 50 * time.Millisecond,
		rotationCooldown:  time.Nanosecond,
	}
}

func storedHash(te *TriggerEngine, key string) (string, bool) {
	v, ok := te.secretHashes.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func waitHashGone(t *testing.T, te *TriggerEngine, key string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := storedHash(te, key); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hash for %s was never rolled back: the change is recorded as applied and will not be retried", key)
}

// PE-01 (D1) regression: on a secret-lock timeout the new hash used to stay
// stored, so every later poll returning the same value was a "no change"
// no-op and the update was never delivered.
func TestExecuteRotation_LockTimeout_DoesNotSuppressRetry(t *testing.T) {
	te := newOutcomeTestEngine(t)
	const key = "aws:db_password"
	sec := config.SecretMapping{Name: "db_password"}

	// Another rotation of this secret holds the lock for the whole test.
	if err := te.LockManager.AcquireLock("db_password", time.Second); err != nil {
		t.Fatal(err)
	}
	defer te.LockManager.ReleaseLock("db_password")

	te.ExecuteRotation("aws", "db_password", syntheticV2, sec)
	waitHashGone(t, te, key)

	// Next poll with the same provider value must dispatch again (the hash
	// is re-recorded synchronously by the dispatch).
	te.ExecuteRotation("aws", "db_password", syntheticV2, sec)
	if _, ok := storedHash(te, key); !ok {
		t.Fatal("retry after a lock timeout was treated as 'no change'")
	}
	waitHashGone(t, te, key) // let the second attempt finish before the test ends
}

func drainEventStream() {
	for {
		select {
		case <-observability.EventStream:
		default:
			return
		}
	}
}

func collectEventTypes() []string {
	var out []string
	for {
		select {
		case ev := <-observability.EventStream:
			out = append(out, fmt.Sprint(ev["event_type"]))
		default:
			return out
		}
	}
}

// PE-01 (D2): deferred and zero-rotated outcomes must never produce a
// RotationSucceeded event; deferred must also make the value retryable.
func TestFinishRotation_NoSuccessEventUnlessRotated(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantSuccess bool
		wantRetry   bool // hash rolled back
	}{
		{"deferred", fmt.Errorf("%w: 1 of 1 targets not rotated", watcher.ErrRotationDeferred), false, true},
		{"zero rotated", watcher.ErrNoTargetsRotated, false, false},
		{"failed", fmt.Errorf("simulated failure"), false, true},
		{"rotated", nil, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			te := newOutcomeTestEngine(t)
			const key = "aws:db_password"
			te.secretHashes.Store(key, "h-new")
			drainEventStream()

			te.finishRotation(rotationOutcome{
				providerName: "aws", secretName: "db_password", cacheKey: key, newHash: "h-new", start: time.Now(),
			}, c.err)

			events := collectEventTypes()
			gotSuccess := false
			for _, e := range events {
				if e == "rotation_succeeded" {
					gotSuccess = true
				}
			}
			if gotSuccess != c.wantSuccess {
				t.Fatalf("RotationSucceeded emitted=%v, want %v (events %v)", gotSuccess, c.wantSuccess, events)
			}
			_, kept := storedHash(te, key)
			if kept == c.wantRetry {
				t.Fatalf("hash kept=%v, want retry=%v", kept, c.wantRetry)
			}
		})
	}
}

// PE-31: an outdated attempt's rollback must never discard a newer value
// recorded concurrently by another caller.
func TestRollbackHash_DoesNotDiscardNewerObservation(t *testing.T) {
	te := newOutcomeTestEngine(t)
	te.secretHashes.Store("k", "h-v3")
	te.rollbackHash("k", "h-v2")
	if h, _ := storedHash(te, "k"); h != "h-v3" {
		t.Fatalf("rollback of v2 removed the newer v3 hash (now %q)", h)
	}
	te.rollbackHash("k", "h-v3")
	if _, ok := storedHash(te, "k"); ok {
		t.Fatal("rollback of the current hash should remove it")
	}
}

// PE-31 regression (run with -race): poll, webhook and IPC reconcile can call
// ExecuteRotation concurrently for the same key. The hash check and the
// hash+cache store used to be unsynchronised, so the stored hash could
// describe a different value than the cache entry that was dispatched.
func TestExecuteRotation_ConcurrentCallers_HashMatchesCache(t *testing.T) {
	te := newOutcomeTestEngine(t)
	sec := config.SecretMapping{Name: "db_password", Rotation: config.RotationConfigV2{Strategy: "none"}}
	const key = "aws:db_password"

	for round := 0; round < 3000; round++ {
		a := map[string]string{"value": fmt.Sprintf("synthetic-a-%d", round)}
		b := map[string]string{"value": fmt.Sprintf("synthetic-b-%d", round)}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, v := range []map[string]string{a, b, a, b, a, b, a, b} {
			wg.Add(1)
			go func(v map[string]string) {
				defer wg.Done()
				<-start
				te.ExecuteRotation("aws", "db_password", v, sec)
			}(v)
		}
		close(start)
		wg.Wait()

		cached, ok := te.Cache.Get(key)
		h, hok := storedHash(te, key)
		if !ok || !hok {
			t.Fatalf("round %d: missing cache (%v) or hash (%v)", round, ok, hok)
		}
		if ComputeHash(cached) != h {
			t.Fatalf("round %d: stored hash does not describe the cached (dispatched) value", round)
		}
	}
}

// End to end through the real ReloaderController: a reload that matches a
// target but rotates nothing must not emit RotationSucceeded (PE-01).
func TestExecuteRotation_ZeroRotated_NoSuccessEvent(t *testing.T) {
	inspect := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{ID: "c-1", Name: "/app", HostConfig: &container.HostConfig{}},
		Config:            &container.Config{Labels: map[string]string{"com.docker.compose.project": "p", "com.docker.compose.service": "app"}},
	}
	body, _ := json.Marshal(inspect)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/c-1/json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))

	rc, err := watcher.NewReloaderController(zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	rc.Targets.Store("c-1", &watcher.TargetContainer{ID: "c-1", Strategy: "unsupported", Secrets: []string{"db_password"}})

	te := newOutcomeTestEngine(t)
	te.Reloader = rc
	drainEventStream()

	te.ExecuteRotation("aws", "db_password", syntheticV3, config.SecretMapping{Name: "db_password"})

	// The outcome is delivered asynchronously; wait for the secret lock to
	// be released, which happens after TriggerReload returns.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := te.LockManager.AcquireLock("db_password", 10*time.Millisecond); err == nil {
			te.LockManager.ReleaseLock("db_password")
			break
		}
	}
	time.Sleep(200 * time.Millisecond) // onComplete runs in its own goroutine
	for _, e := range collectEventTypes() {
		if e == "rotation_succeeded" {
			t.Fatal("RotationSucceeded emitted although zero containers were rotated")
		}
	}
}

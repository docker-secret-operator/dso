package watcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// composeInspectHandler serves ContainerInspect for each id with the given
// Compose project/service labels, counts SIGHUP kills per id, and answers
// 204 to everything else.
func composeInspectHandler(t *testing.T, labels map[string][2]string, kills *sync.Map) http.Handler {
	t.Helper()
	bodies := map[string][]byte{}
	for id, ps := range labels {
		in := minimalInspect("c-"+id, id)
		in.Config.Labels = map[string]string{
			"com.docker.compose.project": ps[0],
			"com.docker.compose.service": ps[1],
		}
		b, _ := json.Marshal(in)
		bodies[id] = b
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for id, body := range bodies {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/"+id+"/json") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
				return
			}
			if strings.Contains(r.URL.Path, "/containers/"+id+"/kill") {
				v, _ := kills.LoadOrStore(id, new(int32))
				atomic.AddInt32(v.(*int32), 1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func killCount(kills *sync.Map, id string) int32 {
	v, ok := kills.Load(id)
	if !ok {
		return 0
	}
	return atomic.LoadInt32(v.(*int32))
}

// waitOutcome runs TriggerReload and waits for onComplete.
func waitOutcome(t *testing.T, r *ReloaderController, secret string) error {
	t.Helper()
	done := make(chan error, 1)
	if err := r.TriggerReload(context.Background(), secret, func(err error) { done <- err }); err != nil {
		t.Fatalf("TriggerReload returned a synchronous error: %v", err)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("onComplete was never called")
		return nil
	}
}

// holdServiceLock marks a rotation of project/service as in progress. Both
// the pre-M1 key (bare service name) and the project-scoped key are held so
// the test describes "this service in this project is busy" under either
// keying scheme.
func holdServiceLock(r *ReloaderController, project, service string) {
	now := time.Now()
	r.rotationLocks.Store(service, &lockInfo{startTime: now, serviceName: service})
	r.rotationLocks.Store(project+"/"+service, &lockInfo{startTime: now, serviceName: service})
}

// PE-01 (D2) regression: when every matched target is skipped because its
// service lock is held, onComplete used to receive nil, which the trigger
// engine turned into CompleteRotation + RotationSucceeded and never retried.
func TestTriggerReload_AllTargetsBusy_ReportsDeferredNotSuccess(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-a": {"projA", "db"}}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"db_password"}})
	holdServiceLock(r, "projA", "db")

	err := waitOutcome(t, r, "db_password")
	if !errors.Is(err, ErrRotationDeferred) {
		t.Fatalf("all targets busy: onComplete(%v), want ErrRotationDeferred", err)
	}
	if n := killCount(kills, "c-a"); n != 0 {
		t.Fatalf("busy target must not be touched, got %d signals", n)
	}
}

// PE-01: a partially busy reload is still "not applied" for the skipped
// targets, so the overall outcome must not be success.
func TestTriggerReload_SomeTargetsBusy_ReportsDeferred(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{
		"c-a": {"projA", "db"},
		"c-b": {"projB", "api"},
	}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "signal", Secrets: []string{"db_password"}})
	r.Targets.Store("c-b", &TargetContainer{ID: "c-b", Strategy: "signal", Secrets: []string{"db_password"}})
	holdServiceLock(r, "projA", "db")

	err := waitOutcome(t, r, "db_password")
	if !errors.Is(err, ErrRotationDeferred) {
		t.Fatalf("partially busy: onComplete(%v), want ErrRotationDeferred", err)
	}
	if n := killCount(kills, "c-b"); n != 1 {
		t.Fatalf("free target should still be rotated once, got %d", n)
	}
}

// PE-30 regression: the service lock was keyed by the bare Compose service
// name, so `db` rotating in project A silently skipped `db` in project B.
func TestTriggerReload_ServiceLockScopedToComposeProject(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-b": {"projB", "db"}}, kills))
	r.Targets.Store("c-b", &TargetContainer{ID: "c-b", Strategy: "signal", Secrets: []string{"db_password"}})
	// Project A's `db` is mid-rotation, under the pre-M1 key ("db") as well
	// as the project-scoped key.
	holdServiceLock(r, "projA", "db")

	if err := waitOutcome(t, r, "db_password"); err != nil {
		t.Fatalf("project B rotation outcome = %v, want nil", err)
	}
	if n := killCount(kills, "c-b"); n != 1 {
		t.Fatalf("project B's db must be rotated despite project A's lock, got %d signals", n)
	}
	if _, held := r.rotationLocks.Load(serviceLockKey("projB", "db")); held {
		t.Fatal("project B lock must be released after the signal rotation")
	}
	if _, held := r.rotationLocks.Load(serviceLockKey("projA", "db")); !held {
		t.Fatal("project B's rotation must not release project A's lock")
	}
}

// PE-01: a reload that matched targets but rotated none of them (no
// rotating strategy) must not be reported as success.
func TestTriggerReload_ZeroRotated_NotReportedAsSuccess(t *testing.T) {
	kills := &sync.Map{}
	r := newMockController(t, composeInspectHandler(t, map[string][2]string{"c-a": {"projA", "db"}}, kills))
	r.Targets.Store("c-a", &TargetContainer{ID: "c-a", Strategy: "manual-unsupported", Secrets: []string{"db_password"}})

	err := waitOutcome(t, r, "db_password")
	if !errors.Is(err, ErrNoTargetsRotated) {
		t.Fatalf("zero rotated: onComplete(%v), want ErrNoTargetsRotated", err)
	}
}

func TestReloadOutcome(t *testing.T) {
	realErr := errors.New("boom")
	cases := []struct {
		name                         string
		err                          error
		dispatched, skipped, matched int
		want                         error
	}{
		{"all rotated", nil, 2, 0, 2, nil},
		{"real error wins", realErr, 1, 1, 2, realErr},
		{"busy", nil, 1, 1, 2, ErrRotationDeferred},
		{"nothing rotated", nil, 0, 0, 1, ErrNoTargetsRotated},
	}
	for _, c := range cases {
		got := reloadOutcome(c.err, c.dispatched, c.skipped, c.matched)
		if (c.want == nil) != (got == nil) || (c.want != nil && !errors.Is(got, c.want)) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if serviceLockKey("", "db") != "db" || serviceLockKey("p", "db") != "p/db" {
		t.Error("serviceLockKey format changed")
	}
}

// PE-30 under concurrency (run with -race): parallel reloads for the same
// service name in different projects all run, and every lock is released.
func TestTriggerReload_ConcurrentProjectsSameService_NoCollision(t *testing.T) {
	kills := &sync.Map{}
	labels := map[string][2]string{}
	ids := []string{"c-1", "c-2", "c-3", "c-4"}
	for i, id := range ids {
		labels[id] = [2]string{"proj" + string(rune('A'+i)), "db"}
	}
	r := newMockController(t, composeInspectHandler(t, labels, kills))
	for _, id := range ids {
		r.Targets.Store(id, &TargetContainer{ID: id, Strategy: "signal", Secrets: []string{"s-" + id}})
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := waitOutcome(t, r, "s-"+id); err != nil {
				t.Errorf("%s: outcome %v", id, err)
			}
		}(id)
	}
	wg.Wait()
	for _, id := range ids {
		if n := killCount(kills, id); n != 1 {
			t.Errorf("%s rotated %d times, want 1", id, n)
		}
	}
	r.rotationLocks.Range(func(k, _ interface{}) bool {
		t.Errorf("leaked rotation lock %v", k)
		return true
	})
}

// PE-33 regression: in the restart strategy a failed file-secret injection
// was only logged; the replacement was started without its secret file and
// the rotation reported success.
func TestTriggerReload_RestartFileInjectionFailure_IsFailure(t *testing.T) {
	const (
		origID   = "orig-1"
		newID    = "new-1"
		filePath = "/run/secrets/dso/db"
	)
	inspect := minimalInspect("app", origID)
	inspect.Config.Labels = map[string]string{}
	inspectJSON, _ := json.Marshal(inspect)
	createJSON, _ := json.Marshal(map[string]string{"Id": newID})

	var (
		mu        sync.Mutex
		startedID []string
		removed   []string
	)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodGet && strings.Contains(p, "/containers/"+origID+"/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(inspectJSON)
		case strings.Contains(p, "/containers/create"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(createJSON)
		case strings.Contains(p, "/containers/"+newID+"/archive"):
			http.Error(w, `{"message":"simulated copy failure"}`, http.StatusInternalServerError)
		case strings.HasSuffix(p, "/start"):
			mu.Lock()
			startedID = append(startedID, p)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			mu.Lock()
			removed = append(removed, p)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	r := newMockController(t, handler)
	r.Cache = &fakeSecretCache{data: map[string]map[string]string{
		"vault:db": {"value": "synthetic-placeholder"},
	}}
	r.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets: []dsoConfig.SecretMapping{{
			Name:     "db",
			Provider: "vault",
			Inject:   dsoConfig.InjectionConfig{Type: "file", Path: filePath},
			Mappings: map[string]string{"value": "DB"},
		}},
	}
	r.Targets.Store(origID, &TargetContainer{ID: origID, Strategy: "restart", Secrets: []string{"db"}})

	err := waitOutcome(t, r, "db")
	if err == nil || !strings.Contains(err.Error(), "file secret injection") {
		t.Fatalf("expected a file-injection failure outcome, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, p := range startedID {
		if strings.Contains(p, "/containers/"+newID+"/") {
			t.Fatalf("replacement without its secret file must never be started (calls: %v)", startedID)
		}
	}
	discarded := false
	for _, p := range removed {
		if strings.Contains(p, "/containers/"+newID) {
			discarded = true
		}
	}
	if !discarded {
		t.Fatalf("replacement container should be removed on injection failure (deletes: %v)", removed)
	}
}

// Replicas of one (project, service) matched by the same reload are part of
// the same rotation: the second replica must share the lock and be rotated,
// not be reported as deferred. Deferring it would make every poll re-dispatch
// the secret and still never reach the second replica.
func TestTriggerReload_SiblingReplicasShareTheReloadLock(t *testing.T) {
	inspectCalls := map[string]*int32{"r-1": new(int32), "r-2": new(int32)}
	bodies := map[string][]byte{}
	for id := range inspectCalls {
		in := minimalInspect("app-"+id, id)
		in.Config.Labels = map[string]string{
			"com.docker.compose.project": "proj",
			"com.docker.compose.service": "app",
		}
		bodies[id], _ = json.Marshal(in)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for id, n := range inspectCalls {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/containers/"+id+"/json") {
				// First inspect (lock key lookup) succeeds; the restart
				// goroutine's own inspect fails so the test needs no further
				// Docker mocking -- reaching it proves the target was rotated.
				if atomic.AddInt32(n, 1) == 1 {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(bodies[id])
					return
				}
				http.Error(w, `{"message":"simulated"}`, http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	r := newMockController(t, handler)
	for id := range inspectCalls {
		r.Targets.Store(id, &TargetContainer{ID: id, Strategy: "restart", Secrets: []string{"db"}})
	}

	err := waitOutcome(t, r, "db")
	if errors.Is(err, ErrRotationDeferred) {
		t.Fatalf("sibling replica was deferred instead of rotated: %v", err)
	}
	for id, n := range inspectCalls {
		if got := atomic.LoadInt32(n); got < 2 {
			t.Errorf("replica %s was never rotated (inspect calls %d)", id, got)
		}
	}
	r.rotationLocks.Range(func(k, _ interface{}) bool {
		t.Errorf("leaked rotation lock %v", k)
		return true
	})
}

func TestReloadLock_RetainRelease(t *testing.T) {
	released := 0
	l := newReloadLock(func() { released++ })
	if !l.retain() {
		t.Fatal("retain on a held lock must succeed")
	}
	l.release()
	if released != 0 {
		t.Fatal("lock released while a holder remains")
	}
	l.release()
	if released != 1 {
		t.Fatalf("expected exactly one unlock, got %d", released)
	}
	if l.retain() {
		t.Fatal("retain after full release must fail so the caller re-acquires")
	}
}

package watcher

// M1 regression tests for the rolling strategy's non-proxy failure path:
//
//   - Workstream A: the (project, service) rotation lock is held until the
//     rolling goroutine's failure handling -- the fallback restart, its health
//     gate and any rollback -- has completed, not released as soon as
//     rs.Execute returns.
//   - Workstream B: the fallback (executeSimpleRestart) health-gates its
//     replacement and rolls back to the original instead of deleting the
//     healthy original the moment the replacement starts.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/docker-secret-operator/dso/internal/rotation"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// rollingFake is a fake Docker daemon for TriggerReload's rolling branch on
// container "c1" (Compose project "proj", service "app"), plus signal-strategy
// containers used to check lock isolation. Shadow containers created by
// rs.Execute are "shadow-N"; replacements created by the fallback restart are
// "fallback-N".
type rollingFake struct {
	t              *testing.T
	shadowStartErr bool   // rs.Execute fails at ContainerStart (non-health class)
	shadowHealth   string // Health.Status reported for shadow-N
	fallbackHealth string // Health.Status reported for fallback-N
	origStartErr   bool   // ContainerStart of c1 fails (rollback cannot restore)

	// When non-nil, the first health inspect of fallback-1 closes
	// fallbackEntered and blocks until releaseFallback is closed: A's
	// failure handling is then provably in flight.
	fallbackEntered chan struct{}
	releaseFallback chan struct{}

	bodies map[string][]byte

	mu        sync.Mutex
	log       []string
	shadows   int
	fallbacks int
	blocked   bool
}

func newRollingFake(t *testing.T) *rollingFake {
	f := &rollingFake{t: t, shadowHealth: "healthy", fallbackHealth: "healthy", bodies: map[string][]byte{}}
	f.addContainer("c1", "app-1", "proj", "app")
	return f
}

func (f *rollingFake) addContainer(id, name, project, service string) {
	in := minimalInspect(name, id)
	in.State = &container.State{Running: true}
	in.Config.Labels = map[string]string{
		"com.docker.compose.project": project,
		"com.docker.compose.service": service,
	}
	f.bodies[id], _ = json.Marshal(in)
}

func (f *rollingFake) healthBody(id, status string) []byte {
	in := minimalInspect("app-1", id)
	in.State = &container.State{Running: true, Health: &container.Health{Status: status}}
	b, _ := json.Marshal(in)
	return b
}

func (f *rollingFake) record(r *http.Request) {
	f.mu.Lock()
	f.log = append(f.log, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	f.mu.Unlock()
}

// count returns how many recorded requests have method and contain sub.
func (f *rollingFake) count(method, sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.log {
		if strings.HasPrefix(e, method+" ") && strings.Contains(e, sub) {
			n++
		}
	}
	return n
}

func (f *rollingFake) counters() (shadows, fallbacks int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shadows, f.fallbacks
}

func writeCreated(w http.ResponseWriter, id string) {
	b, _ := json.Marshal(container.CreateResponse{ID: id})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(b)
}

func writeInspect(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (f *rollingFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	p := r.URL.Path
	switch {
	case strings.HasSuffix(p, "/containers/create"):
		f.mu.Lock()
		var id string
		if strings.Contains(r.URL.Query().Get("name"), "_dso_new_") {
			f.shadows++
			id = "shadow-" + strconv.Itoa(f.shadows)
		} else {
			f.fallbacks++
			id = "fallback-" + strconv.Itoa(f.fallbacks)
		}
		f.mu.Unlock()
		writeCreated(w, id)
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/shadow-"):
		writeInspect(w, f.healthBody("shadow", f.shadowHealth))
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/fallback-"):
		f.mu.Lock()
		block := f.fallbackEntered != nil && !f.blocked && strings.Contains(p, "/containers/fallback-1/")
		if block {
			f.blocked = true
		}
		f.mu.Unlock()
		if block {
			close(f.fallbackEntered)
			<-f.releaseFallback
		}
		writeInspect(w, f.healthBody("fallback", f.fallbackHealth))
	case strings.Contains(p, "/containers/shadow-") && strings.HasSuffix(p, "/start") && f.shadowStartErr:
		http.Error(w, `{"message":"simulated shadow start failure"}`, http.StatusInternalServerError)
	case strings.HasSuffix(p, "/containers/c1/start") && f.origStartErr:
		http.Error(w, `{"message":"simulated original start failure"}`, http.StatusInternalServerError)
	default:
		for id, body := range f.bodies {
			if r.Method == http.MethodGet && strings.Contains(p, "/containers/"+id+"/json") {
				writeInspect(w, body)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func newRollingFakeController(t *testing.T, f *rollingFake) *ReloaderController {
	t.Helper()
	r := newMockController(t, f)
	r.Config = &dsoConfig.Config{}
	r.Targets.Store("c1", &TargetContainer{ID: "c1", Strategy: "rolling", Secrets: []string{"X", "Y"}})
	return r
}

// triggerAsync starts a reload and returns the channel onComplete reports on.
func triggerAsync(t *testing.T, r *ReloaderController, secret string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	if err := r.TriggerReload(context.Background(), secret, func(err error) { done <- err }); err != nil {
		t.Fatalf("TriggerReload(%s) returned a synchronous error: %v", secret, err)
	}
	return done
}

func awaitOutcome(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: onComplete was never called", what)
		return nil
	}
}

// Workstream A: rotation A (secret X) fails its rolling swap and enters the
// non-proxy fallback restart, which is held mid-health-check by a barrier.
// During that window a rotation B for a DIFFERENT secret (Y) on the SAME
// service must be deferred without touching any container, while other
// services and the same service name in another Compose project keep
// rotating independently. Once A's failure handling completes the lock is
// released and B proceeds into its own container operation.
func TestTriggerReload_RollingFallback_HoldsServiceLockUntilFailureHandlingCompletes(t *testing.T) {
	f := newRollingFake(t)
	f.shadowStartErr = true
	f.fallbackEntered = make(chan struct{})
	f.releaseFallback = make(chan struct{})
	f.addContainer("c-other", "other-1", "proj", "other")
	f.addContainer("c-proj2", "app-1", "proj2", "app")
	r := newRollingFakeController(t, f)
	r.Targets.Store("c-other", &TargetContainer{ID: "c-other", Strategy: "signal", Secrets: []string{"Z"}})
	r.Targets.Store("c-proj2", &TargetContainer{ID: "c-proj2", Strategy: "signal", Secrets: []string{"Z"}})

	doneA := triggerAsync(t, r, "X")
	released := false
	release := func() {
		if !released {
			released = true
			close(f.releaseFallback)
		}
	}
	t.Cleanup(release) // never leave A's goroutine parked on failure

	select {
	case <-f.fallbackEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("rotation A never reached its fallback health check")
	}

	// (1) A still holds the service lock while its failure handling runs.
	if _, held := r.rotationLocks.Load("proj/app"); !held {
		t.Fatal("service lock proj/app was released before A's fallback/failure handling completed")
	}

	// (2) B, same service, different secret: deferred, no container touched.
	shadowsBefore, fallbacksBefore := f.counters()
	errB := awaitOutcome(t, triggerAsync(t, r, "Y"), "rotation B during A's fallback")
	if !errors.Is(errB, ErrRotationDeferred) {
		t.Fatalf("rotation B during A's fallback: outcome %v, want ErrRotationDeferred", errB)
	}
	if s, fb := f.counters(); s != shadowsBefore || fb != fallbacksBefore {
		t.Fatalf("rotation B created containers while A held the lock (shadows %d->%d, fallbacks %d->%d)",
			shadowsBefore, s, fallbacksBefore, fb)
	}

	// Isolation: a different service in the same project, and the same
	// service name in a different project, are not blocked by A's lock.
	if err := awaitOutcome(t, triggerAsync(t, r, "Z"), "independent services"); err != nil {
		t.Fatalf("independent services were blocked by A's lock: %v", err)
	}
	if f.count(http.MethodPost, "/containers/c-other/kill") != 1 || f.count(http.MethodPost, "/containers/c-proj2/kill") != 1 {
		t.Fatalf("independent services were not rotated while A held proj/app: %v", f.log)
	}

	// (3) A's failure handling completes; the lock is released afterwards.
	release()
	errA := awaitOutcome(t, doneA, "rotation A")
	if errA == nil || !strings.Contains(errA.Error(), "failed to start new container") {
		t.Fatalf("rotation A outcome %v, want the rolling failure reported", errA)
	}
	assertNoLeakedLocks(t, r)

	shadowsBefore, _ = f.counters()
	errB = awaitOutcome(t, triggerAsync(t, r, "Y"), "rotation B after A")
	if errors.Is(errB, ErrRotationDeferred) {
		t.Fatalf("rotation B after A completed was still deferred: %v", errB)
	}
	if s, _ := f.counters(); s != shadowsBefore+1 {
		t.Fatalf("rotation B did not enter its container operation after A released the lock (shadows %d->%d)", shadowsBefore, s)
	}
	assertNoLeakedLocks(t, r)
}

// M1, end to end through TriggerReload with a health-class failure: the
// shadow carrying the new secret is rejected by WaitHealthy. The fallback
// restart would redeploy that same rejected secret after stopping the
// original, so it is skipped entirely: no replacement is created and the
// original is never renamed, stopped, removed or restarted. The outcome
// reports the health rejection (classified, not string-matched) and the skip.
func TestTriggerReload_RollingHealthFailure_SkipsFallbackAndKeepsOriginal(t *testing.T) {
	f := newRollingFake(t)
	f.shadowHealth = "unhealthy"
	r := newRollingFakeController(t, f)

	err := awaitOutcome(t, triggerAsync(t, r, "X"), "rotation")
	if !errors.Is(err, rotation.ErrUnhealthy) {
		t.Fatalf("outcome %v, want a failure classified rotation.ErrUnhealthy", err)
	}
	for _, want := range []string{"health verification failed", "fallback restart skipped"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("outcome %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "fallback restart also failed") {
		t.Errorf("outcome %q reports a fallback that must not have run", err)
	}
	// executeSimpleRestart never ran: no fallback replacement was created.
	if _, fallbacks := f.counters(); fallbacks != 0 {
		t.Fatalf("fallback restart created %d replacement(s) after a health-class rejection: %v", fallbacks, f.log)
	}
	// The original was not touched at all.
	for _, op := range []struct{ method, sub string }{
		{http.MethodPost, "/containers/c1/rename"},
		{http.MethodPost, "/containers/c1/stop"},
		{http.MethodPost, "/containers/c1/start"},
		{http.MethodDelete, "/containers/c1?"},
	} {
		if n := f.count(op.method, op.sub); n != 0 {
			t.Fatalf("original container received %d %s %s request(s) after a health-class rejection: %v", n, op.method, op.sub, f.log)
		}
	}
	// The rejected shadow itself was discarded by rs.Execute's rollback.
	if f.count(http.MethodDelete, "/containers/shadow-1") == 0 {
		t.Error("rejected shadow container was not removed")
	}
	if _, degraded := r.IsDegraded("app"); degraded {
		t.Error("non-proxy health rejection must not mark the service degraded (original still serving)")
	}
	assertNoLeakedLocks(t, r)
}

// Workstream B, end to end through TriggerReload with a NON-health failure
// (the shadow cannot be started -- a Docker API error): the fallback restart
// still runs; its replacement is rejected by its own health gate, so it rolls
// back. The original must never be deleted, it must be restarted by the
// rollback, and the reported outcome must name both failures.
func TestTriggerReload_RollingNonHealthFailure_FallbackRollsBackAndKeepsOriginal(t *testing.T) {
	f := newRollingFake(t)
	f.shadowStartErr = true
	f.fallbackHealth = "unhealthy"
	r := newRollingFakeController(t, f)

	err := awaitOutcome(t, triggerAsync(t, r, "X"), "rotation")
	if err == nil {
		t.Fatal("outcome nil, want failure")
	}
	if _, fallbacks := f.counters(); fallbacks != 1 {
		t.Fatalf("non-health failure must take the fallback restart exactly once, got %d replacement(s): %v", fallbacks, f.log)
	}
	for _, want := range []string{"failed to start new container", "fallback restart also failed", "rolled back to the original container"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("outcome %q lacks %q", err, want)
		}
	}
	if n := f.count(http.MethodDelete, "/containers/c1?"); n != 0 {
		t.Fatalf("original container was deleted %d time(s) although no replacement passed health verification", n)
	}
	if f.count(http.MethodPost, "/containers/c1/start") == 0 {
		t.Error("original container was not restarted by the rollback")
	}
	if f.count(http.MethodDelete, "/containers/fallback-1") == 0 {
		t.Error("unhealthy fallback replacement was not removed")
	}
	assertNoLeakedLocks(t, r)
}

// Workstream B: a fallback whose rollback also fails is reported as such
// through the reload outcome -- never as success, never as only the rolling
// error.
func TestTriggerReload_RollingFallbackRollbackFailure_IsSurfaced(t *testing.T) {
	old := rollbackRetryUnit
	rollbackRetryUnit = time.Millisecond
	t.Cleanup(func() { rollbackRetryUnit = old })

	f := newRollingFake(t)
	f.shadowStartErr = true
	f.fallbackHealth = "unhealthy"
	f.origStartErr = true
	r := newRollingFakeController(t, f)

	err := awaitOutcome(t, triggerAsync(t, r, "X"), "rotation")
	if err == nil {
		t.Fatal("outcome nil, want failure")
	}
	for _, want := range []string{"fallback restart also failed", "rollback to the original container FAILED"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("outcome %q lacks %q", err, want)
		}
	}
	if _, degraded := r.IsDegraded("app"); !degraded {
		t.Error("service must be marked degraded when the fallback rollback fails")
	}
	if n := f.count(http.MethodDelete, "/containers/c1?"); n != 0 {
		t.Fatalf("original container was deleted %d time(s)", n)
	}
	assertNoLeakedLocks(t, r)
}

// ---- executeSimpleRestart lifecycle (unit level) ---------------------------

// simpleFake answers executeSimpleRestart's calls for original "orig-1" and
// replacement "new-1" and records every request in order.
type simpleFake struct {
	health       string // replacement Health.Status
	origStartErr bool
	execErr      bool
	log          reqLog
}

func (s *simpleFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.log.add(r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery)
	p := r.URL.Path
	switch {
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/orig-1/json"):
		b, _ := json.Marshal(minimalInspect("myapp", "orig-1"))
		writeInspect(w, b)
	case r.Method == http.MethodGet && strings.Contains(p, "/containers/new-1/json"):
		in := minimalInspect("myapp", "new-1")
		in.State = &container.State{Running: true, Health: &container.Health{Status: s.health}}
		b, _ := json.Marshal(in)
		writeInspect(w, b)
	case strings.HasSuffix(p, "/containers/create"):
		writeCreated(w, "new-1")
	case strings.HasSuffix(p, "/containers/orig-1/start") && s.origStartErr:
		http.Error(w, `{"message":"simulated original start failure"}`, http.StatusInternalServerError)
	case strings.HasSuffix(p, "/exec") && s.execErr:
		http.Error(w, `{"message":"simulated exec failure"}`, http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// index returns the position of the first request with method and sub, or -1.
func (l *reqLog) index(method, sub string) int {
	return l.indexWhere(method, func(e string) bool { return strings.Contains(e, sub) })
}

// indexEnds is index for requests whose line ends with suffix (so the
// rename back to "myapp" is not confused with the rename to "myapp_old_*").
func (l *reqLog) indexEnds(method, suffix string) int {
	return l.indexWhere(method, func(e string) bool { return strings.HasSuffix(e, suffix) })
}

func (l *reqLog) indexWhere(method string, match func(string) bool) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.r {
		if strings.HasPrefix(e, method+" ") && match(e) {
			return i
		}
	}
	return -1
}

// A replacement that starts but fails health verification is stopped and
// removed, the original gets its name back and is restarted -- and is never
// deleted.
func TestExecuteSimpleRestart_UnhealthyReplacement_RollsBackKeepingOriginal(t *testing.T) {
	s := &simpleFake{health: "unhealthy"}
	rc := newMockController(t, s)

	err := rc.executeSimpleRestart(context.Background(), "orig-1", map[string]string{"K": "v"}, nil, nil, time.Second, "svc")
	if err == nil || !strings.Contains(err.Error(), "failed health verification") || !strings.Contains(err.Error(), "rolled back to the original container") {
		t.Fatalf("err = %v, want health-verification failure with successful rollback", err)
	}
	if n := s.log.n(http.MethodDelete, "/containers/orig-1"); n != 0 {
		t.Fatalf("original deleted %d time(s) after an unhealthy replacement: %v", n, s.log.r)
	}
	health := s.log.index(http.MethodGet, "/containers/new-1/json")
	stopNew := s.log.index(http.MethodPost, "/containers/new-1/stop")
	removeNew := s.log.index(http.MethodDelete, "/containers/new-1")
	renameBack := s.log.indexEnds(http.MethodPost, "/containers/orig-1/rename?name=myapp")
	startOrig := s.log.index(http.MethodPost, "/containers/orig-1/start")
	if health < 0 || stopNew < 0 || removeNew < 0 || renameBack < 0 || startOrig < 0 {
		t.Fatalf("rollback sequence incomplete: %v", s.log.r)
	}
	if !(health < stopNew && stopNew < removeNew && removeNew < renameBack && renameBack < startOrig) {
		t.Fatalf("rollback out of order (health %d, stop new %d, rm new %d, rename %d, start orig %d): %v",
			health, stopNew, removeNew, renameBack, startOrig, s.log.r)
	}
	if _, degraded := rc.IsDegraded("svc"); degraded {
		t.Error("service must not be degraded after a successful rollback")
	}
}

// A file-mode secret probe failure after a healthy start also rolls back.
func TestExecuteSimpleRestart_ExecProbeFailure_RollsBack(t *testing.T) {
	s := &simpleFake{health: "healthy", execErr: true}
	rc := newMockController(t, s)
	files := []rotation.FileSecretSpec{{Path: "/run/secrets/k", Data: map[string]string{"k": "test-placeholder"}}}

	err := rc.executeSimpleRestart(context.Background(), "orig-1", nil, files, nil, time.Second, "svc")
	if err == nil || !strings.Contains(err.Error(), "file-mode secret probe failed") {
		t.Fatalf("err = %v, want exec probe failure", err)
	}
	if n := s.log.n(http.MethodDelete, "/containers/orig-1"); n != 0 {
		t.Fatalf("original deleted after a failed probe: %v", s.log.r)
	}
	if s.log.n(http.MethodPost, "/containers/orig-1/start") == 0 || s.log.n(http.MethodDelete, "/containers/new-1") == 0 {
		t.Fatalf("probe failure did not roll back: %v", s.log.r)
	}
}

// Happy path: the original is removed only after the replacement passed its
// health check, and the replacement is kept.
func TestExecuteSimpleRestart_HealthyReplacement_RemovesOriginalAfterVerification(t *testing.T) {
	s := &simpleFake{health: "healthy"}
	rc := newMockController(t, s)

	if err := rc.executeSimpleRestart(context.Background(), "orig-1", nil, nil, nil, time.Second, "svc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	health := s.log.index(http.MethodGet, "/containers/new-1/json")
	removeOrig := s.log.index(http.MethodDelete, "/containers/orig-1")
	if health < 0 || removeOrig < 0 || removeOrig < health {
		t.Fatalf("original must be removed only after health verification (health %d, rm orig %d): %v", health, removeOrig, s.log.r)
	}
	if n := s.log.n(http.MethodDelete, "/containers/new-1"); n != 0 {
		t.Fatalf("healthy replacement was removed: %v", s.log.r)
	}
}

// A rollback that cannot restart the original is reported as a rollback
// failure (and the service degraded), never as success.
func TestExecuteSimpleRestart_RollbackFailure_IsReported(t *testing.T) {
	old := rollbackRetryUnit
	rollbackRetryUnit = time.Millisecond
	t.Cleanup(func() { rollbackRetryUnit = old })

	s := &simpleFake{health: "unhealthy", origStartErr: true}
	rc := newMockController(t, s)

	err := rc.executeSimpleRestart(context.Background(), "orig-1", nil, nil, nil, time.Second, "svc")
	if err == nil || !strings.Contains(err.Error(), "rollback to the original container FAILED") {
		t.Fatalf("err = %v, want rollback failure reported", err)
	}
	if _, degraded := rc.IsDegraded("svc"); !degraded {
		t.Error("service must be marked degraded when rollback fails")
	}
	if n := s.log.n(http.MethodDelete, "/containers/orig-1"); n != 0 {
		t.Fatalf("original deleted after a failed rollback: %v", s.log.r)
	}
}

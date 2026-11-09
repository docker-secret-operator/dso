package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// replicaDocker is a fake Docker daemon for the restart strategy that records,
// per original replica, when its replacement began (first rename of the
// original) and ended (removal of the original on success, or the rename back
// on failure), so a test can measure how many replicas were being replaced at
// the same time.
type replicaDocker struct {
	t        *testing.T
	failOn   map[string]bool // original IDs whose ContainerCreate fails
	hold     time.Duration   // how long a replacement stays "in flight"
	mu       sync.Mutex
	renames  map[string]int
	inflight int
	maxIn    int
	finished map[string]string // orig ID -> "ok" | "failed"
	bodies   map[string][]byte
	// project maps orig ID -> Compose project when a test overrides the
	// default "proj"; inflightBy/maxBy track concurrency per project.
	project    map[string]string
	inflightBy map[string]int
	maxBy      map[string]int
}

// setProject relabels replica id into Compose project p (service stays "app").
func (d *replicaDocker) setProject(id, p string) {
	in := minimalInspect("app-"+id, id)
	in.Config.Labels = map[string]string{
		"com.docker.compose.project": p,
		"com.docker.compose.service": "app",
	}
	d.bodies[id], _ = json.Marshal(in)
	if d.project == nil {
		d.project = map[string]string{}
	}
	d.project[id] = p
}

func newReplicaDocker(t *testing.T, ids []string, failOn map[string]bool) *replicaDocker {
	d := &replicaDocker{
		t: t, failOn: failOn, hold: 40 * time.Millisecond,
		renames: map[string]int{}, finished: map[string]string{}, bodies: map[string][]byte{},
	}
	for _, id := range ids {
		in := minimalInspect("app-"+id, id)
		in.Config.Labels = map[string]string{
			"com.docker.compose.project": "proj",
			"com.docker.compose.service": "app",
		}
		d.bodies[id], _ = json.Marshal(in)

		newIn := minimalInspect("app-"+id, "new-"+id)
		newIn.State = &container.State{Running: true, Health: &container.Health{Status: "healthy"}}
		d.bodies["new-"+id], _ = json.Marshal(newIn)
	}
	return d
}

func (d *replicaDocker) begin(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inflight++
	if d.inflight > d.maxIn {
		d.maxIn = d.inflight
	}
	if p, ok := d.project[id]; ok {
		if d.inflightBy == nil {
			d.inflightBy, d.maxBy = map[string]int{}, map[string]int{}
		}
		d.inflightBy[p]++
		if d.inflightBy[p] > d.maxBy[p] {
			d.maxBy[p] = d.inflightBy[p]
		}
	}
}

func (d *replicaDocker) end(id, result string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inflight--
	d.finished[id] = result
	if p, ok := d.project[id]; ok {
		d.inflightBy[p]--
	}
}

// origFromPath returns the original replica ID addressed by a
// /containers/<id>/... path, if any.
func (d *replicaDocker) origFromPath(p string) (string, bool) {
	for id := range d.bodies {
		if strings.HasPrefix(id, "new-") {
			continue
		}
		if strings.Contains(p, "/containers/"+id+"/") || strings.HasSuffix(p, "/containers/"+id) {
			return id, true
		}
	}
	return "", false
}

func (d *replicaDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	// Replacement containers: health inspect (held open briefly so overlapping
	// replacements would be observed), start, stop.
	for id, body := range d.bodies {
		if strings.HasPrefix(id, "new-") && strings.Contains(p, "/containers/"+id+"/json") {
			time.Sleep(d.hold)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
	}
	if strings.HasSuffix(p, "/containers/create") {
		orig := strings.TrimPrefix(r.URL.Query().Get("name"), "app-")
		if d.failOn[orig] {
			http.Error(w, `{"message":"simulated create failure"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"new-` + orig + `"}`))
		return
	}
	if orig, ok := d.origFromPath(p); ok {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(p, "/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(d.bodies[orig])
			return
		case strings.HasSuffix(p, "/rename"):
			d.mu.Lock()
			d.renames[orig]++
			n := d.renames[orig]
			d.mu.Unlock()
			if n == 1 {
				d.begin(orig) // replacement of this replica starts
			} else {
				d.end(orig, "failed") // name restored after a failure
			}
		case r.Method == http.MethodDelete:
			d.end(orig, "ok") // original removed after a healthy replacement
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func newReplicaController(t *testing.T, d *replicaDocker, ids []string) *ReloaderController {
	t.Helper()
	r := newMockController(t, d)
	r.Config = &dsoConfig.Config{}
	for _, id := range ids {
		r.Targets.Store(id, &TargetContainer{ID: id, Strategy: "restart", Secrets: []string{"db"}})
	}
	return r
}

// PE-30/31 follow-up: every replica of a scaled service is rotated, one at a
// time -- never all at once -- and each only after the previous replacement
// passed its health check.
func TestTriggerReload_ScaledService_RotatesAllReplicasSequentially(t *testing.T) {
	ids := []string{"r-1", "r-2", "r-3"}
	d := newReplicaDocker(t, ids, nil)
	r := newReplicaController(t, d, ids)

	if err := waitOutcome(t, r, "db"); err != nil {
		t.Fatalf("outcome = %v, want nil", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for _, id := range ids {
		if d.finished[id] != "ok" {
			t.Errorf("replica %s not rotated (result %q)", id, d.finished[id])
		}
	}
	if d.maxIn != 1 {
		t.Fatalf("up to %d replicas were being replaced concurrently, want exactly 1 at a time", d.maxIn)
	}
	r.rotationLocks.Range(func(k, _ interface{}) bool {
		t.Errorf("leaked rotation lock %v", k)
		return true
	})
}

// A failed replica is rolled back by its strategy and reported, but it does
// not stop the remaining replicas from being attempted, still one at a time.
func TestTriggerReload_ScaledService_FailureDoesNotSkipRemainingReplicas(t *testing.T) {
	ids := []string{"r-1", "r-2", "r-3"}
	d := newReplicaDocker(t, ids, map[string]bool{"r-2": true})
	r := newReplicaController(t, d, ids)

	err := waitOutcome(t, r, "db")
	if err == nil || !strings.Contains(err.Error(), "simulated create failure") {
		t.Fatalf("outcome = %v, want the replica failure reported", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	want := map[string]string{"r-1": "ok", "r-2": "failed", "r-3": "ok"}
	for id, w := range want {
		if d.finished[id] != w {
			t.Errorf("replica %s result %q, want %q", id, d.finished[id], w)
		}
	}
	if d.maxIn != 1 {
		t.Fatalf("up to %d replicas were being replaced concurrently, want 1", d.maxIn)
	}
}

// A long sequential rotation keeps refreshing its service lock, so it is not
// reclaimed as stale by a concurrent reload part-way through.
func TestLockInfo_TouchDefersStaleReclaim(t *testing.T) {
	l := &lockInfo{startTime: time.Now().Add(-10 * time.Minute), serviceName: "app"}
	if l.idleFor() < 5*time.Minute {
		t.Fatal("an untouched old lock must look stale")
	}
	l.touch()
	if l.idleFor() > time.Minute {
		t.Fatalf("a touched lock must not look stale, idle %v", l.idleFor())
	}
}

// The sequencer chains steps per service key only: different services are
// independent, and the first replica of each service never waits.
func TestReplicaSequencer_ChainsPerService(t *testing.T) {
	q := make(replicaSequencer)
	noop := func() {}
	a1 := q.next("p/a", noop)
	b1 := q.next("p/b", noop)
	a2 := q.next("p/a", noop)

	ctx := context.Background()
	if err := a1.await(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b1.await(ctx); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := a2.await(waitCtx); err == nil {
		t.Fatal("second replica of p/a must wait for the first")
	}
	a1.done()
	if err := a2.await(ctx); err != nil {
		t.Fatalf("second replica must proceed once the first is done: %v", err)
	}
}

// panicGate panics inside a rotation goroutine at a chosen point while
// recording how many replicas were inside it at once.
type panicGate struct {
	mu       sync.Mutex
	hits     int
	inflight int
	maxIn    int
}

func (g *panicGate) trip(hold time.Duration) {
	g.mu.Lock()
	g.hits++
	g.inflight++
	if g.inflight > g.maxIn {
		g.maxIn = g.inflight
	}
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.inflight--
		g.mu.Unlock()
	}()
	time.Sleep(hold)
	panic("synthetic rotation panic")
}

// panicEmitter is a ReloaderController.Server whose Emit panics when the
// rolling goroutine announces its swap (after step.await, before Execute).
type panicEmitter struct{ g *panicGate }

func (p panicEmitter) Emit(msg string) {
	if strings.Contains(msg, "Rolling Swap Start") {
		p.g.trip(20 * time.Millisecond)
	}
}

func assertNoLeakedLocks(t *testing.T, r *ReloaderController) {
	t.Helper()
	r.rotationLocks.Range(func(k, _ interface{}) bool {
		t.Errorf("leaked rotation lock %v", k)
		return true
	})
}

// M1 verification (point 4): a replica whose rolling-rotation goroutine
// panics is recovered AND still releases the next replica. Every replica
// panics here, so if recovery skipped step.done() the second replica would
// wait forever and onComplete would never fire.
func TestTriggerReload_ScaledService_RollingPanicStillReleasesNextReplica(t *testing.T) {
	ids := []string{"r-1", "r-2", "r-3"}
	d := newReplicaDocker(t, ids, nil)
	r := newReplicaController(t, d, ids)
	for _, id := range ids {
		r.Targets.Store(id, &TargetContainer{ID: id, Strategy: "rolling", Secrets: []string{"db"}})
	}
	g := &panicGate{}
	r.Server = panicEmitter{g: g}

	err := waitOutcome(t, r, "db")
	if err == nil || !strings.Contains(err.Error(), "panic during rolling rotation") {
		t.Fatalf("outcome = %v, want the recovered panic reported", err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.hits != len(ids) {
		t.Fatalf("%d of %d replicas reached their rotation; a panicking replica blocked the next", g.hits, len(ids))
	}
	if g.maxIn != 1 {
		t.Fatalf("%d replicas were rotating concurrently across panics, want 1", g.maxIn)
	}
	assertNoLeakedLocks(t, r)
}

// Same guarantee for the restart strategy, with the panic injected at the
// very end of a replica's rotation (after its health check passed). The
// recovery defer runs markDegraded + finish (lock release, error recorded)
// before step.done() releases the next replica.
func TestTriggerReload_ScaledService_RestartPanicStillReleasesNextReplica(t *testing.T) {
	ids := []string{"r-1", "r-2", "r-3"}
	d := newReplicaDocker(t, ids, nil)
	r := newReplicaController(t, d, ids)
	g := &panicGate{}
	r.Logger = zaptest.NewLogger(t, zaptest.WrapOptions(zap.Hooks(func(e zapcore.Entry) error {
		if e.Message == "Rotation successful, removing old container" {
			g.trip(20 * time.Millisecond)
		}
		return nil
	})))

	err := waitOutcome(t, r, "db")
	if err == nil || !strings.Contains(err.Error(), "panic during restart rotation") {
		t.Fatalf("outcome = %v, want the recovered panic reported", err)
	}
	g.mu.Lock()
	hits, maxIn := g.hits, g.maxIn
	g.mu.Unlock()
	if hits != len(ids) {
		t.Fatalf("%d of %d replicas reached their final step; a panicking replica blocked the next", hits, len(ids))
	}
	if maxIn != 1 {
		t.Fatalf("%d replicas were rotating concurrently across panics, want 1", maxIn)
	}
	if _, degraded := r.IsDegraded("app"); !degraded {
		t.Error("a panicked restart rotation must leave the service marked degraded")
	}
	assertNoLeakedLocks(t, r)
}

// M1 verification (point 2) at the TriggerReload level: sequencing is keyed
// by Compose project + service. Two projects each running a scaled `app`
// service are rotated one replica at a time WITHIN each project, but the two
// projects are not serialized against each other.
func TestTriggerReload_ScaledService_SequencingKeyedByProjectAndService(t *testing.T) {
	ids := []string{"a-1", "a-2", "b-1", "b-2"}
	d := newReplicaDocker(t, ids, nil)
	d.hold = 100 * time.Millisecond
	for _, id := range ids {
		d.setProject(id, "proj-"+id[:1])
	}
	r := newReplicaController(t, d, ids)

	if err := waitOutcome(t, r, "db"); err != nil {
		t.Fatalf("outcome = %v, want nil", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, id := range ids {
		if d.finished[id] != "ok" {
			t.Errorf("replica %s result %q, want ok", id, d.finished[id])
		}
	}
	for _, p := range []string{"proj-a", "proj-b"} {
		if d.maxBy[p] != 1 {
			t.Errorf("%s: %d replicas replaced concurrently, want 1", p, d.maxBy[p])
		}
	}
	if d.maxIn != 2 {
		t.Errorf("max concurrent replacements across projects = %d, want 2 (same service name in different projects must not be serialized)", d.maxIn)
	}
	assertNoLeakedLocks(t, r)
}

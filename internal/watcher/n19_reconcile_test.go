package watcher

// N19 follow-up: bounded startup / cache-warm reconciliation of EXISTING gated
// containers, and the identity/ledger guarantees of a re-delivery. Docker-free: the
// daemon is an httptest handler (container list, start/restart recording), the
// in-container operations are the rotation.DeliveryOps seam of gateRec, and the REAL
// gate arming, verification, service locks and OperationGuard consultation run in
// between. Nothing here is real Docker evidence; the runtime premise stays UNVERIFIED.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/rotation"
	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

// lockedCache is a concurrency-safe SecretCache the tests can update while the
// reconciliation worker reads it.
type lockedCache struct {
	mu   sync.Mutex
	data map[string]map[string]string
	gets int
}

func (c *lockedCache) Get(k string) (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	v, ok := c.data[k]
	return v, ok
}

func (c *lockedCache) set(k string, v map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = map[string]map[string]string{}
	}
	c.data[k] = v
}

type gateWorld struct {
	t     *testing.T
	r     *ReloaderController
	gate  *gateRec
	cache *lockedCache
	logs  *observer.ObservedLogs

	mu        sync.Mutex
	list      []container.Summary
	listCalls int
	lifecycle []string // POST .../start|restart|stop|kill paths: must stay empty
	listGate  chan struct{}
	override  map[string]func(*container.InspectResponse)
}

func (w *gateWorld) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	p := req.URL.Path
	switch {
	case req.Method == http.MethodGet && strings.HasSuffix(p, "/containers/json"):
		w.mu.Lock()
		w.listCalls++
		gateCh := w.listGate
		list := append([]container.Summary(nil), w.list...)
		w.mu.Unlock()
		if gateCh != nil {
			<-gateCh
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(list)
	case req.Method == http.MethodPost && (strings.HasSuffix(p, "/start") || strings.HasSuffix(p, "/restart") ||
		strings.HasSuffix(p, "/stop") || strings.HasSuffix(p, "/kill")):
		w.mu.Lock()
		w.lifecycle = append(w.lifecycle, p)
		w.mu.Unlock()
		rw.WriteHeader(http.StatusNoContent)
	default:
		rw.WriteHeader(http.StatusNoContent)
	}
}

func (w *gateWorld) lists() int { w.mu.Lock(); defer w.mu.Unlock(); return w.listCalls }

func newGateWorld(t *testing.T) *gateWorld {
	t.Helper()
	w := &gateWorld{t: t, gate: &gateRec{}, override: map[string]func(*container.InspectResponse){}}
	r := newMockController(t, w)
	core, logs := observer.New(zap.DebugLevel)
	r.Logger = zap.New(core)
	w.r, w.logs = r, logs
	w.cache = &lockedCache{data: map[string]map[string]string{"vault:db": {"value": n19RestartSentinel}}}
	r.Cache = w.cache
	r.Config = &dsoConfig.Config{
		Providers: map[string]dsoConfig.ProviderConfig{"vault": {}},
		Secrets:   []dsoConfig.SecretMapping{n22FileSecret()},
	}
	// the gated config every re-armed container carries
	cfg := container.Config{Entrypoint: []string{"/app/entry"}, Cmd: []string{"serve"}}
	hc := &container.HostConfig{Tmpfs: map[string]string{"/run/secrets/dso": ""}}
	if err := rotation.PrepareGatedCandidate(&cfg, hc, []rotation.FileSecretSpec{{Path: "/run/secrets/dso", Data: map[string]string{"value": "x"}, UID: 1000, GID: 1000}}, 0); err != nil {
		t.Fatal(err)
	}
	w.gate.last = createReq{Config: cfg, HostConfig: hc}
	w.gate.onEvent = func(ev string) {
		if strings.HasPrefix(ev, "release:") {
			w.gate.mu.Lock()
			w.gate.flagPresent = true // the flag now exists on the tmpfs
			w.gate.mu.Unlock()
		}
	}
	ops := w.gate.ops()
	base := ops.Inspect
	ops.Inspect = func(ctx context.Context, id string) (container.InspectResponse, error) {
		in, err := base(ctx, id)
		w.mu.Lock()
		edit := w.override[id]
		w.mu.Unlock()
		if edit != nil {
			edit(&in)
		}
		return in, err
	}
	r.deliveryOps = ops
	return w
}

func (w *gateWorld) addRunning(id, name string) {
	w.mu.Lock()
	w.list = append(w.list, container.Summary{ID: id, Names: []string{"/" + name}, State: "running", Status: "Up 3 seconds",
		Labels: map[string]string{rotation.GateLabel: "marker"}})
	w.mu.Unlock()
	w.r.Targets.Store(id, &TargetContainer{ID: id, Strategy: "restart", Secrets: []string{"db"}})
}

func (w *gateWorld) addExited(id, name, status string, code int, restartPolicy string) {
	w.mu.Lock()
	w.list = append(w.list, container.Summary{ID: id, Names: []string{"/" + name}, State: "exited", Status: status,
		Labels: map[string]string{rotation.GateLabel: "marker"}})
	w.override[id] = func(in *container.InspectResponse) {
		in.State = &container.State{Running: false, ExitCode: code, FinishedAt: "2026-01-01T00:00:00Z"}
		in.HostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(restartPolicy)}
	}
	w.mu.Unlock()
}

func (w *gateWorld) lifecycleCalls() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.lifecycle...)
}

func (w *gateWorld) logged(sub string) int {
	n := 0
	for _, e := range w.logs.All() {
		if strings.Contains(e.Message, sub) {
			n++
		}
	}
	return n
}

func (w *gateWorld) released() bool { return w.gate.has("release:/run/secrets/dso") }

func countOf(list []string, want string) int {
	n := 0
	for _, e := range list {
		if e == want {
			n++
		}
	}
	return n
}

// (1)(2)(7): the container was started (and sits at its gate) BEFORE the agent subscribed
// to Docker events, or the agent restarted while it waited. No start event will ever come:
// the startup pass must find it through discovery and serve it from the cache.
func TestN19Reconcile_StartupDeliversToAContainerAlreadyWaitingAtItsGate(t *testing.T) {
	w := newGateWorld(t)
	w.addRunning("app-1", "app")
	rep := w.r.ReconcileGatedContainers(t.Context())
	if rep.Delivered != 1 || rep.Examined != 1 {
		t.Fatalf("report %+v", rep)
	}
	ev := w.gate.eventList()
	if i, j := indexOf(ev, "inject:/run/secrets/dso"), indexOf(ev, "release:/run/secrets/dso"); i < 0 || j < i {
		t.Fatalf("events %v: delivery must precede the release", ev)
	}
	if len(w.lifecycleCalls()) != 0 {
		t.Fatalf("the pass issued container lifecycle calls: %v", w.lifecycleCalls())
	}
}

// (2)(3)(6): the cache is empty (agent restart, provider down). The pass delivers nothing,
// issues no provider request (it only reads the cache), and the container is delivered
// as soon as a value is cached, by ONE coalesced worker.
func TestN19Reconcile_EmptyCacheWaitsThenCacheWarmDelivers(t *testing.T) {
	w := newGateWorld(t)
	w.cache.data = map[string]map[string]string{}
	w.addRunning("app-1", "app")
	rep := w.r.ReconcileGatedContainers(t.Context())
	if rep.Waiting != 1 || w.gate.has("inject:/run/secrets/dso") || w.released() {
		t.Fatalf("report %+v events %v: nothing may be delivered without a value", rep, w.gate.eventList())
	}
	// repeated passes and repeated start events while the provider is down: still closed
	for i := 0; i < 5; i++ {
		_ = w.r.handleContainerEvent(t.Context(), gatedStartEvent("app-1", true))
		w.r.ReconcileGatedContainers(t.Context())
	}
	if w.gate.has("inject:/run/secrets/dso") || w.released() {
		t.Fatalf("delivered with an empty cache: %v", w.gate.eventList())
	}
	calls := w.lists()
	// the provider recovered and the first value is cached: NotifyCacheWarm serves it
	w.cache.set("vault:db", map[string]string{"value": n19RestartSentinel})
	w.r.NotifyCacheWarm(t.Context())
	deadline := time.Now().Add(5 * time.Second)
	for !w.released() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !w.released() {
		t.Fatalf("cache warm did not deliver: %v", w.gate.eventList())
	}
	if w.lists() <= calls {
		t.Fatal("the warm-cache trigger never ran a pass")
	}
	// nothing waits any more: a further notification costs no Docker call at all
	time.Sleep(50 * time.Millisecond)
	before := w.lists()
	w.r.NotifyCacheWarm(t.Context())
	time.Sleep(50 * time.Millisecond)
	if w.lists() != before {
		t.Fatal("a cache notification with nothing waiting still queried Docker")
	}
}

// (4)(5): a gate that timed out (exit 75) on a container WITHOUT a restart policy is never
// restarted by DSO; it is diagnosed ONCE with the documented recovery command. A restart
// policy (the daemon restarts it) and any other exit status (an operator's stop) produce
// no diagnostic and no lifecycle call.
func TestN19Reconcile_TimedOutGate_NeverRestartedButDiagnosedOnce(t *testing.T) {
	w := newGateWorld(t)
	w.addExited("e-none", "timed-out", "Exited (75) 5 minutes ago", 75, "no")
	w.addExited("e-always", "supervised", "Exited (75) 5 minutes ago", 75, "always")
	w.addExited("e-stop", "operator-stopped", "Exited (143) 5 minutes ago", 143, "no")
	for i := 0; i < 3; i++ {
		rep := w.r.ReconcileGatedContainers(t.Context())
		if rep.Delivered != 0 {
			t.Fatalf("report %+v: an exited container must never be delivered to", rep)
		}
	}
	if n := w.logged("docker start timed-out"); n != 1 {
		t.Fatalf("diagnostic logged %d times, want exactly once; logs: %v", n, w.logs.All())
	}
	if w.logged("docker start supervised") != 0 || w.logged("docker start operator-stopped") != 0 {
		t.Fatal("a diagnostic was emitted for a container that does not need it")
	}
	if len(w.lifecycleCalls()) != 0 || w.gate.has("inject:/run/secrets/dso") {
		t.Fatalf("a stopped container was touched: %v %v", w.lifecycleCalls(), w.gate.eventList())
	}
	// the die event of the gate's own timeout reports the same command (once)
	w.r.gateDiag.Delete("e-none@2026-01-01T00:00:00Z")
	_ = w.r.handleContainerEvent(t.Context(), eventsDie("e-none", "timed-out", "75"))
	if w.logged("docker start timed-out") != 2 {
		t.Fatalf("die(75) event did not report the recovery command: %v", w.logs.All())
	}
}

// (8)(9)(10): a candidate a rotation owns, a rotation artifact, a service with an
// unresolved operation, a busy service, an unregistered container and a foreign-owned
// one are never released; once the obstacle is gone the same pass delivers.
func TestN19Reconcile_NeverReleasesWhatARotationOrAnOperationOwns(t *testing.T) {
	w := newGateWorld(t)
	w.addRunning("app-1", "app")
	var blocked = true
	w.r.Guard = guardFn(func(keys ...string) (string, bool) {
		if blocked {
			return "unresolved operation rot-x (restart, last gate_started, status critical_error)", true
		}
		return "", false
	})
	expectClosed := func(label string) {
		t.Helper()
		w.r.ReconcileGatedContainers(t.Context())
		if w.released() || w.gate.has("inject:/run/secrets/dso") {
			t.Fatalf("%s: the gate was touched: %v", label, w.gate.eventList())
		}
	}
	expectClosed("unresolved operation (also: startup recovery not settled)")
	if w.logged("unresolved operation") == 0 {
		t.Fatal("the refusal was not explained")
	}
	blocked = false

	w.r.markGateOwned("app-1")
	expectClosed("rotation candidate in flight")
	w.r.clearGateOwned("app-1")

	w.r.markProxyPending("app-1")
	expectClosed("candidate not yet ready")
	w.r.clearProxyPending("app-1")

	// the in-memory service lock of a running rotation (same key TriggerReload takes)
	w.r.rotationLocks.Store("app-1", &lockInfo{startTime: time.Now(), serviceName: "app-1"})
	expectClosed("a rotation of the service is running")
	w.r.rotationLocks.Delete("app-1")

	w.r.Targets.Delete("app-1")
	expectClosed("not a registered target")
	w.r.Targets.Store("app-1", &TargetContainer{ID: "app-1", Strategy: "restart", Secrets: []string{"db"}})

	w.r.Config.Agent.OwnerID = "me"
	w.mu.Lock()
	w.list[0].Labels["dso.owner"] = "someone-else"
	w.mu.Unlock()
	expectClosed("owned by another agent")
	w.mu.Lock()
	delete(w.list[0].Labels, "dso.owner")
	w.mu.Unlock()
	w.r.Config.Agent.OwnerID = ""

	w.mu.Lock()
	w.list[0].Names = []string{"/app_dso_new_ab12"}
	w.mu.Unlock()
	expectClosed("rotation artifact name")
	w.mu.Lock()
	w.list[0].Names = []string{"/app"}
	w.mu.Unlock()

	if rep := w.r.ReconcileGatedContainers(t.Context()); rep.Delivered != 1 || !w.released() {
		t.Fatalf("with every obstacle gone the pass must deliver: %+v %v", rep, w.gate.eventList())
	}
}

type guardFn func(keys ...string) (string, bool)

func (g guardFn) RotationBlocked(keys ...string) (string, bool) { return g(keys...) }

// Repeated reconciliation is idempotent: once released, further passes only observe.
func TestN19Reconcile_RepeatedPassesAreIdempotent(t *testing.T) {
	w := newGateWorld(t)
	w.addRunning("app-1", "app")
	w.r.ReconcileGatedContainers(t.Context())
	injects := countOf(w.gate.eventList(), "inject:/run/secrets/dso")
	for i := 0; i < 3; i++ {
		if rep := w.r.ReconcileGatedContainers(t.Context()); rep.Released != 1 || rep.Delivered != 0 {
			t.Fatalf("pass %d: %+v", i, rep)
		}
	}
	if got := countOf(w.gate.eventList(), "inject:/run/secrets/dso"); got != injects || countOf(w.gate.eventList(), "release:/run/secrets/dso") != 1 {
		t.Fatalf("a released gate was delivered to again: %v", w.gate.eventList())
	}
}

// The pass is bounded (containers examined) and its triggers coalesce into one worker.
func TestN19Reconcile_BoundedAndCoalesced(t *testing.T) {
	w := newGateWorld(t)
	for i := 0; i < maxGateScan+7; i++ {
		id := "c" + strings.Repeat("x", 3) + string(rune('a'+i%26)) + string(rune('A'+i/26))
		w.mu.Lock()
		w.list = append(w.list, container.Summary{ID: id, Names: []string{"/" + id}, State: "running", Labels: map[string]string{rotation.GateLabel: "m"}})
		w.mu.Unlock() // unregistered targets: each is a cheap skip
	}
	rep := w.r.ReconcileGatedContainers(t.Context())
	if rep.Examined != maxGateScan || !rep.Truncated {
		t.Fatalf("pass not bounded: %+v", rep)
	}

	// coalescing: many requests while a pass is blocked collapse into one more pass
	w2 := newGateWorld(t)
	w2.listGate = make(chan struct{})
	for i := 0; i < 50; i++ {
		w2.r.RequestGateReconcile(t.Context())
	}
	deadline := time.Now().Add(5 * time.Second)
	for w2.lists() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(w2.listGate)
	for w2.r.gateScanBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := w2.lists(); n < 1 || n > 2 {
		t.Fatalf("50 requests produced %d passes; they must coalesce into at most 2", n)
	}
}

// Fairness (finding 1a): with more than maxGateScan RUNNING gated containers, a hard
// prefix cut by sorted ID would examine the SAME first maxGateScan containers on every
// single pass forever -- container maxGateScan+1 would never be reconciled. The scan must
// instead rotate so repeated passes eventually cover every container. The cache is left
// empty so every container the scan actually reaches ends up recorded in gateWaiting (and
// stays there across passes, since it is still running) -- a precise, cumulative record of
// everything any pass has ever examined.
func TestN19Reconcile_FairnessAcrossPasses_AllEventuallyExamined(t *testing.T) {
	w := newGateWorld(t)
	w.cache.data = map[string]map[string]string{} // no cached value: every examined container waits
	total := maxGateScan*2 + 13
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("c%05d", i)
		w.addRunning(id, id)
	}
	passes := total/maxGateScan + 2
	for i := 0; i < passes; i++ {
		rep := w.r.ReconcileGatedContainers(t.Context())
		if rep.Examined != maxGateScan {
			t.Fatalf("pass %d: expected a full bounded pass, got %+v", i, rep)
		}
	}
	seen := 0
	w.r.gateWaiting.Range(func(_, _ interface{}) bool { seen++; return true })
	if seen != total {
		t.Fatalf("after %d passes of %d each, only %d/%d containers were ever examined -- "+
			"the same prefix is being re-scanned every pass instead of rotating", passes, maxGateScan, seen, total)
	}
}

// Fairness (finding 1b): All:true means exited rotation artifacts/shadows are listed
// alongside running containers. They must never consume the bounded scan budget ahead of
// a RUNNING container waiting at its gate -- that gate is what keeps an application from
// starting. More exited(75) artifacts than the entire budget must still leave every running
// container served in the same pass.
func TestN19Reconcile_ExitedArtifactsDoNotStarveRunningContainers(t *testing.T) {
	w := newGateWorld(t)
	for i := 0; i < maxGateScan+10; i++ {
		id := fmt.Sprintf("a-exited-%04d", i)
		w.addExited(id, fmt.Sprintf("exited-%04d", i), "Exited (75) 5 minutes ago", 75, "no")
	}
	const nRunning = 5
	for i := 0; i < nRunning; i++ {
		w.addRunning(fmt.Sprintf("z-running-%d", i), fmt.Sprintf("app-%d", i))
	}
	rep := w.r.ReconcileGatedContainers(t.Context())
	// The mock delivery seam shares one gate flag across containers, so after the first
	// delivery the rest observe the flag already present (gateReleased rather than
	// gateDelivered) -- both outcomes mean the container's gate is open and it was served
	// this pass, which is what matters here.
	if served := rep.Delivered + rep.Released; served != nRunning {
		t.Fatalf("exited artifacts starved the running-container scan budget: %+v", rep)
	}
}

// Fairness (finding 1c): gateWaiting bookkeeping must still converge even when a pass is
// truncated -- it does not require having examined a container THIS pass, only knowing
// whether it is still running, which the single listing call already answers for every
// container returned. A container that stops being listed (removed/stopped) must have its
// gateWaiting entry cleared even while other containers overflow the scan budget.
func TestN19Reconcile_WaitingBookkeepingConvergesEvenWhenTruncated(t *testing.T) {
	w := newGateWorld(t)
	w.cache.data = map[string]map[string]string{}
	w.addRunning("stale-waiter", "stale-waiter")
	w.r.ReconcileGatedContainers(t.Context()) // marks "stale-waiter" as waiting
	if _, waiting := w.r.gateWaiting.Load("stale-waiter"); !waiting {
		t.Fatal("setup: expected stale-waiter to be recorded as waiting")
	}

	// Now overflow the budget with other running containers, and stop tracking the
	// original one (it is no longer in the listing at all -- e.g. removed, or no longer
	// gated): its gateWaiting entry must still be cleared even though this pass truncates.
	w.mu.Lock()
	w.list = w.list[:0]
	w.mu.Unlock()
	for i := 0; i < maxGateScan+7; i++ {
		id := fmt.Sprintf("c%05d", i)
		w.addRunning(id, id)
	}
	rep := w.r.ReconcileGatedContainers(t.Context())
	if !rep.Truncated {
		t.Fatalf("expected this pass to be truncated: %+v", rep)
	}
	if _, stillWaiting := w.r.gateWaiting.Load("stale-waiter"); stillWaiting {
		t.Fatal("gateWaiting bookkeeping for a container no longer listed was skipped because the pass was truncated")
	}
}

// Failed and partial re-delivery: the gate stays closed, nothing is journalled, and the
// delivery runs under the SAME service lock a rotation takes.
func TestN19Reconcile_FailedAndPartialDeliveryNeverReleases_HoldsServiceLock_NoJournal(t *testing.T) {
	w := newGateWorld(t)
	j := &memJ{}
	w.r.Journal = j
	second := n22FileSecret()
	second.Name, second.Inject.Path = "api", "/run/secrets/dso/api"
	w.r.Config.Secrets = append(w.r.Config.Secrets, second)
	w.cache.set("vault:api", map[string]string{"token": "t"})
	w.gate.last.HostConfig.Tmpfs = map[string]string{"/run/secrets/dso": ""}
	cfg := w.gate.last.Config
	hc := w.gate.last.HostConfig
	specs := []rotation.FileSecretSpec{
		{Path: "/run/secrets/dso", Data: map[string]string{"value": "x"}, UID: 1000, GID: 1000},
		{Path: "/run/secrets/dso/api", Data: map[string]string{"token": "t"}, UID: 1000, GID: 1000},
	}
	if err := rotation.PrepareGatedCandidate(&cfg, hc, specs, 0); err != nil {
		t.Fatal(err)
	}
	w.gate.last = createReq{Config: cfg, HostConfig: hc}
	w.addRunning("app-1", "app")
	w.r.Targets.Store("app-1", &TargetContainer{ID: "app-1", Strategy: "restart", Secrets: []string{"db", "api"}})

	lockHeld := false
	n := 0
	ops := *w.r.deliveryOps
	baseInject := ops.Inject
	ops.Inject = func(c context.Context, id string, s rotation.FileSecretSpec) error {
		if _, ok := w.r.rotationLocks.Load("app-1"); ok {
			lockHeld = true
		}
		n++
		if n == 2 {
			return errSimulated // the second destination fails after the first was written
		}
		return baseInject(c, id, s)
	}
	w.r.deliveryOps = &ops

	if rep := w.r.ReconcileGatedContainers(t.Context()); rep.Failed != 1 || rep.Delivered != 0 {
		t.Fatalf("report %+v", rep)
	}
	if w.released() {
		t.Fatalf("a partial delivery released the gate: %v", w.gate.eventList())
	}
	if !lockHeld {
		t.Fatal("the re-delivery did not hold the service rotation lock")
	}
	if _, still := w.r.rotationLocks.Load("app-1"); still {
		t.Fatal("the service lock leaked")
	}
	if len(j.transitions()) != 0 {
		t.Fatalf("a re-delivery wrote journal records: %v (no competing re-delivery journal)", j.transitions())
	}
}

// (C) The re-delivery hands over a private COPY of the cached value, with the keyed
// identity of exactly that copy: a provider/cache update while it runs changes neither what
// is delivered nor the identity computed for it, and a LATER delivery to a replica carries
// its own version. A re-delivery never writes a journal record or touches the ledger
// (the watcher has no ledger handle): an applied-version claim only ever comes from a
// journalled managed operation.
func TestN19Redelivery_DeliversPrivateCopy_PerReplicaVersions_NoClaim(t *testing.T) {
	w := newGateWorld(t)
	j := &memJ{}
	w.r.Journal = j
	w.r.DeliveredVersion = func(prov, sec string, data map[string]string) string {
		return "v:" + prov + ":" + sec + ":" + data["value"]
	}
	w.addRunning("app-1", "app-1")
	w.addRunning("app-2", "app-2")

	type seen struct{ value, version string }
	var got []seen
	ops := *w.r.deliveryOps
	baseInject := ops.Inject
	ops.Inject = func(c context.Context, id string, s rotation.FileSecretSpec) error {
		got = append(got, seen{s.Data["value"], s.Version})
		// the provider moves on WHILE this delivery runs
		w.cache.set("vault:db", map[string]string{"value": "V3"})
		return baseInject(c, id, s)
	}
	w.r.deliveryOps = &ops
	w.cache.set("vault:db", map[string]string{"value": "V2"})

	w.mu.Lock()
	w.list = w.list[:1] // first pass: replica 1 only
	w.mu.Unlock()
	w.r.ReconcileGatedContainers(t.Context())
	w.mu.Lock()
	w.list = append(w.list, container.Summary{ID: "app-2", Names: []string{"/app-2"}, State: "running", Labels: map[string]string{rotation.GateLabel: "m"}})
	w.mu.Unlock()
	w.gate.mu.Lock()
	w.gate.flagPresent = false // replica 2 has an empty tmpfs
	w.gate.mu.Unlock()
	w.r.ReconcileGatedContainers(t.Context())

	if len(got) < 2 {
		t.Fatalf("deliveries %+v", got)
	}
	if got[0] != (seen{"V2", "v:vault:db:V2"}) {
		t.Fatalf("first replica: %+v: the delivered value and its identity must be the same private copy", got[0])
	}
	if got[1] != (seen{"V3", "v:vault:db:V3"}) {
		t.Fatalf("second replica: %+v: a later delivery carries its own version", got[1])
	}
	if len(j.transitions()) != 0 {
		t.Fatalf("a re-delivery claimed state in the journal: %v", j.transitions())
	}
}

// The start event path shares the same guards (the former handler had neither the service
// lock nor the operation guard).
func TestN19Redelivery_StartEvent_ConsultsGuardAndLock(t *testing.T) {
	w := newGateWorld(t)
	w.addRunning("app-1", "app")
	w.r.Guard = guardFn(func(...string) (string, bool) { return "unresolved operation", true })
	_ = w.r.handleContainerEvent(t.Context(), gatedStartEvent("app-1", true))
	if w.released() || w.gate.has("inject:/run/secrets/dso") {
		t.Fatalf("start event bypassed the operation guard: %v", w.gate.eventList())
	}
	w.r.Guard = nil
	w.r.rotationLocks.Store("app-1", &lockInfo{startTime: time.Now(), serviceName: "app-1"})
	_ = w.r.handleContainerEvent(t.Context(), gatedStartEvent("app-1", true))
	if w.released() {
		t.Fatal("start event ran beside a rotation of the same service")
	}
	w.r.rotationLocks.Delete("app-1")
	_ = w.r.handleContainerEvent(t.Context(), gatedStartEvent("app-1", true))
	if !w.released() {
		t.Fatalf("start event did not deliver once free: %v", w.gate.eventList())
	}
}

func eventsDie(id, name, exit string) events.Message {
	return events.Message{Action: "die", Actor: events.Actor{ID: id, Attributes: map[string]string{
		"name": name, "exitCode": exit, rotation.GateLabel: "marker"}}}
}

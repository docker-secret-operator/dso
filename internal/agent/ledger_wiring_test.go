package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/providers"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/api"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// S6 engine tests (docs/design/m1-ledger-reconciliation.md sections 3, 5, 6, 8,
// 11.2). Docker-free and deterministic: the REAL TriggerEngine (StartAll, first
// poll, commitObservation, dispatch goroutine, finishRotation, startup
// recovery and ledger reconcile) runs against
//   - a REAL StateTracker and REAL ledger.Store + S3 FileKeyProvider in
//     temporary directories,
//   - the in-process "file" provider (the poll reads <dir>/db.json),
//   - the fake Docker daemon of recovery_safety_test.go for startup recovery,
//   - a fake reloader (the reloadFn seam) that does what TriggerReload does
//     with respect to M0: it asks the REAL operation guard for every target,
//     holds a per-service lock (busy => deferred), and writes the strategy's
//     transitions through the REAL StateTracker.Record. A manual (REST/sync)
//     rotation is the same reloader called without a rotation id, exactly as
//     internal/server.startManualRotation calls TriggerReload.
// A "restart" of the agent is Stop + a new engine on the same directories.

const hKey = "vault:db"

type hRun struct {
	rid, version string
	manual       bool
}

type engHarness struct {
	t        *testing.T
	ldir     string // ledger directory ("" = ledger not configured)
	sdir     string
	lkdir    string
	pdir     string
	strategy string
	fileMode bool
	cli      *client.Client

	te    *TriggerEngine
	rc    *watcher.ReloaderController
	cfg   *config.Config
	store *providers.SecretStoreManager
	logs  *observer.ObservedLogs
	open  func(te *TriggerEngine) // ledger opener (default: openLedgerWith on ldir)

	mu          sync.Mutex
	services    map[string][]string // service -> live container ids
	order       []string
	nextID      int
	ran         []hRun
	engineCalls int
	completed   int
	crashAt     string
	failAt      string // the run fails (onComplete with an error) after this transition
	crashed     chan string
	hold        chan struct{}
	svcLock     sync.Mutex
}

func newEngHarness(t *testing.T, strategy string, services map[string][]string, order ...string) *engHarness {
	t.Helper()
	h := &engHarness{
		t: t, ldir: lbLedgerDir(t), sdir: t.TempDir(), lkdir: t.TempDir(), pdir: t.TempDir(),
		strategy: strategy, fileMode: strategy == "signal", services: services, order: order,
		crashed: make(chan string, 4),
	}
	if len(order) == 0 {
		for s := range services {
			h.order = append(h.order, s)
		}
	}
	h.cli = fakeDockerClient(t, newFakeDaemon())
	t.Cleanup(h.stop)
	return h
}

func fakeDockerClient(t *testing.T, d *fakeDaemon) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.WithVersion("1.41"), client.WithHost("tcp://127.0.0.1:2375"),
		client.WithHTTPClient(&http.Client{Transport: &recoveryMockTransport{reqFunc: func(req *http.Request) (*http.Response, error) {
			rec := &captureWriter{h: http.Header{}}
			d.handler().ServeHTTP(rec, req)
			return rec.response(), nil
		}}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func (h *engHarness) secret() config.SecretMapping {
	sec := config.SecretMapping{Name: lbSecret, Provider: lbProvider, Rotation: config.RotationConfigV2{Enabled: true, Strategy: h.strategy}}
	if h.fileMode {
		sec.Inject.Type = "file"
	}
	return sec
}

func (h *engHarness) setProvider(value string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.pdir, lbSecret+".json"), []byte(`{"password":"`+value+`"}`), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func data(value string) map[string]string { return map[string]string{"password": value} }

func (h *engHarness) key(service string) ledger.Key {
	return ledger.Key{Project: "shop", Service: service, Provider: lbProvider, Secret: lbSecret}
}

// live is the ledger live-set seam: the harness containers per service.
func (h *engHarness) live(providerName, secretName string) (map[ledger.Key][]ledger.LiveReplica, error) {
	if providerName != lbProvider || secretName != lbSecret {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[ledger.Key][]ledger.LiveReplica{}
	for _, s := range h.order {
		for i, id := range h.services[s] {
			out[h.key(s)] = append(out[h.key(s)], ledger.LiveReplica{Number: fmt.Sprint(i + 1), ContainerID: id,
				ContainerName: fmt.Sprintf("shop-%s-%d", s, i+1), Created: lbCreated(id), Eligible: true})
		}
	}
	return out, nil
}

func (h *engHarness) refreshTargetsLocked() {
	if h.rc == nil {
		return
	}
	h.rc.Targets.Range(func(k, _ interface{}) bool { h.rc.Targets.Delete(k); return true })
	for _, s := range h.order {
		for _, id := range h.services[s] {
			h.rc.Targets.Store(id, &watcher.TargetContainer{ID: id, Strategy: h.strategy, Secrets: []string{lbSecret}})
		}
	}
}

// manualMarker tags the context of a manual rotation so the fake reloader can
// tell it from an automatic dispatch (both now carry a rotation id, because a
// manual rotation enters through TriggerEngine.RotateSecretManually).
type manualMarker struct{}

// reload is the fake TriggerReload (see the file comment).
func (h *engHarness) reload(ctx context.Context, secretName string, onComplete func(error)) error {
	rid := rotation.RotationIDFrom(ctx)
	manual := ctx.Value(manualMarker{}) != nil
	h.mu.Lock()
	te := h.te
	if te == nil {
		h.mu.Unlock()
		return errors.New("harness stopped")
	}
	if !manual {
		h.engineCalls++
	}
	var keys, orig []string
	for _, s := range h.order {
		keys = append(keys, s)
		keys = append(keys, h.services[s]...)
		orig = append(orig, h.services[s]...)
	}
	h.mu.Unlock()
	done := func(err error) {
		onComplete(err)
		if !manual {
			h.mu.Lock()
			h.completed++
			h.mu.Unlock()
		}
	}
	if reason, blocked := te.guard.RotationBlocked(keys...); blocked {
		go done(fmt.Errorf("%w (%s): %w", watcher.ErrRotationBlocked, reason, watcher.ErrRotationDeferred))
		return nil
	}
	if !h.svcLock.TryLock() {
		go done(fmt.Errorf("%w: 1 of 1 targets not rotated", watcher.ErrRotationDeferred))
		return nil
	}
	if rid == "" {
		rid = rotation.NewRotationID()
	}
	h.mu.Lock()
	hold := h.hold
	crashAt := h.crashAt
	failAt := h.failAt
	h.mu.Unlock()
	st := te.StateTracker
	go func() {
		defer h.svcLock.Unlock()
		jctx := rotation.WithRotationID(context.Background(), rid)
		h.mu.Lock()
		var cand []string
		for range orig {
			if h.strategy == "signal" {
				break
			}
			h.nextID++
			cand = append(cand, fmt.Sprintf("c-%d", h.nextID))
		}
		h.mu.Unlock()
		if h.strategy == "signal" {
			cand = orig
		}
		for i, tr := range lbSteps(h.strategy) {
			ev := rotation.JournalEvent{RotationID: rid, Strategy: h.strategy, Transition: tr}
			if i == 0 {
				ev.ServiceID, ev.OriginalIDs = h.order[0], orig
			} else if h.strategy != "signal" {
				ev.CandidateIDs = cand
			}
			if err := st.Record(jctx, ev); err != nil {
				done(err)
				return
			}
			if i == 0 && hold != nil {
				<-hold // the strategy's Docker work is in progress
			}
			if tr == rotation.TrCandidateReady && h.strategy != "signal" {
				h.mu.Lock()
				n := 0
				for _, s := range h.order {
					for j := range h.services[s] {
						h.services[s][j] = cand[n]
						n++
					}
				}
				h.refreshTargetsLocked()
				h.mu.Unlock()
			}
			if tr == crashAt {
				h.crashed <- rid
				return // the process "dies": no onComplete, no finishRotation
			}
			if tr == failAt {
				done(errors.New("simulated strategy failure"))
				return
			}
		}
		ver := ""
		if rec := JournalRecordByID(st, rid); rec != nil {
			ver = rec.SecretVersion
		}
		h.mu.Lock()
		h.ran = append(h.ran, hRun{rid: rid, version: ver, manual: manual})
		h.mu.Unlock()
		done(nil)
	}()
	return nil
}

// manualRotate is the REST/Web UI rotation: the engine's managed entry point
// RotateSecretManually, the method internal/server's handler calls.
func (h *engHarness) manualRotate() chan error {
	res := make(chan error, 1)
	ctx := context.WithValue(context.Background(), manualMarker{}, true)
	if err := h.te.RotateSecretManually(ctx, lbSecret, "test-actor", func(err error) { res <- err }); err != nil {
		res <- err
	}
	return res
}

// start builds a new engine on the harness directories ("agent start"). With
// poll, StartAll runs (recovery, ledger reconcile, CleanupOldStates, first
// poll); without, only the startup recovery runs.
func (h *engHarness) start(poll bool) {
	h.t.Helper()
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	h.logs = logs
	h.cfg = &config.Config{
		Providers: map[string]config.ProviderConfig{lbProvider: {Type: "file", Config: map[string]string{"path": h.pdir}, Retry: config.RetryConfig{Backoff: "20ms"}}},
		Agent:     config.AgentConfig{Watch: config.WatchConfig{PollingInterval: "1h"}},
		Secrets:   []config.SecretMapping{h.secret()},
	}
	cache := NewSecretCache(time.Hour)
	h.store = providers.NewSecretStoreManager(logger)
	rc, err := watcher.NewReloaderController(logger)
	if err != nil {
		h.t.Fatal(err)
	}
	te, err := NewTriggerEngineWithDirs(cache, h.store, rc, logger, h.cfg, h.cli, h.sdir, h.lkdir)
	if err != nil {
		h.t.Fatal(err)
	}
	te.reloadFn = h.reload
	te.rotationCooldown = time.Nanosecond
	h.mu.Lock()
	h.te, h.rc = te, rc
	h.engineCalls, h.completed = 0, 0
	h.refreshTargetsLocked()
	h.mu.Unlock()
	switch {
	case h.open != nil:
		h.open(te)
	case h.ldir != "":
		kp, err := ledger.NewFileKeyProvider(h.ldir)
		if err != nil {
			h.t.Fatal(err)
		}
		te.openLedgerWith(h.ldir, kp, ledger.Config{Keys: kp, After: lbAfter})
	}
	if te.ledger.enabled() {
		te.ledger.liveFn = h.live
	}
	te.BeginStartup()
	if poll {
		if err := te.StartAll(); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	te.performAutomaticRecovery()
}

func (h *engHarness) stop() {
	if h.te == nil {
		return
	}
	h.waitIdle()
	h.mu.Lock()
	te := h.te
	h.te = nil
	h.mu.Unlock()
	te.Stop()
	h.store.Shutdown()
}

// waitIdle waits until every dispatch the engine decided (one "Secret
// rotated" log each) reached the reloader or was abandoned before it (lock
// timeout, journal failure), and every reloader call finished.
func (h *engHarness) waitIdle() {
	h.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		decided := h.logs.FilterMessageSnippet("Secret rotated:").Len()
		aborted := h.logs.FilterMessageSnippet("Failed to acquire rotation lock").Len() + h.logs.FilterMessage("Failed to record rotation state").Len()
		h.mu.Lock()
		idle := h.completed == h.engineCalls && h.engineCalls+aborted >= decided
		h.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatal("engine dispatches did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitCalls waits until the engine handed n dispatches to the reloader and
// all of them finished.
func (h *engHarness) waitCalls(n int) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		h.mu.Lock()
		ok := h.engineCalls >= n && h.completed == h.engineCalls
		h.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("expected %d finished engine dispatches", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (h *engHarness) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.engineCalls
}

func (h *engHarness) runs() []hRun {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hRun(nil), h.ran...)
}

// waitFirstDecision waits for the first poll's decision log and returns the
// decision ("dispatched" for a dispatch).
func (h *engHarness) waitFirstDecision() RotationDecision {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e := h.logs.FilterMessageSnippet("Secret rotated:").All(); len(e) > 0 {
			return RotationDispatched
		}
		if e := h.logs.FilterMessage("Secret observed; no rotation dispatched").All(); len(e) > 0 {
			return RotationDecision(e[0].ContextMap()["decision"].(string))
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("the first poll made no decision")
	return ""
}

func (h *engHarness) ver(value string) ledger.Version {
	h.t.Helper()
	v, err := h.te.ledger.v.NewVersion(lbProvider, lbSecret, data(value))
	if err != nil {
		h.t.Fatal(err)
	}
	return v
}

func (h *engHarness) entry(service string) ledger.Entry {
	h.t.Helper()
	e, ok := h.te.ledger.v.Get(h.key(service))
	if !ok {
		h.t.Fatalf("no ledger entry for %s", service)
	}
	return e
}

func (h *engHarness) decide(value string) RotationDecision {
	return h.te.ExecuteRotationDecision(lbProvider, lbSecret, data(value), h.secret())
}

// assertNoDispatchSettled checks that nothing was handed to the reloader and
// no dispatch was decided by this engine.
func (h *engHarness) assertNoDispatch(what string) {
	h.t.Helper()
	time.Sleep(50 * time.Millisecond)
	if n := h.calls(); n != 0 {
		h.t.Fatalf("%s: %d dispatch(es), want 0", what, n)
	}
	if _, ok := h.te.lastRotations.Load(hKey); ok {
		h.t.Fatalf("%s: a dispatch was decided", what)
	}
}

// ============================================================ O-5 / scenarios 1, 2

func TestLedgerEngine_FirstStart_ExistingWorkloads_ZeroRotations(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2", "a3"}})
	h.setProvider("one")
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationAdopted {
		t.Fatalf("first poll decision %s, want adopted", d)
	}
	h.assertNoDispatch("first start")
	e := h.entry("app")
	if e.State != ledger.EntryAdopted || e.Applied != nil || len(e.Replicas) != 3 || e.DesiredSeq != 1 {
		t.Fatalf("entry %+v", e)
	}
	for _, r := range e.Replicas {
		if r.Adoption != ledger.AdoptionUnknown || r.Delivery != ledger.DeliveryNone {
			t.Fatalf("replica %+v", r)
		}
	}
	if _, ok := h.te.secretHashes.Load(hKey); !ok {
		t.Fatal("the baseline hash was not stored")
	}
	if _, ok := h.te.Cache.Get(hKey); !ok {
		t.Fatal("the cache was not populated (PE-03)")
	}
	if recs := h.te.StateTracker.snapshotRecords(); len(recs) != 0 {
		t.Fatalf("journal written: %+v", recs)
	}
}

// ============================================================ restart unchanged / changed

func TestLedgerEngine_Restart_UnchangedDesired_ZeroRotations(t *testing.T) {
	t.Run("adopted_unknown", func(t *testing.T) {
		h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
		h.setProvider("one")
		h.start(true)
		h.waitFirstDecision()
		h.stop()
		h.start(true)
		if d := h.waitFirstDecision(); d != RotationUnchanged {
			t.Fatalf("decision %s, want unchanged", d)
		}
		h.assertNoDispatch("restart, adopted unknown")
	})
	t.Run("applied_equals_desired", func(t *testing.T) {
		h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
		h.setProvider("one")
		h.start(true)
		h.waitFirstDecision()
		h.setProvider("two")
		if d := h.decide("two"); d != RotationDispatched {
			t.Fatalf("a change after the baseline: %s", d)
		}
		h.waitCalls(1)
		if e := h.entry("app"); !appliedIs(e, h.ver("two")) || e.State != ledger.EntryIdle {
			t.Fatalf("entry %+v", e)
		}
		h.stop()
		h.start(true)
		if d := h.waitFirstDecision(); d != RotationUnchanged {
			t.Fatalf("decision %s, want unchanged", d)
		}
		h.assertNoDispatch("restart, applied == desired")
		if n := len(h.runs()); n != 1 {
			t.Fatalf("%d rotations in total, want 1", n)
		}
	})
}

func TestLedgerEngine_Restart_ChangedDesired_ExactlyOneRotationPerKey(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2"}, "worker": {"w1"}}, "app", "worker")
	h.setProvider("one")
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationAdopted {
		t.Fatalf("decision %s", d)
	}
	h.stop()
	h.setProvider("three") // changed while the agent was down
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationDispatched {
		t.Fatalf("decision %s, want dispatched", d)
	}
	h.waitCalls(1)
	runs := h.runs()
	if len(runs) != 1 || runs[0].manual {
		t.Fatalf("runs %+v, want exactly one engine rotation", runs)
	}
	three := h.ver("three")
	if runs[0].version != FormatJournalSecretVersion(three) {
		t.Fatalf("O-2: the journal names %q", runs[0].version)
	}
	for _, s := range []string{"app", "worker"} {
		e := h.entry(s)
		if !appliedIs(e, three) || e.State != ledger.EntryIdle {
			t.Fatalf("%s: %+v", s, e)
		}
		for _, r := range e.Replicas {
			if r.RotationID != runs[0].rid || r.Delivery != ledger.DeliveryVerified || r.Adoption != ledger.AdoptionVerified {
				t.Fatalf("%s replica %+v", s, r)
			}
		}
	}
	// The next poll of the same value: nothing more.
	if d := h.decide("three"); d != RotationUnchanged {
		t.Fatalf("second poll %s", d)
	}
	h.waitIdle()
	if n := h.calls(); n != 1 {
		t.Fatalf("%d dispatches, want 1", n)
	}
}

// ============================================================ ledger disabled

func TestLedgerEngine_LedgerDisabled_NoMassRotation(t *testing.T) {
	disabled := func(t *testing.T, h *engHarness, why string) {
		t.Helper()
		if h.te.ledger == nil || h.te.ledger.mode != ledgerDisabled {
			t.Fatalf("%s: ledger not disabled: %+v", why, h.te.ledger)
		}
		if h.logs.FilterMessageSnippet("ledger DISABLED").Len() == 0 {
			t.Fatalf("%s: no operator-visible warning", why)
		}
		if d := h.waitFirstDecision(); d != RotationLedgerDegraded {
			t.Fatalf("%s: decision %s, want ledger_degraded", why, d)
		}
		h.assertNoDispatch(why)
	}

	t.Run("unwritable_dir", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write a 0500 directory")
		}
		h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2"}})
		parent := filepath.Join(h.ldir, "ro")
		if err := os.Mkdir(parent, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		dir := filepath.Join(parent, "ledger")
		h.open = func(te *TriggerEngine) { te.openLedger(dir) } // the production opener
		h.setProvider("one")
		h.start(true)
		disabled(t, h, "unwritable dir")
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("something was created: %v", err)
		}
		// Today's behaviour for a later change in the same process (advisory).
		if d := h.decide("two"); d != RotationDispatched {
			t.Fatalf("a change after the in-memory baseline: %s", d)
		}
		h.waitCalls(1)
	})

	t.Run("lost_key", func(t *testing.T) {
		h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
		h.setProvider("one")
		h.start(true)
		h.waitFirstDecision()
		h.stop()
		if err := os.Remove(filepath.Join(h.ldir, "ledger.key")); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(h.ldir, "ledger.json"))
		if err != nil {
			t.Fatal(err)
		}
		h.setProvider("two") // even a change while down is not rotated without a ledger
		h.start(true)
		disabled(t, h, "lost key")
		after, _ := os.ReadFile(filepath.Join(h.ldir, "ledger.json"))
		if !bytes.Equal(before, after) {
			t.Fatal("the ledger was modified while its key is lost")
		}
		if _, err := os.Stat(filepath.Join(h.ldir, "ledger.key")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a lost key was regenerated")
		}
	})

	t.Run("run_dso_refused", func(t *testing.T) {
		h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
		h.open = func(te *TriggerEngine) { te.openLedger("/run/dso/ledger") }
		h.setProvider("one")
		h.start(true)
		disabled(t, h, "/run/dso")
	})
}

// ============================================================ crash window W6

func TestLedgerEngine_CrashW6_WithO2_ZeroReRotations(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	two := h.ver("two")
	h.mu.Lock()
	h.crashAt = rotation.TrCompleted // the journal committed; the ledger update never ran
	h.mu.Unlock()
	h.setProvider("two")
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	var rid string
	select {
	case rid = <-h.crashed:
	case <-time.After(10 * time.Second):
		t.Fatal("no crash")
	}
	rec := JournalRecordByID(h.te.StateTracker, rid)
	if rec == nil || rec.Status != "completed" || rec.SecretVersion != FormatJournalSecretVersion(two) {
		t.Fatalf("journal %+v", rec)
	}
	e := h.entry("app")
	if e.Applied != nil || e.Replicas[0].Delivery != ledger.DeliveryRequested || e.Replicas[0].RotationID != rid {
		t.Fatalf("pre-crash ledger %+v", e)
	}
	h.mu.Lock()
	h.crashAt = ""
	h.completed = h.engineCalls // the dead dispatch never reports
	h.mu.Unlock()
	h.stop()

	h.start(true) // recovery, reconcile (W6 from the journal-carried version), first poll
	if d := h.waitFirstDecision(); d != RotationUnchanged {
		t.Fatalf("first poll after the crash: %s, want unchanged", d)
	}
	h.assertNoDispatch("after W6")
	e = h.entry("app")
	if !appliedIs(e, two) || e.State != ledger.EntryIdle {
		t.Fatalf("reconciled entry %+v", e)
	}
	if r := e.Replicas[0]; r.Delivery != ledger.DeliveryVerified || r.Adoption != ledger.AdoptionVerified || r.RotationID != rid {
		t.Fatalf("replica %+v", r)
	}
	if h.logs.FilterMessage("Applied-version ledger reconciled from the journal").Len() == 0 {
		t.Fatal("no reconcile change reported")
	}
}

// ============================================================ signal

func TestLedgerEngine_SignalReload_AdoptionUnverified(t *testing.T) {
	h := newEngHarness(t, "signal", map[string][]string{"app": {"s1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(1)
	e := h.entry("app")
	if !appliedIs(e, h.ver("two")) || e.State != ledger.EntryIdle {
		t.Fatalf("entry %+v", e)
	}
	r := e.Replicas[0]
	if r.Delivery != ledger.DeliveryVerified || r.Adoption != ledger.AdoptionUnverified || r.Reason != string(ledger.WhyAdoptionUnverifiedSignal) {
		t.Fatalf("replica %+v", r)
	}
	// No signal loop: the same value again rotates nothing, also after a restart.
	if d := h.decide("two"); d != RotationUnchanged {
		t.Fatalf("again: %s", d)
	}
	h.setProvider("two")
	h.stop()
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationUnchanged {
		t.Fatalf("after restart: %s", d)
	}
	h.assertNoDispatch("signal after restart")
}

// ============================================================ BQ-3: IPC Rotated

func TestLedgerEngine_IPCRotated_NoOverReport(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false) // no poller: the IPC reconcile is the first observation of the process
	srv := &AgentServer{Cache: h.te.Cache, Store: h.store, Logger: zap.NewNop(), Config: h.cfg, Trigger: h.te}
	reconcile := func() api.ReconcileResponse {
		t.Helper()
		var resp api.ReconcileResponse
		if err := srv.TriggerReconciliation(&api.ReconcileRequest{Secret: lbSecret}, &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Results) != 1 || resp.Results[0].Error != "" {
			t.Fatalf("results %+v", resp.Results)
		}
		return resp
	}
	// The old code reported Rotated=true here (empty in-memory hash) although the
	// value was only adopted.
	if resp := reconcile(); resp.Results[0].Rotated || resp.SecretsRotated != 0 {
		t.Fatalf("first observation reported as rotated: %+v", resp)
	}
	h.assertNoDispatch("IPC first observation")
	if resp := reconcile(); resp.Results[0].Rotated {
		t.Fatalf("unchanged reported as rotated: %+v", resp)
	}
	h.setProvider("two")
	if resp := reconcile(); !resp.Results[0].Rotated || resp.SecretsRotated != 1 {
		t.Fatalf("a real change not reported: %+v", resp)
	}
	h.waitCalls(1)
	// Restart, unchanged: not rotated.
	h.stop()
	h.start(false)
	srv.Trigger, srv.Cache, srv.Store = h.te, h.te.Cache, h.store
	if resp := reconcile(); resp.Results[0].Rotated {
		t.Fatalf("restart with an unchanged value reported as rotated: %+v", resp)
	}
	if n := h.calls(); n != 0 {
		t.Fatalf("%d dispatches after restart", n)
	}
}

// ============================================================ scenario 5

func TestLedgerEngine_Scenario05_ProviderUnavailableAtStartup(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.start(true) // the provider has no db.json yet: every fetch fails
	deadline := time.Now().Add(10 * time.Second)
	for h.logs.FilterMessage("Secret fetch failed, will retry").Len() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the poller did not retry")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, ok := h.te.secretHashes.Load(hKey); ok {
		t.Fatal("an outage stored a hash")
	}
	if snap := h.te.ledger.v.Snapshot(); len(snap.Ledger.Entries) != 0 {
		t.Fatalf("an outage recorded ledger entries: %+v", snap.Ledger.Entries)
	}
	gen := h.te.ledger.store.Health().Generation
	h.assertNoDispatch("outage")
	h.setProvider("one") // the provider recovers
	if d := h.waitFirstDecision(); d != RotationAdopted {
		t.Fatalf("decision after recovery %s, want adopted", d)
	}
	h.assertNoDispatch("after recovery")
	if g := h.te.ledger.store.Health().Generation; g != gen+1 {
		t.Fatalf("generation %d -> %d, want exactly one write (the adoption)", gen, g)
	}
	h.stop()
	kp, err := ledger.NewFileKeyProvider(h.ldir)
	if err != nil {
		t.Fatal(err)
	}
	s, res, err := ledger.Open(h.ldir, ledger.Config{Keys: kp, After: lbAfter})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if res.Mode != ledger.ModeDurable || res.Reason != ledger.ReasonNone || res.Preserved != "" {
		t.Fatalf("the ledger did not survive intact: %+v", res)
	}
	if _, ok := s.Get(h.key("app")); !ok {
		t.Fatal("the adoption was not persisted")
	}
}

// ============================================================ scenario 8

func TestLedgerEngine_Scenario08_ConcurrentProviderUpdates(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("base")
	h.start(false)
	if d := h.decide("base"); d != RotationAdopted {
		t.Fatalf("baseline %s", d)
	}
	lastSeq := h.entry("app").DesiredSeq
	for round := 0; round < 25; round++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func(v string) {
				defer wg.Done()
				<-start
				h.decide(v)
			}(fmt.Sprintf("r%d-v%d", round, i))
		}
		close(start)
		wg.Wait()
		cached, ok := h.te.Cache.Get(hKey)
		hv, hok := h.te.secretHashes.Load(hKey)
		e := h.entry("app")
		if !ok {
			t.Fatalf("round %d: no cache", round)
		}
		// The ledger's desired is the value the per-key mutex let win last: the
		// cached (dispatched or baselined) value. No lost update.
		v, err := h.te.ledger.v.NewVersion(lbProvider, lbSecret, cached)
		if err != nil {
			t.Fatal(err)
		}
		if e.Desired == nil || ledger.Compare(*e.Desired, v) != ledger.Same {
			t.Fatalf("round %d: ledger desired is not the last observed value", round)
		}
		if hok && hv.(string) != ComputeHash(cached) {
			t.Fatalf("round %d: hash and cache disagree", round)
		}
		if e.DesiredSeq < lastSeq {
			t.Fatalf("round %d: desired_seq went back %d -> %d", round, lastSeq, e.DesiredSeq)
		}
		lastSeq = e.DesiredSeq
	}
	// In process the ledger may lag behind (stale-low: overlapping dispatches of
	// one secret share a journal key, so a correlation can be refused, finding
	// F-S6-1) but never over-claims: whenever applied is not the desired value
	// the ledger still wants exactly that rotation.
	h.waitIdle()
	final, _ := h.te.Cache.Get(hKey)
	fv := h.ver(final["password"])
	e := h.entry("app")
	if e.Desired == nil || ledger.Compare(*e.Desired, fv) != ledger.Same {
		t.Fatal("final desired is not the last observed value")
	}
	if !appliedIs(e, fv) && !h.te.ledger.v.NeedsRotation(h.key("app")).Rotate() {
		t.Fatalf("the ledger lags and does not want the rotation: %+v", e)
	}
	// After a restart the first poll of the final value converges with at most
	// one rotation, and a second poll rotates nothing.
	h.setProvider(final["password"])
	h.stop()
	h.start(false)
	h.decide(final["password"])
	h.waitIdle()
	if n := h.calls(); n > 1 {
		t.Fatalf("%d rotations to converge, want at most 1", n)
	}
	if d := h.decide(final["password"]); d != RotationUnchanged {
		t.Fatalf("second poll %s", d)
	}
	e = h.entry("app")
	if !appliedIs(e, fv) || e.State != ledger.EntryIdle {
		for _, r := range h.te.StateTracker.snapshotRecords() {
			t.Logf("journal %s status=%s seq=%d tr=%s orig=%v svc=%s", r.RotationID, r.Status, r.Seq, r.Transition, r.OriginalIDs(), r.ServiceID)
		}
		for _, l := range h.logs.FilterLevelExact(zapcore.WarnLevel).All() {
			t.Logf("WARN %s %v", l.Message, l.ContextMap())
		}
		t.Fatalf("not converged: %+v", e)
	}
}

// ============================================================ scenario 9 and the guard

func TestLedgerEngine_Scenario09_ConcurrentManualAndAutomatic(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	hold := make(chan struct{})
	h.mu.Lock()
	h.hold = hold
	h.mu.Unlock()
	manual := h.manualRotate() // REST/sync: TriggerReload without a rotation id
	time.Sleep(20 * time.Millisecond)
	h.mu.Lock()
	h.hold = nil
	h.mu.Unlock()
	// The poll sees a change while the manual rotation holds the service.
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(1) // deferred: the service is busy
	if n := len(h.runs()); n != 0 {
		t.Fatalf("%d rotations ran while the manual one holds the service", n)
	}
	// One journal operation: the manual one (the deferred dispatch's record was
	// removed by finishRotation, as before S6).
	recs := h.te.StateTracker.snapshotRecords()
	if len(recs) != 1 || recs[0].Seq == 0 {
		t.Fatalf("journal %+v, want only the manual operation", recs)
	}
	manualRID := recs[0].RotationID
	// A manual rotation enters through the engine (RotateSecretManually), so the
	// ledger holds ITS delivery request; the deferred automatic dispatch neither
	// took the entry over nor left a claim of its own.
	e := h.entry("app")
	if e.Applied != nil || e.Replicas[0].Delivery != ledger.DeliveryRequested || e.Replicas[0].RotationID != manualRID {
		t.Fatalf("the manual operation does not hold the request, or the deferred dispatch took it over: %+v", e)
	}
	close(hold)
	if err := <-manual; err != nil {
		t.Fatal(err)
	}
	// The manual rotation delivered the cached value ("one"), not the newer
	// desired one: applied is the DELIVERED version, desired stays the newest, so
	// the next poll rotates exactly once.
	e = h.entry("app")
	if !appliedIs(e, h.ver("one")) || e.State != ledger.EntryPending || e.Replicas[0].RotationID != manualRID ||
		e.Replicas[0].Delivery != ledger.DeliveryVerified {
		t.Fatalf("the manual rotation was not recorded as the delivered version: %+v", e)
	}
	// The next poll retries (hash rolled back) and the ledger cites that one.
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("retry decision %s", d)
	}
	h.waitCalls(2)
	runs := h.runs()
	if len(runs) != 2 || !runs[0].manual || runs[1].manual {
		t.Fatalf("runs %+v", runs)
	}
	e = h.entry("app")
	if !appliedIs(e, h.ver("two")) || e.Replicas[0].RotationID != runs[1].rid {
		t.Fatalf("entry %+v", e)
	}
}

func TestLedgerEngine_GuardAlwaysAsked_LedgerNeverBypasses(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	// An unresolved operation for the service (an operator must act).
	ctx := rotation.WithRotationID(context.Background(), "rot-crit")
	if err := h.te.StateTracker.Record(ctx, rotation.JournalEvent{RotationID: "rot-crit", ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal}); err != nil {
		t.Fatal(err)
	}
	if err := h.te.StateTracker.MarkCriticalRecord(JournalRecordByID(h.te.StateTracker, "rot-crit"), "test"); err != nil {
		t.Fatal(err)
	}
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("the ledger wants it: %s", d)
	}
	h.waitCalls(1)
	if n := len(h.runs()); n != 0 {
		t.Fatal("the guard was bypassed")
	}
	if e := h.entry("app"); e.Applied != nil || e.State != ledger.EntryPending {
		t.Fatalf("entry %+v", e)
	}
	_ = h.te.StateTracker.MarkRecoveredRecord(JournalRecordByID(h.te.StateTracker, "rot-crit"))
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("after the operator: %s", d)
	}
	h.waitCalls(2)
	if n := len(h.runs()); n != 1 {
		t.Fatalf("%d rotations", n)
	}
}

// ============================================================ W14 / O-7

func TestLedgerEngine_JournalStartFails_NoDispatchNoCitation(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a 0500 directory")
	}
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	if err := os.Chmod(h.sdir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.sdir, 0o700) })
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	waitHashGone(t, h.te, hKey)
	time.Sleep(50 * time.Millisecond)
	if n := h.calls(); n != 0 {
		t.Fatalf("%d reloads without a durable journal record", n)
	}
	e := h.entry("app")
	for _, r := range e.Replicas {
		if r.RotationID != "" || r.Delivery != ledger.DeliveryNone {
			t.Fatalf("the ledger cites an unjournalled operation: %+v", r)
		}
	}
	if len(h.te.StateTracker.snapshotRecords()) != 0 {
		t.Fatal("a non-durable record stayed in memory (F-B3)")
	}
	if err := os.Chmod(h.sdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("retry %s", d)
	}
	h.waitCalls(1)
}

// ============================================================ wiring units

func TestLedgerWiring_DefaultDirPinned(t *testing.T) {
	if defaultLedgerDir != "/var/lib/dso/ledger" {
		t.Fatalf("default ledger dir %q", defaultLedgerDir)
	}
	if strings.HasPrefix(defaultLedgerDir, "/run/") || strings.HasPrefix(defaultLedgerDir, "/var/run/") {
		t.Fatal("the ledger must never live on tmpfs")
	}
}

// Engines built without NewTriggerEngine keep the legacy first-observation
// dispatch (unit tests, NewTriggerEngineWithDirs).
func TestLedgerWiring_NotConfigured_LegacyDecision(t *testing.T) {
	te := newOutcomeTestEngine(t)
	if te.ledger != nil {
		t.Fatal("configured")
	}
	d := te.ExecuteRotationDecision("aws", "x", syntheticV2, config.SecretMapping{Name: "x", Rotation: config.RotationConfigV2{Strategy: "none"}})
	if d != RotationDispatched {
		t.Fatalf("legacy first observation: %s", d)
	}
	if d := te.ExecuteRotationDecision("aws", "x", syntheticV2, config.SecretMapping{Name: "x", Rotation: config.RotationConfigV2{Strategy: "none"}}); d != RotationUnchanged {
		t.Fatalf("legacy repeat: %s", d)
	}
}

// The production live set: a read-only inspection of the reloader targets,
// keyed by Compose project/service (or the container name), artifacts skipped.
func TestLedgerWiring_LiveSetFromDockerInspect(t *testing.T) {
	inspect := map[string]container.InspectResponse{}
	add := func(id, name, created string, labels map[string]string) {
		inspect[id] = container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: id, Name: "/" + name, Created: created},
			Config:            &container.Config{Labels: labels},
		}
	}
	compose := func(svc, n string) map[string]string {
		return map[string]string{"com.docker.compose.project": "shop", "com.docker.compose.service": svc, "com.docker.compose.container-number": n}
	}
	add("id-a2", "shop-app-2", "2026-01-01T00:00:02Z", compose("app", "2"))
	add("id-a1", "shop-app-1", "2026-01-01T00:00:01Z", compose("app", "1"))
	add("id-p", "plain", "2026-01-01T00:00:03Z", nil)
	add("id-x", "app_dso_new_1700000000", "2026-01-01T00:00:04Z", compose("app", "3"))
	var calls []string
	var mu sync.Mutex
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls = append(calls, req.Method+" "+req.URL.Path)
		mu.Unlock()
		for id, ins := range inspect {
			if strings.HasSuffix(req.URL.Path, "/containers/"+id+"/json") && req.Method == http.MethodGet {
				b, _ := json.Marshal(ins)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: stringBody(string(b))}, nil
			}
		}
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: stringBody(`{"message":"no such container"}`)}, nil
	})
	rc, err := watcher.NewReloaderController(zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for id := range inspect {
		rc.Targets.Store(id, &watcher.TargetContainer{ID: id, Strategy: "restart", Secrets: []string{"db"}})
	}
	rc.Targets.Store("id-other", &watcher.TargetContainer{ID: "id-other", Secrets: []string{"other"}})
	te := &TriggerEngine{Reloader: rc, DockerClient: cli, Logger: zap.NewNop()}
	live, err := te.ledgerLiveSet(context.Background(), "vault", "db")
	if err != nil {
		t.Fatal(err)
	}
	app := live[ledger.Key{Project: "shop", Service: "app", Provider: "vault", Secret: "db"}]
	plain := live[ledger.Key{Service: "plain", Provider: "vault", Secret: "db"}]
	if len(live) != 2 || len(app) != 2 || len(plain) != 1 {
		t.Fatalf("live %+v", live)
	}
	if app[0].Number != "1" || app[0].ContainerID != "id-a1" || app[1].Number != "2" || app[0].Created != "2026-01-01T00:00:01Z" || !app[0].Eligible {
		t.Fatalf("app replicas %+v", app)
	}
	if plain[0].Number != "" || plain[0].ContainerName != "plain" {
		t.Fatalf("plain %+v", plain)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "GET ") {
			t.Fatalf("the live set mutated Docker: %s", c)
		}
	}
	// An inspection failure is reported, never guessed around.
	rc.Targets.Store("id-gone", &watcher.TargetContainer{ID: "id-gone", Secrets: []string{"db"}})
	if _, err := te.ledgerLiveSet(context.Background(), "vault", "db"); err == nil {
		t.Fatal("a failed inspection was ignored")
	}
}

// ledger_wiring.go is the production caller of the bridge.
func TestLedgerWiring_UsesTheBridge(t *testing.T) {
	src, err := os.ReadFile("ledger_wiring.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CorrelateAfterJournalStart(", "RecordRotationOutcomeFor(", "ReconcileLedgerFromTracker(", "FormatJournalSecretVersion("} {
		if !bytes.Contains(src, []byte(name)) {
			t.Errorf("ledger_wiring.go does not call %s", name)
		}
	}
}

// Ledger-disabled diagnostics (M1 acceptance prompt section 8-9): a ledger that
// opens disabled is never silent. These tests pin the classification (unsafe
// permissions / key lost / corrupt key / newer schema / lock held / not
// configured), the exact warning content, the status exposure and that the
// remediation never advises a recursive chmod/chown. The engine tests use the
// production opener (openLedger) on temporary directories.

type fakeFI struct {
	mode os.FileMode
	uid  uint32
	gid  uint32
}

func (f fakeFI) Name() string       { return "x" }
func (f fakeFI) Size() int64        { return 0 }
func (f fakeFI) Mode() os.FileMode  { return f.mode | os.ModeDir }
func (f fakeFI) ModTime() time.Time { return time.Time{} }
func (f fakeFI) IsDir() bool        { return true }
func (f fakeFI) Sys() any           { return &struct{ Uid, Gid uint32 }{f.uid, f.gid} }

func TestLedgerDiag_UnsafeStateDirectory_NamesPathActualExpectedAndSupportedFix(t *testing.T) {
	// The upgraded installation: /var/lib/dso root:dso 0770, ledger dir 0700 root.
	infos := map[string]fakeFI{
		"/var":                {0o755, 0, 0},
		"/var/lib":            {0o755, 0, 0},
		"/var/lib/dso":        {0o770, 0, 1001},
		"/var/lib/dso/ledger": {0o700, 0, 0},
	}
	in := ledgerInspector{
		stat: func(p string) (os.FileInfo, error) {
			if fi, ok := infos[p]; ok {
				return fi, nil
			}
			return nil, os.ErrNotExist
		},
		euid: func() int { return 0 },
	}
	d := classifyLedgerOpen("/var/lib/dso/ledger", ledger.OpenResult{Reason: ledger.ReasonUnsafeDir, Detail: "securefs: directory chain"}, nil, in)
	if d.code != LedgerCodeUnsafePermissions || d.path != "/var/lib/dso" {
		t.Fatalf("diagnosis %+v", d)
	}
	if !strings.Contains(d.actual, "uid 0 gid 1001, mode 0770") || d.expected != "root:dso, mode 0750" {
		t.Fatalf("actual %q expected %q", d.actual, d.expected)
	}
	for _, want := range []string{"sudo docker dso bootstrap agent", "sudo chown root:dso /var/lib/dso", "sudo chmod 0750 /var/lib/dso", "no -R"} {
		if !strings.Contains(d.remediation, want) {
			t.Errorf("remediation lacks %q: %s", want, d.remediation)
		}
	}
}

func TestLedgerDiag_RemediationNeverRecursive(t *testing.T) {
	in := defaultLedgerInspector()
	all := []ledgerDiagnosis{
		diagKeyLost("/d", "x"), diagCorrupt("/d", "x"), diagUnsupportedOS("x"), diagUnsafe("/d", "x", in),
		{remediation: unsafeRemediation(ledgerStateDir)}, {remediation: unsafeRemediation("/var/lib/dso/ledger")},
		{remediation: unsafeRemediation("/var/lib/dso/ledger/ledger.key")},
		classifyLedgerOpen("/d", ledger.OpenResult{Reason: ledger.ReasonLocked}, nil, in),
		classifyLedgerOpen("/d", ledger.OpenResult{Reason: ledger.ReasonNewerSchema}, nil, in),
		classifyLedgerError("/run/dso/x", fmt.Errorf("wrapped: %w", &ledger.KeyError{Kind: ledger.ErrKeyDirectory, Reason: "x"}), in),
	}
	for _, d := range all {
		for _, bad := range []string{" -R", "-R ", "--recursive", "chmod -r", "chown -r"} {
			if strings.Contains(d.remediation, bad) && !strings.Contains(d.remediation, "no -R") && !strings.Contains(d.remediation, "never -R") {
				t.Errorf("remediation advises recursion (%q): %s", bad, d.remediation)
			}
		}
		if d.remediation == "" {
			t.Errorf("empty remediation for %+v", d)
		}
	}
}

func TestLedgerDiag_Classification(t *testing.T) {
	in := defaultLedgerInspector()
	for name, tc := range map[string]struct {
		res  ledger.OpenResult
		kerr error
		want string
	}{
		"unsafe dir":      {ledger.OpenResult{Reason: ledger.ReasonUnsafeDir}, nil, LedgerCodeUnsafePermissions},
		"key unsafe":      {ledger.OpenResult{Reason: ledger.ReasonKeyUnsafe}, &ledger.KeyError{Kind: ledger.ErrKeyUnsafe, Reason: "mode 0644"}, LedgerCodeUnsafePermissions},
		"key lost":        {ledger.OpenResult{Reason: ledger.ReasonKeyUnsafe}, &ledger.KeyError{Kind: ledger.ErrKeyLost, Reason: "missing"}, LedgerCodeKeyLost},
		"key malformed":   {ledger.OpenResult{Reason: ledger.ReasonKeyMalformed}, &ledger.KeyError{Kind: ledger.ErrKeyCorrupt, Reason: "size"}, LedgerCodeCorruptState},
		"newer schema":    {ledger.OpenResult{Reason: ledger.ReasonNewerSchema}, nil, LedgerCodeUnsupportedSchema},
		"locked":          {ledger.OpenResult{Reason: ledger.ReasonLocked}, nil, LedgerCodeLockHeld},
		"unsupported":     {ledger.OpenResult{Reason: ledger.ReasonUnsupportedPlatform}, nil, LedgerCodeUnsupportedOS},
		"ledger corrupt":  {ledger.OpenResult{Reason: ledger.ReasonCorrupt}, nil, LedgerCodeCorruptState},
		"too large":       {ledger.OpenResult{Reason: ledger.ReasonTooLarge}, nil, LedgerCodeCorruptState},
		"unreadable":      {ledger.OpenResult{Reason: ledger.ReasonLedgerUnreadable}, nil, LedgerCodeCorruptState},
		"preserve failed": {ledger.OpenResult{Reason: ledger.ReasonPreserveFailed}, nil, LedgerCodeCorruptState},
		"keygen failed":   {ledger.OpenResult{Reason: ledger.ReasonKeyGenerateFailed}, nil, LedgerCodeUnavailable},
	} {
		if got := classifyLedgerOpen("/d", tc.res, tc.kerr, in).code; got != tc.want {
			t.Errorf("%s: code %q, want %q", name, got, tc.want)
		}
	}
}

// disabledEngine runs the engine with the production opener on dir and returns
// the harness (first poll not awaited).
func disabledEngine(t *testing.T, h *engHarness, dir string) {
	t.Helper()
	h.open = func(te *TriggerEngine) { te.openLedger(dir) }
	h.setProvider("one")
	h.start(true)
	if h.te.ledger == nil || h.te.ledger.mode != ledgerDisabled {
		t.Fatalf("ledger not disabled: %+v", h.te.ledger)
	}
	h.waitFirstDecision() // let the first poll finish before the test tears the directories down
}

func disabledWarning(t *testing.T, h *engHarness) map[string]any {
	t.Helper()
	logs := h.logs.FilterMessageSnippet("ledger DISABLED").All()
	if len(logs) != 1 {
		t.Fatalf("the DISABLED warning must be emitted exactly once at engine start, got %d", len(logs))
	}
	m := logs[0].ContextMap()
	msg := logs[0].Message
	for _, want := range []string{"durable version tracking is unavailable", "adopted without rotating", "no mass rotation", "may go undetected"} {
		if !strings.Contains(msg, want) {
			t.Errorf("warning lacks %q: %s", want, msg)
		}
	}
	for _, k := range []string{"code", "remediation", "degraded_behaviour", "reason"} {
		if m[k] == nil || m[k] == "" {
			t.Errorf("warning field %q missing: %v", k, m)
		}
	}
	return m
}

func TestLedgerDiag_Engine_UnsafeParent0770_WarnsOnceAndExposesStatus(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	parent := filepath.Join(h.ldir, "lib")
	dir := filepath.Join(parent, "ledger")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o770); err != nil { // the pre-upgrade /var/lib/dso
		t.Fatal(err)
	}
	disabledEngine(t, h, dir)
	m := disabledWarning(t, h)
	if m["code"] != LedgerCodeUnsafePermissions || m["path"] != parent {
		t.Fatalf("warning fields %v", m)
	}
	if a, _ := m["actual"].(string); !strings.Contains(a, "mode 0770") {
		t.Errorf("actual %q", a)
	}
	if e, _ := m["expected"].(string); e == "" {
		t.Errorf("expected missing: %v", m)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("files were written under an unsafe parent: %v", ents)
	}
	st := h.te.LedgerStatus()
	if st.State != api.LedgerDisabled || st.Code != LedgerCodeUnsafePermissions || st.Remediation == "" || st.Degraded == "" || !strings.Contains(st.Message, "0770") {
		t.Fatalf("status %+v", st)
	}
	if d := h.waitFirstDecision(); d != RotationLedgerDegraded {
		t.Fatalf("decision %s", d)
	}
	h.assertNoDispatch("unsafe parent")
	if n := h.logs.FilterMessageSnippet("ledger DISABLED").Len(); n != 1 {
		t.Fatalf("the warning repeated: %d", n)
	}
}

func TestLedgerDiag_Engine_KeyLost_NeverRegenerated(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	h.stop()
	if err := os.Remove(filepath.Join(h.ldir, "ledger.key")); err != nil {
		t.Fatal(err)
	}
	h.start(true)
	if h.te.ledger.mode != ledgerDisabled {
		t.Fatalf("ledger %+v", h.te.ledger)
	}
	h.waitFirstDecision()
	m := disabledWarning(t, h)
	if m["code"] != LedgerCodeKeyLost {
		t.Fatalf("code %v", m["code"])
	}
	if _, err := os.Stat(filepath.Join(h.ldir, "ledger.key")); !os.IsNotExist(err) {
		t.Fatal("a lost key was regenerated")
	}
	if !strings.Contains(m["remediation"].(string), "never regenerates") {
		t.Errorf("remediation %v", m["remediation"])
	}
}

func TestLedgerDiag_Engine_CorruptKey(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	if err := os.WriteFile(filepath.Join(h.ldir, "ledger.key"), []byte("short"), 0o400); err != nil {
		t.Fatal(err)
	}
	disabledEngine(t, h, h.ldir)
	if m := disabledWarning(t, h); m["code"] != LedgerCodeCorruptState {
		t.Fatalf("fields %v", m)
	}
	if b, _ := os.ReadFile(filepath.Join(h.ldir, "ledger.key")); string(b) != "short" {
		t.Fatal("the corrupt key was modified")
	}
}

func TestLedgerDiag_Engine_NewerSchema_Untouched(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	kp, err := ledger.NewFileKeyProvider(h.ldir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kp.EnsureKey(); err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf(`{"schema_version": %d}`, ledger.SchemaVersion+5))
	if err := os.WriteFile(filepath.Join(h.ldir, "ledger.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	disabledEngine(t, h, h.ldir)
	if m := disabledWarning(t, h); m["code"] != LedgerCodeUnsupportedSchema {
		t.Fatalf("fields %v", m)
	}
	if b, _ := os.ReadFile(filepath.Join(h.ldir, "ledger.json")); string(b) != string(body) {
		t.Fatal("a newer-schema ledger was modified")
	}
}

func TestLedgerDiag_Engine_LockHeld(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	kp, err := ledger.NewFileKeyProvider(h.ldir)
	if err != nil {
		t.Fatal(err)
	}
	holder, res, err := ledger.Open(h.ldir, ledger.Config{Keys: kp})
	if err != nil || res.Mode != ledger.ModeDurable {
		t.Fatalf("holder open: %v %+v", err, res)
	}
	t.Cleanup(func() { _ = holder.Close() })
	disabledEngine(t, h, h.ldir)
	if m := disabledWarning(t, h); m["code"] != LedgerCodeLockHeld {
		t.Fatalf("fields %v", m)
	}
}

func TestLedgerDiag_Status_ActiveAndNotConfigured(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	if st := h.te.LedgerStatus(); st.State != api.LedgerActive || st.Code != LedgerCodeActive {
		t.Fatalf("active status %+v", st)
	}
	if h.logs.FilterMessageSnippet("ledger DISABLED").Len() != 0 {
		t.Fatal("an enabled ledger warned")
	}
	h.waitFirstDecision()
	h.stop()

	h2 := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h2.ldir = ""
	h2.setProvider("one")
	h2.start(true)
	if st := h2.te.LedgerStatus(); st.State != api.LedgerNotConfigured || st.Code != LedgerCodeNotConfigured {
		t.Fatalf("not-configured status %+v", st)
	}
	h2.waitFirstDecision()
}

func TestLedgerDiag_NoSecretOrKeyMaterialInDiagnostics(t *testing.T) {
	// Texts are built from paths, modes, uids and static reasons only. Feed a
	// key error whose Err mimics a leaky system error and check nothing but the
	// static reason is carried for the key-state diagnoses.
	const sentinel = "S3CR3T-SENTINEL-VALUE"
	err := &ledger.KeyError{Kind: ledger.ErrKeyCorrupt, KeyID: "k-0011223344556677", Reason: "has 3 bytes, want 59"}
	for _, d := range []ledgerDiagnosis{classifyLedgerError("/d", err, defaultLedgerInspector())} {
		blob := fmt.Sprintf("%+v", d)
		if strings.Contains(blob, sentinel) || strings.Contains(blob, "k-0011223344556677") {
			t.Fatalf("diagnosis leaks: %s", blob)
		}
	}
}

// ============================================================ first-poll matrix gaps

// New ledger + an existing workload: desired = V, applied = UNKNOWN, every
// replica adoption unknown; never applied = V (O-5).
func TestLedgerEngine_FirstStart_RecordsDesiredNeverApplied(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2"}})
	h.setProvider("one")
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationAdopted {
		t.Fatalf("decision %s", d)
	}
	h.assertNoDispatch("first start")
	e := h.entry("app")
	if e.Desired == nil || ledger.Compare(*e.Desired, h.ver("one")) != ledger.Same {
		t.Fatalf("desired is not V: %+v", e.Desired)
	}
	if e.Applied != nil {
		t.Fatalf("applied must be UNKNOWN (nil) for a new ledger and an existing workload, got %+v", e.Applied)
	}
	for _, r := range e.Replicas {
		if r.Adoption != ledger.AdoptionUnknown || r.Adoption == ledger.AdoptionVerified {
			t.Fatalf("replica adoption %q, want unknown", r.Adoption)
		}
	}
}

// The fix path: the ledger opens disabled under a 0770 state directory; after
// the (non-recursive) permission repair the next start opens it durably, adopts
// the existing workload without rotating, and a later change rotates once.
func TestLedgerEngine_AvailableAfterRepair(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	parent := filepath.Join(h.ldir, "lib")
	dir := filepath.Join(parent, "ledger")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o770); err != nil {
		t.Fatal(err)
	}
	disabledEngine(t, h, dir)
	if d := h.waitFirstDecision(); d != RotationLedgerDegraded {
		t.Fatalf("decision while disabled: %s", d)
	}
	h.stop()

	if err := os.Chmod(parent, 0o750); err != nil { // the repair: this directory only
		t.Fatal(err)
	}
	h.start(true)
	if !h.te.ledger.enabled() {
		t.Fatalf("the ledger stayed disabled after the repair: %+v", h.te.ledger)
	}
	if h.logs.FilterMessageSnippet("ledger DISABLED").Len() != 0 {
		t.Fatal("a repaired ledger still warned DISABLED")
	}
	if d := h.waitFirstDecision(); d != RotationAdopted {
		t.Fatalf("first poll after the repair: %s, want adopted", d)
	}
	h.assertNoDispatch("after the repair")
	if e := h.entry("app"); e.Applied != nil || e.Desired == nil {
		t.Fatalf("entry %+v", e)
	}
	if st := h.te.LedgerStatus(); st.State != api.LedgerActive {
		t.Fatalf("status %+v", st)
	}
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("a change after the repair: %s", d)
	}
	h.waitCalls(1)
}

// Provider unavailable at RESTART with an existing ledger: nothing is recorded
// or rotated during the outage; when the provider returns, an unchanged value
// is unchanged and a changed one rotates exactly once.
func TestLedgerEngine_ProviderUnavailableAtRestart(t *testing.T) {
	for name, after := range map[string]string{"unchanged_after_outage": "one", "changed_during_outage": "two"} {
		t.Run(name, func(t *testing.T) {
			h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
			h.setProvider("one")
			h.start(true)
			if d := h.waitFirstDecision(); d != RotationAdopted {
				t.Fatalf("decision %s", d)
			}
			one := h.ver("one")
			h.stop()
			if err := os.Remove(filepath.Join(h.pdir, lbSecret+".json")); err != nil {
				t.Fatal(err)
			}
			h.start(true) // the provider has nothing: every fetch fails
			deadline := time.Now().Add(10 * time.Second)
			for h.logs.FilterMessage("Secret fetch failed, will retry").Len() < 2 {
				if time.Now().After(deadline) {
					t.Fatal("the poller did not retry")
				}
				time.Sleep(5 * time.Millisecond)
			}
			h.assertNoDispatch("outage at restart")
			e := h.entry("app")
			if e.Desired == nil || ledger.Compare(*e.Desired, one) != ledger.Same {
				t.Fatalf("the outage changed the recorded desired version: %+v", e.Desired)
			}
			gen := h.te.ledger.store.Health().Generation
			h.setProvider(after)
			d := h.waitFirstDecision()
			if after == "one" {
				if d != RotationUnchanged {
					t.Fatalf("decision %s, want unchanged", d)
				}
				h.assertNoDispatch("unchanged after the outage")
				if g := h.te.ledger.store.Health().Generation; g != gen {
					t.Fatalf("an unchanged value wrote the ledger: %d -> %d", gen, g)
				}
				return
			}
			if d != RotationDispatched {
				t.Fatalf("decision %s, want dispatched", d)
			}
			h.waitCalls(1)
			if n := len(h.runs()); n != 1 {
				t.Fatalf("%d rotations, want exactly 1", n)
			}
		})
	}
}

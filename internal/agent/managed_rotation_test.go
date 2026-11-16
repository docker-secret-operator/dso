package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/pkg/api"
)

// Managed-operation boundary tests (M1 acceptance section 6-7): every managed
// rotation entry point -- automatic polling, webhook, IPC reconcile and the manual
// REST/Web UI rotation (TriggerEngine.RotateSecretManually) -- shares one
// lifecycle: journal start with the delivered version, ledger correlation,
// dispatch through the reloader (operation guard asked per target), finishRotation
// and RecordRotationOutcome. Docker-free and deterministic: they run on the
// S6 engine harness (real engine, real StateTracker, real ledger store and key
// provider, fake reloader that asks the real guard).

func waitManual(t *testing.T, ch chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the manual rotation did not finish")
		return nil
	}
}

func lastRun(t *testing.T, h *engHarness) hRun {
	t.Helper()
	r := h.runs()
	if len(r) == 0 {
		t.Fatal("no rotation ran")
	}
	return r[len(r)-1]
}

// (1) automatic rotation updates the ledger with the version the journal carries.
func TestManagedRotation_Automatic_UpdatesLedger(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(1)
	run := lastRun(t, h)
	rec := JournalRecordByID(h.te.StateTracker, run.rid)
	two := h.ver("two")
	if rec == nil || rec.Status != "completed" || rec.SecretVersion != FormatJournalSecretVersion(two) || rec.DispatchTargets != 1 || !rec.DispatchFinalized {
		t.Fatalf("journal %+v", rec)
	}
	e := h.entry("app")
	if !appliedIs(e, two) || e.State != ledger.EntryIdle || e.Replicas[0].RotationID != run.rid || e.Replicas[0].Delivery != ledger.DeliveryVerified {
		t.Fatalf("entry %+v", e)
	}
}

// (2) a manual rotation reconciles the correct ledger record: the operation's own
// journal record, the version it delivered, per service and per replica.
func TestManagedRotation_Manual_UpdatesCorrectLedgerRecord(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"web": {"w1", "w2"}, "worker": {"k1"}}, "web", "worker")
	h.setProvider("one")
	h.start(false)
	h.decide("one") // baseline: desired one, applied unknown
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	run := lastRun(t, h)
	if !run.manual {
		t.Fatalf("run %+v", run)
	}
	one := h.ver("one")
	rec := JournalRecordByID(h.te.StateTracker, run.rid)
	if rec == nil || rec.Status != "completed" || rec.SecretVersion != FormatJournalSecretVersion(one) ||
		rec.DispatchTargets != 3 || !rec.DispatchFinalized || rec.SecretName != lbSecret || rec.ProviderName != lbProvider {
		t.Fatalf("journal %+v", rec)
	}
	for _, svc := range []string{"web", "worker"} {
		e := h.entry(svc)
		if !appliedIs(e, one) || e.State != ledger.EntryIdle {
			t.Fatalf("%s: entry %+v", svc, e)
		}
		for _, r := range e.Replicas {
			if r.RotationID != run.rid || r.Delivery != ledger.DeliveryVerified || r.Adoption != ledger.AdoptionVerified {
				t.Fatalf("%s: replica %+v", svc, r)
			}
		}
	}
	// Per-replica identity is preserved.
	if e := h.entry("web"); len(e.Replicas) != 2 || e.Replicas[0].ContainerName != "shop-web-1" || e.Replicas[1].ContainerName != "shop-web-2" {
		t.Fatalf("web replicas %+v", e.Replicas)
	}
	// Manual semantics are kept: asked again for the SAME verified version, the
	// operator's rotation runs again (the ledger is advisory, O-3).
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	if n := len(h.runs()); n != 2 {
		t.Fatalf("%d runs, want 2: a manual rotation of the applied version must still rotate", n)
	}
	second := lastRun(t, h)
	if e := h.entry("web"); !appliedIs(e, one) || e.Replicas[0].RotationID != second.rid {
		t.Fatalf("web after the second manual rotation: %+v", e)
	}
}

// (3) managed IPC (dso sync / apply): the reconcile reaches the same lifecycle.
func TestManagedRotation_IPCReconcile_UpdatesLedger(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	srv := &AgentServer{Cache: h.te.Cache, Store: h.store, Logger: zap.NewNop(), Config: h.cfg, Trigger: h.te}
	reconcile := func() api.ReconcileResponse {
		t.Helper()
		var resp api.ReconcileResponse
		if err := srv.TriggerReconciliation(&api.ReconcileRequest{Secret: lbSecret}, &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	reconcile() // baseline
	h.setProvider("two")
	if resp := reconcile(); !resp.Results[0].Rotated {
		t.Fatalf("a changed value was not rotated: %+v", resp)
	}
	h.waitCalls(1)
	run := lastRun(t, h)
	if e := h.entry("app"); !appliedIs(e, h.ver("two")) || e.Replicas[0].RotationID != run.rid {
		t.Fatalf("entry %+v", e)
	}
	// Documented restriction: the IPC reconcile is not a forced rotation. An
	// unchanged value rotates nothing (the operator's forced rotation is REST).
	if resp := reconcile(); resp.Results[0].Rotated {
		t.Fatalf("unchanged value rotated: %+v", resp)
	}
}

// (4) a successful manual rotation of a value whose automatic delivery failed does
// not cause a second, automatic rotation of the same version.
func TestManagedRotation_ManualSuccess_NoRedundantAutomaticRotation(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	h.decide("two")
	h.waitCalls(1)
	// The automatic delivery of "three" fails; the poller will retry it.
	h.mu.Lock()
	h.failAt = rotation.TrIntentRenameOriginal
	h.mu.Unlock()
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(2)
	waitHashGone(t, h.te, hKey)
	if e := h.entry("app"); !appliedIs(e, h.ver("two")) || e.State != ledger.EntryFailed {
		t.Fatalf("after the failed delivery: %+v", e)
	}
	h.mu.Lock()
	h.failAt = ""
	h.mu.Unlock()
	// The operator rotates by hand: the cache holds "three".
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	if e := h.entry("app"); !appliedIs(e, h.ver("three")) || e.State != ledger.EntryIdle {
		t.Fatalf("after the manual rotation: %+v", e)
	}
	// The next poll of the same value rotates nothing, now ...
	if d := h.decide("three"); d != RotationUnchanged {
		t.Fatalf("next poll: %s, want unchanged", d)
	}
	// ... and not after a restart either (the ledger carries it).
	h.setProvider("three")
	h.stop()
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationUnchanged {
		t.Fatalf("first poll after the restart: %s, want unchanged", d)
	}
	h.assertNoDispatch("manual-delivered version after a restart")
}

// With the ledger disabled the poller's dedupe alone still honours the manual
// delivery of a value whose automatic delivery had failed.
func TestManagedRotation_ManualSuccess_LedgerDisabled_NoRedundantRotation(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.open = func(te *TriggerEngine) { te.disableLedger("test: ledger unavailable") }
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	h.mu.Lock()
	h.failAt = rotation.TrIntentRenameOriginal
	h.mu.Unlock()
	h.decide("two")
	h.waitCalls(1)
	waitHashGone(t, h.te, hKey)
	h.mu.Lock()
	h.failAt = ""
	h.mu.Unlock()
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	rec := JournalRecordByID(h.te.StateTracker, lastRun(t, h).rid)
	if rec == nil || rec.SecretVersion != "" {
		t.Fatalf("a disabled ledger must not journal a version: %+v", rec)
	}
	if d := h.decide("two"); d != RotationUnchanged {
		t.Fatalf("next poll: %s, want unchanged", d)
	}
}

// (5) a failed manual rotation does not advance applied, and does not disturb the
// poller's observation state.
func TestManagedRotation_ManualFailure_DoesNotAdvanceApplied(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	h.decide("two")
	h.waitCalls(1)
	hashBefore, _ := storedHash(h.te, hKey)
	h.mu.Lock()
	h.failAt = rotation.TrIntentRenameOriginal
	h.mu.Unlock()
	if err := waitManual(t, h.manualRotate()); err == nil {
		t.Fatal("the simulated failure was not reported")
	}
	e := h.entry("app")
	if !appliedIs(e, h.ver("two")) || e.State != ledger.EntryFailed || e.LastOutcome == nil || e.LastOutcome.Kind != "failed" {
		t.Fatalf("entry %+v", e)
	}
	if r := e.Replicas[0]; r.Delivery != ledger.DeliveryNone || r.Outcome != ledger.ReplicaFailed {
		t.Fatalf("replica %+v", r)
	}
	if hashAfter, ok := storedHash(h.te, hKey); !ok || hashAfter != hashBefore {
		t.Fatal("a failed manual rotation changed the poller's observation state")
	}
}

// (7) a manual rotation interrupted before the journal committed: the restart
// leaves applied alone and the interrupted operation unresolved.
func TestManagedRotation_ManualInterruptedBeforeCommit(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	one := h.ver("one")
	h.mu.Lock()
	h.crashAt = rotation.TrIntentCreateCandidate // before candidate_ready / any commit
	h.mu.Unlock()
	h.manualRotate()
	var rid string
	select {
	case rid = <-h.crashed:
	case <-time.After(10 * time.Second):
		t.Fatal("no crash")
	}
	if rec := JournalRecordByID(h.te.StateTracker, rid); rec == nil || rec.Status != "in_progress" || rec.Committed {
		t.Fatalf("journal %+v", rec)
	}
	h.mu.Lock()
	h.crashAt = ""
	h.mu.Unlock()
	h.stop()
	h.start(true)
	e := h.entry("app")
	if !appliedIs(e, one) {
		t.Fatalf("an interrupted manual rotation changed applied: %+v", e)
	}
	if r := e.Replicas[0]; r.Delivery != ledger.DeliveryNone || r.Outcome != ledger.ReplicaFailed || r.Reason != string(ledger.WhyInterrupted) {
		t.Fatalf("replica %+v", r)
	}
	if h.logs.FilterMessage("Applied-version ledger reconciled from the journal").Len() == 0 {
		t.Fatal("the interrupted request was not reconciled")
	}
}

// (8)+(9) the journal committed, the ledger update did not happen (here: failed),
// and the restart reconciles from the journal alone: applied advances without
// re-executing anything.
func TestManagedRotation_Manual_LedgerUpdateFails_RestartReconcilesWithoutReExecution(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	hold := make(chan struct{})
	h.mu.Lock()
	h.hold = hold
	h.mu.Unlock()
	manual := h.manualRotate()
	time.Sleep(30 * time.Millisecond)
	// Fault seam: the ledger store refuses every update from now on.
	_ = h.te.ledger.store.Close()
	h.mu.Lock()
	h.hold = nil
	h.mu.Unlock()
	close(hold)
	if err := waitManual(t, manual); err != nil {
		t.Fatal(err)
	}
	run := lastRun(t, h)
	if rec := JournalRecordByID(h.te.StateTracker, run.rid); rec == nil || rec.Status != "completed" {
		t.Fatalf("journal %+v", rec)
	}
	if h.logs.FilterMessageSnippet("rotation outcome not recorded").Len() == 0 {
		t.Fatal("the ledger failure was not reported")
	}
	h.stop()
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationUnchanged {
		t.Fatalf("first poll after the restart: %s, want unchanged", d)
	}
	h.assertNoDispatch("after the reconcile")
	e := h.entry("app")
	if !appliedIs(e, h.ver("one")) || e.Replicas[0].RotationID != run.rid || e.Replicas[0].Delivery != ledger.DeliveryVerified {
		t.Fatalf("reconciled entry %+v", e)
	}
}

// (9) a manual operation committed in the journal and the process died before the
// ledger was told: the restart applies it from the journal-carried version.
func TestManagedRotation_Manual_CrashAfterCommit_ReconcilesWithoutReExecution(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	h.mu.Lock()
	h.crashAt = rotation.TrCompleted
	h.mu.Unlock()
	h.manualRotate()
	var rid string
	select {
	case rid = <-h.crashed:
	case <-time.After(10 * time.Second):
		t.Fatal("no crash")
	}
	if e := h.entry("app"); e.Applied != nil || e.Replicas[0].Delivery != ledger.DeliveryRequested {
		t.Fatalf("pre-crash ledger %+v", e)
	}
	h.mu.Lock()
	h.crashAt = ""
	h.mu.Unlock()
	h.stop()
	h.start(true)
	if d := h.waitFirstDecision(); d != RotationUnchanged {
		t.Fatalf("first poll after the crash: %s, want unchanged", d)
	}
	h.assertNoDispatch("after the manual W6")
	if e := h.entry("app"); !appliedIs(e, h.ver("one")) || e.Replicas[0].RotationID != rid {
		t.Fatalf("entry %+v", e)
	}
}

// (11) the operation guard is always asked: an unresolved operation keeps a manual
// rotation from mutating the service, and the ledger does not claim it.
func TestManagedRotation_Manual_GuardAlwaysAsked(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	ctx := rotation.WithRotationID(context.Background(), "rot-crit")
	if err := h.te.StateTracker.Record(ctx, rotation.JournalEvent{RotationID: "rot-crit", ServiceID: "app", Strategy: "restart", Transition: rotation.TrIntentRenameOriginal}); err != nil {
		t.Fatal(err)
	}
	if err := h.te.StateTracker.MarkCriticalRecord(JournalRecordByID(h.te.StateTracker, "rot-crit"), "test"); err != nil {
		t.Fatal(err)
	}
	err := waitManual(t, h.manualRotate())
	if err == nil {
		t.Fatal("the guard did not refuse the manual rotation")
	}
	if n := len(h.runs()); n != 0 {
		t.Fatal("the guard was bypassed by a manual rotation")
	}
	e := h.entry("app")
	if e.Applied != nil || e.Replicas[0].Delivery != ledger.DeliveryNone {
		t.Fatalf("a refused manual rotation left a ledger claim: %+v", e)
	}
	// Only the unresolved record remains: the refused operation's record is removed.
	if recs := h.te.StateTracker.snapshotRecords(); len(recs) != 1 || recs[0].RotationID != "rot-crit" {
		t.Fatalf("journal %+v", recs)
	}
}

// (12) signal delivery and application adoption stay distinguishable, also for a
// manual rotation.
func TestManagedRotation_Manual_SignalIsNeverAdoptionVerified(t *testing.T) {
	h := newEngHarness(t, "signal", map[string][]string{"app": {"s1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	if err := waitManual(t, h.manualRotate()); err != nil {
		t.Fatal(err)
	}
	e := h.entry("app")
	r := e.Replicas[0]
	if !appliedIs(e, h.ver("one")) || r.Delivery != ledger.DeliveryVerified || r.Adoption != ledger.AdoptionUnverified || r.Reason != string(ledger.WhyAdoptionUnverifiedSignal) {
		t.Fatalf("entry %+v", e)
	}
}

// The manual entry point refuses what it cannot rotate without side effects.
func TestManagedRotation_Manual_Refusals(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	ctx := context.Background()
	if err := h.te.RotateSecretManually(ctx, "no-such-secret", "t", nil); !errors.Is(err, ErrManualUnknownSecret) {
		t.Fatalf("unknown secret: %v", err)
	}
	// The dispatch lock of the secret is held by another dispatch.
	if err := h.te.LockManager.AcquireLock(lbSecret, time.Second); err != nil {
		t.Fatal(err)
	}
	h.te.secretLockTimeout = 50 * time.Millisecond
	if err := h.te.RotateSecretManually(ctx, lbSecret, "t", nil); !errors.Is(err, ErrManualBusy) {
		t.Fatalf("busy: %v", err)
	}
	h.te.LockManager.ReleaseLock(lbSecret)
	if recs := h.te.StateTracker.snapshotRecords(); len(recs) != 0 {
		t.Fatalf("a refused manual rotation left journal records: %+v", recs)
	}
	// No managed container uses the secret: nothing is journalled or claimed.
	h.mu.Lock()
	h.services = map[string][]string{}
	h.refreshTargetsLocked()
	h.mu.Unlock()
	if err := h.te.RotateSecretManually(ctx, lbSecret, "t", nil); !errors.Is(err, ErrManualNoTargets) {
		t.Fatalf("no targets: %v", err)
	}
	if recs := h.te.StateTracker.snapshotRecords(); len(recs) != 0 {
		t.Fatalf("journal %+v", recs)
	}
}

// The caller's operation context is the daemon-bound context of the rotation: its
// values, deadline and cancellation reach the reloader, with the operation's
// identity added; the actor is logged, never a secret value.
func TestManagedRotation_Manual_KeepsTheCallersContext(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	type ctxKey struct{}
	var got context.Context
	h.te.reloadFn = func(ctx context.Context, secretName string, onComplete func(error)) error {
		got = ctx
		go onComplete(nil)
		return nil
	}
	base, cancel := context.WithTimeout(context.WithValue(context.Background(), ctxKey{}, "daemon"), time.Hour)
	done := make(chan error, 1)
	if err := h.te.RotateSecretManually(base, lbSecret, "webui:user:operator", func(err error) { done <- err }); err != nil {
		t.Fatal(err)
	}
	if err := waitManual(t, done); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Value(ctxKey{}) != "daemon" || rotation.RotationIDFrom(got) == "" {
		t.Fatalf("context %v", got)
	}
	if _, ok := got.Deadline(); !ok {
		t.Fatal("the caller's deadline was dropped")
	}
	cancel()
	select {
	case <-got.Done():
	default:
		t.Fatal("cancelling the caller's context does not reach the rotation")
	}
	logs := h.logs.FilterMessage("Managed rotation operation started").All()
	if len(logs) != 1 || logs[0].ContextMap()["actor"] != "webui:user:operator" || logs[0].ContextMap()["origin"] != "manual" {
		t.Fatalf("log %+v", logs)
	}
}

// ============================================================ item (g)

// installPartial installs a reloader that delivers a dispatch of two targets (the
// containers a1 of service "web" and k1 of service "worker") the way the real
// strategies do: both write their transitions to the ONE journal record of the
// dispatch. failWorker makes the second target fail.
func installPartial(h *engHarness, failWorker bool) chan struct{} {
	done := make(chan struct{}, 4)
	h.te.reloadFn = func(ctx context.Context, secretName string, onComplete func(error)) error {
		rid := rotation.RotationIDFrom(ctx)
		h.mu.Lock()
		if ctx.Value(manualMarker{}) == nil {
			h.engineCalls++
			h.completed++
		}
		h.mu.Unlock()
		go func() {
			jctx := rotation.WithRotationID(context.Background(), rid)
			st := h.te.StateTracker
			write := func(svc, orig string, steps []string) {
				for i, tr := range steps {
					ev := rotation.JournalEvent{RotationID: rid, Strategy: "restart", Transition: tr, ServiceID: svc, OriginalIDs: []string{orig}}
					if i > 0 {
						ev.CandidateIDs = []string{"c-" + orig}
					}
					if err := st.Record(jctx, ev); err != nil {
						panic(err)
					}
				}
			}
			write("web", "w1", lbSteps("restart"))
			var err error
			if failWorker {
				write("worker", "k1", []string{rotation.TrIntentRenameOriginal, rotation.TrFailed})
				err = errors.New("worker: replacement failed")
			} else {
				write("worker", "k1", lbSteps("restart"))
			}
			onComplete(err)
			done <- struct{}{}
		}()
		return nil
	}
	return done
}

func twoServiceHarness(t *testing.T) *engHarness {
	t.Helper()
	h := newEngHarness(t, "restart", map[string][]string{"web": {"w1"}, "worker": {"k1"}}, "web", "worker")
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	return h
}

// (6) Item (g), the reachable-or-not question for FAILED targets: when some
// targets of one dispatch succeed and another fails, the engine receives an error,
// the shared journal record ends rollback_required (whatever the successful target
// wrote), and the ledger records the failure for every entry of the dispatch --
// never delivery verified, never applied. Pinned: the partial dispatch is
// distinguishable from a full one, which advances both entries.
func TestPartialDelivery_FailedTarget_NeverRecordedAsApplied(t *testing.T) {
	h := twoServiceHarness(t)
	done := installPartial(h, true)
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	<-done
	h.waitIdle()
	var rid string
	for _, r := range h.te.StateTracker.snapshotRecords() {
		rid = r.RotationID
		if r.Status != "rollback_required" {
			t.Fatalf("the shared record of a partly failed dispatch is %s, want rollback_required (the successful target's 'completed' must not stand)", r.Status)
		}
		if err := journalDeliveryRefusal(r); err == nil {
			t.Fatal("a partly failed dispatch is delivery evidence")
		}
	}
	if rid == "" {
		t.Fatal("no journal record")
	}
	for _, svc := range []string{"web", "worker"} {
		e := h.entry(svc)
		if e.Applied != nil || e.State != ledger.EntryFailed || e.Replicas[0].Delivery != ledger.DeliveryNone || e.Replicas[0].Outcome != ledger.ReplicaFailed {
			t.Fatalf("%s recorded a partly failed dispatch: %+v", svc, e)
		}
	}
	// Distinguishable from a full delivery: the retry (the failed hash was rolled
	// back) succeeds on both targets and advances both entries.
	done = installPartial(h, false)
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("retry decision %s", d)
	}
	<-done
	for _, svc := range []string{"web", "worker"} {
		if e := h.entry(svc); !appliedIs(e, h.ver("two")) || e.State != ledger.EntryIdle {
			t.Fatalf("%s after the full delivery: %+v", svc, e)
		}
	}
}

// Item (g): an entry whose targets are not rotated by the dispatch (strategy none)
// is not "applied" by the dispatch's success, while the entry that was rotated is.
// Before the fix the shared record's success advanced both.
func TestPartialDelivery_NonRotatingEntry_NotApplied(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"web": {"w1"}, "batch": {"b1"}}, "web", "batch")
	h.setProvider("one")
	h.start(false)
	h.te.ledger.kindFn = func(string, string) map[ledger.Key]ledgerKeyKinds {
		return map[ledger.Key]ledgerKeyKinds{h.key("web"): {rotating: true}, h.key("batch"): {inert: true}}
	}
	h.decide("one")
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(1)
	if e := h.entry("web"); !appliedIs(e, h.ver("two")) {
		t.Fatalf("web %+v", e)
	}
	e := h.entry("batch")
	if e.Applied != nil || e.Replicas[0].Delivery != ledger.DeliveryNone || e.Replicas[0].Outcome != ledger.ReplicaPending || e.Replicas[0].Reason != string(ledger.WhyDeferred) {
		t.Fatalf("a target the dispatch did not rotate was recorded as delivered: %+v", e)
	}
}

// Item (g): an entry whose targets were only SIGNALLED is never adoption verified,
// even when another target's restart transitions were written to the shared record
// last (the record carries only the last writer's strategy).
func TestPartialDelivery_MixedSignalAndRestart_SignalNeverAdoptionVerified(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"web": {"w1"}, "cron": {"s1"}}, "cron", "web")
	h.setProvider("one")
	h.start(false)
	h.te.ledger.kindFn = func(string, string) map[ledger.Key]ledgerKeyKinds {
		return map[ledger.Key]ledgerKeyKinds{h.key("web"): {rotating: true}, h.key("cron"): {signal: true}}
	}
	h.decide("one")
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	h.waitCalls(1)
	web, cron := h.entry("web").Replicas[0], h.entry("cron").Replicas[0]
	if web.Delivery != ledger.DeliveryVerified || web.Adoption != ledger.AdoptionVerified {
		t.Fatalf("web %+v", web)
	}
	if cron.Delivery != ledger.DeliveryVerified || cron.Adoption != ledger.AdoptionUnverified || cron.Reason != string(ledger.WhyAdoptionUnverifiedSignal) {
		t.Fatalf("a signalled target was recorded adoption-verified: %+v", cron)
	}
}

// Item (g), crash window: several targets share ONE journal record, and the first
// target to finish writes the record's 'completed' status. A process that dies
// before the OTHER target finished leaves a record that looks like a success. The
// restart must not apply the version to the whole dispatch.
func TestPartialDelivery_CrashBeforeAllTargetsFinished_NotAppliedAtRestart(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2"}})
	h.setProvider("one")
	h.start(true)
	h.waitFirstDecision()
	crashed := make(chan string, 1)
	h.te.reloadFn = func(ctx context.Context, secretName string, onComplete func(error)) error {
		rid := rotation.RotationIDFrom(ctx)
		h.mu.Lock()
		h.engineCalls++
		h.completed++ // the dead dispatch never reports
		h.mu.Unlock()
		go func() {
			jctx := rotation.WithRotationID(context.Background(), rid)
			// Replica a1 is fully rotated ...
			for i, tr := range lbSteps("restart") {
				ev := rotation.JournalEvent{RotationID: rid, Strategy: "restart", Transition: tr, ServiceID: "app", OriginalIDs: []string{"a1"}}
				if i > 0 {
					ev.CandidateIDs = []string{"c-a1"}
				}
				if err := h.te.StateTracker.Record(jctx, ev); err != nil {
					panic(err)
				}
			}
			crashed <- rid // ... and the process dies before replica a2 finished.
		}()
		return nil
	}
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("decision %s", d)
	}
	var rid string
	select {
	case rid = <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatal("no crash")
	}
	rec := JournalRecordByID(h.te.StateTracker, rid)
	if rec == nil || rec.Status != "completed" || rec.DispatchTargets != 2 || rec.DispatchFinalized {
		t.Fatalf("the shared record %+v: expected the first target's 'completed' without engine finalisation", rec)
	}
	h.stop()
	h.start(true)
	e := h.entry("app")
	if e.Applied != nil {
		t.Fatalf("a dispatch of which only one replica finished was recorded as applied at restart: %+v", e)
	}
	for _, r := range e.Replicas {
		if r.Delivery == ledger.DeliveryVerified {
			t.Fatalf("replica %+v recorded as delivered", r)
		}
	}
	// The safe cost: the first poll after the restart delivers the version again.
	if d := h.waitFirstDecision(); d != RotationDispatched {
		t.Fatalf("first poll after the restart: %s", d)
	}
}

// The bridge rule behind the crash test, in isolation: a multi-target record that
// the engine never finalised is not delivery evidence; a finalised one, and a
// record without a recorded target count, keep the plain rule.
func TestBridge_UnfinalisedMultiTargetRecord_IsNotEvidence(t *testing.T) {
	mk := func(targets int, finalized bool) *RotationState {
		return &RotationState{RotationID: "rot-x", Strategy: "restart", Transition: rotation.TrCompleted, Status: "completed", Seq: 9, Committed: false,
			DispatchTargets: targets, DispatchFinalized: finalized}
	}
	if err := journalDeliveryRefusal(mk(2, false)); !errors.Is(err, ledger.ErrNoEvidence) {
		t.Fatalf("unfinalised multi-target: %v", err)
	}
	jr := JournalRecordFor(mk(2, false))
	if jr.CandidateVerified || jr.Committed || jr.SignalDone {
		t.Fatalf("snapshot of an unfinalised multi-target record carries evidence: %+v", jr)
	}
	for _, rec := range []*RotationState{mk(2, true), mk(1, false), mk(0, false)} {
		if err := journalDeliveryRefusal(rec); err != nil {
			t.Fatalf("%+v: %v", rec, err)
		}
	}
}

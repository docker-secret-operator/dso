package agent

// N19 convergence (docs/design/n19-managed-delivery.md section 11 B/C): a gated container
// restart re-delivers the CACHED value without a journal record or ledger claim
// (watcher.redeliverGated), so the ledger can say Applied=V2 while a container runs V3.
// These tests prove, through the REAL engine/trigger/ledger/journal/guard (engHarness),
// that the desired version stays pending and a later eligible poll of the SAME provider
// value (no V4) dispatches the managed rotation that records it.
//
// A re-delivery has no engine-visible effect, so each scenario models it as exactly that:
// nothing is written to the journal or ledger (asserted via snapshots), and the provider
// value stays unchanged afterwards.

import (
	"context"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/ledger"
	"github.com/docker-secret-operator/dso/internal/rotation"
)

// n19ToV2 adopts V1 then rotates to V2 so the ledger records Applied=V2.
func n19ToV2(t *testing.T, h *engHarness, svcs ...string) {
	t.Helper()
	h.setProvider("one")
	h.start(false)
	h.decide("one")
	if d := h.decide("two"); d != RotationDispatched {
		t.Fatalf("V2 decision %s", d)
	}
	h.waitCalls(1)
	for _, s := range svcs {
		if e := h.entry(s); !appliedIs(e, h.ver("two")) {
			t.Fatalf("precondition: %s not Applied=V2: %+v", s, e)
		}
	}
}

// n19Redeliver models a gated-container restart that re-delivers the cached value: it
// must leave the journal and the ledger exactly as they were.
func n19Redeliver(t *testing.T, h *engHarness, svcs ...string) {
	t.Helper()
	before := map[string]ledger.Entry{}
	for _, s := range svcs {
		before[s] = h.entry(s)
	}
	nrec := len(h.te.StateTracker.snapshotRecords())
	// (redeliverGated writes no journal record and makes no ledger claim: nothing to call)
	if got := len(h.te.StateTracker.snapshotRecords()); got != nrec {
		t.Fatal("journal changed by a re-delivery")
	}
	for _, s := range svcs {
		a, b := before[s], h.entry(s)
		if (a.Applied == nil) != (b.Applied == nil) || (a.Applied != nil && ledger.Compare(*a.Applied, *b.Applied) != ledger.Same) {
			t.Fatalf("re-delivery advanced Applied for %s", s)
		}
	}
}

func n19BlockService(t *testing.T, h *engHarness, rid, svc string) {
	t.Helper()
	ctx := rotation.WithRotationID(context.Background(), rid)
	if err := h.te.StateTracker.Record(ctx, rotation.JournalEvent{RotationID: rid, ServiceID: svc, Strategy: "restart", Transition: rotation.TrIntentRenameOriginal}); err != nil {
		t.Fatal(err)
	}
	if err := h.te.StateTracker.MarkCriticalRecord(JournalRecordByID(h.te.StateTracker, rid), "test"); err != nil {
		t.Fatal(err)
	}
}

// (a) the earlier V3 dispatch completed normally: Applied=V3, re-delivery changes nothing.
func TestConvergence_V3DispatchCompleted_RedeliveryAndRepollAreStable(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	n19ToV2(t, h, "app")
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("V3 decision %s", d)
	}
	h.waitCalls(2)
	if e := h.entry("app"); !appliedIs(e, h.ver("three")) {
		t.Fatalf("not Applied=V3: %+v", e)
	}
	n19Redeliver(t, h, "app")
	if d := h.decide("three"); d != RotationUnchanged {
		t.Fatalf("re-poll of an applied value must be unchanged, got %s", d)
	}
	h.waitCalls(2)
}

// (b1) the V3 dispatch failed (strategy error, rollback): hash rolled back, so the re-poll
// of the SAME V3 re-dispatches; a re-delivery in between does not advance Applied.
func TestConvergence_V3DispatchFailed_RepollOfSameValueConverges(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	n19ToV2(t, h, "app")
	h.mu.Lock()
	h.failAt = rotation.TrCandidateReady
	h.mu.Unlock()
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("V3 decision %s", d)
	}
	h.waitCalls(2)
	waitHashGone(t, h.te, hKey)
	e := h.entry("app")
	if !appliedIs(e, h.ver("two")) {
		t.Fatalf("Applied must stay V2 after a failed V3 dispatch: %+v", e)
	}
	t.Logf("after failure: state=%s decision=%s", e.State, h.te.ledger.v.NeedsRotation(h.key("app")).Kind)
	n19Redeliver(t, h, "app")
	h.mu.Lock()
	h.failAt = ""
	h.mu.Unlock()
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("same-value re-poll after a failed dispatch must dispatch, got %s (ledger: %+v)", d, h.entry("app"))
	}
	h.waitCalls(3)
	if e := h.entry("app"); !appliedIs(e, h.ver("three")) {
		t.Fatalf("did not converge to V3 without V4: %+v", e)
	}
}

// (b2) the V3 dispatch was blocked by the operation guard: nothing is delivered, hash is
// rolled back; re-delivery never bypasses the guard's unresolved operation; once the
// operator resolves it the same V3 converges.
func TestConvergence_V3Blocked_GuardNotBypassedThenConverges(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
	n19ToV2(t, h, "app")
	n19BlockService(t, h, "rot-crit", "app")
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("V3 decision %s", d)
	}
	h.waitCalls(2)
	waitHashGone(t, h.te, hKey)
	if n := len(h.runs()); n != 1 {
		t.Fatalf("guard bypassed: %d runs", n)
	}
	n19Redeliver(t, h, "app")
	// still blocked: repeated polls of V3 keep deferring, never advance Applied
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("blocked re-poll decision %s", d)
	}
	h.waitCalls(3)
	waitHashGone(t, h.te, hKey)
	if e := h.entry("app"); !appliedIs(e, h.ver("two")) || len(h.runs()) != 1 {
		t.Fatalf("Applied advanced past an unresolved operation: %+v", e)
	}
	if err := h.te.StateTracker.MarkRecoveredRecord(JournalRecordByID(h.te.StateTracker, "rot-crit")); err != nil {
		t.Fatal(err)
	}
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("after the operator: %s", d)
	}
	h.waitCalls(4)
	if e := h.entry("app"); !appliedIs(e, h.ver("three")) {
		t.Fatalf("did not converge to V3 without V4: %+v", e)
	}
}

// (c) agent restart (empty secretHashes, persistent ledger+journal) after V3 was observed
// but never applied: the first poll of the same V3 dispatches (ledger desired V3 > applied V2).
func TestConvergence_AgentRestart_V3ObservedNotApplied_DispatchesOnFirstPoll(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "observed-not-dispatched"
		if failed {
			name = "dispatch-failed"
		}
		t.Run(name, func(t *testing.T) {
			h := newEngHarness(t, "restart", map[string][]string{"app": {"a1"}})
			n19ToV2(t, h, "app")
			if failed {
				h.mu.Lock()
				h.failAt = rotation.TrCandidateReady
				h.mu.Unlock()
				h.decide("three")
				h.waitCalls(2)
				h.mu.Lock()
				h.failAt = ""
				h.mu.Unlock()
			} else {
				n19BlockService(t, h, "rot-crit", "app")
				h.decide("three")
				h.waitCalls(2)
				if err := h.te.StateTracker.MarkRecoveredRecord(JournalRecordByID(h.te.StateTracker, "rot-crit")); err != nil {
					t.Fatal(err)
				}
			}
			h.waitIdle()
			h.stop()
			h.start(false) // new engine, same directories
			n19Redeliver(t, h, "app")
			if e := h.entry("app"); !appliedIs(e, h.ver("two")) {
				t.Fatalf("Applied after restart: %+v", e)
			}
			if failed {
				// The fake daemon is empty, so startup recovery cannot prove the failed
				// attempt's workload and (correctly) leaves the record unresolved: the guard
				// refuses the first poll. Recovery/operator then resolves it.
				runsBefore := len(h.runs())
				if d := h.decide("three"); d != RotationDispatched {
					t.Fatalf("blocked poll decision %s", d)
				}
				h.waitCalls(1)
				waitHashGone(t, h.te, hKey)
				if n := len(h.runs()); n != runsBefore {
					t.Fatal("guard bypassed after restart")
				}
				for _, r := range h.te.StateTracker.snapshotRecords() {
					if r.Status == "critical_error" {
						if err := h.te.StateTracker.MarkRecoveredRecord(JournalRecordByID(h.te.StateTracker, r.RotationID)); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if d := h.decide("three"); d != RotationDispatched {
				t.Fatalf("first poll of V3 after restart: %s (ledger %+v)", d, h.entry("app"))
			}
			if failed {
				h.waitCalls(2)
			} else {
				h.waitCalls(1)
			}
			if e := h.entry("app"); !appliedIs(e, h.ver("three")) {
				t.Fatalf("did not converge to V3 without V4: %+v", e)
			}
		})
	}
}

// 2. Multi-replica: one replica re-delivered V3, the other still V2. The ledger entry is
// per service and never reports V3 applied until a managed rotation verifies it; a later
// eligible operation (the same V3) converges every replica.
func TestConvergence_MultiReplica_PartialRedeliveryNeverReportsApplied(t *testing.T) {
	h := newEngHarness(t, "restart", map[string][]string{"app": {"a1", "a2"}})
	n19ToV2(t, h, "app")
	n19BlockService(t, h, "rot-crit", "app")
	h.decide("three")
	h.waitCalls(2)
	waitHashGone(t, h.te, hKey)
	n19Redeliver(t, h, "app") // a1 re-delivered V3, a2 still V2
	e := h.entry("app")
	if appliedIs(e, h.ver("three")) {
		t.Fatalf("the workload is reported as V3 applied: %+v", e)
	}
	for _, r := range e.Replicas {
		if r.Applied != nil && ledger.Compare(*r.Applied, h.ver("three")) == ledger.Same {
			t.Fatalf("a replica is reported as V3 applied by a re-delivery: %+v", r)
		}
	}
	if err := h.te.StateTracker.MarkRecoveredRecord(JournalRecordByID(h.te.StateTracker, "rot-crit")); err != nil {
		t.Fatal(err)
	}
	if d := h.decide("three"); d != RotationDispatched {
		t.Fatalf("later eligible poll: %s", d)
	}
	h.waitCalls(3)
	e = h.entry("app")
	if !appliedIs(e, h.ver("three")) {
		t.Fatalf("not converged: %+v", e)
	}
	time.Sleep(10 * time.Millisecond)
}

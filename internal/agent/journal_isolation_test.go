package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"github.com/docker-secret-operator/dso/internal/watcher"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// F-S6-1 / F-B2 regression suite (docs/audit/51-F-S6-1-JOURNAL-ISOLATION.md).
//
// These tests drive the REAL TriggerEngine dispatch path (ExecuteRotationDecision ->
// StartRotationWithVersion -> reload -> finishRotation) against a REAL StateTracker in
// a temp directory. Only the reload itself is faked (the reloadFn seam), exactly like
// TriggerReload does: it returns immediately and reports the real outcome later
// through onComplete, so several dispatches of one secret can be in flight at once.
// That is a supported state: the secret lock is released when TriggerReload returns,
// while the strategies' detached goroutines keep the per-service locks.

type fakeDispatch struct {
	id       string
	complete func(error)
}

type isolationRig struct {
	te         *TriggerEngine
	st         *StateTracker
	dir        string
	dispatches chan fakeDispatch
}

func newIsolationRig(t *testing.T, targets map[string][]string) *isolationRig {
	t.Helper()
	te := newOutcomeTestEngine(t)
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	te.StateTracker = st
	te.Reloader = &watcher.ReloaderController{Logger: zap.NewNop()}
	for id, secrets := range targets {
		te.Reloader.Targets.Store(id, &watcher.TargetContainer{ID: id, Secrets: secrets})
	}
	r := &isolationRig{te: te, st: st, dir: dir, dispatches: make(chan fakeDispatch, 64)}
	te.reloadFn = func(ctx context.Context, _ string, onComplete func(error)) error {
		r.dispatches <- fakeDispatch{id: rotation.RotationIDFrom(ctx), complete: onComplete}
		return nil
	}
	return r
}

// dispatch observes value and returns the in-flight dispatch it started.
func (r *isolationRig) dispatch(t *testing.T, provider, secret string, value map[string]string) fakeDispatch {
	t.Helper()
	if d := r.te.ExecuteRotationDecision(provider, secret, value, config.SecretMapping{Name: secret}); d != RotationDispatched {
		t.Fatalf("decision = %q, want dispatched", d)
	}
	select {
	case d := <-r.dispatches:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch never reached the reloader")
	}
	return fakeDispatch{}
}

// finish reports d's outcome and waits for finishRotation to have run (the
// reloadFn's onComplete is synchronous in finishRotation).
func (d fakeDispatch) finish(err error) { d.complete(err) }

func recordOf(st *StateTracker, id string) *RotationState { return JournalRecordByID(st, id) }

func ids(rs []*RotationState) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.RotationID+"="+r.Status)
	}
	return out
}

// Overlap of two dispatches of one secret where the second is deferred (its services
// are still held by the first). The deferred dispatch's cleanup must not erase the
// first dispatch's in-progress record.
func TestFS61_DeferredSecondDispatchDoesNotEraseFirstRecord(t *testing.T) {
	r := newIsolationRig(t, map[string][]string{"ctr-a": {"db"}, "ctr-b": {"db"}})
	d1 := r.dispatch(t, "vault", "db", syntheticV2)
	d2 := r.dispatch(t, "vault", "db", syntheticV3)
	if d1.id == "" || d1.id == d2.id {
		t.Fatalf("dispatch ids %q %q must be distinct and non-empty", d1.id, d2.id)
	}
	// Strategy of dispatch 1 is journalling progress under its rotation id.
	if err := r.st.Record(context.Background(), rotation.JournalEvent{
		RotationID: d1.id, ServiceID: "svc-a", Strategy: "rolling", OriginalIDs: []string{"ctr-a"},
		Transition: rotation.TrIntentHandoff,
	}); err != nil {
		t.Fatal(err)
	}
	// The second dispatch is deferred (service busy): finishRotation cleans ITS record up.
	d2.finish(fmt.Errorf("%w: 2 of 2 targets not rotated", watcher.ErrRotationDeferred))

	rec := recordOf(r.st, d1.id)
	if rec == nil {
		t.Fatalf("dispatch 1's in-progress record was erased by dispatch 2 (records: %v)", ids(r.st.snapshotRecords()))
	}
	if rec.Status != "in_progress" || rec.PendingIntent != rotation.TrIntentHandoff || rec.Strategy != "rolling" {
		t.Fatalf("dispatch 1's record was altered: %+v", rec)
	}
	if rec.ProviderName != "vault" || rec.SecretName != "db" {
		t.Fatalf("dispatch 1's record lost its identity (journal-only stub): %+v", rec)
	}
	if recordOf(r.st, d2.id) != nil {
		t.Fatalf("the deferred dispatch's own record must be gone")
	}
	d1.finish(nil)
	if rec := recordOf(r.st, d1.id); rec == nil || rec.Status != "completed" {
		t.Fatalf("dispatch 1 did not complete its own record: %+v", rec)
	}
}

// A stale completion (dispatch 1 finishing) must complete dispatch 1's record, never
// the newer dispatch 2's record of the same secret (a service subset overlap: B free,
// A still held by dispatch 1).
func TestFS61_StaleCompletionCannotCompleteNewerDispatch(t *testing.T) {
	r := newIsolationRig(t, map[string][]string{"ctr-a": {"db"}})
	d1 := r.dispatch(t, "vault", "db", syntheticV2)
	d2 := r.dispatch(t, "vault", "db", syntheticV3)
	d1.finish(nil)
	if rec := recordOf(r.st, d2.id); rec == nil || rec.Status != "in_progress" {
		t.Fatalf("dispatch 1's completion touched dispatch 2's record: %+v (records %v)", rec, ids(r.st.snapshotRecords()))
	}
	if rec := recordOf(r.st, d1.id); rec == nil || rec.Status != "completed" {
		t.Fatalf("dispatch 1's own record is not completed: %+v", rec)
	}
	d2.finish(nil)
	if rec := recordOf(r.st, d2.id); rec == nil || rec.Status != "completed" {
		t.Fatalf("dispatch 2's own record is not completed: %+v", rec)
	}
}

// A stale rollback (dispatch 1 failing) must not mark dispatch 2's record rollback_required.
func TestFS61_StaleRollbackCannotRollBackNewerDispatch(t *testing.T) {
	r := newIsolationRig(t, map[string][]string{"ctr-a": {"db"}})
	d1 := r.dispatch(t, "vault", "db", syntheticV2)
	d2 := r.dispatch(t, "vault", "db", syntheticV3)
	d1.finish(errors.New("simulated strategy failure"))
	if rec := recordOf(r.st, d2.id); rec == nil || rec.Status != "in_progress" {
		t.Fatalf("dispatch 1's failure rolled back dispatch 2's record: %+v (records %v)", rec, ids(r.st.snapshotRecords()))
	}
	if rec := recordOf(r.st, d1.id); rec == nil || rec.Status != "rollback_required" {
		t.Fatalf("dispatch 1's own record is not rollback_required: %+v", rec)
	}
	d2.finish(nil)
	if rec := recordOf(r.st, d2.id); rec == nil || rec.Status != "completed" {
		t.Fatalf("dispatch 2's own record is not completed: %+v", rec)
	}
}

// F-B2: the signal strategy keeps the same container ids across rotations. A new
// dispatch must not replace the completed predecessor: its journal evidence
// (signal_done and the keyed version) is what the ledger and recovery still need.
func TestFB2_CompletedPredecessorSurvivesNewDispatchWithSameContainerIDs(t *testing.T) {
	r := newIsolationRig(t, map[string][]string{"ctr-a": {"db"}})
	d1 := r.dispatch(t, "vault", "db", syntheticV2)
	if err := r.st.Record(context.Background(), rotation.JournalEvent{
		RotationID: d1.id, ServiceID: "svc-a", Strategy: "signal", OriginalIDs: []string{"ctr-a"},
		Transition: rotation.TrSignalDone,
	}); err != nil {
		t.Fatal(err)
	}
	d1.finish(nil)
	before := recordOf(r.st, d1.id)
	if before == nil || before.Status != "completed" || before.Transition != rotation.TrSignalDone {
		t.Fatalf("precondition: %+v", before)
	}
	d2 := r.dispatch(t, "vault", "db", syntheticV3)
	after := recordOf(r.st, d1.id)
	if after == nil {
		t.Fatalf("the completed predecessor %s was replaced by %s (records %v)", d1.id, d2.id, ids(r.st.snapshotRecords()))
	}
	if after.Transition != rotation.TrSignalDone || after.Seq != before.Seq || after.Status != "completed" {
		t.Fatalf("predecessor evidence changed: before %+v after %+v", before, after)
	}
	d2.finish(nil)
}

// Many overlapping dispatches of one secret completing / failing / deferring in a
// seeded random order: every record ends in exactly the state its own outcome states.
func TestFS61_ManyOverlappingDispatches_EachRecordFollowsItsOwnOutcome(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 7, 42} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			r := newIsolationRig(t, map[string][]string{"ctr-a": {"db"}})
			rng := rand.New(rand.NewSource(seed))
			type op struct {
				d    fakeDispatch
				kind string
			}
			var ops []op
			for i := 0; i < 8; i++ {
				v := map[string]string{"value": fmt.Sprintf("synthetic-%d-%d", seed, i)}
				kinds := []string{"ok", "fail", "defer"}
				ops = append(ops, op{r.dispatch(t, "vault", "db", v), kinds[rng.Intn(len(kinds))]})
			}
			rng.Shuffle(len(ops), func(i, j int) { ops[i], ops[j] = ops[j], ops[i] })
			for _, o := range ops {
				switch o.kind {
				case "ok":
					o.d.finish(nil)
				case "fail":
					o.d.finish(errors.New("simulated failure"))
				case "defer":
					o.d.finish(fmt.Errorf("%w: busy", watcher.ErrRotationDeferred))
				}
			}
			for _, o := range ops {
				rec := recordOf(r.st, o.d.id)
				switch o.kind {
				case "ok":
					if rec == nil || rec.Status != "completed" {
						t.Fatalf("%s should be completed, got %+v", o.d.id, rec)
					}
				case "fail":
					if rec == nil || rec.Status != "rollback_required" {
						t.Fatalf("%s should be rollback_required, got %+v", o.d.id, rec)
					}
				case "defer":
					if rec != nil {
						t.Fatalf("%s was deferred and must have no record, got %+v", o.d.id, rec)
					}
				}
			}
		})
	}
}

// Control (passes before and after): dispatches of different secrets, and of one
// secret name under different providers, never touch each other's records.
func TestFS61_DifferentSecretsAndProviders_AreIndependent(t *testing.T) {
	r := newIsolationRig(t, map[string][]string{"ctr-a": {"db", "api"}})
	a := r.dispatch(t, "vault", "db", syntheticV2)
	b := r.dispatch(t, "vault", "api", syntheticV2)
	c := r.dispatch(t, "aws", "db", syntheticV3)
	a.finish(nil)
	b.finish(errors.New("simulated failure"))
	if rec := recordOf(r.st, c.id); rec == nil || rec.Status != "in_progress" || rec.ProviderName != "aws" {
		t.Fatalf("aws:db record disturbed: %+v", rec)
	}
	if rec := recordOf(r.st, a.id); rec == nil || rec.Status != "completed" {
		t.Fatalf("vault:db: %+v", rec)
	}
	if rec := recordOf(r.st, b.id); rec == nil || rec.Status != "rollback_required" {
		t.Fatalf("vault:api: %+v", rec)
	}
	c.finish(nil)
}

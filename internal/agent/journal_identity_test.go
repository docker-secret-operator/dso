package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// State-level tests of the operation-identity journal (F-S6-1). The dispatch-level
// regressions are in journal_isolation_test.go.

func newIDTracker(t *testing.T) (*StateTracker, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return st, dir
}

func rec(t *testing.T, st *StateTracker, id, svc, strategy string, orig []string, tr string) {
	t.Helper()
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: id, ServiceID: svc, Strategy: strategy, OriginalIDs: orig, Transition: tr,
	}); err != nil {
		t.Fatalf("Record %s %s: %v", id, tr, err)
	}
}

func statusOf(st *StateTracker, id string) string {
	if r := st.recordByID(id); r != nil {
		return r.Status
	}
	return "<absent>"
}

// Two services consuming the same secret, dispatched separately and journalled
// concurrently: each record keeps its own service, strategy and transitions.
func TestIdentity_TwoServicesSameSecret_Concurrent(t *testing.T) {
	st, dir := newIDTracker(t)
	for _, id := range []string{"rot-a", "rot-b"} {
		if err := st.StartRotationWithID(id, "vault", "db", "ctr-a,ctr-b", ""); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	run := func(id, svc, strategy string, orig string, steps []string) {
		defer wg.Done()
		for _, tr := range steps {
			if err := st.Record(context.Background(), rotation.JournalEvent{
				RotationID: id, ServiceID: svc, Strategy: strategy, OriginalIDs: []string{orig}, Transition: tr,
			}); err != nil {
				t.Errorf("%s %s: %v", id, tr, err)
			}
		}
	}
	wg.Add(2)
	go run("rot-a", "svc-a", "rolling", "ctr-a", lbSteps("rolling"))
	go run("rot-b", "svc-b", "signal", "ctr-b", lbSteps("signal"))
	wg.Wait()
	a, b := st.recordByID("rot-a"), st.recordByID("rot-b")
	if a.ServiceID != "svc-a" || a.Strategy != "rolling" || a.Transition != rotation.TrCompleted || a.Status != "completed" {
		t.Fatalf("rot-a: %+v", a)
	}
	if b.ServiceID != "svc-b" || b.Strategy != "signal" || b.Transition != rotation.TrSignalDone || b.Status != "completed" {
		t.Fatalf("rot-b: %+v", b)
	}
	st2, _ := NewStateTracker(dir, zap.NewNop())
	if len(st2.snapshotRecords()) != 2 {
		t.Fatalf("persisted %d records, want 2", len(st2.snapshotRecords()))
	}
}

// Same secret name in different projects (different container ids) and under
// different providers (same ids): four independent records.
func TestIdentity_ProjectsAndProviders_AreIndependent(t *testing.T) {
	st, _ := newIDTracker(t)
	_ = st.StartRotationWithID("p1", "vault", "db", "proj1-ctr", "")
	_ = st.StartRotationWithID("p2", "vault", "db", "proj2-ctr", "")
	_ = st.StartRotationWithID("p3", "aws", "db", "proj1-ctr", "")
	_ = st.StartRotationWithID("p4", "vault", "db", "proj1-ctr", "") // same triple as p1
	if err := st.CompleteRotationID("p1"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkRollbackID("p3"); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteStateID("p2"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"p1": "completed", "p2": "<absent>", "p3": "rollback_required", "p4": "in_progress"}
	for id, w := range want {
		if got := statusOf(st, id); got != w {
			t.Errorf("%s: %s, want %s", id, got, w)
		}
	}
}

// A completes before B, and B before A: each finishes only its own record.
func TestIdentity_CompletionOrder_BothWays(t *testing.T) {
	for _, order := range [][2]string{{"A", "B"}, {"B", "A"}} {
		st, _ := newIDTracker(t)
		_ = st.StartRotationWithID("A", "vault", "db", "o", "")
		_ = st.StartRotationWithID("B", "vault", "db", "o", "")
		_ = st.CompleteRotationID(order[0])
		if got := statusOf(st, order[1]); got != "in_progress" {
			t.Fatalf("order %v: completing %s touched %s (%s)", order, order[0], order[1], got)
		}
		_ = st.CompleteRotationID(order[1])
		if statusOf(st, "A") != "completed" || statusOf(st, "B") != "completed" {
			t.Fatalf("order %v: A=%s B=%s", order, statusOf(st, "A"), statusOf(st, "B"))
		}
	}
}

// A fails while B succeeds; A rolls back while B is still in progress.
func TestIdentity_FailureAndRollbackAreScopedToTheOperation(t *testing.T) {
	st, _ := newIDTracker(t)
	_ = st.StartRotationWithID("A", "vault", "db", "o", "")
	_ = st.StartRotationWithID("B", "vault", "db", "o", "")
	_ = st.MarkRollbackID("A")
	if statusOf(st, "B") != "in_progress" {
		t.Fatalf("B touched by A's rollback: %s", statusOf(st, "B"))
	}
	_ = st.CompleteRotationID("B")
	if statusOf(st, "A") != "rollback_required" || statusOf(st, "B") != "completed" {
		t.Fatalf("A=%s B=%s", statusOf(st, "A"), statusOf(st, "B"))
	}
	// A critical_error is never downgraded or completed by a stale outcome call.
	_ = st.StartRotationWithID("C", "vault", "db", "o", "")
	_ = st.MarkCriticalErrorID("C", "operator needed")
	_ = st.MarkRollbackID("C")
	_ = st.CompleteRotationID("C")
	if statusOf(st, "C") != "critical_error" {
		t.Fatalf("critical_error changed to %s", statusOf(st, "C"))
	}
	if err := st.DeleteStateID("C"); err == nil || statusOf(st, "C") != "critical_error" {
		t.Fatalf("critical_error record must not be deletable by id: %v", err)
	}
}

// Unknown, empty and reused identities fail closed and modify nothing.
func TestIdentity_UnknownEmptyAndReusedIDsFailClosed(t *testing.T) {
	st, dir := newIDTracker(t)
	_ = st.StartRotationWithID("A", "vault", "db", "o", "")
	before, _ := os.ReadFile(dir + "/rotations.json")
	for name, err := range map[string]error{
		"complete unknown": st.CompleteRotationID("nope"),
		"rollback unknown": st.MarkRollbackID("nope"),
		"delete unknown":   st.DeleteStateID("nope"),
		"recover unknown":  st.MarkRecoveredID("nope"),
		"critical unknown": st.MarkCriticalErrorID("nope", "x"),
		"complete empty":   st.CompleteRotationID(""),
		"start reused":     st.StartRotationWithID("A", "other", "secret", "x", ""),
	} {
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if !errors.Is(st.CompleteRotationID("nope"), ErrRotationNotFound) {
		t.Error("unknown id must report ErrRotationNotFound")
	}
	if !errors.Is(st.StartRotationWithID("A", "x", "y", "z", ""), ErrRotationIDInUse) {
		t.Error("a reused id must report ErrRotationIDInUse")
	}
	after, _ := os.ReadFile(dir + "/rotations.json")
	if string(before) != string(after) {
		t.Fatal("a refused call modified the journal")
	}
	if r := st.recordByID("A"); r.ProviderName != "vault" || r.SecretName != "db" || r.Status != "in_progress" {
		t.Fatalf("A altered: %+v", r)
	}
}

// A stale snapshot cannot resolve or flag a newer state of its own record.
func TestIdentity_StaleSnapshotsAreRefused(t *testing.T) {
	st, _ := newIDTracker(t)
	_ = st.StartRotationWithID("A", "vault", "db", "o", "")
	stale := st.snapshotRecords()[0] // in_progress, seq 0
	rec(t, st, "A", "svc", "signal", []string{"o"}, rotation.TrSignalDone)
	_ = st.CompleteRotationID("A")
	if err := st.MarkRecoveredRecord(stale); err == nil {
		t.Fatal("stale snapshot resolved a completed record")
	}
	if err := st.MarkCriticalRecord(stale, "x"); err == nil {
		t.Fatal("stale snapshot flagged a completed record")
	}
	if got := statusOf(st, "A"); got != "completed" {
		t.Fatalf("A = %s", got)
	}
	// A fresh snapshot may (recovery's own use).
	if err := st.MarkRecoveredRecord(st.snapshotRecords()[0]); err != nil {
		t.Fatal(err)
	}
}

// Wrong owner: an event from another owner is refused before anything is merged.
func TestIdentity_ForeignOwnerEventRefused(t *testing.T) {
	st, _ := newIDTracker(t)
	ctx := context.Background()
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "A", OwnerID: "owner-1", ServiceID: "svc", Strategy: "rolling", Transition: rotation.TrIntentCreateCandidate}); err != nil {
		t.Fatal(err)
	}
	before := st.recordByID("A")
	err := st.Record(ctx, rotation.JournalEvent{RotationID: "A", OwnerID: "owner-2", ServiceID: "evil", Strategy: "signal", Transition: rotation.TrCompleted})
	if err == nil {
		t.Fatal("a foreign owner's event was accepted")
	}
	after := st.recordByID("A")
	if after.ServiceID != before.ServiceID || after.Seq != before.Seq || after.Status != "in_progress" || after.OwnerID != "owner-1" {
		t.Fatalf("record modified by a refused event: %+v", after)
	}
}

// Crash with several unresolved operations of one secret: every one is preserved,
// each still blocks its service, recovery's view is complete and ordered.
func TestIdentity_CrashWithMultipleUnresolvedOperations(t *testing.T) {
	st, dir := newIDTracker(t)
	for i, svc := range []string{"svc-a", "svc-b", "svc-c"} {
		id := fmt.Sprintf("rot-%d", i)
		if err := st.StartRotationWithID(id, "vault", "db", "ctr-"+svc, ""); err != nil {
			t.Fatal(err)
		}
		rec(t, st, id, svc, "rolling", []string{"ctr-" + svc}, rotation.TrIntentHandoff)
	}
	// A fourth dispatch of the same secret after the crash window: must not displace any.
	st2, _ := NewStateTracker(dir, zap.NewNop()) // "restart": 3 inherited in_progress
	if err := st2.StartRotationWithID("rot-new", "vault", "db", "ctr-svc-a", ""); err != nil {
		t.Fatal(err)
	}
	un := st2.GetUnfinishedRotationsAtStartup()
	if len(un) != 4 {
		t.Fatalf("unfinished = %v", ids(un))
	}
	g := newJournalGuard(st2)
	for _, svc := range []string{"svc-a", "svc-b", "svc-c"} {
		if _, blocked := g.RotationBlocked(svc); !blocked {
			t.Fatalf("%s is no longer blocked", svc)
		}
	}
	for i := 0; i < 50; i++ {
		got := ids(st2.GetUnfinishedRotationsAtStartup())
		again := ids(st2.GetUnfinishedRotationsAtStartup())
		if fmt.Sprint(got) != fmt.Sprint(again) {
			t.Fatalf("unstable order %v vs %v", got, again)
		}
	}
}

// Recovery's record selection and the snapshot order are independent of Go's map
// iteration order and of the order the records were created in (seeded shuffles).
func TestIdentity_RecoveryDeterministicAcrossInsertionOrders(t *testing.T) {
	type spec struct {
		id     string
		orig   string
		cand   string
		status string // completed | in_progress
		steps  []string
	}
	specs := []spec{
		{"rot-r0", "o0", "o1", "completed", []string{rotation.TrIntentRenameOriginal, rotation.TrCandidateReady, rotation.TrCompleted}},
		{"rot-r1", "o1", "c1", "in_progress", []string{rotation.TrIntentRenameOriginal, rotation.TrCandidateReady}},
		{"rot-r2", "o1", "c2", "in_progress", []string{rotation.TrIntentRenameOriginal}},
		{"rot-r3", "o1", "o1", "completed", []string{rotation.TrIntentSignalPublish, rotation.TrSignalDone}},
	}
	var wantSnap string
	for _, seed := range []int64{1, 2, 3, 4, 5, 6, 7, 8} {
		st, _ := newIDTracker(t)
		order := rand.New(rand.NewSource(seed)).Perm(len(specs))
		for _, i := range order {
			sp := specs[i]
			_ = st.StartRotationWithID(sp.id, "vault", "db", sp.orig, "")
			for _, tr := range sp.steps {
				ev := rotation.JournalEvent{RotationID: sp.id, ServiceID: "svc", Strategy: "restart", OriginalIDs: []string{sp.orig}, Transition: tr}
				if sp.cand != sp.orig {
					ev.CandidateIDs = []string{sp.cand}
				}
				if err := st.Record(context.Background(), ev); err != nil {
					t.Fatal(err)
				}
			}
		}
		ar := &AutomaticRecovery{st: st}
		for i := 0; i < 40; i++ {
			got := ar.journalRecordFor("o1", nil)
			if got == nil || got.Status == "completed" {
				t.Fatalf("seed %d iter %d: an unresolved record must win, got %+v", seed, i, got)
			}
			if again := ar.journalRecordFor("o1", nil); again.RotationID != got.RotationID {
				t.Fatalf("seed %d: unstable selection %s vs %s", seed, got.RotationID, again.RotationID)
			}
		}
		var snap []string
		for _, r := range st.snapshotRecords() {
			snap = append(snap, r.RotationID)
		}
		if seed == 1 {
			wantSnap = fmt.Sprint(snap)
		} else if fmt.Sprint(snap) != wantSnap {
			t.Fatalf("snapshot order depends on insertion order: %v vs %v", snap, wantSnap)
		}
	}
}

// Repeated recovery is idempotent: the second run changes nothing.
func TestIdentity_RepeatedRecoveryIsIdempotent(t *testing.T) {
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1.41/containers/json" {
			return &http.Response{StatusCode: 200, Body: jsonBody([]container.Summary{})}, nil
		}
		return &http.Response{StatusCode: 404, Body: emptyBody()}, nil
	})
	dir := t.TempDir()
	a, _ := NewStateTracker(dir, zap.NewNop())
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("rot-%d", i)
		_ = a.StartRotationWithID(id, "vault", "db", fmt.Sprintf("o%d", i), "")
		rec(t, a, id, fmt.Sprintf("svc-%d", i), "rolling", []string{fmt.Sprintf("o%d", i)}, rotation.TrIntentCreateCandidate)
	}
	st, _ := NewStateTracker(dir, zap.NewNop())
	run := func() {
		ar := NewAutomaticRecovery(cli, zap.NewNop(), st)
		_ = ar.RecoverFromCrash(context.Background())
	}
	statuses := func() string {
		var out []string
		for _, r := range st.snapshotRecords() {
			out = append(out, r.RotationID+"="+r.Status)
		}
		return fmt.Sprint(out)
	}
	run()
	if len(st.snapshotRecords()) != 3 {
		t.Fatalf("records dropped or added by recovery: %d", len(st.snapshotRecords()))
	}
	s1 := statuses()
	run()
	if s2 := statuses(); s1 != s2 || len(st.snapshotRecords()) != 3 {
		t.Fatalf("recovery is not idempotent: %s then %s", s1, s2)
	}
}

// The guard stays effective: an unresolved operation of a service blocks it, and a
// new dispatch or a manual (journal-only) operation of the same service neither
// lifts nor replaces that block.
func TestIdentity_GuardStillBlocksAcrossManualAndAutomatic(t *testing.T) {
	st, _ := newIDTracker(t)
	_ = st.StartRotationWithID("auto-1", "vault", "db", "ctr", "")
	rec(t, st, "auto-1", "svc", "signal", []string{"ctr"}, rotation.TrIntentSignalPublish)
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "auto-1", ServiceID: "svc", Strategy: "signal", Transition: rotation.TrSignalFailed,
		State: "partially_published", ManualRecovery: true,
	}); err != nil {
		t.Fatal(err)
	}
	g := newJournalGuard(st)
	if _, b := g.RotationBlocked("svc"); !b {
		t.Fatal("precondition: critical_error blocks")
	}
	_ = st.StartRotationWithID("auto-2", "vault", "db", "ctr", "")
	_ = st.DeleteStateID("auto-2")                                                              // the refused dispatch cleans up
	rec(t, st, "manual-1", "svc", "rolling", []string{"ctr"}, rotation.TrIntentCreateCandidate) // manual, journal-only
	if _, b := g.RotationBlocked("svc"); !b {
		t.Fatal("a later dispatch or a manual operation lifted the block")
	}
	if statusOf(st, "auto-1") != "critical_error" {
		t.Fatalf("auto-1 = %s", statusOf(st, "auto-1"))
	}
}

// Concurrent journalling from many operations while the ledger snapshot is taken:
// no data race (-race), every snapshot is internally consistent.
func TestIdentity_ConcurrentJournalAndLedgerSnapshot(t *testing.T) {
	st, _ := newIDTracker(t)
	const n = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var rd sync.WaitGroup
	rd.Add(1)
	go func() {
		defer rd.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			snap := JournalSnapshotFromTracker(st, LedgerReconcileInput{Now: time.Now()})
			seen := map[string]bool{}
			for _, r := range snap.Records {
				if seen[r.RotationID] {
					t.Errorf("duplicate rotation id %s in one snapshot", r.RotationID)
				}
				seen[r.RotationID] = true
			}
		}
	}()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("rot-%d", i)
			_ = st.StartRotationWithID(id, "vault", "db", "ctr", "")
			for _, tr := range lbSteps("rolling") {
				_ = st.Record(context.Background(), rotation.JournalEvent{RotationID: id, ServiceID: fmt.Sprintf("svc-%d", i), Strategy: "rolling", Transition: tr})
			}
			_ = st.CompleteRotationID(id)
		}(i)
	}
	wg.Wait()
	close(stop)
	rd.Wait()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("rot-%d", i)
		if r := st.recordByID(id); r == nil || r.Status != "completed" || r.ServiceID != fmt.Sprintf("svc-%d", i) {
			t.Fatalf("%s: %+v", id, r)
		}
	}
}

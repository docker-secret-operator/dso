package agent

import (
	"net/http"
	"time"

	"context"
	"github.com/docker/docker/api/types/container"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Regression (docs/audit/28-STATETRACKER-IDENTITY-REVIEW.md, finding F1):
// StartRotationWithID used to assign st.states[provider:secret:originals] blindly.
// A trigger-engine record that held an UNRESOLVED operation (critical_error, e.g. a
// partially published signal rotation) was therefore replaced by the next dispatch
// of the same secret BEFORE the operation guard ran, and the guard then saw only a
// fresh in_progress record of the running process: the block was silently lifted.
func TestStartRotation_DoesNotEraseUnresolvedRecordOfSameKey(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartRotationWithID("rot-1", "vault", "db", "orig", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "rot-1", ServiceID: "svc", Strategy: "signal", OriginalIDs: []string{"orig"},
		Transition: rotation.TrSignalFailed, State: "partially_published", ManualRecovery: true,
	}); err != nil {
		t.Fatal(err)
	}
	g := newJournalGuard(st)
	if _, b := g.RotationBlocked("svc"); !b {
		t.Fatal("precondition: critical_error must block")
	}

	// Next poll of the same secret: same provider/secret/original container.
	if err := st.StartRotationWithID("rot-2", "vault", "db", "orig", ""); err != nil {
		t.Fatal(err)
	}
	if _, b := g.RotationBlocked("svc"); !b {
		t.Fatal("a new dispatch erased the unresolved rot-1 record: the guard no longer blocks")
	}
	// The dispatch is then refused by the guard and finishRotation deletes ITS record
	// by key; that must not take rot-1 with it.
	if err := st.DeleteState("vault", "db", "orig"); err != nil {
		t.Fatal(err)
	}
	if _, b := g.RotationBlocked("svc"); !b {
		t.Fatal("deleting the refused dispatch's record erased the unresolved record")
	}
	// Survives a restart, still critical_error, with its identity.
	st2, _ := NewStateTracker(dir, zap.NewNop())
	var found bool
	for _, r := range st2.snapshotRecords() {
		if r.RotationID == "rot-1" && r.Status == "critical_error" {
			found = true
		}
	}
	if !found {
		t.Fatal("unresolved rot-1 not in the persisted journal")
	}
}

// Same for a record inherited in_progress from a previous process.
func TestStartRotation_DoesNotEraseInheritedInProgressRecord(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, zap.NewNop())
	if err := st.StartRotationWithID("rot-1", "vault", "db", "orig", ""); err != nil {
		t.Fatal(err)
	}
	st2, _ := NewStateTracker(dir, zap.NewNop()) // "restart": rot-1 is inherited in_progress
	g := newJournalGuard(st2)
	if _, b := g.RotationBlocked("orig"); !b {
		t.Fatal("precondition: inherited in_progress must block")
	}
	_ = st2.StartRotationWithID("rot-2", "vault", "db", "orig", "")
	_ = st2.DeleteState("vault", "db", "orig") // what finishRotation does for a refused dispatch
	if _, b := g.RotationBlocked("orig"); !b {
		t.Fatal("inherited unresolved record erased by a later dispatch")
	}
}

// A stale snapshot (rot-1) can never update a newer rotation (rot-2) of the same key.
func TestMarkRecord_StaleSnapshotCannotTouchNewerRotation(t *testing.T) {
	st, _ := NewStateTracker(t.TempDir(), zap.NewNop())
	_ = st.StartRotationWithID("rot-1", "vault", "db", "orig", "")
	stale := st.snapshotRecords()[0]
	_ = st.CompleteRotation("vault", "db", "orig")
	_ = st.StartRotationWithID("rot-2", "vault", "db", "orig", "")
	if err := st.MarkRecoveredRecord(stale); err == nil {
		t.Fatal("stale snapshot of rot-1 resolved a record")
	}
	if err := st.MarkCriticalRecord(stale, "x"); err == nil {
		t.Fatal("stale snapshot of rot-1 resolved a record")
	}
	for _, r := range st.snapshotRecords() {
		if r.RotationID == "rot-2" && r.Status != "in_progress" {
			t.Fatalf("rot-2 mutated by a stale snapshot: %s", r.Status)
		}
	}
}

// Accessors hand out independent deep copies.
func TestAccessors_ReturnIndependentDeepCopies(t *testing.T) {
	st, _ := NewStateTracker(t.TempDir(), zap.NewNop())
	_ = st.StartRotationWithID("rot-1", "vault", "db", "a,b", "")
	_ = st.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-1", CandidateIDs: []string{"c1"}, Transition: rotation.TrCandidateCreated})
	_ = st.MarkRollback("vault", "db", "a,b") // so GetPendingRotations also returns it
	mutate := func(r *RotationState) {
		r.Status, r.ErrorMessage, r.RotationID = "tampered", "tampered", "tampered"
		for i := range r.OriginalContainerIDs {
			r.OriginalContainerIDs[i] = "X"
		}
		for i := range r.CandidateContainerIDs {
			r.CandidateContainerIDs[i] = "X"
		}
	}
	for name, get := range map[string]func() []*RotationState{
		"snapshotRecords": st.snapshotRecords,
		"GetPending":      st.GetPendingRotations,
		"GetUnfinished":   st.GetUnfinishedRotationsAtStartup,
	} {
		got := get()
		if len(got) != 1 {
			t.Fatalf("%s: %d records", name, len(got))
		}
		mutate(got[0])
	}
	live := st.snapshotRecords()[0]
	if live.RotationID != "rot-1" || live.Status != "rollback_required" ||
		live.OriginalContainerIDs[0] != "a" || live.OriginalContainerIDs[1] != "b" || live.CandidateContainerIDs[0] != "c1" {
		t.Fatalf("live record changed through a returned copy: %+v", live)
	}
}

// Concurrent Record + recovery-style Mark*Record + snapshots: no race (-race) and the
// persisted file stays parseable.
func TestConcurrentRecordAndMarkRecord_PersistedFileStaysValid(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, zap.NewNop())
	_ = st.StartRotationWithID("rot-1", "vault", "db", "orig", "")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = st.Record(context.Background(), rotation.JournalEvent{RotationID: "rot-1", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c"}})
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				for _, r := range st.snapshotRecords() {
					_ = st.MarkCriticalRecord(r, "m")
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = st.GetUnfinishedRotationsAtStartup()
				_ = st.GetPendingRotations()
			}
		}()
	}
	wg.Wait()
	st2, _ := NewStateTracker(dir, zap.NewNop())
	if c, _ := st2.Corrupt(); c || len(st2.snapshotRecords()) != 1 {
		t.Fatalf("persisted journal damaged: corrupt=%v n=%d", c, len(st2.snapshotRecords()))
	}
}

// Regression (finding F2): ValidateStateOnStartup used to mark EVERY pending record
// older than 24h "recovered" by key, including critical_error (an operator-required
// operation) and an inherited in_progress record (an interrupted operation recovery
// has not yet examined). That silently released the dispatch guard and, for the
// in_progress case, skipped recovery altogether. Only an ordinary failed attempt
// (rollback_required: the strategy already restored the previous workload) may age out.
func TestValidateStateOnStartup_NeverDiscardsUnresolvedOperations(t *testing.T) {
	cli := newRecoveryTestClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1.41/containers/json" {
			return &http.Response{StatusCode: 200, Body: jsonBody([]container.Summary{})}, nil
		}
		return &http.Response{StatusCode: 204, Body: emptyBody()}, nil
	})
	backdate := func(st *StateTracker) {
		st.mu.Lock()
		for _, live := range st.states {
			live.StartTime = live.StartTime.Add(-48 * time.Hour)
			live.LastUpdate = live.LastUpdate.Add(-48 * time.Hour)
		}
		st.mu.Unlock()
	}

	// critical_error, started by the trigger engine (provider:secret:ids key).
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, zap.NewNop())
	_ = st.StartRotationWithID("rot-1", "vault", "db", "orig", "")
	_ = st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "rot-1", ServiceID: "svc", Strategy: "signal", OriginalIDs: []string{"orig"},
		Transition: rotation.TrSignalFailed, State: "partially_published", ManualRecovery: true,
	})
	backdate(st)
	NewAutomaticRecovery(cli, zap.NewNop(), st).ValidateStateOnStartup(context.Background())
	if _, b := newJournalGuard(st).RotationBlocked("svc"); !b {
		t.Fatal("a >24h critical_error record was discarded at startup: the guard no longer blocks")
	}

	// in_progress inherited from a previous process.
	dir2 := t.TempDir()
	a, _ := NewStateTracker(dir2, zap.NewNop())
	_ = a.StartRotationWithID("rot-2", "vault", "db", "orig", "")
	b, _ := NewStateTracker(dir2, zap.NewNop())
	backdate(b)
	NewAutomaticRecovery(cli, zap.NewNop(), b).ValidateStateOnStartup(context.Background())
	if len(b.GetUnfinishedRotationsAtStartup()) != 1 {
		t.Fatal("an inherited in_progress record >24h old was discarded before recovery could examine it")
	}
}

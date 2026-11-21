package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Batch A / step S1 (A-03, F-18): staged copy-on-write persistence of the StateTracker.
// Invariant under test: when persisting fails before the rename (the commit point),
// in-memory state equals the last successfully persisted state, and a returned error
// means the journal file is unchanged. A directory-fsync failure AFTER the rename is a
// durability outcome (logged), not a failed commit.
//
// Docker-free. The root-independent write fault is a directory at
// <stateDir>/rotations.json.tmp, which makes the temporary-file open fail.

func stagedTracker(t *testing.T, logger *zap.Logger) (*StateTracker, string) {
	t.Helper()
	dir := t.TempDir()
	if logger == nil {
		logger = zap.NewNop()
	}
	st, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	return st, dir
}

// breakWrites makes every later persist fail before the rename; the returned function
// repairs it.
func breakWrites(t *testing.T, dir string) (repair func()) {
	t.Helper()
	tmp := filepath.Join(dir, "rotations.json.tmp")
	_ = os.Remove(tmp)
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatalf("cannot install the write fault: %v", err)
	}
	return func() {
		if err := os.Remove(tmp); err != nil {
			t.Fatalf("cannot remove the write fault: %v", err)
		}
	}
}

type memSnap struct {
	states    string
	inherited string
}

func snapMem(t *testing.T, st *StateTracker) memSnap {
	t.Helper()
	st.mu.RLock()
	defer st.mu.RUnlock()
	b, err := json.Marshal(st.states)
	if err != nil {
		t.Fatal(err)
	}
	ib, err := json.Marshal(st.inherited)
	if err != nil {
		t.Fatal(err)
	}
	return memSnap{string(b), string(ib)}
}

func readJournal(t *testing.T, dir string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	return b
}

// seed writes a journal with: "a" (in_progress, becomes INHERITED after the reopen),
// "old" (completed, 30 days old) and "crit" (critical_error, service svc). It returns a
// tracker reopened from disk.
func seedStaged(t *testing.T, fresh bool) (*StateTracker, string) {
	t.Helper()
	st, dir := stagedTracker(t, nil)
	if err := st.StartRotationWithID("a", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRotationWithID("old", "p", "s2", "c3", "c4"); err != nil {
		t.Fatal(err)
	}
	if err := st.CompleteRotationID("old"); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "crit", ServiceID: "svc", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "state_unknown",
	}); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.states[opKey("old")].LastUpdate = time.Now().Add(-30 * 24 * time.Hour)
	err := st.persistState()
	st.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		return st, dir
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	re, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return re, dir
}

type stagedWriter struct {
	name  string
	fresh bool // fresh (non-reopened) tracker: record "a" is not inherited
	do    func(st *StateTracker) error
}

func stagedWriters() []stagedWriter {
	ctx := context.Background()
	snap := func(st *StateTracker, id string) *RotationState { return st.recordByID(id) }
	return []stagedWriter{
		{"StartRotationWithID", false, func(st *StateTracker) error { return st.StartRotationWithID("new", "p", "s3", "c5", "c6") }},
		{"Record existing", false, func(st *StateTracker) error {
			return st.Record(ctx, rotation.JournalEvent{RotationID: "a", Transition: rotation.TrIntentCreateCandidate})
		}},
		{"Record unknown id", false, func(st *StateTracker) error {
			return st.Record(ctx, rotation.JournalEvent{RotationID: "unknown", Transition: rotation.TrIntentCreateCandidate})
		}},
		{"Record supersedes critical", false, func(st *StateTracker) error {
			return st.Record(ctx, rotation.JournalEvent{RotationID: "a", ServiceID: "svc", Transition: rotation.TrCompleted})
		}},
		{"CompleteRotationID", false, func(st *StateTracker) error { return st.CompleteRotationID("a") }},
		{"MarkRollbackID", false, func(st *StateTracker) error { return st.MarkRollbackID("a") }},
		{"DeleteStateID", false, func(st *StateTracker) error { return st.DeleteStateID("a") }},
		{"MarkRecoveredID", false, func(st *StateTracker) error { return st.MarkRecoveredID("a") }},
		{"MarkCriticalErrorID", false, func(st *StateTracker) error { return st.MarkCriticalErrorID("a", "boom") }},
		{"MarkRecoveredRecord", false, func(st *StateTracker) error { return st.MarkRecoveredRecord(snap(st, "a")) }},
		{"MarkCriticalRecord", false, func(st *StateTracker) error { return st.MarkCriticalRecord(snap(st, "a"), "boom") }},
		{"CleanupOldStates", false, func(st *StateTracker) error { return st.CleanupOldStates(time.Hour) }},
		{"CompleteRotation (legacy)", true, func(st *StateTracker) error { return st.CompleteRotation("p", "s", "c1") }},
		{"MarkRollback (legacy)", true, func(st *StateTracker) error { return st.MarkRollback("p", "s", "c1") }},
		{"MarkRecovered (legacy)", true, func(st *StateTracker) error { return st.MarkRecovered("p", "s", "c1") }},
		{"MarkCriticalError (legacy)", true, func(st *StateTracker) error { return st.MarkCriticalError("p", "s", "c1", "boom") }},
		{"DeleteState (legacy)", true, func(st *StateTracker) error { return st.DeleteState("p", "s", "c1") }},
	}
}

func TestStagedPersist_FailedWriteLeavesMemoryAndFileUnchanged(t *testing.T) {
	for _, w := range stagedWriters() {
		t.Run(w.name, func(t *testing.T) {
			st, dir := seedStaged(t, w.fresh)
			beforeMem := snapMem(t, st)
			beforeFile := readJournal(t, dir)
			repair := breakWrites(t, dir)

			if err := w.do(st); err == nil {
				t.Fatal("the write was expected to fail")
			}
			if got := snapMem(t, st); got != beforeMem {
				t.Fatalf("memory changed by a failed write:\nbefore %+v\nafter  %+v", beforeMem, got)
			}
			if got := readJournal(t, dir); !bytes.Equal(got, beforeFile) {
				t.Fatal("the journal file changed although the write returned an error")
			}

			repair()
			// A later successful write must persist exactly the pre-failure state plus its own change:
			// the failed mutation is not smuggled in (also through Close).
			if err := st.Close(); err != nil {
				t.Fatalf("Close after repair: %v", err)
			}
			if got := readJournal(t, dir); !bytes.Equal(got, beforeFile) {
				t.Fatal("Close persisted something other than the last committed state")
			}
		})
	}
}

func TestStagedPersist_SuccessfulWritePublishesStagedState(t *testing.T) {
	for _, w := range stagedWriters() {
		t.Run(w.name, func(t *testing.T) {
			st, dir := seedStaged(t, w.fresh)
			if err := w.do(st); err != nil {
				// Some writers legitimately refuse (e.g. unknown id on a stale snapshot); none may here.
				t.Fatalf("write failed: %v", err)
			}
			re, err := NewStateTracker(dir, zap.NewNop())
			if err != nil {
				t.Fatal(err)
			}
			st.mu.RLock()
			got, _ := json.Marshal(st.states)
			st.mu.RUnlock()
			re.mu.RLock()
			want, _ := json.Marshal(re.states)
			re.mu.RUnlock()
			if string(got) != string(want) {
				t.Fatalf("memory differs from the committed file:\nmemory %s\nfile   %s", got, want)
			}
		})
	}
}

func TestStagedPersist_LaterWriteDoesNotPersistFailedMutation(t *testing.T) {
	st, dir := seedStaged(t, false)
	repair := breakWrites(t, dir)
	if err := st.MarkCriticalErrorID("a", "boom"); err == nil {
		t.Fatal("expected failure")
	}
	if err := st.StartRotationWithID("phantom-check", "p", "s9", "c9", "c10"); err == nil {
		t.Fatal("expected failure")
	}
	repair()
	if err := st.MarkRollbackID("a"); err != nil {
		t.Fatal(err)
	}
	re, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	a := re.recordByID("a")
	if a == nil || a.Status != "rollback_required" || a.ErrorMessage != "" {
		t.Fatalf("a failed MarkCriticalErrorID leaked into a later write: %+v", a)
	}
	if re.recordByID("phantom-check") != nil {
		t.Fatal("a failed StartRotation left a phantom record that a later write persisted")
	}
}

func TestStagedPersist_UnknownRotationIDLeavesNoRecordOnFailure(t *testing.T) {
	st, dir := seedStaged(t, false)
	repair := breakWrites(t, dir)
	err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "ghost", Transition: rotation.TrIntentCreateCandidate})
	if err == nil {
		t.Fatal("expected failure")
	}
	if st.recordByID("ghost") != nil {
		t.Fatal("a failed event for an unknown rotation id left a journal-only record in memory")
	}
	repair()
	// A refusal (foreign owner) leaves no record for an unknown id either.
	st.mu.Lock()
	st.states[opKey("owned")] = &RotationState{RotationID: "owned", OwnerID: "o1", Status: "in_progress", key: opKey("owned")}
	st.mu.Unlock()
	before := snapMem(t, st)
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "owned", OwnerID: "o2", Transition: rotation.TrIntentCreateCandidate}); err == nil {
		t.Fatal("a foreign owner must be refused")
	}
	if got := snapMem(t, st); got != before {
		t.Fatal("a refused event changed memory")
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "ghost2", DeliveredVersions: []string{"plain"}, Transition: rotation.TrSecretsDelivered}); err == nil {
		t.Fatal("a plaintext delivered version must be refused")
	}
	if st.recordByID("ghost2") != nil {
		t.Fatal("a refused event for an unknown rotation id left a record in memory")
	}
}

func TestStagedPersist_SupersedeLoopStaged(t *testing.T) {
	st, dir := seedStaged(t, false)
	beforeCrit := st.recordByID("crit")
	if beforeCrit == nil || beforeCrit.Status != "critical_error" {
		t.Fatalf("seed is not as expected: %+v", beforeCrit)
	}
	repair := breakWrites(t, dir)
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "a", ServiceID: "svc", Transition: rotation.TrCompleted}); err == nil {
		t.Fatal("expected failure")
	}
	if c := st.recordByID("crit"); c.Status != "critical_error" || c.Detail != beforeCrit.Detail {
		t.Fatalf("the supersede loop changed another record although the write failed: %+v", c)
	}
	repair()
	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "a", ServiceID: "svc", Transition: rotation.TrCompleted}); err != nil {
		t.Fatal(err)
	}
	if c := st.recordByID("crit"); c.Status != "recovered" {
		t.Fatalf("a successful write must supersede the critical record: %+v", c)
	}
}

func TestStagedPersist_DeleteRestoresInherited(t *testing.T) {
	st, dir := seedStaged(t, false)
	st.mu.RLock()
	was := st.inherited[opKey("a")]
	st.mu.RUnlock()
	if !was {
		t.Fatal("seed: a must be inherited")
	}
	repair := breakWrites(t, dir)
	if err := st.DeleteStateID("a"); err == nil {
		t.Fatal("expected failure")
	}
	st.mu.RLock()
	still := st.inherited[opKey("a")]
	st.mu.RUnlock()
	if !still || st.recordByID("a") == nil {
		t.Fatal("a failed delete must leave the record and its inherited flag")
	}
	repair()
	if err := st.DeleteStateID("a"); err != nil {
		t.Fatal(err)
	}
	st.mu.RLock()
	_, left := st.inherited[opKey("a")]
	st.mu.RUnlock()
	if left {
		t.Fatal("a committed delete must drop the inherited flag")
	}
}

func TestStagedPersist_SnapshotWrittenOnlyAfterSuccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(st *StateTracker, rec *RotationState) error
		want string
	}{
		{"recovered", func(st *StateTracker, rec *RotationState) error { return st.MarkRecoveredRecord(rec) }, "recovered"},
		{"critical", func(st *StateTracker, rec *RotationState) error { return st.MarkCriticalRecord(rec, "boom") }, "critical_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, dir := seedStaged(t, false)
			rec := st.recordByID("a")
			orig := *rec
			repair := breakWrites(t, dir)
			if err := tc.do(st, rec); err == nil {
				t.Fatal("expected failure")
			}
			if rec.Status != orig.Status || !rec.LastUpdate.Equal(orig.LastUpdate) || rec.ErrorMessage != orig.ErrorMessage {
				t.Fatalf("the caller's snapshot was written although the write failed: %+v", rec)
			}
			repair()
			if err := tc.do(st, rec); err != nil {
				t.Fatal(err)
			}
			if rec.Status != tc.want {
				t.Fatalf("the snapshot must be updated after success, got %q", rec.Status)
			}
		})
	}
}

func TestStagedPersist_CleanupOldStatesStaged(t *testing.T) {
	st, dir := seedStaged(t, false)
	before := snapMem(t, st)
	repair := breakWrites(t, dir)
	if err := st.CleanupOldStates(time.Hour); err == nil {
		t.Fatal("expected failure")
	}
	if got := snapMem(t, st); got != before {
		t.Fatal("a failed CleanupOldStates removed records from memory")
	}
	repair()
	if err := st.CleanupOldStates(time.Hour); err != nil {
		t.Fatal(err)
	}
	if st.recordByID("old") != nil {
		t.Fatal("the old completed record must be removed after a committed cleanup")
	}
	for _, id := range []string{"a", "crit"} {
		if st.recordByID(id) == nil {
			t.Fatalf("record %q outside the retention predicate was removed", id)
		}
	}
	st.mu.RLock()
	inh := st.inherited[opKey("a")]
	st.mu.RUnlock()
	if !inh {
		t.Fatal("CleanupOldStates must not change the inherited set")
	}
	// Nothing qualifies: no persist (no .tmp, no error even with the fault installed).
	repair = breakWrites(t, dir)
	defer repair()
	if err := st.CleanupOldStates(time.Hour); err != nil {
		t.Fatalf("a cleanup with nothing to remove must not write: %v", err)
	}
}

func TestStagedPersist_ConcurrentWritersNoLostUpdate(t *testing.T) {
	st, dir := stagedTracker(t, nil)
	const n = 40
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = st.snapshotRecords()
					_ = st.GetUnfinishedRotationsAtStartup()
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("r%02d", i)
			if err := st.StartRotationWithID(id, "p", "s", id, "n"); err != nil {
				t.Errorf("start %s: %v", id, err)
				return
			}
			if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: id, Transition: rotation.TrIntentCreateCandidate}); err != nil {
				t.Errorf("record %s: %v", id, err)
			}
			if i%2 == 0 {
				if err := st.CompleteRotationID(id); err != nil {
					t.Errorf("complete %s: %v", id, err)
				}
			}
		}(i)
	}
	wg.Wait()
	close(stop)
	readers.Wait()
	re, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("r%02d", i)
		r := re.recordByID(id)
		if r == nil {
			t.Fatalf("lost update: %s", id)
		}
		wantStatus := "in_progress"
		if i%2 == 0 {
			wantStatus = "completed"
		}
		if r.Status != wantStatus || r.Seq != 1 {
			t.Fatalf("%s: status %q seq %d, want %q seq 1", id, r.Status, r.Seq, wantStatus)
		}
	}
}

// A failure of the rename itself is a commit failure: the file is untouched, memory is
// unchanged, and the staged content stays in the temporary file.
func TestPersist_RenameIsTheCommitPoint(t *testing.T) {
	st, dir := seedStaged(t, false)
	before := snapMem(t, st)
	journal := filepath.Join(dir, "rotations.json")
	saved := readJournal(t, dir)
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory at the destination makes os.Rename fail.
	if err := os.MkdirAll(filepath.Join(journal, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCriticalErrorID("a", "boom"); err == nil {
		t.Fatal("expected the rename to fail")
	}
	if got := snapMem(t, st); got != before {
		t.Fatal("memory changed by a write whose rename failed")
	}
	if _, err := os.Stat(journal + ".tmp"); err != nil {
		t.Fatalf("the staged temporary file should remain after a failed rename: %v", err)
	}
	if err := os.RemoveAll(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCriticalErrorID("a", "boom"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("after a committed write the temporary file must be gone (renamed)")
	}
	if re, _ := NewStateTracker(dir, zap.NewNop()); re.recordByID("a").Status != "critical_error" {
		t.Fatal("the committed state is not on disk")
	}
}

func TestPersist_DirectorySyncFailureAfterRenameIsNotAFailedCommit(t *testing.T) {
	orig := syncDirFn
	t.Cleanup(func() { syncDirFn = orig })

	t.Run("other error: success, published, Warn", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		st, dir := stagedTracker(t, zap.New(core))
		syncDirFn = func(string) error { return errors.New("injected directory sync failure") }
		if err := st.StartRotationWithID("d1", "p", "s", "c", "n"); err != nil {
			t.Fatalf("a directory-fsync failure after the rename must not fail the commit: %v", err)
		}
		if st.recordByID("d1") == nil {
			t.Fatal("the staged state must be published after a committed rename")
		}
		re, err := NewStateTracker(dir, zap.NewNop())
		if err != nil || re.recordByID("d1") == nil {
			t.Fatal("the committed record is not in the file")
		}
		if logs.Len() != 1 {
			t.Fatalf("expected exactly one Warn, got %d", logs.Len())
		}
		e := logs.All()[0]
		if e.ContextMap()["state_dir"] != dir {
			t.Fatalf("the Warn must name the state directory: %v", e.ContextMap())
		}
	})

	for _, tc := range []struct {
		name string
		err  error
	}{{"EINVAL", syscall.EINVAL}, {"ENOTSUP", syscall.ENOTSUP}, {"wrapped EINVAL", fmt.Errorf("sync: %w", syscall.EINVAL)}} {
		t.Run(tc.name+": unsupported, silent", func(t *testing.T) {
			core, logs := observer.New(zapcore.WarnLevel)
			st, _ := stagedTracker(t, zap.New(core))
			syncDirFn = func(string) error { return tc.err }
			if err := st.StartRotationWithID("d2", "p", "s", "c", "n"); err != nil {
				t.Fatal(err)
			}
			if logs.Len() != 0 {
				t.Fatalf("an unsupported directory sync must not warn: %v", logs.All())
			}
		})
	}

	t.Run("failure before the rename is still a commit failure", func(t *testing.T) {
		st, dir := stagedTracker(t, nil)
		syncDirFn = orig
		breakWrites(t, dir)
		if err := st.StartRotationWithID("d3", "p", "s", "c", "n"); err == nil {
			t.Fatal("a pre-rename failure must be returned")
		}
		if st.recordByID("d3") != nil {
			t.Fatal("memory must be unchanged")
		}
	})
}

// Guard for the touch helper: the same live record touched twice in one stage yields
// the same clone, and a live record is never modified by a staged write.
func TestStagedPersist_TouchReturnsOneCloneAndLeavesLiveRecordUntouched(t *testing.T) {
	st, _ := seedStaged(t, false)
	st.mu.Lock()
	defer st.mu.Unlock()
	live := st.byID("a")
	tx := st.beginStage()
	c1 := tx.touch(live)
	c2 := tx.touch(live)
	if c1 == live || c1 != c2 || tx.touch(c1) != c1 {
		t.Fatal("touch must return one stable clone distinct from the live record")
	}
	c1.Status = "critical_error"
	if live.Status == "critical_error" || !reflect.DeepEqual(tx.states[live.key], c1) {
		t.Fatal("the live record must stay unchanged until commit")
	}
}

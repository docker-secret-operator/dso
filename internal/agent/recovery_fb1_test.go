package agent

import (
	"context"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/rotation"
	"go.uber.org/zap"
)

// F-B1: journalRecordFor used to return the FIRST record (Go map order) naming
// a container, terminal records included. After a completed restart R0
// (o0 -> o1) and an interrupted restart R1 (o1 -> c1), the container o1 is
// named by both, and recovery judged it against R0 or R1 at random. The choice
// is now deterministic: an unresolved record wins over a terminal one, then the
// latest update.

func fb1Tracker(t *testing.T) *StateTracker {
	t.Helper()
	st, err := NewStateTracker(t.TempDir(), zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func fb1Record(t *testing.T, st *StateTracker, rid, orig, cand string, steps []string) {
	t.Helper()
	ctx := context.Background()
	for i, tr := range steps {
		ev := rotation.JournalEvent{RotationID: rid, Strategy: "restart", Transition: tr}
		if i == 0 {
			ev.ServiceID, ev.OriginalIDs = "app", []string{orig}
		} else {
			ev.CandidateIDs = []string{cand}
		}
		if err := st.Record(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJournalRecordFor_PrefersUnresolvedOverCompleted_Deterministic(t *testing.T) {
	st := fb1Tracker(t)
	// R0 completed: o0 -> o1 (o1 is R0's candidate).
	fb1Record(t, st, "rot-r0", "o0", "o1", []string{rotation.TrIntentRenameOriginal, rotation.TrCandidateReady, rotation.TrCompleted})
	time.Sleep(2 * time.Millisecond)
	// R1 unresolved (inherited in_progress shape): o1 -> c1, stopped at candidate_ready.
	fb1Record(t, st, "rot-r1", "o1", "c1", []string{rotation.TrIntentRenameOriginal, rotation.TrCandidateReady})
	// Make R0 the MORE RECENT record: resolution status must still win.
	st.mu.Lock()
	for _, r := range st.states {
		if r.RotationID == "rot-r0" {
			r.LastUpdate = time.Now().Add(time.Hour)
		}
	}
	st.mu.Unlock()

	ar := &AutomaticRecovery{st: st}
	for i := 0; i < 500; i++ {
		got := ar.journalRecordFor("o1", nil)
		if got == nil || got.RotationID != "rot-r1" {
			t.Fatalf("iteration %d: got %+v, want the unresolved rot-r1", i, got)
		}
		// The rotation label names R0, the container ids name R1 too: still R1.
		got = ar.journalRecordFor("o1", map[string]string{rotation.LabelRotationID: "rot-r0"})
		if got == nil || got.RotationID != "rot-r1" {
			t.Fatalf("iteration %d (label): got %+v, want the unresolved rot-r1", i, got)
		}
	}
}

func TestJournalRecordFor_TerminalOnly_LatestWins(t *testing.T) {
	st := fb1Tracker(t)
	fb1Record(t, st, "rot-a", "o0", "o1", []string{rotation.TrIntentRenameOriginal, rotation.TrCompleted})
	time.Sleep(2 * time.Millisecond)
	fb1Record(t, st, "rot-b", "o1", "o2", []string{rotation.TrIntentRenameOriginal, rotation.TrCompleted})
	ar := &AutomaticRecovery{st: st}
	for i := 0; i < 500; i++ {
		if got := ar.journalRecordFor("o1", nil); got == nil || got.RotationID != "rot-b" {
			t.Fatalf("iteration %d: got %+v, want the latest rot-b", i, got)
		}
	}
	if got := ar.journalRecordFor("zz", nil); got != nil {
		t.Fatalf("no record names zz, got %+v", got)
	}
	if got := ar.journalRecordFor("zz", map[string]string{rotation.LabelRotationID: "rot-a"}); got == nil || got.RotationID != "rot-a" {
		t.Fatalf("label fallback: %+v", got)
	}
}

// End to end through the real AutomaticRecovery and the fake daemon (the S4b
// T-W5 "predecessor in journal" layout): the outcome used to flip between
// recovered and critical_error from run to run; it is now always recovered
// (the verified candidate serves; R1 is judged against its own record).
func TestRecovery_FB1_PredecessorInJournal_DeterministicOutcome(t *testing.T) {
	for i := 0; i < 20; i++ {
		d := newFakeDaemon(n21Original(false), n21Candidate(true, "healthy"))
		f := newRec(t, d, "-")
		fb1Record(t, f.st, "rot-r0", "o0", "o1", []string{rotation.TrIntentRenameOriginal, rotation.TrCandidateReady, rotation.TrCompleted})
		if err := f.st.CompleteRotation("vault", "db", "o0"); err != nil {
			t.Fatal(err)
		}
		n21Journal(t, f, rotation.TrCandidateReady)
		f.run()
		var status string
		for _, r := range f.st.snapshotRecords() {
			if r.RotationID == n21RID {
				status = r.Status
			}
		}
		if status != "recovered" {
			t.Fatalf("run %d: status %q, want recovered (deterministic)", i, status)
		}
	}
}

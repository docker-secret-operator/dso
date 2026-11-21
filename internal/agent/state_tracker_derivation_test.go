package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Batch A / step S3 (A-04, A-05, A-10; owner decisions of 20 November 2026): status
// derivation in Record, one manual-escalation predicate for TrFailed, TrSignalFailed
// and TrRolledBack, the cleanup-debt rule, and the recovery boundaries of an escalated
// record. Docker-free. The R-tests reuse the fixtures of recovery_safety_test.go.

func s3Tracker(t *testing.T) (*StateTracker, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	return st, dir
}

func s3Rec(t *testing.T, st *StateTracker, id string) *RotationState {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	r := st.byID(id)
	if r == nil {
		t.Fatalf("no record %q", id)
	}
	return r.clone()
}

func s3Start(t *testing.T, st *StateTracker, id string) {
	t.Helper()
	if err := st.StartRotationWithID(id, "p", "s", "orig-"+id, "cand-"+id); err != nil {
		t.Fatal(err)
	}
}

func s3Record(t *testing.T, st *StateTracker, e rotation.JournalEvent) {
	t.Helper()
	if err := st.Record(context.Background(), e); err != nil {
		t.Fatalf("Record(%s): %v", e.Transition, err)
	}
}

func s3Begin(t *testing.T, st *StateTracker, id, strategy, target string) int {
	t.Helper()
	n, err := st.BeginAttempt(context.Background(), AttemptBegin{RotationID: id, Strategy: strategy, TargetOriginalID: target, ServiceName: "svc"})
	if err != nil {
		t.Fatalf("BeginAttempt: %v", err)
	}
	return n
}

// s3Break makes every later persist fail before the rename; the returned function
// repairs it.
func s3Break(t *testing.T, dir string) (repair func()) {
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

// Tests 1 to 6 and 24: the three manual transitions escalate, for legacy records and
// attempt-bearing records.
func TestS3Escalation_ManualEvents_LegacyAndAttemptBearing(t *testing.T) {
	cases := []struct{ name, transition, state, wantMsg string }{
		{"TrFailed", rotation.TrFailed, "", "manual recovery required"},
		{"TrRolledBack", rotation.TrRolledBack, "", "manual recovery required"},
		{"TrSignalFailed", rotation.TrSignalFailed, "partially_published",
			"signal rotation ended in state partially_published; manual recovery required"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/legacy", func(t *testing.T) {
			st, _ := s3Tracker(t)
			s3Start(t, st, "r1")
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: tc.transition, State: tc.state, ManualRecovery: true})
			got := s3Rec(t, st, "r1")
			if got.Status != "critical_error" || !got.ManualRecovery || got.Transition != tc.transition {
				t.Fatalf("status %q manual %v transition %q, want critical_error / true / %s", got.Status, got.ManualRecovery, got.Transition, tc.transition)
			}
			if got.ErrorMessage != tc.wantMsg {
				t.Fatalf("ErrorMessage = %q, want %q", got.ErrorMessage, tc.wantMsg)
			}
			if len(got.Attempts) != 0 {
				t.Fatalf("a legacy record must stay without attempts, got %+v", got.Attempts)
			}
		})
		t.Run(tc.name+"/attempt-bearing", func(t *testing.T) {
			st, _ := s3Tracker(t)
			s3Start(t, st, "r1")
			n := s3Begin(t, st, "r1", "rolling", "orig-r1")
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: tc.transition, State: tc.state,
				ManualRecovery: true, ManualReason: rotation.ManualReasonRollbackFailed})
			got := s3Rec(t, st, "r1")
			if got.Status != "critical_error" || got.ErrorMessage != tc.wantMsg {
				t.Fatalf("status %q message %q, want critical_error / %q", got.Status, got.ErrorMessage, tc.wantMsg)
			}
			if len(got.Attempts) != 1 {
				t.Fatalf("attempts = %+v", got.Attempts)
			}
			a := got.Attempts[0]
			if a.State != "escalated" || a.ManualReason != rotation.ManualReasonRollbackFailed || a.EndedSeq != got.Seq {
				t.Fatalf("attempt = %+v (record seq %d), want escalated with the reason stored in the same write", a, got.Seq)
			}
		})
	}
}

// Test 7: the wording uses the incoming event's own State and Detail, never values
// merged from earlier events.
func TestS3Escalation_Wording_UsesTheIncomingEventsOwnValues(t *testing.T) {
	cases := []struct{ name, state, detail, want string }{
		{"state", "layout_unknown", "", "manual recovery required: layout_unknown"},
		{"detail only", "", "the original was not removed", "manual recovery required: the original was not removed"},
		{"state wins over detail", "st", "dt", "manual recovery required: st"},
		{"neither", "", "", "manual recovery required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, _ := s3Tracker(t)
			s3Start(t, st, "r1")
			// An earlier event leaves a State and a Detail on the record; they must not leak.
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrIntentCreateCandidate,
				State: "earlier-state", Detail: "earlier-detail"})
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrFailed, ManualRecovery: true,
				State: tc.state, Detail: tc.detail})
			if got := s3Rec(t, st, "r1"); got.ErrorMessage != tc.want {
				t.Fatalf("ErrorMessage = %q, want %q", got.ErrorMessage, tc.want)
			}
		})
	}
}

// A manual escalation always sets the approved wording from the incoming event's own
// State and Detail, even over an explanation already on a critical_error record (the
// automatic rollback marks the record with a detailed message and then journals a manual
// rolled_back event). The diagnostic text stays in Detail. A signal escalation carries
// its signal-specific text.
func TestS3Escalation_ExistingExplanationOnACriticalRecordIsReplacedByTheApprovedWording(t *testing.T) {
	st, _ := s3Tracker(t)
	s3Start(t, st, "r1")
	if err := st.MarkCriticalRecord(s3Rec(t, st, "r1"), "a detailed operator explanation"); err != nil {
		t.Fatal(err)
	}
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrRolledBack, ManualRecovery: true, State: "some_state"})
	// The N21 shape: a detail-bearing manual rolled_back on an already-critical record. The
	// approved wording applies (from the event's own State); the diagnostic text lives in Detail.
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrRolledBack, ManualRecovery: true,
		State: "rollback_previous_unavailable", Detail: "full diagnostic text"})
	if got := s3Rec(t, st, "r1"); got.Status != "critical_error" ||
		got.ErrorMessage != "manual recovery required: rollback_previous_unavailable" || got.Detail != "full diagnostic text" {
		t.Fatalf("status %q message %q detail %q, want the approved wording with the diagnostic kept in Detail", got.Status, got.ErrorMessage, got.Detail)
	}
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "partially_published"})
	if got := s3Rec(t, st, "r1"); got.ErrorMessage != "signal rotation ended in state partially_published; manual recovery required" {
		t.Fatalf("a signal escalation keeps its own text, got %q", got.ErrorMessage)
	}
}

// Test 8: a plain TrFailed has no status mapping.
func TestS3Derivation_PlainTrFailedHasNoStatusMapping(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrFailed, Detail: "nothing was created"})
		if got := s3Rec(t, st, "r1"); got.Status != "in_progress" || got.ManualRecovery || got.ErrorMessage != "" {
			t.Fatalf("status %q manual %v message %q, want in_progress / false / empty", got.Status, got.ManualRecovery, got.ErrorMessage)
		}
		if err := st.MarkRollbackID("r1"); err != nil {
			t.Fatal(err)
		}
		if got := s3Rec(t, st, "r1"); got.Status != "rollback_required" {
			t.Fatalf("status after MarkRollbackID = %q, want rollback_required", got.Status)
		}
	})
	t.Run("attempt-bearing", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		n := s3Begin(t, st, "r1", "rolling", "orig-r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed})
		got := s3Rec(t, st, "r1")
		if got.Attempts[0].State != "failed" || got.Status != "in_progress" {
			t.Fatalf("attempt %q status %q, want failed / in_progress (plain failed carries the status forward)", got.Attempts[0].State, got.Status)
		}
	})
}

func s3Escalate(t *testing.T, st *StateTracker, id string) {
	t.Helper()
	s3Record(t, st, rotation.JournalEvent{RotationID: id, ServiceID: "svc", OriginalIDs: []string{"orig-1"}, CandidateIDs: []string{"new-1"},
		Transition: rotation.TrFailed, ManualRecovery: true, Detail: "operator action required"})
}

// Tests 9 to 11: the escalated record blocks the service through the real guard, is not
// lowered by the engine's rollback verdict, and keeps blocking after a reload.
func TestS3Escalation_GuardBlocksAndSurvivesVerdictAndReload(t *testing.T) {
	st, dir := s3Tracker(t)
	s3Start(t, st, "r1")
	s3Escalate(t, st, "r1")
	g := newJournalGuard(st)
	for _, key := range []string{"svc", "orig-1", "new-1"} {
		if _, blocked := g.RotationBlocked(key); !blocked {
			t.Fatalf("key %q should be blocked by the escalated record", key)
		}
	}
	if _, blocked := g.RotationBlocked("unrelated"); blocked {
		t.Fatal("an unrelated service must not be blocked")
	}

	if err := st.MarkRollbackID("r1"); err != nil {
		t.Fatal(err)
	}
	if got := s3Rec(t, st, "r1"); got.Status != "critical_error" {
		t.Fatalf("MarkRollbackID lowered the escalation to %q", got.Status)
	}

	reloaded, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if got := s3Rec(t, reloaded, "r1"); got.Status != "critical_error" {
		t.Fatalf("after a reload the status is %q", got.Status)
	}
	if _, blocked := newJournalGuard(reloaded).RotationBlocked("svc"); !blocked {
		t.Fatal("after a reload the service must still be blocked")
	}
}

// Test 12 (A-10, option O-b): the debt rule.
func TestS3Debt_CompletedWithDebtStateKeepsStatusAndMarksTheCleanup(t *testing.T) {
	st, _ := s3Tracker(t)
	s3Start(t, st, "debt")
	s3Record(t, st, rotation.JournalEvent{RotationID: "debt", Transition: rotation.TrIntentCleanup})
	s3Record(t, st, rotation.JournalEvent{RotationID: "debt", Transition: rotation.TrCompleted, State: "completed_cleanup_debt"})
	got := s3Rec(t, st, "debt")
	if got.Status != "completed" || got.CleanupStatus != "debt" || got.OperationState != "completed_cleanup_debt" {
		t.Fatalf("status %q cleanup %q state %q, want completed / debt / completed_cleanup_debt", got.Status, got.CleanupStatus, got.OperationState)
	}
	if !candidateVerified(got.Transition) {
		t.Fatalf("a debt completion (transition %q) must still count as delivery evidence", got.Transition)
	}

	s3Start(t, st, "normal")
	s3Record(t, st, rotation.JournalEvent{RotationID: "normal", Transition: rotation.TrIntentCleanup})
	s3Record(t, st, rotation.JournalEvent{RotationID: "normal", Transition: rotation.TrCompleted})
	if got := s3Rec(t, st, "normal"); got.Status != "completed" || got.CleanupStatus != "done" {
		t.Fatalf("a normal completion: status %q cleanup %q, want completed / done", got.Status, got.CleanupStatus)
	}
}

// Test 22: the ordered derivation rules (pure function).
func TestS3Derivation_RuleOrder(t *testing.T) {
	open := AttemptState{Attempt: 1, State: "open"}
	done := AttemptState{Attempt: 1, State: "completed"}
	esc := AttemptState{Attempt: 1, State: "escalated"}
	cases := []struct {
		name     string
		prev     string
		attempts []AttemptState
		manual   bool
		terminal string
		want     string
	}{
		{"manual dominates an open attempt", "in_progress", []AttemptState{open}, true, "", "critical_error"},
		{"an escalated attempt gives critical_error", "in_progress", []AttemptState{esc}, false, "completed", "critical_error"},
		{"sticky critical_error beats a terminal event", "critical_error", nil, false, "completed", "critical_error"},
		{"sticky critical_error beats an open attempt", "critical_error", []AttemptState{open}, false, "", "critical_error"},
		{"an open attempt keeps in_progress over a terminal event", "recovered", []AttemptState{open}, false, "completed", "in_progress"},
		{"a terminal event applies when no attempt is open", "in_progress", []AttemptState{done}, false, "completed", "completed"},
		{"a terminal recovered mapping", "in_progress", nil, false, "recovered", "recovered"},
		{"no event mapping carries the previous status forward", "rollback_required", nil, false, "", "rollback_required"},
		{"legacy record with a terminal event", "in_progress", nil, false, "completed", "completed"},
	}
	for _, tc := range cases {
		if got := deriveStatus(tc.prev, tc.attempts, tc.manual, tc.terminal); got != tc.want {
			t.Errorf("%s: deriveStatus = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Test 26 (S1 staging preserved): the new writers leave memory and file unchanged when
// the write fails, and the marker and the attempt appear together once it succeeds.
func TestS3Staging_NewWritersLeaveMemoryAndFileUnchangedOnFailure(t *testing.T) {
	st, dir := s3Tracker(t)
	s3Start(t, st, "r1")
	n := s3Begin(t, st, "r1", "rolling", "orig-r1")
	before := s3Rec(t, st, "r1")
	fileBefore, err := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if err != nil {
		t.Fatal(err)
	}
	repair := s3Break(t, dir)

	if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed,
		ManualRecovery: true, ManualReason: rotation.ManualReasonStateUnknown}); err == nil {
		t.Fatal("the escalating write was expected to fail")
	}
	if _, err := st.BeginAttempt(context.Background(), AttemptBegin{RotationID: "r1", Strategy: "restart", TargetOriginalID: "other"}); err == nil {
		t.Fatal("BeginAttempt was expected to fail")
	}
	after := s3Rec(t, st, "r1")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a failed write changed the record:\nbefore %+v\nafter  %+v", before, after)
	}
	fileAfter, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	if !bytes.Equal(fileBefore, fileAfter) {
		t.Fatal("a failed write changed the journal file")
	}
	repair()

	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed,
		ManualRecovery: true, ManualReason: rotation.ManualReasonStateUnknown})
	if got := s3Rec(t, st, "r1"); got.Status != "critical_error" || got.Attempts[0].State != "escalated" {
		t.Fatalf("after the fault is removed the same event must commit, got %+v", got)
	}
}

// Test 27: supersede at tracker level (it says nothing about the dispatch path).
func TestS3Supersede_TrackerLevel(t *testing.T) {
	ctx := context.Background()
	t.Run("a later clean terminal event of another record supersedes", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", ServiceID: "svc", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "partially_published"})
		s3Record(t, st, rotation.JournalEvent{RotationID: "r2", ServiceID: "svc", Transition: rotation.TrSignalDone})
		if got := s3Rec(t, st, "r1"); got.Status != "recovered" {
			t.Fatalf("r1 = %q, want recovered", got.Status)
		}
	})
	t.Run("the same record stays critical_error", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", ServiceID: "svc", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "partially_published"})
		if err := st.Record(ctx, rotation.JournalEvent{RotationID: "r1", ServiceID: "svc", Transition: rotation.TrSignalDone}); err != nil {
			t.Fatal(err)
		}
		if got := s3Rec(t, st, "r1"); got.Status != "critical_error" {
			t.Fatalf("same-record terminal event gave %q, want critical_error", got.Status)
		}
	})
	t.Run("a different ServiceID is untouched", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", ServiceID: "svc", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "partially_published"})
		s3Record(t, st, rotation.JournalEvent{RotationID: "r2", ServiceID: "other", Transition: rotation.TrSignalDone})
		if got := s3Rec(t, st, "r1"); got.Status != "critical_error" {
			t.Fatalf("r1 = %q, want critical_error", got.Status)
		}
	})
}

func s3OnlyRotationID(t *testing.T, st *StateTracker) string {
	t.Helper()
	st.mu.RLock()
	defer st.mu.RUnlock()
	for _, r := range st.states {
		return r.RotationID
	}
	t.Fatal("the tracker has no record")
	return ""
}

func s3EscalateRecovery(t *testing.T, f *recFixture, state string) string {
	t.Helper()
	rid := s3OnlyRotationID(t, f.st)
	s3Record(t, f.st, rotation.JournalEvent{RotationID: rid, Transition: rotation.TrFailed, ManualRecovery: true, State: state})
	if got := f.status(); got != "critical_error" {
		t.Fatalf("precondition: the record should be critical_error, got %s", got)
	}
	return rid
}

// R-1 is an INTERIM-BEHAVIOUR test, not a safety guarantee. MarkRecoveredRecord has no
// status guard today (pinned by the S0 characterization of it), so startup recovery can
// lower an escalated critical_error record on the strength of its holder checks alone:
// a verified healthy original under its real name resolves it. A-06 (S4, not approved)
// may change this, and any later change to this test needs its own OD-X11 approval.
func TestS3Recovery_R1_EscalatedRecordWithAHealthyOriginalIsLoweredToday(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"))
	f := newRec(t, d, "o1")
	s3EscalateRecovery(t, f, "")
	f.run()
	if got := f.status(); got != "recovered" {
		t.Fatalf("current behaviour: recovery lowers the record with a verified healthy original, got %s", got)
	}
	if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
		t.Fatalf("nothing may be removed, started or stopped: %+v", d)
	}
}

// R-2: an original not owned by this agent leaves the record escalated, with no mutation.
func TestS3Recovery_R2_OwnershipMismatchStaysEscalated(t *testing.T) {
	foreign := &fakeCtr{id: "o1", name: "app", owner: "owner-B", running: true, health: "healthy", upFor: time.Hour}
	d := newFakeDaemon(foreign, artifact("n1", "app_dso_new_1700000000"))
	f := newRec(t, d, "o1")
	s3EscalateRecovery(t, f, "")
	f.run()
	if got := f.status(); got != "critical_error" {
		t.Fatalf("status = %s, want critical_error", got)
	}
	if len(d.removed) != 0 || len(d.started) != 0 || len(d.stopped) != 0 {
		t.Fatalf("no mutation is allowed on an ownership mismatch: %+v", d)
	}
}

// R-3: a record left blocked for operator confirmation after an automatic rollback is
// never re-resolved by recovery, even with a verified healthy original.
func TestS3Recovery_R3_OperatorConfirmationStateIsNeverReResolved(t *testing.T) {
	d := newFakeDaemon(healthy("o1", "app"))
	f := newRec(t, d, "o1")
	s3EscalateRecovery(t, f, opStateRollbackPreviousUnavailable)
	f.run()
	if got := f.status(); got != "critical_error" {
		t.Fatalf("status = %s, want critical_error (only an operator releases this state)", got)
	}
}

// R-4: no container identity, and an original that is missing without a committed
// rotation, both leave the record escalated with the workloads preserved.
func TestS3Recovery_R4_NoIdentityAndMissingOriginalStayEscalated(t *testing.T) {
	t.Run("no container identity", func(t *testing.T) {
		d := newFakeDaemon(healthy("h", "app"), artifact("n1", "app_dso_new_1"))
		f := newRec(t, d, "")
		s3EscalateRecovery(t, f, "")
		f.run()
		if got := f.status(); got != "critical_error" || len(d.removed) != 0 {
			t.Fatalf("status %s removed %v, want critical_error with nothing removed", got, d.removed)
		}
	})
	t.Run("original missing without a committed rotation", func(t *testing.T) {
		d := newFakeDaemon(healthy("new1", "app"))
		f := newRec(t, d, "o-gone")
		s3EscalateRecovery(t, f, "")
		f.run()
		if got := f.status(); got != "critical_error" || len(d.removed) != 0 {
			t.Fatalf("status %s removed %v, want critical_error with nothing removed", got, d.removed)
		}
	})
}

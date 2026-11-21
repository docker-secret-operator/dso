package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Batch A / step S3 (A-02, A-14, owner decisions of 20 November 2026): BeginAttempt and
// its admission rule, the per-record journal_schema marker, unattributed events on a
// record that carries attempts, the transition table, the legacy implicit attempt, and
// ManualReason. Docker-free. Helpers come from state_tracker_derivation_test.go.

func s3ExpectIllegal(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("%s: err = %v, want ErrIllegalTransition", what, err)
	}
}

// Test 13: admission.
func TestS3BeginAttempt_Admission(t *testing.T) {
	ctx := context.Background()
	begin := func(st *StateTracker, id, strategy, target string, parent int) (int, error) {
		return st.BeginAttempt(ctx, AttemptBegin{RotationID: id, Strategy: strategy, TargetOriginalID: target, ServiceName: "svc", ParentAttempt: parent})
	}

	t.Run("first attempt: open, numbered 1, mirror and ServiceName stored, ServiceID untouched", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", ServiceID: "legacy-svc", Transition: rotation.TrIntentCreateCandidate})
		n, err := st.BeginAttempt(ctx, AttemptBegin{RotationID: "r1", Strategy: "rolling", TargetOriginalID: "o1", ServiceName: "another-name"})
		if err != nil || n != 1 {
			t.Fatalf("BeginAttempt = %d, %v, want 1, nil", n, err)
		}
		got := s3Rec(t, st, "r1")
		a := got.Attempts[0]
		if a.Attempt != 1 || a.State != "open" || a.Strategy != "rolling" || a.TargetOriginalID != "o1" || a.ServiceName != "another-name" || a.BeganSeq != got.Seq {
			t.Fatalf("attempt = %+v (record seq %d)", a, got.Seq)
		}
		if got.Strategy != "rolling" || got.ServiceID != "legacy-svc" || got.Transition != rotation.TrAttemptBegin || got.PendingIntent != "" || got.Status != "in_progress" {
			t.Fatalf("record strategy %q serviceID %q transition %q pending %q status %q", got.Strategy, got.ServiceID, got.Transition, got.PendingIntent, got.Status)
		}
	})

	t.Run("refused: another attempt of the same target is open", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Begin(t, st, "r1", "rolling", "o1")
		before := s3Rec(t, st, "r1")
		_, err := begin(st, "r1", "restart", "o1", 0)
		s3ExpectIllegal(t, "same target open", err)
		if !reflect.DeepEqual(before, s3Rec(t, st, "r1")) {
			t.Fatal("a refused BeginAttempt changed the record")
		}
	})

	t.Run("accepted: another target", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Begin(t, st, "r1", "rolling", "o1")
		if n, err := begin(st, "r1", "rolling", "o2", 0); err != nil || n != 2 {
			t.Fatalf("a second target: %d, %v, want 2, nil", n, err)
		}
	})

	refusals := []struct {
		name  string
		setup func(t *testing.T, st *StateTracker)
	}{
		{"critical_error record", func(t *testing.T, st *StateTracker) {
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrFailed, ManualRecovery: true})
		}},
		{"DispatchFinalized record", func(t *testing.T, st *StateTracker) {
			if err := st.CompleteRotationID("r1"); err != nil {
				t.Fatal(err)
			}
		}},
		{"rollback_required record", func(t *testing.T, st *StateTracker) {
			if err := st.MarkRollbackID("r1"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range refusals {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			st, _ := s3Tracker(t)
			s3Start(t, st, "r1")
			tc.setup(t, st)
			before := s3Rec(t, st, "r1")
			_, err := begin(st, "r1", "rolling", "o1", 0)
			s3ExpectIllegal(t, tc.name, err)
			after := s3Rec(t, st, "r1")
			if !reflect.DeepEqual(before, after) || after.JournalSchema != 0 {
				t.Fatalf("a refused BeginAttempt changed the record or wrote the marker: %+v", after)
			}
		})
	}

	t.Run("parent rules", func(t *testing.T) {
		newWithRolledBackParent := func(t *testing.T) *StateTracker {
			st, _ := s3Tracker(t)
			s3Start(t, st, "r1")
			n := s3Begin(t, st, "r1", "rolling", "o1")
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrRolledBack})
			return st
		}
		st := newWithRolledBackParent(t)
		if got := s3Rec(t, st, "r1"); got.Status != "recovered" {
			t.Fatalf("precondition: a rolled back only attempt gives recovered, got %q", got.Status)
		}
		n, err := begin(st, "r1", "restart", "o1", 1)
		if err != nil || n != 2 {
			t.Fatalf("a fallback after a rolled back parent: %d, %v, want 2, nil", n, err)
		}
		if got := s3Rec(t, st, "r1"); got.Status != "in_progress" || got.Strategy != "restart" {
			t.Fatalf("with the fallback open: status %q strategy %q, want in_progress / restart", got.Status, got.Strategy)
		}

		openParent, _ := s3Tracker(t)
		s3Start(t, openParent, "r1")
		s3Begin(t, openParent, "r1", "rolling", "o1")
		_, err = begin(openParent, "r1", "rolling", "o2", 1)
		s3ExpectIllegal(t, "an open parent", err)

		missing := newWithRolledBackParent(t)
		_, err = begin(missing, "r1", "restart", "o1", 7)
		s3ExpectIllegal(t, "a missing parent", err)

		pending := newWithRolledBackParent(t)
		s3Record(t, pending, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrIntentSwap})
		_, err = begin(pending, "r1", "restart", "o1", 1)
		s3ExpectIllegal(t, "a parent with a pending intent", err)

		committed := newWithRolledBackParent(t)
		s3Record(t, committed, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrSwapCommitted})
		_, err = begin(committed, "r1", "restart", "o1", 1)
		s3ExpectIllegal(t, "a committed parent", err)
	})

	t.Run("input checks and unknown rotation", func(t *testing.T) {
		st, _ := s3Tracker(t)
		if _, err := begin(st, "", "rolling", "o1", 0); err == nil {
			t.Fatal("an empty RotationID must be refused")
		}
		if _, err := begin(st, "r1", "rolling", "", 0); err == nil {
			t.Fatal("an empty target must be refused")
		}
		if n, err := begin(st, "journal-only", "rolling", "o1", 0); err != nil || n != 1 {
			t.Fatalf("an unknown rotation gets a journal-only record like Record does: %d, %v", n, err)
		}
		if got := s3Rec(t, st, "journal-only"); got.Status != "in_progress" || len(got.Attempts) != 1 {
			t.Fatalf("journal-only record = %+v", got)
		}
	})

	t.Run("attempt_begin is not an ordinary attempt-bearing event", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		before := s3Rec(t, st, "r1")
		s3ExpectIllegal(t, "Record(attempt_begin, attempt 1)", st.Record(ctx, rotation.JournalEvent{RotationID: "r1", Attempt: 1, Transition: rotation.TrAttemptBegin}))
		if !reflect.DeepEqual(before, s3Rec(t, st, "r1")) {
			t.Fatal("a refused attempt_begin changed the record")
		}
		// Unattributed, it is a plain legacy transition record: no attempt is created.
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrAttemptBegin})
		got := s3Rec(t, st, "r1")
		if len(got.Attempts) != 0 || got.JournalSchema != 0 || got.Transition != rotation.TrAttemptBegin || got.PendingIntent != "" || got.Status != "in_progress" || got.Seq != 1 {
			t.Fatalf("record = %+v", got)
		}
	})
}

// Tests 14 and 15: the per-record marker, written with the first persisted attempt in
// the same durable write.
func TestS3Marker_WrittenAtomicallyWithTheFirstAttempt(t *testing.T) {
	st, dir := s3Tracker(t)
	s3Start(t, st, "r1")
	fileBefore, _ := os.ReadFile(filepath.Join(dir, "rotations.json"))
	repair := s3Break(t, dir)
	if _, err := st.BeginAttempt(context.Background(), AttemptBegin{RotationID: "r1", Strategy: "rolling", TargetOriginalID: "o1"}); err == nil {
		t.Fatal("BeginAttempt was expected to fail with the write fault")
	}
	if got := s3Rec(t, st, "r1"); got.JournalSchema != 0 || len(got.Attempts) != 0 {
		t.Fatalf("a failed write left schema %d and attempts %+v in memory", got.JournalSchema, got.Attempts)
	}
	if fileAfter, _ := os.ReadFile(filepath.Join(dir, "rotations.json")); string(fileBefore) != string(fileAfter) {
		t.Fatal("a failed write changed the journal file")
	}
	repair()

	n := s3Begin(t, st, "r1", "rolling", "o1")
	if got := s3Rec(t, st, "r1"); got.JournalSchema != 2 || len(got.Attempts) != 1 {
		t.Fatalf("after success: schema %d attempts %+v, want 2 and one attempt together", got.JournalSchema, got.Attempts)
	}
	reloaded, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if got := s3Rec(t, reloaded, "r1"); got.JournalSchema != 2 || len(got.Attempts) != 1 {
		t.Fatalf("persisted: schema %d attempts %+v", got.JournalSchema, got.Attempts)
	}
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrRolledBack})
	s3Begin(t, st, "r1", "restart", "o1")
	if got := s3Rec(t, st, "r1"); got.JournalSchema != 2 {
		t.Fatalf("a later attempt must keep the marker at 2, got %d", got.JournalSchema)
	}
}

// s3WithOpenAttempt returns a tracker whose record r1 has one open attempt.
func s3WithOpenAttempt(t *testing.T) (*StateTracker, int) {
	t.Helper()
	st, _ := s3Tracker(t)
	s3Start(t, st, "r1")
	return st, s3Begin(t, st, "r1", "rolling", "orig-r1")
}

// Tests 16 to 18: an unattributed event never changes the attempt list.
func TestS3Unattributed_EventsOnAnOpenAttempt(t *testing.T) {
	t.Run("an ordinary event merges record-level fields and keeps in_progress", func(t *testing.T) {
		st, _ := s3WithOpenAttempt(t)
		before := s3Rec(t, st, "r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c9"}})
		got := s3Rec(t, st, "r1")
		if !reflect.DeepEqual(before.Attempts, got.Attempts) {
			t.Fatalf("an unattributed event changed the attempts: %+v", got.Attempts)
		}
		if got.Status != "in_progress" || got.Seq != before.Seq+1 || got.Transition != rotation.TrCandidateCreated || !reflect.DeepEqual(got.CandidateContainerIDs, []string{"c9"}) {
			t.Fatalf("status %q seq %d (was %d) transition %q candidates %v", got.Status, got.Seq, before.Seq, got.Transition, got.CandidateContainerIDs)
		}
	})
	t.Run("a manual event escalates the record without touching the attempt", func(t *testing.T) {
		st, _ := s3WithOpenAttempt(t)
		before := s3Rec(t, st, "r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrFailed, ManualRecovery: true, ManualReason: rotation.ManualReasonStateUnknown})
		got := s3Rec(t, st, "r1")
		if got.Status != "critical_error" || !reflect.DeepEqual(before.Attempts, got.Attempts) {
			t.Fatalf("status %q attempts %+v, want critical_error with the attempt untouched (still open)", got.Status, got.Attempts)
		}
	})
	t.Run("an unattributed TrCompleted applies the legacy record-level updates only", func(t *testing.T) {
		st, _ := s3WithOpenAttempt(t)
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrIntentCleanup})
		before := s3Rec(t, st, "r1")
		if before.PendingIntent != rotation.TrIntentCleanup {
			t.Fatalf("precondition: pending intent = %q", before.PendingIntent)
		}
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCompleted})
		got := s3Rec(t, st, "r1")
		if !reflect.DeepEqual(before.Attempts, got.Attempts) || got.Attempts[0].State != "open" {
			t.Fatalf("the open attempt was modified: %+v", got.Attempts)
		}
		if got.Status != "in_progress" {
			t.Fatalf("status = %q, want in_progress (an attempt is still open)", got.Status)
		}
		if got.Seq != before.Seq+1 || got.Transition != rotation.TrCompleted || got.Phase != rotation.TrCompleted || got.PendingIntent != "" || got.CleanupStatus != "done" {
			t.Fatalf("legacy updates: seq %d (was %d) transition %q phase %q pending %q cleanup %q",
				got.Seq, before.Seq, got.Transition, got.Phase, got.PendingIntent, got.CleanupStatus)
		}
	})
}

// Test 19: an unattributed event on an escalated or sticky critical_error record.
func TestS3Unattributed_EventsOnAnEscalatedRecordStayCriticalError(t *testing.T) {
	t.Run("escalated attempt", func(t *testing.T) {
		st, n := s3WithOpenAttempt(t)
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed, ManualRecovery: true})
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCompleted})
		if got := s3Rec(t, st, "r1"); got.Status != "critical_error" || got.Attempts[0].State != "escalated" {
			t.Fatalf("status %q attempt %q, want critical_error / escalated", got.Status, got.Attempts[0].State)
		}
	})
	t.Run("legacy critical_error", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrFailed, ManualRecovery: true})
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Transition: rotation.TrCompleted})
		if got := s3Rec(t, st, "r1"); got.Status != "critical_error" {
			t.Fatalf("status = %q, want critical_error", got.Status)
		}
	})
}

// Test 20: the transition table (JSM 4.5), cell by cell.
func TestS3TransitionTable_EveryCell(t *testing.T) {
	type cell int
	const (
		toOpen cell = iota
		toCompleted
		toRolledBack
		toFailed
		toEscalated
		unchanged // dropped or idempotent: no error, no write
		refused   // ErrIllegalTransition
	)
	events := []struct {
		name string
		e    rotation.JournalEvent
	}{
		{"attempt_begin", rotation.JournalEvent{Transition: rotation.TrAttemptBegin}},
		{"intent", rotation.JournalEvent{Transition: rotation.TrIntentSwap}},
		{"progress ack", rotation.JournalEvent{Transition: rotation.TrCandidateCreated}},
		{"completed", rotation.JournalEvent{Transition: rotation.TrCompleted}},
		{"signal_done", rotation.JournalEvent{Transition: rotation.TrSignalDone}},
		{"rolled_back", rotation.JournalEvent{Transition: rotation.TrRolledBack}},
		{"failed", rotation.JournalEvent{Transition: rotation.TrFailed}},
		{"signal_failed", rotation.JournalEvent{Transition: rotation.TrSignalFailed}},
		{"failed+manual", rotation.JournalEvent{Transition: rotation.TrFailed, ManualRecovery: true}},
		{"rolled_back+manual", rotation.JournalEvent{Transition: rotation.TrRolledBack, ManualRecovery: true}},
		{"signal_failed+manual", rotation.JournalEvent{Transition: rotation.TrSignalFailed, ManualRecovery: true}},
	}
	// rows: the state the attempt is in; columns follow events above.
	table := map[string][]cell{
		"open": {refused, toOpen, toOpen, toCompleted, toCompleted, toRolledBack, toFailed, toFailed, toEscalated, toEscalated, toEscalated},
		"completed": {refused, refused, unchanged, unchanged, unchanged, unchanged, unchanged, unchanged,
			unchanged, unchanged, unchanged},
		"rolled_back": {refused, refused, unchanged, unchanged, unchanged, unchanged, unchanged, unchanged,
			toEscalated, toEscalated, toEscalated},
		"failed": {refused, refused, unchanged, unchanged, unchanged, unchanged, unchanged, unchanged,
			toEscalated, toEscalated, toEscalated},
		"escalated": {refused, refused, unchanged, unchanged, unchanged, unchanged, unchanged, unchanged,
			unchanged, unchanged, unchanged},
	}
	// How to reach each state with a real attempt 1.
	reach := map[string]func(t *testing.T, st *StateTracker, n int){
		"open": func(t *testing.T, st *StateTracker, n int) {},
		"completed": func(t *testing.T, st *StateTracker, n int) {
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrCompleted})
		},
		"rolled_back": func(t *testing.T, st *StateTracker, n int) {
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrRolledBack})
		},
		"failed": func(t *testing.T, st *StateTracker, n int) {
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed})
		},
		"escalated": func(t *testing.T, st *StateTracker, n int) {
			s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed, ManualRecovery: true})
		},
	}
	endState := map[cell]string{toOpen: "open", toCompleted: "completed", toRolledBack: "rolled_back", toFailed: "failed", toEscalated: "escalated"}

	for from, row := range table {
		for i, ev := range events {
			want := row[i]
			t.Run(from+"/"+ev.name, func(t *testing.T) {
				st, n := s3WithOpenAttempt(t)
				reach[from](t, st, n)
				before := s3Rec(t, st, "r1")
				e := ev.e
				e.RotationID, e.Attempt = "r1", n
				err := st.Record(context.Background(), e)
				after := s3Rec(t, st, "r1")
				switch want {
				case refused:
					s3ExpectIllegal(t, ev.name, err)
					if !reflect.DeepEqual(before, after) {
						t.Fatal("a refused event changed the record")
					}
				case unchanged:
					if err != nil || !reflect.DeepEqual(before, after) {
						t.Fatalf("a dropped or idempotent event must change nothing: err %v\nbefore %+v\nafter  %+v", err, before, after)
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
					if got := after.Attempts[0].State; got != endState[want] {
						t.Fatalf("attempt state = %q, want %q", got, endState[want])
					}
				}
			})
		}
	}
}

// Test 21: a stale acknowledgement for an earlier attempt is dropped with a WARN.
func TestS3StaleAck_IsDroppedWithAWarning(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	st, err := NewStateTracker(t.TempDir(), zap.New(core))
	if err != nil {
		t.Fatal(err)
	}
	s3Start(t, st, "r1")
	first := s3Begin(t, st, "r1", "rolling", "o1")
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: first, Transition: rotation.TrRolledBack})
	if _, err := st.BeginAttempt(context.Background(), AttemptBegin{RotationID: "r1", Strategy: "restart", TargetOriginalID: "o1", ParentAttempt: first}); err != nil {
		t.Fatal(err)
	}
	before := s3Rec(t, st, "r1")
	warnsBefore := logs.Len()

	for _, tr := range []string{rotation.TrCandidateCreated, rotation.TrCompleted} {
		if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Attempt: first, Transition: tr}); err != nil {
			t.Fatalf("a stale %s ack must be dropped without an error: %v", tr, err)
		}
	}
	after := s3Rec(t, st, "r1")
	if after.Status != before.Status || after.Transition != before.Transition || after.Seq != before.Seq || !reflect.DeepEqual(before.Attempts, after.Attempts) {
		t.Fatalf("a stale ack changed the record:\nbefore %+v\nafter  %+v", before, after)
	}
	if logs.Len() != warnsBefore+2 {
		t.Fatalf("expected one WARN per dropped ack, got %d new entries", logs.Len()-warnsBefore)
	}
}

// Test 23: legacy records read as one implicit attempt 1; the legacy Strategy mirror.
func TestS3Legacy_ImplicitAttemptAndStrategyMirror(t *testing.T) {
	cases := []struct{ status, want string }{
		{"in_progress", "open"}, {"completed", "completed"}, {"recovered", "rolled_back"},
		{"rollback_required", "failed"}, {"critical_error", "escalated"},
	}
	for _, tc := range cases {
		r := &RotationState{RotationID: "r", Status: tc.status, Strategy: "rolling", ServiceID: "svc", OriginalContainerIDs: []string{"o1"}}
		got := effectiveAttempts(r)
		if len(got) != 1 || got[0].Attempt != 1 || got[0].State != tc.want || got[0].Strategy != "rolling" || got[0].TargetOriginalID != "o1" || got[0].ServiceName != "svc" {
			t.Errorf("status %q: implicit attempt = %+v, want attempt 1 in state %q", tc.status, got, tc.want)
		}
	}
	real := &RotationState{Status: "in_progress", Attempts: []AttemptState{{Attempt: 1, State: "open"}, {Attempt: 2, State: "completed"}}}
	if got := effectiveAttempts(real); !reflect.DeepEqual(got, real.Attempts) {
		t.Errorf("a record with attempts must be read as is, got %+v", got)
	}

	t.Run("an Attempt 1 event on a record without attempts is never merged into the legacy record", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		before := s3Rec(t, st, "r1")
		// An ack-class event is dropped (nil return, WARN); nothing is merged.
		if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Attempt: 1, Transition: rotation.TrCandidateCreated,
			ServiceID: "other-svc", Strategy: "signal", Detail: "must not land", CandidateIDs: []string{"c1"}}); err != nil {
			t.Fatal(err)
		}
		// An intent-class event is refused.
		s3ExpectIllegal(t, "intent for attempt 1 on a record without attempts", st.Record(context.Background(),
			rotation.JournalEvent{RotationID: "r1", Attempt: 1, Transition: rotation.TrIntentSwap, ServiceID: "other-svc"}))
		// A manual escalation addressed to a missing attempt neither escalates nor merges.
		if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Attempt: 1, Transition: rotation.TrFailed, ManualRecovery: true}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, s3Rec(t, st, "r1")) {
			t.Fatalf("an attempt-attributed event without a matching attempt changed the record:\nbefore %+v\nafter  %+v", before, s3Rec(t, st, "r1"))
		}
	})
	t.Run("only BeginAttempt creates attempts: the same event is accepted once attempt 1 exists", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		n := s3Begin(t, st, "r1", "rolling", "o1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrCandidateCreated, CandidateIDs: []string{"c1"}})
		got := s3Rec(t, st, "r1")
		if !reflect.DeepEqual(got.CandidateContainerIDs, []string{"c1"}) || len(got.Attempts) != 1 {
			t.Fatalf("record = %+v", got)
		}
		// AttemptState has no candidate field: the event is applied at record level and the
		// addressed attempt stays open and unchanged.
		if a := got.Attempts[0]; a.Attempt != n || a.State != "open" || a.Strategy != "rolling" || a.TargetOriginalID != "o1" || a.EndedSeq != 0 {
			t.Fatalf("attempt = %+v, want attempt %d open, rolling, o1", a, n)
		}
		if got.Transition != rotation.TrCandidateCreated {
			t.Fatalf("transition = %q, want %q", got.Transition, rotation.TrCandidateCreated)
		}
	})
	t.Run("an out-of-range attempt: intents are refused, acks dropped", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3ExpectIllegal(t, "intent for attempt 2 on a legacy record", st.Record(context.Background(),
			rotation.JournalEvent{RotationID: "r1", Attempt: 2, Transition: rotation.TrIntentSwap}))
		before := s3Rec(t, st, "r1")
		if err := st.Record(context.Background(), rotation.JournalEvent{RotationID: "r1", Attempt: 2, Transition: rotation.TrCandidateCreated}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, s3Rec(t, st, "r1")) {
			t.Fatal("a dropped ack changed the record")
		}
	})
	t.Run("the Strategy mirror follows the open attempt; unattributed events keep today's behaviour", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		first := s3Begin(t, st, "r1", "rolling", "o1")
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: first, Strategy: "ignored", Transition: rotation.TrCandidateCreated})
		if got := s3Rec(t, st, "r1"); got.Strategy != "rolling" {
			t.Fatalf("an attempt-bearing event must not override the mirror, got %q", got.Strategy)
		}
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: first, Transition: rotation.TrRolledBack})
		if _, err := st.BeginAttempt(context.Background(), AttemptBegin{RotationID: "r1", Strategy: "restart", TargetOriginalID: "o1", ParentAttempt: first}); err != nil {
			t.Fatal(err)
		}
		if got := s3Rec(t, st, "r1"); got.Strategy != "restart" {
			t.Fatalf("the mirror must follow the newly open attempt, got %q", got.Strategy)
		}
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Strategy: "signal", Transition: rotation.TrCandidateCreated})
		if got := s3Rec(t, st, "r1"); got.Strategy != "signal" {
			t.Fatalf("an unattributed event overwrites Strategy as before, got %q", got.Strategy)
		}
	})
	t.Run("the mirror returns to the remaining open attempt when another one ends", func(t *testing.T) {
		st, _ := s3Tracker(t)
		s3Start(t, st, "r1")
		s3Begin(t, st, "r1", "rolling", "o1")
		second := s3Begin(t, st, "r1", "restart", "o2")
		if got := s3Rec(t, st, "r1"); got.Strategy != "restart" {
			t.Fatalf("the highest-numbered open attempt gives the mirror, got %q", got.Strategy)
		}
		s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: second, Transition: rotation.TrCompleted})
		if got := s3Rec(t, st, "r1"); got.Strategy != "rolling" || got.Status != "in_progress" {
			t.Fatalf("strategy %q status %q, want rolling / in_progress (attempt 1 is still open)", got.Strategy, got.Status)
		}
	})
}

// Test 24: ManualReason is stored unvalidated, in the same write as the escalation.
func TestS3ManualReason_StoredUnvalidatedInTheEscalatingWrite(t *testing.T) {
	st, n := s3WithOpenAttempt(t)
	s3Record(t, st, rotation.JournalEvent{RotationID: "r1", Attempt: n, Transition: rotation.TrFailed, ManualRecovery: true, ManualReason: "a code this binary does not know"})
	got := s3Rec(t, st, "r1")
	if got.Attempts[0].ManualReason != "a code this binary does not know" || got.Status != "critical_error" || got.Attempts[0].State != "escalated" {
		t.Fatalf("attempt = %+v status %q", got.Attempts[0], got.Status)
	}
	// An unattributed manual event has no attempt to carry the reason: it is accepted.
	st2, _ := s3Tracker(t)
	s3Start(t, st2, "r2")
	s3Record(t, st2, rotation.JournalEvent{RotationID: "r2", Transition: rotation.TrFailed, ManualRecovery: true, ManualReason: rotation.ManualReasonNoWorkload})
	if got := s3Rec(t, st2, "r2"); got.Status != "critical_error" {
		t.Fatalf("status = %q", got.Status)
	}
}

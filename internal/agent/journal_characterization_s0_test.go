package agent

import (
	"context"
	"testing"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/rotation"
)

// Batch S0: characterisation of the journal's CURRENT behaviour, before the first
// behaviour-changing batch (S3). Every test here passes on unchanged production code
// by construction; it records what the tracker does today, not what it should do.
//
// Each test names the decision that may intentionally change its expectation. That
// label is NOT permission to edit the test later: any later edit of these tests needs
// its own OD-X11 approval, and no assertion may be weakened to obtain a pass.
//
// Docker-free: the tracker is exercised through its own API on a temp directory.

// s0Tracker opens a tracker on a fresh temp directory. It is local to S0 so that this
// file does not depend on any other test file's helpers.
func s0Tracker(t *testing.T) (*StateTracker, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStateTracker(dir, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	return st, dir
}

// s0LiveSnapshot returns a clone of the tracker's record for id, as the recovery path
// holds one, so the snapshot-based writers can be called with it.
func s0LiveSnapshot(t *testing.T, st *StateTracker, id string) *RotationState {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	live := st.byID(id)
	if live == nil {
		t.Fatalf("no record %q", id)
	}
	return live.clone()
}

func s0StatusOf(t *testing.T, st *StateTracker, id string) string {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	live := st.byID(id)
	if live == nil {
		t.Fatalf("no record %q", id)
	}
	return live.Status
}

// S0-1. A plain TrFailed event has no status mapping: the record stays in_progress.
//
// May change with: A-04 (derivation of status from the attempt journal) in S3.
func TestS0Characterization_PlainTrFailedLeavesInProgress(t *testing.T) {
	st, _ := s0Tracker(t)
	if err := st.StartRotationWithID("r1", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "r1", Transition: rotation.TrFailed, Detail: "candidate creation failed",
	}); err != nil {
		t.Fatal(err)
	}
	if got := s0StatusOf(t, st, "r1"); got != "in_progress" {
		t.Fatalf("status after a plain TrFailed = %q, want in_progress (current behaviour: no mapping)", got)
	}
	st.mu.Lock()
	live := st.byID("r1")
	transition, manual := live.Transition, live.ManualRecovery
	st.mu.Unlock()
	if transition != rotation.TrFailed {
		t.Fatalf("transition = %q, want %q", transition, rotation.TrFailed)
	}
	if manual {
		t.Fatal("a plain TrFailed must not set ManualRecovery")
	}
}

// S0-2. TrFailed with ManualRecovery leaves the status in_progress and sets only the
// ManualRecovery flag; MarkRollbackID then moves the record to rollback_required.
//
// May change with: A-04 (derivation) and A-05 (escalation) in S3.
//
// Amended 2026-11-20 (owner-approved OD-X11 edit, post-A-05). Before S3 this test pinned:
// status in_progress, empty ErrorMessage, and rollback_required after MarkRollbackID.
// A-05 makes a manual TrFailed escalate; the expectations below record that behaviour.
// The S0 acceptance record describes the original test and stays valid.
func TestS0Characterization_ManualRecoveryFailedThenRollback(t *testing.T) {
	st, _ := s0Tracker(t)
	if err := st.StartRotationWithID("r2", "p", "s", "c1", "c2"); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "r2", Transition: rotation.TrFailed, ManualRecovery: true,
	}); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	live := st.byID("r2")
	status, manual, errMsg := live.Status, live.ManualRecovery, live.ErrorMessage
	st.mu.Unlock()
	if status != "critical_error" {
		t.Fatalf("status after TrFailed+ManualRecovery = %q, want critical_error (A-05)", status)
	}
	if !manual {
		t.Fatal("ManualRecovery must be recorded")
	}
	if errMsg != "manual recovery required" {
		t.Fatalf("ErrorMessage = %q, want %q", errMsg, "manual recovery required")
	}
	if err := st.MarkRollbackID("r2"); err != nil {
		t.Fatal(err)
	}
	if got := s0StatusOf(t, st, "r2"); got != "critical_error" {
		t.Fatalf("status after MarkRollbackID = %q, want critical_error (not lowered)", got)
	}
}

// S0-3. MarkRecoveredRecord has no status guard: over a critical_error record it
// succeeds and sets recovered (it compares only Status and Seq with the snapshot).
//
// May change with: the A-06 guard on the MarkRecovered* writers in S4.
func TestS0Characterization_MarkRecoveredRecordOverCriticalError(t *testing.T) {
	st, _ := s0Tracker(t)
	if err := st.Record(context.Background(), rotation.JournalEvent{
		RotationID: "r3", ServiceID: "svc", Transition: rotation.TrSignalFailed, ManualRecovery: true, State: "state_unknown",
	}); err != nil {
		t.Fatal(err)
	}
	if got := s0StatusOf(t, st, "r3"); got != "critical_error" {
		t.Fatalf("precondition: status = %q, want critical_error", got)
	}
	snap := s0LiveSnapshot(t, st, "r3")
	if err := st.MarkRecoveredRecord(snap); err != nil {
		t.Fatalf("MarkRecoveredRecord over critical_error: %v (current behaviour: succeeds)", err)
	}
	if got := s0StatusOf(t, st, "r3"); got != "recovered" {
		t.Fatalf("status = %q, want recovered", got)
	}
	if snap.Status != "recovered" {
		t.Fatalf("snapshot status = %q, want recovered", snap.Status)
	}
}

// S0-4. Two events for one RotationID with different ServiceIDs leave the last
// writer's ServiceID.
//
// May change with: A-07 (write-once service identity per attempt, S3) and A-08 (identity refusal, S6).
func TestS0Characterization_LastWriterServiceID(t *testing.T) {
	st, _ := s0Tracker(t)
	ctx := context.Background()
	if err := st.Record(ctx, rotation.JournalEvent{
		RotationID: "r4", ServiceID: "svc-a", Transition: rotation.TrIntentCreateCandidate,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Record(ctx, rotation.JournalEvent{
		RotationID: "r4", ServiceID: "svc-b", Transition: rotation.TrCandidateCreated,
	}); err != nil {
		t.Fatalf("a second event with a different ServiceID: %v (current behaviour: accepted)", err)
	}
	st.mu.Lock()
	got := st.byID("r4").ServiceID
	st.mu.Unlock()
	if got != "svc-b" {
		t.Fatalf("ServiceID = %q, want the last writer's svc-b", got)
	}
	// An empty ServiceID never erases the recorded one.
	if err := st.Record(ctx, rotation.JournalEvent{RotationID: "r4", Transition: rotation.TrSecretsDelivered}); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	got = st.byID("r4").ServiceID
	st.mu.Unlock()
	if got != "svc-b" {
		t.Fatalf("ServiceID after an event without one = %q, want svc-b", got)
	}
}

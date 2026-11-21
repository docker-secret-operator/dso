package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func TestStateTracker_Lifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	st, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}

	// StartRotation
	if err := st.StartRotation("aws", "db_pass", "old-abc", "new-xyz"); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}

	// GetPendingRotations — in_progress < 5 min old, so not pending
	pending := st.GetPendingRotations()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending (too fresh), got %d", len(pending))
	}

	// CompleteRotation
	if err := st.CompleteRotation("aws", "db_pass", "old-abc"); err != nil {
		t.Fatalf("CompleteRotation: %v", err)
	}

	// Second rotation for rollback test
	if err := st.StartRotation("aws", "api_key", "old-111", "new-222"); err != nil {
		t.Fatalf("StartRotation 2: %v", err)
	}
	if err := st.MarkRollback("aws", "api_key", "old-111"); err != nil {
		t.Fatalf("MarkRollback: %v", err)
	}

	// rollback_required is always pending
	pending = st.GetPendingRotations()
	if len(pending) == 0 {
		t.Error("expected pending rollback_required rotation")
	}

	// MarkRecovered
	if err := st.MarkRecovered("aws", "api_key", "old-111"); err != nil {
		t.Fatalf("MarkRecovered: %v", err)
	}

	// critical_error path
	if err := st.StartRotation("aws", "tls_cert", "old-333", "new-444"); err != nil {
		t.Fatalf("StartRotation 3: %v", err)
	}
	if err := st.MarkCriticalError("aws", "tls_cert", "old-333", "host unreachable"); err != nil {
		t.Fatalf("MarkCriticalError: %v", err)
	}

	// critical_error is always pending
	pending = st.GetPendingRotations()
	if len(pending) == 0 {
		t.Error("expected pending critical_error rotation")
	}

	// DeleteState
	if err := st.DeleteState("aws", "tls_cert", "old-333"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}

	// CleanupOldStates — completed entry is old enough to clean
	if err := st.CleanupOldStates(0); err != nil {
		t.Fatalf("CleanupOldStates: %v", err)
	}

	// Close
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStateTracker_LoadExisting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	// Create and persist state
	st, _ := NewStateTracker(dir, logger)
	_ = st.StartRotation("vault", "secret", "ctr-aaa", "ctr-bbb")
	_ = st.Close()

	// Reload from same directory — loadStates is exercised
	st2, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	defer func() { _ = st2.Close() }()

	pending := st2.GetPendingRotations()
	_ = pending // in_progress too fresh, but loadStates was exercised
}

func TestStateTracker_CleanupNoOp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, logger)
	defer func() { _ = st.Close() }()

	// CleanupOldStates on empty tracker — should be no-op
	if err := st.CleanupOldStates(time.Hour); err != nil {
		t.Fatalf("CleanupOldStates empty: %v", err)
	}
}

// ============================================================================
// Phase 7 / P2-3: corrupted rotations.json regression coverage.
//
// This section deliberately does NOT change persistence semantics -- the
// Phase 6 audit found the existing behavior (log a Warn, proceed with an
// empty in-memory state map) safe enough given internal/agent/recovery.go's
// CleanupOrphanedContainers acts as an independent, state-file-agnostic
// backstop (see recovery_test.go's TestCleanupOrphanedContainers_* tests).
// These tests exist purely to lock that intended behavior in as a
// regression guard, not to escalate or redesign it.
// ============================================================================

// TestStateTracker_ValidStateLoadsCorrectly is the positive-case control for
// the corruption tests below: a well-formed rotations.json produced by a
// prior tracker instance must load with its pending rotation intact.
func TestStateTracker_ValidStateLoadsCorrectly(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	st1, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("NewStateTracker: %v", err)
	}
	if err := st1.StartRotation("aws", "db_pass", "orig-id", "new-id"); err != nil {
		t.Fatalf("StartRotation: %v", err)
	}
	if err := st1.MarkRollback("aws", "db_pass", "orig-id"); err != nil {
		t.Fatalf("MarkRollback: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st2, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("reload NewStateTracker: %v", err)
	}
	defer func() { _ = st2.Close() }()

	pending := st2.GetPendingRotations()
	if len(pending) != 1 || pending[0].SecretName != "db_pass" {
		t.Fatalf("expected the persisted rollback_required rotation to survive reload, got %+v", pending)
	}
}

// TestStateTracker_MalformedJSONDoesNotCrashOrBlockStartup is the core P2-3
// regression test: a truncated/corrupted rotations.json (e.g. from a crash
// mid-write on a filesystem where the atomic rename didn't fully protect
// against a torn write) must not panic, must not surface as an error from
// NewStateTracker, and must leave the tracker in a clean, usable state (no
// phantom pending rotations reconstructed from garbage) so agent startup
// proceeds normally. The safety net for any real in-flight rotation that
// this discards is CleanupOrphanedContainers, exercised separately in
// internal/agent/recovery_test.go.
func TestStateTracker_MalformedJSONDoesNotCrashOrBlockStartup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	stateFile := filepath.Join(dir, "rotations.json")
	// Deliberately truncated/invalid JSON, as a crash mid-write might leave.
	if err := os.WriteFile(stateFile, []byte(`{"aws:db_pass:orig-id": {"provider_name": "aws", "sec`), 0600); err != nil {
		t.Fatalf("failed to seed corrupted state file: %v", err)
	}

	// Must not panic and must not return an error -- a corrupted state file
	// is a recoverable condition, not a fatal one; the agent must still
	// start.
	st, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("expected NewStateTracker to succeed despite a corrupted state file, got error: %v", err)
	}
	defer func() { _ = st.Close() }()

	// No phantom pending rotations reconstructed from the garbage file.
	pending := st.GetPendingRotations()
	if len(pending) != 0 {
		t.Fatalf("expected zero pending rotations after discarding corrupted state, got %+v", pending)
	}

	// The tracker remains fully usable afterwards -- proves state wasn't
	// left in some broken intermediate condition.
	if err := st.StartRotation("aws", "new_secret", "orig-2", "new-2"); err != nil {
		t.Fatalf("expected the tracker to remain usable after corruption recovery, StartRotation failed: %v", err)
	}
}

// TestStateTracker_EmptyStateFileDoesNotCrash covers the related edge case
// of a zero-byte state file (e.g. a crash exactly at file creation, before
// any content was written) -- JSON unmarshal of an empty byte slice fails
// the same way malformed JSON does, and must be handled identically.
func TestStateTracker_EmptyStateFileDoesNotCrash(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()

	stateFile := filepath.Join(dir, "rotations.json")
	if err := os.WriteFile(stateFile, []byte{}, 0600); err != nil {
		t.Fatalf("failed to seed empty state file: %v", err)
	}

	st, err := NewStateTracker(dir, logger)
	if err != nil {
		t.Fatalf("expected NewStateTracker to succeed with an empty state file, got error: %v", err)
	}
	defer func() { _ = st.Close() }()

	if pending := st.GetPendingRotations(); len(pending) != 0 {
		t.Fatalf("expected zero pending rotations from an empty state file, got %+v", pending)
	}
}

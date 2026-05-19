package agent

import (
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
	defer st2.Close()

	pending := st2.GetPendingRotations()
	_ = pending // in_progress too fresh, but loadStates was exercised
}

func TestStateTracker_CleanupNoOp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	dir := t.TempDir()
	st, _ := NewStateTracker(dir, logger)
	defer st.Close()

	// CleanupOldStates on empty tracker — should be no-op
	if err := st.CleanupOldStates(time.Hour); err != nil {
		t.Fatalf("CleanupOldStates empty: %v", err)
	}
}

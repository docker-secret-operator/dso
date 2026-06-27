package setup

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// ─── newTransaction ───────────────────────────────────────────────────────────

func TestNewTransaction_SetsStatusPending(t *testing.T) {
	tx := newTransaction("plan-001")
	if tx.Status != StatusPending {
		t.Errorf("want StatusPending, got %q", tx.Status)
	}
}

func TestNewTransaction_SetsPlanID(t *testing.T) {
	tx := newTransaction("plan-001")
	if tx.PlanID != "plan-001" {
		t.Errorf("want PlanID 'plan-001', got %q", tx.PlanID)
	}
}

func TestNewTransaction_SetsStartTime(t *testing.T) {
	before := time.Now()
	tx := newTransaction("plan-001")
	if tx.StartTime.Before(before) {
		t.Error("expected StartTime >= before-create time")
	}
}

func TestNewTransaction_HasNonEmptyID(t *testing.T) {
	tx := newTransaction("plan-001")
	if tx.ID == "" {
		t.Error("expected non-empty transaction ID")
	}
}

func TestNewTransaction_IDHasTxPrefix(t *testing.T) {
	tx := newTransaction("plan-001")
	if !strings.HasPrefix(tx.ID, "tx-") {
		t.Errorf("expected ID to start with 'tx-', got %q", tx.ID)
	}
}

func TestNewTransaction_StartsWithNoOperations(t *testing.T) {
	tx := newTransaction("plan-001")
	if len(tx.Operations) != 0 {
		t.Errorf("expected 0 operations, got %d", len(tx.Operations))
	}
}

// ─── appendOperation ─────────────────────────────────────────────────────────

func TestAppendOperation_OperationAdded(t *testing.T) {
	tx := newTransaction("plan-001")
	appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	if len(tx.Operations) != 1 {
		t.Errorf("expected 1 operation, got %d", len(tx.Operations))
	}
}

func TestAppendOperation_SequenceIsOneIndexed(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	if op.Sequence != 1 {
		t.Errorf("want Sequence=1, got %d", op.Sequence)
	}
}

func TestAppendOperation_SequenceIncrementsPerOp(t *testing.T) {
	tx := newTransaction("plan-001")
	appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	op2 := appendOperation(tx, "FILE-001", "file_create", "/etc/dso/dso.yaml")
	if op2.Sequence != 2 {
		t.Errorf("want Sequence=2, got %d", op2.Sequence)
	}
}

func TestAppendOperation_SetsOperID(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "FILE-001", "file_create", "/etc/dso/dso.yaml")
	if op.OperID != "FILE-001" {
		t.Errorf("want OperID 'FILE-001', got %q", op.OperID)
	}
}

func TestAppendOperation_SetsType(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	if op.Type != "directory_create" {
		t.Errorf("want Type 'directory_create', got %q", op.Type)
	}
}

func TestAppendOperation_SetsTarget(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	if op.Target != "/etc/dso" {
		t.Errorf("want Target '/etc/dso', got %q", op.Target)
	}
}

func TestAppendOperation_StartsAsPending(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	if op.Status != StatusPending {
		t.Errorf("want StatusPending, got %q", op.Status)
	}
}

func TestAppendOperation_ReturnsPointerIntoSlice(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	op.Status = StatusRunning
	// The pointer must reflect the mutation in tx.Operations.
	if tx.Operations[0].Status != StatusRunning {
		t.Error("expected pointer to refer to element inside tx.Operations")
	}
}

// ─── State transitions ────────────────────────────────────────────────────────

func TestMarkRunning_SetsStatusRunning(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markRunning(op)
	if op.Status != StatusRunning {
		t.Errorf("want StatusRunning, got %q", op.Status)
	}
}

func TestMarkCompleted_SetsStatusCompleted(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markCompleted(op)
	if op.Status != StatusCompleted {
		t.Errorf("want StatusCompleted, got %q", op.Status)
	}
}

func TestMarkCompleted_SetsReversible(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markCompleted(op)
	if !op.Reversible {
		t.Error("expected Reversible=true after markCompleted")
	}
}

func TestMarkCompleted_SetsEndedAt(t *testing.T) {
	before := time.Now()
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markCompleted(op)
	if op.EndedAt.Before(before) {
		t.Error("expected EndedAt to be set after markCompleted")
	}
}

func TestMarkFailed_SetsStatusFailed(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markFailed(op, errors.New("disk full"))
	if op.Status != StatusFailed {
		t.Errorf("want StatusFailed, got %q", op.Status)
	}
}

func TestMarkFailed_SetsError(t *testing.T) {
	sentinel := errors.New("disk full")
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markFailed(op, sentinel)
	if !errors.Is(op.Error, sentinel) {
		t.Errorf("expected sentinel error, got %v", op.Error)
	}
}

func TestMarkFailed_NotReversible(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markFailed(op, errors.New("boom"))
	if op.Reversible {
		t.Error("expected Reversible=false after markFailed")
	}
}

func TestMarkFailed_SetsEndedAt(t *testing.T) {
	before := time.Now()
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markFailed(op, errors.New("boom"))
	if op.EndedAt.Before(before) {
		t.Error("expected EndedAt to be set after markFailed")
	}
}

// ─── Full lifecycle: Pending → Running → Completed ────────────────────────────

func TestTxOperation_FullSuccessLifecycle(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")

	if op.Status != StatusPending {
		t.Errorf("initial: want Pending, got %q", op.Status)
	}
	markRunning(op)
	if op.Status != StatusRunning {
		t.Errorf("after running: want Running, got %q", op.Status)
	}
	markCompleted(op)
	if op.Status != StatusCompleted {
		t.Errorf("after completed: want Completed, got %q", op.Status)
	}
	if !op.Reversible {
		t.Error("after completed: want Reversible=true")
	}
}

func TestTxOperation_FullFailureLifecycle(t *testing.T) {
	tx := newTransaction("plan-001")
	op := appendOperation(tx, "DIR-001", "directory_create", "/etc/dso")
	markRunning(op)
	markFailed(op, errors.New("no space"))

	if op.Status != StatusFailed {
		t.Errorf("want Failed, got %q", op.Status)
	}
	if op.Error == nil {
		t.Error("want non-nil Error after failure")
	}
	if op.Reversible {
		t.Error("want Reversible=false after failure")
	}
}

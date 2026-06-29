package setup

import (
	"fmt"
	"os"
	"time"
)

// newTransaction creates a fresh transaction for executing a plan.
// The transaction starts in StatusPending; the Applier moves it forward.
func newTransaction(planID string) *Transaction {
	return &Transaction{
		ID:        generateTransactionID(),
		PlanID:    planID,
		Status:    StatusPending,
		StartTime: time.Now(),
	}
}

func generateTransactionID() string {
	return fmt.Sprintf("tx-%s", time.Now().Format("20060102-150405.000000000"))
}

// appendOperation adds a new Pending operation to the transaction and returns
// a pointer to it. The caller updates the operation in place as it runs.
func appendOperation(tx *Transaction, operID, opType, target string) *TxOperation {
	tx.Operations = append(tx.Operations, TxOperation{
		Sequence:  len(tx.Operations) + 1,
		OperID:    operID,
		Type:      opType,
		Target:    target,
		Status:    StatusPending,
		StartedAt: time.Now(),
	})
	return &tx.Operations[len(tx.Operations)-1]
}

// markRunning transitions an operation to the Running state.
func markRunning(op *TxOperation) {
	op.Status = StatusRunning
}

// markCompleted transitions an operation to Completed and marks it reversible.
func markCompleted(op *TxOperation) {
	op.Status = StatusCompleted
	op.Reversible = true
	op.EndedAt = time.Now()
}

// markFailed transitions an operation to Failed and records the cause.
func markFailed(op *TxOperation, err error) {
	op.Status = StatusFailed
	op.Error = err
	op.Reversible = false
	op.EndedAt = time.Now()
}

// ─── Rollback snapshots ───────────────────────────────────────────────────────
// Stored in TxOperation.Before / TxOperation.After so that Phase 7 Rollback
// can replay every operation in reverse without re-reading the OS.

// DirSnapshot records directory state before a create/modify operation.
type DirSnapshot struct {
	Existed bool
	Mode    os.FileMode
	Owner   string
}

// FileSnapshot records file state before a write operation.
type FileSnapshot struct {
	Existed bool
	Content []byte // nil when Existed is false
	Mode    os.FileMode
	Owner   string
}

// PermSnapshot records permission state before a chmod/chown.
type PermSnapshot struct {
	Mode  os.FileMode
	Owner string
}

// ServiceSnapshot records systemd service state before enable/start/stop.
type ServiceSnapshot struct {
	Enabled bool
	Active  bool
}

// GroupSnapshot records Unix group state before a create/add-member operation.
type GroupSnapshot struct {
	Existed bool
	Members []string
}

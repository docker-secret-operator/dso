package setup

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// rollbackExec is the internal contract for a single resource type's rollback handler.
type rollbackExec interface {
	rollback(ctx context.Context, op *TxOperation) error
}

// Rollback replays a Transaction in reverse order, restoring pre-Apply state.
// It consumes only the Transaction — it never reads Environment or InstallPlan.
// Rollback continues past individual failures; all failures are collected and returned.
type Rollback struct {
	emitter    *Emitter
	directory  rollbackExec
	file       rollbackExec
	permission rollbackExec
	service    rollbackExec
}

// newRollback constructs a Rollback wired to the given emitter and real OS hooks.
func newRollback(emitter *Emitter) *Rollback {
	return &Rollback{
		emitter:    emitter,
		directory:  newDirectoryRollback(emitter),
		file:       newFileRollback(emitter),
		permission: newPermissionRollback(emitter),
		service:    newServiceRollback(emitter),
	}
}

// Execute reverses every reversible operation in tx, in reverse execution order.
// It always returns a non-nil RollbackResult; errors are collected, not returned.
func (r *Rollback) Execute(ctx context.Context, tx *Transaction) *RollbackResult {
	result := &RollbackResult{
		TransactionID: tx.ID,
		StartTime:     time.Now(),
	}

	tx.Status = StatusRollbackRunning
	r.emitter.emit(EventRollbackStarted, tx, nil)

	// Iterate in reverse order so the last applied operation is undone first.
	for i := len(tx.Operations) - 1; i >= 0; i-- {
		op := &tx.Operations[i]
		if !op.Reversible {
			continue // skip failed or un-started operations
		}

		r.emitter.emit(EventRollbackOperationStarted, op, nil)
		if err := r.dispatch(ctx, op); err != nil {
			result.Failed = append(result.Failed, RollbackFailure{
				OperID: op.OperID,
				Target: op.Target,
				Error:  err,
			})
			r.emitter.emit(EventRollbackOperationFailed, op, err)
			continue
		}
		result.Completed = append(result.Completed, op.OperID)
		r.emitter.emit(EventRollbackOperationCompleted, op, nil)
	}

	result.EndTime = time.Now()

	if len(result.Failed) > 0 {
		tx.Status = StatusRollbackFailed
		r.emitter.emit(EventRollbackFailed, result, fmt.Errorf("%d rollback operation(s) failed", len(result.Failed)))
	} else {
		tx.Status = StatusRolledBack
		r.emitter.emit(EventRollbackCompleted, result, nil)
	}

	return result
}

// dispatch routes an operation to the appropriate rollback handler by type.
func (r *Rollback) dispatch(ctx context.Context, op *TxOperation) error {
	switch {
	case strings.HasPrefix(op.Type, "directory_"):
		return r.directory.rollback(ctx, op)
	case strings.HasPrefix(op.Type, "file_"):
		return r.file.rollback(ctx, op)
	case op.Type == "permission_change":
		return r.permission.rollback(ctx, op)
	case strings.HasPrefix(op.Type, "service_"):
		return r.service.rollback(ctx, op)
	default:
		// group_ operations and any unknown types are skipped without error.
		// Group rollback is deferred to a later phase.
		return nil
	}
}

package setup

import (
	"context"
	"fmt"
	"os"
)

// PermissionExecutor applies chmod/chown operations declared in the InstallPlan.
// OS interactions are injected via function fields so tests never touch the disk.
type PermissionExecutor struct {
	ops     []PermissionChange
	emitter *Emitter
	// Injectable OS hooks — defaults point to real syscalls.
	stat    func(string) (os.FileInfo, error)
	chmod   func(string, os.FileMode) error
	chown   func(string, string) error
	ownerOf func(string) (string, error)
}

func newPermissionExecutor(ops []PermissionChange, emitter *Emitter) *PermissionExecutor {
	return &PermissionExecutor{
		ops:     ops,
		emitter: emitter,
		stat:    os.Stat,
		chmod:   os.Chmod,
		chown:   chownPath,
		ownerOf: ownerOfPath,
	}
}

func (e *PermissionExecutor) execute(ctx context.Context, tx *Transaction) error {
	for i := range e.ops {
		if err := e.executeOne(ctx, &e.ops[i], tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *PermissionExecutor) executeOne(_ context.Context, op *PermissionChange, tx *Transaction) error {
	txOp := appendOperation(tx, op.ID, "permission_change", op.Path)
	markRunning(txOp)
	e.emitter.emit(EventOperationStarted, txOp, nil)

	// Snapshot current mode and owner for rollback.
	// op.Current is the plan's expected mode; live stat overrides it when available.
	// op.Owner is the TARGET owner (what we're setting), not the original — so we
	// must read the actual current owner from the OS.
	before := &PermSnapshot{Mode: op.Current}
	if info, err := e.stat(op.Path); err == nil {
		before.Mode = info.Mode()
	}
	before.Owner, _ = e.ownerOf(op.Path) // best-effort; empty string means skip chown in rollback
	txOp.Before = before

	if err := e.chmod(op.Path, op.Target); err != nil {
		markFailed(txOp, err)
		e.emitter.emit(EventOperationFailed, txOp, err)
		return fmt.Errorf("%s: chmod %s: %w", op.ID, op.Path, err)
	}

	if op.Owner != "" {
		if err := e.chown(op.Path, op.Owner); err != nil {
			markFailed(txOp, err)
			e.emitter.emit(EventOperationFailed, txOp, err)
			return fmt.Errorf("%s: chown %s: %w", op.ID, op.Path, err)
		}
	}

	txOp.After = &PermSnapshot{Mode: op.Target, Owner: op.Owner}
	markCompleted(txOp)
	e.emitter.emit(EventOperationCompleted, txOp, nil)
	return nil
}

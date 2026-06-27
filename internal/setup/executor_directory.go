package setup

import (
	"context"
	"fmt"
	"os"
)

// DirectoryExecutor creates or modifies directories declared in the InstallPlan.
// OS interactions are injected via function fields so tests never touch the disk.
type DirectoryExecutor struct {
	ops     []DirectoryChange
	emitter *Emitter
	// Injectable OS hooks — defaults point to real syscalls.
	stat  func(string) (os.FileInfo, error)
	mkdir func(string, os.FileMode) error
	chown func(string, string) error // "path", "user:group"
}

func newDirectoryExecutor(ops []DirectoryChange, emitter *Emitter) *DirectoryExecutor {
	return &DirectoryExecutor{
		ops:     ops,
		emitter: emitter,
		stat:    os.Stat,
		mkdir:   os.MkdirAll,
		chown:   chownPath,
	}
}

func (e *DirectoryExecutor) execute(ctx context.Context, tx *Transaction) error {
	for i := range e.ops {
		if err := e.executeOne(ctx, &e.ops[i], tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *DirectoryExecutor) executeOne(_ context.Context, op *DirectoryChange, tx *Transaction) error {
	txOp := appendOperation(tx, op.ID, "directory_"+op.Operation, op.Path)
	markRunning(txOp)
	e.emitter.emit(EventOperationStarted, txOp, nil)

	// Snapshot pre-operation state for rollback.
	before := &DirSnapshot{}
	if info, err := e.stat(op.Path); err == nil {
		before.Existed = true
		before.Mode = info.Mode()
	}
	txOp.Before = before

	if err := e.mkdir(op.Path, op.Mode); err != nil {
		markFailed(txOp, err)
		e.emitter.emit(EventOperationFailed, txOp, err)
		return fmt.Errorf("%s: mkdir %s: %w", op.ID, op.Path, err)
	}

	if op.Owner != "" {
		if err := e.chown(op.Path, op.Owner); err != nil {
			markFailed(txOp, err)
			e.emitter.emit(EventOperationFailed, txOp, err)
			return fmt.Errorf("%s: chown %s: %w", op.ID, op.Path, err)
		}
	}

	txOp.After = &DirSnapshot{Existed: true, Mode: op.Mode, Owner: op.Owner}
	markCompleted(txOp)
	e.emitter.emit(EventOperationCompleted, txOp, nil)
	return nil
}

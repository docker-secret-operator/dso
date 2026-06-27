package setup

import (
	"context"
	"fmt"
	"os"
)

// FileExecutor creates or modifies files declared in the InstallPlan.
// OS interactions are injected via function fields so tests never touch the disk.
type FileExecutor struct {
	ops     []FileChange
	emitter *Emitter
	// Injectable OS hooks — defaults point to real syscalls.
	stat      func(string) (os.FileInfo, error)
	readFile  func(string) ([]byte, error)
	writeFile func(string, []byte, os.FileMode) error
	chown     func(string, string) error
	ownerOf   func(string) (string, error)
}

func newFileExecutor(ops []FileChange, emitter *Emitter) *FileExecutor {
	return &FileExecutor{
		ops:       ops,
		emitter:   emitter,
		stat:      os.Stat,
		readFile:  os.ReadFile,
		writeFile: os.WriteFile,
		chown:     chownPath,
		ownerOf:   ownerOfPath,
	}
}

func (e *FileExecutor) execute(ctx context.Context, tx *Transaction) error {
	for i := range e.ops {
		if err := e.executeOne(ctx, &e.ops[i], tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *FileExecutor) executeOne(_ context.Context, op *FileChange, tx *Transaction) error {
	txOp := appendOperation(tx, op.ID, "file_"+op.Operation, op.Path)
	markRunning(txOp)
	e.emitter.emit(EventOperationStarted, txOp, nil)

	// Snapshot pre-operation state for rollback (capture existing content).
	before := &FileSnapshot{}
	if content, err := e.readFile(op.Path); err == nil {
		before.Existed = true
		before.Content = content
		if info, err := e.stat(op.Path); err == nil {
			before.Mode = info.Mode()
		}
		before.Owner, _ = e.ownerOf(op.Path) // best-effort; empty string is safe
	}
	txOp.Before = before

	if err := e.writeFile(op.Path, op.Content, op.Mode); err != nil {
		markFailed(txOp, err)
		e.emitter.emit(EventOperationFailed, txOp, err)
		return fmt.Errorf("%s: write %s: %w", op.ID, op.Path, err)
	}

	if op.Owner != "" {
		if err := e.chown(op.Path, op.Owner); err != nil {
			markFailed(txOp, err)
			e.emitter.emit(EventOperationFailed, txOp, err)
			return fmt.Errorf("%s: chown %s: %w", op.ID, op.Path, err)
		}
	}

	txOp.After = &FileSnapshot{
		Existed: true,
		Content: op.Content,
		Mode:    op.Mode,
		Owner:   op.Owner,
	}
	markCompleted(txOp)
	e.emitter.emit(EventOperationCompleted, txOp, nil)
	return nil
}

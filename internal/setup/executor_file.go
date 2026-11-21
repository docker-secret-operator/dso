package setup

import (
	"context"
	"fmt"
	"os"
	"time"
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
	chmod     func(string, os.FileMode) error
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
		chmod:     os.Chmod,
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

	// A replacement the operator opted into is backed up first; if the backup cannot be
	// written the file is NOT replaced.
	if op.BackupExisting && before.Existed {
		backup, err := e.backupExisting(op.Path, before.Content)
		if err != nil {
			markFailed(txOp, err)
			e.emitter.emit(EventOperationFailed, txOp, err)
			return fmt.Errorf("%s: back up %s before replacing it: %w", op.ID, op.Path, err)
		}
		txOp.BackupPath = backup
	}

	if err := e.writeFile(op.Path, op.Content, op.Mode); err != nil {
		markFailed(txOp, err)
		e.emitter.emit(EventOperationFailed, txOp, err)
		return fmt.Errorf("%s: write %s: %w", op.ID, op.Path, err)
	}

	// os.WriteFile only applies its mode argument when CREATING a file — on
	// an already-existing path (e.g. re-running setup/repair) it leaves the
	// existing mode untouched, so an over-permissive file would silently
	// stay that way while txOp.After below claims op.Mode was applied.
	// Chmod explicitly so the declared mode is always enforced.
	if err := e.chmod(op.Path, op.Mode); err != nil {
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

// backupExisting writes content to a timestamped 0600 copy next to path
// (<path>.bak-<UTC timestamp>, with a numeric suffix if that name is taken) and returns the
// backup's path. The backup may hold credentials, so it is never more permissive than 0600.
func (e *FileExecutor) backupExisting(path string, content []byte) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for n := 0; n < 100; n++ {
		name := fmt.Sprintf("%s.bak-%s", path, stamp)
		if n > 0 {
			name = fmt.Sprintf("%s-%d", name, n)
		}
		if _, err := e.stat(name); err == nil {
			continue // taken
		}
		if err := e.writeFile(name, content, 0o600); err != nil {
			return "", err
		}
		if err := e.chmod(name, 0o600); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("no free backup name next to %s", path)
}

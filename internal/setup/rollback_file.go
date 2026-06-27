package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// FileRollback reverses FileExecutor operations.
// Injectable OS hooks allow tests to run without touching the filesystem.
type FileRollback struct {
	emitter    *Emitter
	removeFile func(string) error
	writeFile  func(string, []byte, os.FileMode) error
	chown      func(string, string) error
}

func newFileRollback(emitter *Emitter) *FileRollback {
	return &FileRollback{
		emitter:    emitter,
		removeFile: os.Remove,
		writeFile:  os.WriteFile,
		chown:      chownPath,
	}
}

func (r *FileRollback) rollback(_ context.Context, op *TxOperation) error {
	before, ok := op.Before.(*FileSnapshot)
	if !ok {
		return fmt.Errorf("%s: missing FileSnapshot for rollback", op.OperID)
	}

	if !before.Existed {
		// File was newly created; delete it.
		if err := r.removeFile(op.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: remove %s: %w", op.OperID, op.Target, err)
		}
		return nil
	}

	// File pre-existed; restore its original content, mode, and owner.
	if err := r.writeFile(op.Target, before.Content, before.Mode); err != nil {
		return fmt.Errorf("%s: restore %s: %w", op.OperID, op.Target, err)
	}
	if before.Owner != "" {
		if err := r.chown(op.Target, before.Owner); err != nil {
			return fmt.Errorf("%s: restore owner %s: %w", op.OperID, op.Target, err)
		}
	}
	return nil
}

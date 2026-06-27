package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// DirectoryRollback reverses DirectoryExecutor operations.
// Injectable OS hooks allow tests to run without touching the filesystem.
type DirectoryRollback struct {
	emitter   *Emitter
	stat      func(string) (os.FileInfo, error)
	removeDir func(string) error        // default: os.Remove (fails if non-empty)
	chmod     func(string, os.FileMode) error
	chown     func(string, string) error
}

func newDirectoryRollback(emitter *Emitter) *DirectoryRollback {
	return &DirectoryRollback{
		emitter:   emitter,
		stat:      os.Stat,
		removeDir: os.Remove,
		chmod:     os.Chmod,
		chown:     chownPath,
	}
}

func (r *DirectoryRollback) rollback(_ context.Context, op *TxOperation) error {
	before, ok := op.Before.(*DirSnapshot)
	if !ok {
		return fmt.Errorf("%s: missing DirSnapshot for rollback", op.OperID)
	}

	if !before.Existed {
		// Directory was newly created; remove it if it still exists and is empty.
		if err := r.removeDir(op.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s: remove %s: %w", op.OperID, op.Target, err)
		}
		return nil
	}

	// Directory pre-existed; restore its original mode and owner.
	if before.Mode != 0 {
		if err := r.chmod(op.Target, before.Mode); err != nil {
			return fmt.Errorf("%s: restore mode %s: %w", op.OperID, op.Target, err)
		}
	}
	if before.Owner != "" {
		if err := r.chown(op.Target, before.Owner); err != nil {
			return fmt.Errorf("%s: restore owner %s: %w", op.OperID, op.Target, err)
		}
	}
	return nil
}

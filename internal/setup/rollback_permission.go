package setup

import (
	"context"
	"fmt"
	"os"
)

// PermissionRollback reverses PermissionExecutor operations.
// Injectable OS hooks allow tests to run without touching the filesystem.
type PermissionRollback struct {
	emitter *Emitter
	chmod   func(string, os.FileMode) error
	chown   func(string, string) error
}

func newPermissionRollback(emitter *Emitter) *PermissionRollback {
	return &PermissionRollback{
		emitter: emitter,
		chmod:   os.Chmod,
		chown:   chownPath,
	}
}

func (r *PermissionRollback) rollback(_ context.Context, op *TxOperation) error {
	before, ok := op.Before.(*PermSnapshot)
	if !ok {
		return fmt.Errorf("%s: missing PermSnapshot for rollback", op.OperID)
	}

	// Restore original mode.
	if before.Mode != 0 {
		if err := r.chmod(op.Target, before.Mode); err != nil {
			return fmt.Errorf("%s: restore mode %s: %w", op.OperID, op.Target, err)
		}
	}

	// Restore original owner (captured from live OS during Apply, not from the plan).
	if before.Owner != "" {
		if err := r.chown(op.Target, before.Owner); err != nil {
			return fmt.Errorf("%s: restore owner %s: %w", op.OperID, op.Target, err)
		}
	}
	return nil
}

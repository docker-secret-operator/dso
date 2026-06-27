package setup

import (
	"context"
	"fmt"
)

// ServiceRollback reverses ServiceExecutor operations.
// It compares Before and After snapshots and issues only the sysctls needed
// to restore the service to exactly the state it was in before Apply ran.
// Injectable hooks allow tests to run without systemd.
type ServiceRollback struct {
	emitter *Emitter
	enable  func(ctx context.Context, name string) error
	start   func(ctx context.Context, name string) error
	stop    func(ctx context.Context, name string) error
	disable func(ctx context.Context, name string) error
}

func newServiceRollback(emitter *Emitter) *ServiceRollback {
	return &ServiceRollback{
		emitter: emitter,
		enable:  systemctlEnable,
		start:   systemctlStart,
		stop:    systemctlStop,
		disable: systemctlDisable,
	}
}

func (r *ServiceRollback) rollback(ctx context.Context, op *TxOperation) error {
	before, ok := op.Before.(*ServiceSnapshot)
	if !ok {
		return fmt.Errorf("%s: missing ServiceSnapshot for rollback", op.OperID)
	}
	after, ok := op.After.(*ServiceSnapshot)
	if !ok {
		return fmt.Errorf("%s: missing After ServiceSnapshot for rollback", op.OperID)
	}

	// Undo running state first (stop before disable, matching systemd best practice).
	if after.Active && !before.Active {
		if err := r.stop(ctx, op.Target); err != nil {
			return fmt.Errorf("%s: stop %s: %w", op.OperID, op.Target, err)
		}
	} else if !after.Active && before.Active {
		if err := r.start(ctx, op.Target); err != nil {
			return fmt.Errorf("%s: restart %s: %w", op.OperID, op.Target, err)
		}
	}

	// Undo enabled state.
	if after.Enabled && !before.Enabled {
		if err := r.disable(ctx, op.Target); err != nil {
			return fmt.Errorf("%s: disable %s: %w", op.OperID, op.Target, err)
		}
	} else if !after.Enabled && before.Enabled {
		if err := r.enable(ctx, op.Target); err != nil {
			return fmt.Errorf("%s: re-enable %s: %w", op.OperID, op.Target, err)
		}
	}

	return nil
}

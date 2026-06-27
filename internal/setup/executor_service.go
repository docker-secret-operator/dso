package setup

import (
	"context"
	"fmt"
	"os/exec"
)

// ServiceExecutor enables and starts systemd services declared in the InstallPlan.
// systemctl invocations are injected via function fields so tests never require systemd.
type ServiceExecutor struct {
	ops     []ServiceChange
	emitter *Emitter
	// Injectable systemctl hooks — defaults call the real binary.
	enable    func(ctx context.Context, name string) error
	start     func(ctx context.Context, name string) error
	stop      func(ctx context.Context, name string) error
	disable   func(ctx context.Context, name string) error
	isEnabled func(name string) (bool, error)
	isActive  func(name string) (bool, error)
}

func newServiceExecutor(ops []ServiceChange, emitter *Emitter) *ServiceExecutor {
	return &ServiceExecutor{
		ops:       ops,
		emitter:   emitter,
		enable:    systemctlEnable,
		start:     systemctlStart,
		stop:      systemctlStop,
		disable:   systemctlDisable,
		isEnabled: systemctlIsEnabled,
		isActive:  systemctlIsActive,
	}
}

func (e *ServiceExecutor) execute(ctx context.Context, tx *Transaction) error {
	for i := range e.ops {
		if err := e.executeOne(ctx, &e.ops[i], tx); err != nil {
			return err
		}
	}
	return nil
}

func (e *ServiceExecutor) executeOne(ctx context.Context, op *ServiceChange, tx *Transaction) error {
	txOp := appendOperation(tx, op.ID, "service_"+op.Operation, op.Name)
	markRunning(txOp)
	e.emitter.emit(EventOperationStarted, txOp, nil)

	// Snapshot service state before the operation so rollback can restore it.
	enabled, _ := e.isEnabled(op.Name)
	active, _ := e.isActive(op.Name)
	txOp.Before = &ServiceSnapshot{Enabled: enabled, Active: active}

	var execErr error
	switch op.Operation {
	case "enable":
		execErr = e.enable(ctx, op.Name)
	case "start":
		execErr = e.start(ctx, op.Name)
	case "stop":
		execErr = e.stop(ctx, op.Name)
	case "disable":
		execErr = e.disable(ctx, op.Name)
	default:
		execErr = fmt.Errorf("unknown service operation %q", op.Operation)
	}

	if execErr != nil {
		markFailed(txOp, execErr)
		e.emitter.emit(EventOperationFailed, txOp, execErr)
		return fmt.Errorf("%s: systemctl %s %s: %w", op.ID, op.Operation, op.Name, execErr)
	}

	txOp.After = &ServiceSnapshot{
		Enabled: op.Operation == "enable" || enabled,
		Active:  op.Operation == "start" || active,
	}
	markCompleted(txOp)
	e.emitter.emit(EventOperationCompleted, txOp, nil)
	return nil
}

// ─── Real systemctl helpers ───────────────────────────────────────────────────

func systemctlEnable(ctx context.Context, name string) error {
	return exec.CommandContext(ctx, "systemctl", "enable", name).Run()
}

func systemctlStart(ctx context.Context, name string) error {
	return exec.CommandContext(ctx, "systemctl", "start", name).Run()
}

func systemctlStop(ctx context.Context, name string) error {
	return exec.CommandContext(ctx, "systemctl", "stop", name).Run()
}

func systemctlDisable(ctx context.Context, name string) error {
	return exec.CommandContext(ctx, "systemctl", "disable", name).Run()
}

// systemctlIsEnabled returns true when systemctl reports the service as enabled.
// A non-zero exit code (service not found, not enabled) is treated as false, not an error.
func systemctlIsEnabled(name string) (bool, error) {
	err := exec.Command("systemctl", "is-enabled", "--quiet", name).Run()
	return err == nil, nil
}

// systemctlIsActive returns true when systemctl reports the service as running.
func systemctlIsActive(name string) (bool, error) {
	err := exec.Command("systemctl", "is-active", "--quiet", name).Run()
	return err == nil, nil
}

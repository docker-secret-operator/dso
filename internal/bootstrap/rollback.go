package bootstrap

import (
	"context"
	"fmt"
	"sync"
)

// TransactionExecutor executes bootstrap operations with transactional rollback on failure
type TransactionExecutor struct {
	mu         sync.Mutex
	operations []Operation
	rollbacks  []RollbackFunc
	logger     Logger
}

// NewTransactionExecutor creates a new transaction executor
func NewTransactionExecutor(logger Logger) *TransactionExecutor {
	return &TransactionExecutor{
		operations: []Operation{},
		rollbacks:  []RollbackFunc{},
		logger:     logger,
	}
}

// AddOperation adds an operation and its rollback function
func (te *TransactionExecutor) AddOperation(op Operation, rollback RollbackFunc) {
	te.mu.Lock()
	defer te.mu.Unlock()

	te.operations = append(te.operations, op)
	te.rollbacks = append(te.rollbacks, rollback)
	te.logger.Info("Operation queued", "operation", op.Name())
}

// Execute executes all operations in sequence, rolling back on first failure
func (te *TransactionExecutor) Execute(ctx context.Context) error {
	te.mu.Lock()
	// Take a snapshot of operations to avoid holding the lock during execution
	operations := make([]Operation, len(te.operations))
	copy(operations, te.operations)
	te.mu.Unlock()

	completed := 0

	for i, op := range operations {
		// Check context cancellation before each operation
		select {
		case <-ctx.Done():
			te.logger.Error("Bootstrap cancelled via context", "error", ctx.Err().Error())
			// Rollback on cancellation
			rollbackErr := te.rollback(ctx, completed)
			return fmt.Errorf("bootstrap cancelled: %w (rollback: %v)", ctx.Err(), rollbackErr)
		default:
		}

		te.logger.Info("Executing operation", "operation", op.Name(), "step", i+1, "of", len(operations))

		if err := op.Execute(ctx); err != nil {
			te.logger.Error("Operation failed, initiating rollback", "operation", op.Name(), "error", err.Error())

			// Rollback completed operations in reverse order
			rollbackErr := te.rollback(ctx, completed)

			// Wrap both errors
			return fmt.Errorf("operation '%s' failed: %w (rollback: %v)", op.Name(), err, rollbackErr)
		}

		completed++
		te.logger.Info("Operation completed", "operation", op.Name())
	}

	te.logger.Info("Transaction completed successfully", "operations", len(operations))
	return nil
}

// rollback executes rollback functions for completed operations in reverse order
func (te *TransactionExecutor) rollback(ctx context.Context, completedCount int) error {
	if completedCount == 0 {
		te.logger.Info("No operations completed, nothing to rollback")
		return nil
	}

	te.mu.Lock()
	// Take snapshot of operations to rollback
	rollbackOps := make([]Operation, completedCount)
	rollbackFuncs := make([]RollbackFunc, completedCount)
	copy(rollbackOps, te.operations[:completedCount])
	copy(rollbackFuncs, te.rollbacks[:completedCount])
	te.mu.Unlock()

	te.logger.Warn("Rolling back", "operations", completedCount)

	var rollbackErrors []string

	// Execute rollbacks in reverse order (LIFO)
	for i := completedCount - 1; i >= 0; i-- {
		op := rollbackOps[i]
		rollback := rollbackFuncs[i]

		te.logger.Info("Rolling back operation", "operation", op.Name())

		if err := rollback(ctx); err != nil {
			errMsg := fmt.Sprintf("%s: %v", op.Name(), err)
			rollbackErrors = append(rollbackErrors, errMsg)
			te.logger.Error("Rollback failed", "operation", op.Name(), "error", err.Error())
		} else {
			te.logger.Info("Operation rolled back", "operation", op.Name())
		}
	}

	if len(rollbackErrors) > 0 {
		return ErrRollback("transaction", "operation", fmt.Errorf("rollback errors: %v", rollbackErrors))
	}

	te.logger.Info("Rollback completed successfully")
	return nil
}

// GetOperationCount returns the number of queued operations
func (te *TransactionExecutor) GetOperationCount() int {
	te.mu.Lock()
	defer te.mu.Unlock()
	return len(te.operations)
}

// ClearOperations clears all queued operations and rollbacks
func (te *TransactionExecutor) ClearOperations() {
	te.mu.Lock()
	defer te.mu.Unlock()

	te.operations = []Operation{}
	te.rollbacks = []RollbackFunc{}
	te.logger.Info("Transaction cleared")
}

// SimpleOperation is a convenience struct for simple operations
type SimpleOperation struct {
	name string
	fn   func(context.Context) error
}

// NewSimpleOperation creates a simple operation
func NewSimpleOperation(name string, fn func(context.Context) error) Operation {
	return &SimpleOperation{
		name: name,
		fn:   fn,
	}
}

// Name returns the operation name
func (so *SimpleOperation) Name() string {
	return so.name
}

// Execute executes the operation
func (so *SimpleOperation) Execute(ctx context.Context) error {
	return so.fn(ctx)
}

// BootstrapOperations defines standard bootstrap operations
type BootstrapOperations struct {
	logger Logger
	fsOps  *FilesystemOps
	svc    *SystemdManager
	perm   *PermissionManager
}

// NewBootstrapOperations creates bootstrap operations
func NewBootstrapOperations(logger Logger, fsOps *FilesystemOps, svc *SystemdManager, perm *PermissionManager) *BootstrapOperations {
	return &BootstrapOperations{
		logger: logger,
		fsOps:  fsOps,
		svc:    svc,
		perm:   perm,
	}
}

// CreateDirectoriesOp creates bootstrap directories with rollback
func (bo *BootstrapOperations) CreateDirectoriesOp(dsoGID int) (Operation, RollbackFunc) {
	op := NewSimpleOperation("create-directories", func(ctx context.Context) error {
		return bo.fsOps.SafeCreateDirectory(ctx, "/etc/dso", 0750, 0, dsoGID)
	})

	rollback := func(ctx context.Context) error {
		// Don't delete /etc/dso on rollback - user may have other config
		bo.logger.Warn("Skipping deletion of /etc/dso on rollback")
		return nil
	}

	return op, rollback
}

// WriteConfigOp writes configuration file with rollback
func (bo *BootstrapOperations) WriteConfigOp(configPath string, content []byte) (Operation, RollbackFunc) {
	op := NewSimpleOperation("write-config", func(ctx context.Context) error {
		return bo.fsOps.SafeWriteFile(ctx, configPath, content, 0640)
	})

	rollback := func(ctx context.Context) error {
		return bo.fsOps.SafeRemove(ctx, configPath)
	}

	return op, rollback
}

// InstallServiceOp installs systemd service with rollback
// Note: Context is provided at execution time, not construction time
func (bo *BootstrapOperations) InstallServiceOp() (Operation, RollbackFunc) {
	op := NewSimpleOperation("install-service", func(ctx context.Context) error {
		return bo.svc.InstallServiceFile(ctx, bo.fsOps)
	})

	rollback := func(ctx context.Context) error {
		return bo.svc.RemoveServiceFile(ctx, bo.fsOps)
	}

	return op, rollback
}

// ReloadSystemdOp reloads systemd with rollback
func (bo *BootstrapOperations) ReloadSystemdOp() (Operation, RollbackFunc) {
	op := NewSimpleOperation("reload-systemd", func(ctx context.Context) error {
		return bo.svc.ReloadSystemd(ctx)
	})

	// Reload doesn't need specific rollback - it's idempotent
	rollback := func(ctx context.Context) error {
		return bo.svc.ReloadSystemd(ctx)
	}

	return op, rollback
}

// EnableServiceOp enables systemd service with rollback
func (bo *BootstrapOperations) EnableServiceOp() (Operation, RollbackFunc) {
	op := NewSimpleOperation("enable-service", func(ctx context.Context) error {
		return bo.svc.EnableService(ctx)
	})

	rollback := func(ctx context.Context) error {
		return bo.svc.DisableService(ctx)
	}

	return op, rollback
}

// SetupPermissionsOp sets up permissions with rollback
func (bo *BootstrapOperations) SetupPermissionsOp(invokerUID, invokerGID, dsoGID int) (Operation, RollbackFunc) {
	op := NewSimpleOperation("setup-permissions", func(ctx context.Context) error {
		return bo.perm.SetupBootstrapPermissions(ctx, invokerUID, invokerGID)
	})

	rollback := func(ctx context.Context) error {
		// Permission rollback is complex - just log what was done
		bo.logger.Warn("Manual permission rollback may be needed")
		return nil
	}

	return op, rollback
}

// VerifyInstallationOp verifies the bootstrap installation
func (bo *BootstrapOperations) VerifyInstallationOp(configPath string) (Operation, RollbackFunc) {
	op := NewSimpleOperation("verify-installation", func(ctx context.Context) error {
		// Check config file exists
		if _, err := bo.fsOps.ValidatePath("/", configPath); err != nil {
			return fmt.Errorf("config file validation failed: %w", err)
		}

		// Check directories exist
		requiredDirs := []string{"/etc/dso", "/var/lib/dso", "/var/run/dso", "/var/log/dso"}
		for _, dir := range requiredDirs {
			if _, err := bo.fsOps.ValidatePath("/", dir); err != nil {
				return fmt.Errorf("directory validation failed for %s: %w", dir, err)
			}
		}

		return nil
	})

	// No rollback needed for verification
	rollback := func(ctx context.Context) error {
		return nil
	}

	return op, rollback
}

// BuildBootstrapTransaction constructs the standard bootstrap transaction sequence
func BuildBootstrapTransaction(logger Logger, fsOps *FilesystemOps, svc *SystemdManager, perm *PermissionManager,
	configPath string, configContent []byte, invokerUID, invokerGID, dsoGID int) *TransactionExecutor {

	tx := NewTransactionExecutor(logger)
	ops := NewBootstrapOperations(logger, fsOps, svc, perm)

	// 1. Create directories
	dirOp, dirRollback := ops.CreateDirectoriesOp(dsoGID)
	tx.AddOperation(dirOp, dirRollback)

	// 2. Write configuration
	cfgOp, cfgRollback := ops.WriteConfigOp(configPath, configContent)
	tx.AddOperation(cfgOp, cfgRollback)

	// 3. Install systemd service file
	// Context is passed when Execute() is called, not here
	svcOp, svcRollback := ops.InstallServiceOp()
	tx.AddOperation(svcOp, svcRollback)

	// 4. Reload systemd daemon
	reloadOp, reloadRollback := ops.ReloadSystemdOp()
	tx.AddOperation(reloadOp, reloadRollback)

	// 5. Enable service
	enableOp, enableRollback := ops.EnableServiceOp()
	tx.AddOperation(enableOp, enableRollback)

	// 6. Setup permissions
	permOp, permRollback := ops.SetupPermissionsOp(invokerUID, invokerGID, dsoGID)
	tx.AddOperation(permOp, permRollback)

	// 7. Verify installation
	verifyOp, verifyRollback := ops.VerifyInstallationOp(configPath)
	tx.AddOperation(verifyOp, verifyRollback)

	return tx
}

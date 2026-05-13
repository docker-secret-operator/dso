package bootstrap

import (
	"context"
	"fmt"
	"testing"
)

// TestTransactionExecutor tests transactional execution
func TestTransactionExecutor(t *testing.T) {
	logger := &testLogger{}
	tx := NewTransactionExecutor(logger)

	if tx.GetOperationCount() != 0 {
		t.Errorf("Initial operation count mismatch: got %d, want 0", tx.GetOperationCount())
	}

	// Add operations
	op1Called := false
	op2Called := false

	op1 := NewSimpleOperation("op1", func(ctx context.Context) error {
		op1Called = true
		return nil
	})

	op2 := NewSimpleOperation("op2", func(ctx context.Context) error {
		op2Called = true
		return nil
	})

	tx.AddOperation(op1, func(ctx context.Context) error { return nil })
	tx.AddOperation(op2, func(ctx context.Context) error { return nil })

	if tx.GetOperationCount() != 2 {
		t.Errorf("Operation count mismatch: got %d, want 2", tx.GetOperationCount())
	}

	// Execute
	ctx := context.Background()
	err := tx.Execute(ctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !op1Called || !op2Called {
		t.Error("Operations were not called")
	}
}

// TestTransactionRollback tests automatic rollback on failure
func TestTransactionRollback(t *testing.T) {
	logger := &testLogger{}
	tx := NewTransactionExecutor(logger)

	executed := []string{}
	rolledBack := []string{}

	// Operation 1: succeeds
	op1 := NewSimpleOperation("op1", func(ctx context.Context) error {
		executed = append(executed, "op1")
		return nil
	})

	// Operation 2: fails
	op2 := NewSimpleOperation("op2", func(ctx context.Context) error {
		executed = append(executed, "op2")
		return fmt.Errorf("op2 failed")
	})

	// Rollback 1
	rb1 := func(ctx context.Context) error {
		rolledBack = append(rolledBack, "op1-rollback")
		return nil
	}

	// Rollback 2
	rb2 := func(ctx context.Context) error {
		rolledBack = append(rolledBack, "op2-rollback")
		return nil
	}

	tx.AddOperation(op1, rb1)
	tx.AddOperation(op2, rb2)

	// Execute (should fail and rollback)
	ctx := context.Background()
	err := tx.Execute(ctx)
	if err == nil {
		t.Fatal("Execute() should have returned error")
	}

	// Verify op1 was executed
	if len(executed) < 1 || executed[0] != "op1" {
		t.Errorf("op1 not executed: %v", executed)
	}

	// Verify op2 was executed and failed
	if len(executed) < 2 || executed[1] != "op2" {
		t.Errorf("op2 not executed: %v", executed)
	}

	// Verify LIFO rollback (op1 rolled back first, then op2)
	if len(rolledBack) != 1 {
		t.Errorf("Rollback count mismatch: got %d rollbacks, want 1 (op1 only)", len(rolledBack))
	}

	if len(rolledBack) > 0 && rolledBack[0] != "op1-rollback" {
		t.Errorf("Rollback order mismatch: got %v, expected op1 to roll back first", rolledBack)
	}
}

// TestSimpleOperation tests simple operations
func TestSimpleOperation(t *testing.T) {
	called := false
	op := NewSimpleOperation("test-op", func(ctx context.Context) error {
		called = true
		return nil
	})

	if op.Name() != "test-op" {
		t.Errorf("Name mismatch: got %q, want %q", op.Name(), "test-op")
	}

	err := op.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !called {
		t.Fatal("Operation function was not called")
	}
}

// TestTransactionClear tests clearing operations
func TestTransactionClear(t *testing.T) {
	logger := &testLogger{}
	tx := NewTransactionExecutor(logger)

	tx.AddOperation(NewSimpleOperation("op1", func(ctx context.Context) error {
		return nil
	}), func(ctx context.Context) error {
		return nil
	})

	if tx.GetOperationCount() != 1 {
		t.Errorf("Before clear: got %d operations, want 1", tx.GetOperationCount())
	}

	tx.ClearOperations()

	if tx.GetOperationCount() != 0 {
		t.Errorf("After clear: got %d operations, want 0", tx.GetOperationCount())
	}
}

// TestTransactionWithContextCancellation tests handling cancelled context
func TestTransactionWithContextCancellation(t *testing.T) {
	logger := &testLogger{}
	tx := NewTransactionExecutor(logger)

	op := NewSimpleOperation("op", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	tx.AddOperation(op, func(ctx context.Context) error {
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tx.Execute(ctx)
	if err == nil {
		t.Fatal("Execute() should have returned context error")
	}
}

// TestBootstrapOperations tests standard bootstrap operation helpers
func TestBootstrapOperations(t *testing.T) {
	logger := &testLogger{}
	fsOps := NewFilesystemOps(logger, true)
	svc := NewSystemdManager(logger, true)
	perm := NewPermissionManager(logger, true)

	ops := NewBootstrapOperations(logger, fsOps, svc, perm)

	if ops == nil {
		t.Fatal("NewBootstrapOperations returned nil")
	}

	// Verify we can create bootstrap operations
	dirOp, dirRollback := ops.CreateDirectoriesOp(1001)
	if dirOp == nil || dirRollback == nil {
		t.Fatal("CreateDirectoriesOp returned nil")
	}

	cfgOp, cfgRollback := ops.WriteConfigOp("/tmp/test.yaml", []byte("test"))
	if cfgOp == nil || cfgRollback == nil {
		t.Fatal("WriteConfigOp returned nil")
	}

	verifyOp, verifyRollback := ops.VerifyInstallationOp("/tmp/test.yaml")
	if verifyOp == nil || verifyRollback == nil {
		t.Fatal("VerifyInstallationOp returned nil")
	}
}

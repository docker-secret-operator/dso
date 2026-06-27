package setup

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// ─── Snapshot builders ────────────────────────────────────────────────────────

func completedDirOp(id, path string, before, after *DirSnapshot) TxOperation {
	op := TxOperation{
		Sequence:  1,
		OperID:    id,
		Type:      "directory_create",
		Target:    path,
		Status:    StatusCompleted,
		Before:    before,
		After:     after,
		Reversible: true,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	return op
}

func completedFileOp(id, path string, before, after *FileSnapshot) TxOperation {
	return TxOperation{
		Sequence:  1,
		OperID:    id,
		Type:      "file_create",
		Target:    path,
		Status:    StatusCompleted,
		Before:    before,
		After:     after,
		Reversible: true,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
}

func completedPermOp(id, path string, before, after *PermSnapshot) TxOperation {
	return TxOperation{
		Sequence:  1,
		OperID:    id,
		Type:      "permission_change",
		Target:    path,
		Status:    StatusCompleted,
		Before:    before,
		After:     after,
		Reversible: true,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
}

func completedServiceOp(id, name string, before, after *ServiceSnapshot) TxOperation {
	return TxOperation{
		Sequence:  1,
		OperID:    id,
		Type:      "service_enable",
		Target:    name,
		Status:    StatusCompleted,
		Before:    before,
		After:     after,
		Reversible: true,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
}

// txWithOps builds a Transaction pre-loaded with the given operations.
func txWithOps(ops ...TxOperation) *Transaction {
	tx := newTransaction("plan-rollback-test")
	for i, op := range ops {
		op.Sequence = i + 1
		tx.Operations = append(tx.Operations, op)
	}
	tx.Status = StatusFailed
	return tx
}

// ─── DirectoryRollback ────────────────────────────────────────────────────────

func TestDirectoryRollback_NewDir_RemovesCalled(t *testing.T) {
	var removed string
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(p string) error { removed = p; return nil }
	rb.chmod = noopChmod
	rb.chown = noopChown

	op := completedDirOp("DIR-001", "/new/dir",
		&DirSnapshot{Existed: false},
		&DirSnapshot{Existed: true, Mode: 0700},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != "/new/dir" {
		t.Errorf("want removeDir called with '/new/dir', got %q", removed)
	}
}

func TestDirectoryRollback_NewDir_AlreadyGone_NoError(t *testing.T) {
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(_ string) error { return os.ErrNotExist }
	rb.chmod = noopChmod
	rb.chown = noopChown

	op := completedDirOp("DIR-001", "/gone",
		&DirSnapshot{Existed: false},
		&DirSnapshot{Existed: true},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Errorf("expected no error for already-removed dir, got %v", err)
	}
}

func TestDirectoryRollback_ExistingDir_RestoresMode(t *testing.T) {
	var restoredMode os.FileMode
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(_ string) error { t.Fatal("removeDir must not be called for existing dir"); return nil }
	rb.chmod = func(_ string, m os.FileMode) error { restoredMode = m; return nil }
	rb.chown = noopChown

	op := completedDirOp("DIR-001", "/existing",
		&DirSnapshot{Existed: true, Mode: 0755, Owner: ""},
		&DirSnapshot{Existed: true, Mode: 0700},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restoredMode != 0755 {
		t.Errorf("want restored mode 0755, got %04o", restoredMode)
	}
}

func TestDirectoryRollback_ExistingDir_RestoresOwner(t *testing.T) {
	var restoredOwner string
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(_ string) error { return nil }
	rb.chmod = noopChmod
	rb.chown = func(_ string, o string) error { restoredOwner = o; return nil }

	op := completedDirOp("DIR-001", "/existing",
		&DirSnapshot{Existed: true, Mode: 0755, Owner: "original:group"},
		&DirSnapshot{Existed: true, Mode: 0700, Owner: "dso:dso"},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restoredOwner != "original:group" {
		t.Errorf("want owner 'original:group', got %q", restoredOwner)
	}
}

func TestDirectoryRollback_ExistingDir_EmptyOwner_SkipsChown(t *testing.T) {
	var chownCalled bool
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(_ string) error { return nil }
	rb.chmod = noopChmod
	rb.chown = func(_, _ string) error { chownCalled = true; return nil }

	op := completedDirOp("DIR-001", "/existing",
		&DirSnapshot{Existed: true, Mode: 0755, Owner: ""},
		&DirSnapshot{Existed: true},
	)
	_ = rb.rollback(context.Background(), &op)
	if chownCalled {
		t.Error("chown must not be called when Before.Owner is empty")
	}
}

func TestDirectoryRollback_RemoveError_ReturnsError(t *testing.T) {
	rb := newDirectoryRollback(&Emitter{})
	rb.removeDir = func(_ string) error { return errors.New("permission denied") }
	rb.chmod = noopChmod
	rb.chown = noopChown

	op := completedDirOp("DIR-001", "/stuck",
		&DirSnapshot{Existed: false},
		&DirSnapshot{Existed: true},
	)
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error from removeDir failure")
	}
}

func TestDirectoryRollback_MissingSnapshot_ReturnsError(t *testing.T) {
	rb := newDirectoryRollback(&Emitter{})
	op := TxOperation{OperID: "DIR-001", Type: "directory_create", Target: "/x", Before: nil}
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error for missing snapshot")
	}
}

// ─── FileRollback ─────────────────────────────────────────────────────────────

func TestFileRollback_NewFile_Removed(t *testing.T) {
	var removed string
	rb := newFileRollback(&Emitter{})
	rb.removeFile = func(p string) error { removed = p; return nil }
	rb.writeFile = noopWriteFile
	rb.chown = noopChown

	op := completedFileOp("FILE-001", "/new/file.yaml",
		&FileSnapshot{Existed: false},
		&FileSnapshot{Existed: true},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != "/new/file.yaml" {
		t.Errorf("want removeFile called with '/new/file.yaml', got %q", removed)
	}
}

func TestFileRollback_NewFile_AlreadyGone_NoError(t *testing.T) {
	rb := newFileRollback(&Emitter{})
	rb.removeFile = func(_ string) error { return os.ErrNotExist }
	rb.writeFile = noopWriteFile
	rb.chown = noopChown

	op := completedFileOp("FILE-001", "/gone.yaml",
		&FileSnapshot{Existed: false},
		&FileSnapshot{Existed: true},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Errorf("expected no error for already-gone file, got %v", err)
	}
}

func TestFileRollback_ExistingFile_RestoresContent(t *testing.T) {
	var writtenContent []byte
	rb := newFileRollback(&Emitter{})
	rb.removeFile = func(_ string) error { t.Fatal("removeFile must not be called for existing file"); return nil }
	rb.writeFile = func(_ string, b []byte, _ os.FileMode) error { writtenContent = b; return nil }
	rb.chown = noopChown

	original := []byte("original config content")
	op := completedFileOp("FILE-001", "/etc/dso/dso.yaml",
		&FileSnapshot{Existed: true, Content: original, Mode: 0600},
		&FileSnapshot{Existed: true, Content: []byte("new content")},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(writtenContent) != string(original) {
		t.Errorf("want original content restored, got %q", writtenContent)
	}
}

func TestFileRollback_ExistingFile_RestoresMode(t *testing.T) {
	var restoredMode os.FileMode
	rb := newFileRollback(&Emitter{})
	rb.removeFile = func(_ string) error { return nil }
	rb.writeFile = func(_ string, _ []byte, m os.FileMode) error { restoredMode = m; return nil }
	rb.chown = noopChown

	op := completedFileOp("FILE-001", "/etc/dso/dso.yaml",
		&FileSnapshot{Existed: true, Content: []byte("x"), Mode: 0644},
		&FileSnapshot{Existed: true, Mode: 0600},
	)
	_ = rb.rollback(context.Background(), &op)
	if restoredMode != 0644 {
		t.Errorf("want mode 0644, got %04o", restoredMode)
	}
}

func TestFileRollback_ExistingFile_RestoresOwner(t *testing.T) {
	var restoredOwner string
	rb := newFileRollback(&Emitter{})
	rb.removeFile = func(_ string) error { return nil }
	rb.writeFile = noopWriteFile
	rb.chown = func(_ string, o string) error { restoredOwner = o; return nil }

	op := completedFileOp("FILE-001", "/etc/dso/dso.yaml",
		&FileSnapshot{Existed: true, Content: []byte("x"), Mode: 0600, Owner: "admin:admin"},
		&FileSnapshot{Existed: true},
	)
	_ = rb.rollback(context.Background(), &op)
	if restoredOwner != "admin:admin" {
		t.Errorf("want owner 'admin:admin', got %q", restoredOwner)
	}
}

func TestFileRollback_MissingSnapshot_ReturnsError(t *testing.T) {
	rb := newFileRollback(&Emitter{})
	op := TxOperation{OperID: "FILE-001", Type: "file_create", Target: "/f", Before: nil}
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error for missing snapshot")
	}
}

// ─── PermissionRollback ───────────────────────────────────────────────────────

func TestPermissionRollback_RestoresMode(t *testing.T) {
	var restoredMode os.FileMode
	rb := newPermissionRollback(&Emitter{})
	rb.chmod = func(_ string, m os.FileMode) error { restoredMode = m; return nil }
	rb.chown = noopChown

	op := completedPermOp("PERM-001", "/etc/dso",
		&PermSnapshot{Mode: 0755, Owner: ""},
		&PermSnapshot{Mode: 0750},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restoredMode != 0755 {
		t.Errorf("want restored mode 0755, got %04o", restoredMode)
	}
}

func TestPermissionRollback_RestoresOwner(t *testing.T) {
	var restoredOwner string
	rb := newPermissionRollback(&Emitter{})
	rb.chmod = noopChmod
	rb.chown = func(_ string, o string) error { restoredOwner = o; return nil }

	op := completedPermOp("PERM-001", "/etc/dso",
		&PermSnapshot{Mode: 0755, Owner: "original:group"},
		&PermSnapshot{Mode: 0750, Owner: "dso:dso"},
	)
	_ = rb.rollback(context.Background(), &op)
	if restoredOwner != "original:group" {
		t.Errorf("want owner 'original:group', got %q", restoredOwner)
	}
}

func TestPermissionRollback_EmptyOwner_SkipsChown(t *testing.T) {
	var chownCalled bool
	rb := newPermissionRollback(&Emitter{})
	rb.chmod = noopChmod
	rb.chown = func(_, _ string) error { chownCalled = true; return nil }

	op := completedPermOp("PERM-001", "/etc/dso",
		&PermSnapshot{Mode: 0755, Owner: ""},
		&PermSnapshot{Mode: 0750},
	)
	_ = rb.rollback(context.Background(), &op)
	if chownCalled {
		t.Error("chown must not be called when Before.Owner is empty")
	}
}

func TestPermissionRollback_ChmodError_ReturnsError(t *testing.T) {
	rb := newPermissionRollback(&Emitter{})
	rb.chmod = errChmod
	rb.chown = noopChown

	op := completedPermOp("PERM-001", "/etc/dso",
		&PermSnapshot{Mode: 0755},
		&PermSnapshot{Mode: 0750},
	)
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error from chmod failure")
	}
}

func TestPermissionRollback_MissingSnapshot_ReturnsError(t *testing.T) {
	rb := newPermissionRollback(&Emitter{})
	op := TxOperation{OperID: "PERM-001", Type: "permission_change", Target: "/x", Before: nil}
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error for missing snapshot")
	}
}

// ─── ServiceRollback ──────────────────────────────────────────────────────────

func TestServiceRollback_WasEnabled_StaysEnabled_NoOp(t *testing.T) {
	var disableCalled bool
	rb := newServiceRollback(&Emitter{})
	rb.enable = noopCtxHook
	rb.disable = func(_ context.Context, _ string) error { disableCalled = true; return nil }
	rb.start = noopCtxHook
	rb.stop = noopCtxHook

	op := completedServiceOp("SERVICE-001", "dso-agent.service",
		&ServiceSnapshot{Enabled: true, Active: false},
		&ServiceSnapshot{Enabled: true, Active: false}, // no change
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disableCalled {
		t.Error("disable must not be called when enabled state unchanged")
	}
}

func TestServiceRollback_NewlyEnabled_Disables(t *testing.T) {
	var disabled string
	rb := newServiceRollback(&Emitter{})
	rb.enable = noopCtxHook
	rb.disable = func(_ context.Context, n string) error { disabled = n; return nil }
	rb.start = noopCtxHook
	rb.stop = noopCtxHook

	op := completedServiceOp("SERVICE-001", "dso-agent.service",
		&ServiceSnapshot{Enabled: false, Active: false},
		&ServiceSnapshot{Enabled: true, Active: false},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disabled != "dso-agent.service" {
		t.Errorf("want disable called with 'dso-agent.service', got %q", disabled)
	}
}

func TestServiceRollback_WasEnabled_GotDisabled_ReEnables(t *testing.T) {
	var enabled string
	rb := newServiceRollback(&Emitter{})
	rb.enable = func(_ context.Context, n string) error { enabled = n; return nil }
	rb.disable = noopCtxHook
	rb.start = noopCtxHook
	rb.stop = noopCtxHook

	op := completedServiceOp("SERVICE-001", "dso-agent.service",
		&ServiceSnapshot{Enabled: true, Active: false},
		&ServiceSnapshot{Enabled: false, Active: false},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled != "dso-agent.service" {
		t.Errorf("want enable called with 'dso-agent.service', got %q", enabled)
	}
}

func TestServiceRollback_NewlyStarted_Stops(t *testing.T) {
	var stopped string
	rb := newServiceRollback(&Emitter{})
	rb.enable = noopCtxHook
	rb.disable = noopCtxHook
	rb.start = noopCtxHook
	rb.stop = func(_ context.Context, n string) error { stopped = n; return nil }

	op := completedServiceOp("SERVICE-001", "dso-agent.service",
		&ServiceSnapshot{Enabled: false, Active: false},
		&ServiceSnapshot{Enabled: false, Active: true},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stopped != "dso-agent.service" {
		t.Errorf("want stop called with 'dso-agent.service', got %q", stopped)
	}
}

func TestServiceRollback_WasActive_GotStopped_Restarts(t *testing.T) {
	var started string
	rb := newServiceRollback(&Emitter{})
	rb.enable = noopCtxHook
	rb.disable = noopCtxHook
	rb.start = func(_ context.Context, n string) error { started = n; return nil }
	rb.stop = noopCtxHook

	op := completedServiceOp("SERVICE-001", "dso-agent.service",
		&ServiceSnapshot{Enabled: true, Active: true},
		&ServiceSnapshot{Enabled: true, Active: false},
	)
	if err := rb.rollback(context.Background(), &op); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started != "dso-agent.service" {
		t.Errorf("want start called with 'dso-agent.service', got %q", started)
	}
}

func TestServiceRollback_MissingBefore_ReturnsError(t *testing.T) {
	rb := newServiceRollback(&Emitter{})
	op := TxOperation{
		OperID: "SERVICE-001", Type: "service_enable", Target: "dso.service",
		Before: nil,
		After:  &ServiceSnapshot{Enabled: true},
	}
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error for missing Before snapshot")
	}
}

func TestServiceRollback_MissingAfter_ReturnsError(t *testing.T) {
	rb := newServiceRollback(&Emitter{})
	op := TxOperation{
		OperID: "SERVICE-001", Type: "service_enable", Target: "dso.service",
		Before: &ServiceSnapshot{Enabled: false},
		After:  nil,
	}
	if err := rb.rollback(context.Background(), &op); err == nil {
		t.Error("expected error for missing After snapshot")
	}
}

// ─── Rollback engine (rollback.go) ───────────────────────────────────────────

func TestRollback_EmptyTransaction_ReturnsCompleted(t *testing.T) {
	tx := newTransaction("plan-001")
	tx.Status = StatusFailed

	rb := newRollback(&Emitter{})
	result := rb.Execute(context.Background(), tx)

	if len(result.Failed) != 0 {
		t.Errorf("expected no failures, got %d", len(result.Failed))
	}
	if tx.Status != StatusRolledBack {
		t.Errorf("want StatusRolledBack, got %q", tx.Status)
	}
}

func TestRollback_EmptyTransaction_EmitsStartedAndCompleted(t *testing.T) {
	em := &Emitter{}
	var types []EventType
	em.Subscribe(func(e Event) { types = append(types, e.Type) })

	tx := newTransaction("plan-001")
	tx.Status = StatusFailed

	rb := newRollback(em)
	rb.Execute(context.Background(), tx)

	if len(types) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(types))
	}
	if types[0] != EventRollbackStarted {
		t.Errorf("want EventRollbackStarted first, got %q", types[0])
	}
	if types[len(types)-1] != EventRollbackCompleted {
		t.Errorf("want EventRollbackCompleted last, got %q", types[len(types)-1])
	}
}

func TestRollback_ReverseOrder(t *testing.T) {
	// Build a tx with 3 ops in order: DIR-001, FILE-001, PERM-001.
	// Rollback must reverse them: PERM-001, FILE-001, DIR-001.
	var rollbackOrder []string

	em := &Emitter{}

	dir := newDirectoryRollback(em)
	dir.removeDir = func(p string) error { rollbackOrder = append(rollbackOrder, p); return nil }
	dir.chmod = noopChmod
	dir.chown = noopChown

	file := newFileRollback(em)
	file.removeFile = func(p string) error { rollbackOrder = append(rollbackOrder, p); return nil }
	file.writeFile = noopWriteFile
	file.chown = noopChown

	perm := newPermissionRollback(em)
	perm.chmod = func(p string, _ os.FileMode) error { rollbackOrder = append(rollbackOrder, p); return nil }
	perm.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  dir,
		file:       file,
		permission: perm,
		service:    newServiceRollback(em),
	}

	tx := txWithOps(
		TxOperation{Sequence: 1, OperID: "DIR-001", Type: "directory_create", Target: "/dir",
			Before: &DirSnapshot{Existed: false}, After: &DirSnapshot{Existed: true},
			Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now()},
		TxOperation{Sequence: 2, OperID: "FILE-001", Type: "file_create", Target: "/file",
			Before: &FileSnapshot{Existed: false}, After: &FileSnapshot{Existed: true},
			Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now()},
		TxOperation{Sequence: 3, OperID: "PERM-001", Type: "permission_change", Target: "/perm",
			Before: &PermSnapshot{Mode: 0755}, After: &PermSnapshot{Mode: 0750},
			Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now()},
	)

	rb.Execute(context.Background(), tx)

	want := []string{"/perm", "/file", "/dir"}
	if len(rollbackOrder) != len(want) {
		t.Fatalf("want %d rollback calls, got %d: %v", len(want), len(rollbackOrder), rollbackOrder)
	}
	for i, w := range want {
		if rollbackOrder[i] != w {
			t.Errorf("rollback[%d]: want %q, got %q", i, w, rollbackOrder[i])
		}
	}
}

func TestRollback_SkipsNonReversibleOps(t *testing.T) {
	var removeCalled bool
	em := &Emitter{}

	dir := newDirectoryRollback(em)
	dir.removeDir = func(_ string) error { removeCalled = true; return nil }
	dir.chmod = noopChmod
	dir.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  dir,
		file:       newFileRollback(em),
		permission: newPermissionRollback(em),
		service:    newServiceRollback(em),
	}

	// Op that failed (Reversible=false) must be skipped.
	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "DIR-001", Type: "directory_create", Target: "/dir",
		Before:    &DirSnapshot{Existed: false},
		Status:    StatusFailed,
		Reversible: false,
	})

	rb.Execute(context.Background(), tx)
	if removeCalled {
		t.Error("must not attempt rollback for non-reversible operation")
	}
}

func TestRollback_ContinuesPastFailure(t *testing.T) {
	em := &Emitter{}

	// First op (rolled back last due to reversal): file — succeeds.
	// Second op (rolled back first due to reversal): perm — fails.
	// Engine must still attempt the file rollback even though perm failed.
	var fileRolledBack bool

	file := newFileRollback(em)
	file.removeFile = func(_ string) error { fileRolledBack = true; return nil }
	file.writeFile = noopWriteFile
	file.chown = noopChown

	perm := newPermissionRollback(em)
	perm.chmod = errChmod // always fails
	perm.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  newDirectoryRollback(em),
		file:       file,
		permission: perm,
		service:    newServiceRollback(em),
	}

	tx := txWithOps(
		TxOperation{Sequence: 1, OperID: "FILE-001", Type: "file_create", Target: "/f",
			Before: &FileSnapshot{Existed: false}, After: &FileSnapshot{Existed: true},
			Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now()},
		TxOperation{Sequence: 2, OperID: "PERM-001", Type: "permission_change", Target: "/p",
			Before: &PermSnapshot{Mode: 0755}, After: &PermSnapshot{Mode: 0750},
			Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now()},
	)

	result := rb.Execute(context.Background(), tx)

	if !fileRolledBack {
		t.Error("file rollback must execute even after perm rollback failure")
	}
	if len(result.Failed) != 1 {
		t.Errorf("want 1 failure, got %d", len(result.Failed))
	}
	if result.Failed[0].OperID != "PERM-001" {
		t.Errorf("want PERM-001 failure, got %q", result.Failed[0].OperID)
	}
}

func TestRollback_PartialFailure_SetsRollbackFailed(t *testing.T) {
	em := &Emitter{}

	perm := newPermissionRollback(em)
	perm.chmod = errChmod
	perm.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  newDirectoryRollback(em),
		file:       newFileRollback(em),
		permission: perm,
		service:    newServiceRollback(em),
	}

	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "PERM-001", Type: "permission_change", Target: "/p",
		Before: &PermSnapshot{Mode: 0755}, After: &PermSnapshot{Mode: 0750},
		Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now(),
	})

	rb.Execute(context.Background(), tx)

	if tx.Status != StatusRollbackFailed {
		t.Errorf("want StatusRollbackFailed, got %q", tx.Status)
	}
}

func TestRollback_AllSuccess_SetsRolledBack(t *testing.T) {
	em := &Emitter{}

	dir := newDirectoryRollback(em)
	dir.removeDir = func(_ string) error { return nil }
	dir.chmod = noopChmod
	dir.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  dir,
		file:       newFileRollback(em),
		permission: newPermissionRollback(em),
		service:    newServiceRollback(em),
	}

	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "DIR-001", Type: "directory_create", Target: "/d",
		Before: &DirSnapshot{Existed: false}, After: &DirSnapshot{Existed: true},
		Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now(),
	})

	rb.Execute(context.Background(), tx)

	if tx.Status != StatusRolledBack {
		t.Errorf("want StatusRolledBack, got %q", tx.Status)
	}
}

func TestRollback_PartialFailure_EmitsRollbackFailed(t *testing.T) {
	em := &Emitter{}
	var types []EventType
	em.Subscribe(func(e Event) { types = append(types, e.Type) })

	perm := newPermissionRollback(em)
	perm.chmod = errChmod
	perm.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  newDirectoryRollback(em),
		file:       newFileRollback(em),
		permission: perm,
		service:    newServiceRollback(em),
	}

	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "PERM-001", Type: "permission_change", Target: "/p",
		Before: &PermSnapshot{Mode: 0755}, After: &PermSnapshot{Mode: 0750},
		Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now(),
	})
	rb.Execute(context.Background(), tx)

	last := types[len(types)-1]
	if last != EventRollbackFailed {
		t.Errorf("want EventRollbackFailed last on partial failure, got %q", last)
	}
}

func TestRollback_CompletedCollectsOperIDs(t *testing.T) {
	em := &Emitter{}

	dir := newDirectoryRollback(em)
	dir.removeDir = func(_ string) error { return nil }
	dir.chmod = noopChmod
	dir.chown = noopChown

	rb := &Rollback{
		emitter:    em,
		directory:  dir,
		file:       newFileRollback(em),
		permission: newPermissionRollback(em),
		service:    newServiceRollback(em),
	}

	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "DIR-001", Type: "directory_create", Target: "/d",
		Before: &DirSnapshot{Existed: false}, After: &DirSnapshot{Existed: true},
		Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now(),
	})

	result := rb.Execute(context.Background(), tx)

	if len(result.Completed) != 1 || result.Completed[0] != "DIR-001" {
		t.Errorf("want Completed=['DIR-001'], got %v", result.Completed)
	}
}

func TestRollback_TransactionIDPropagated(t *testing.T) {
	tx := newTransaction("plan-xyz")
	tx.Status = StatusFailed

	result := newRollback(&Emitter{}).Execute(context.Background(), tx)
	if result.TransactionID != tx.ID {
		t.Errorf("want TransactionID=%q, got %q", tx.ID, result.TransactionID)
	}
}

func TestRollback_EndTimeSet(t *testing.T) {
	tx := newTransaction("plan-001")
	tx.Status = StatusFailed

	result := newRollback(&Emitter{}).Execute(context.Background(), tx)
	if result.EndTime.IsZero() {
		t.Error("expected EndTime to be set")
	}
}

func TestRollback_Idempotent_SecondRunOnEmpty(t *testing.T) {
	tx := newTransaction("plan-001")
	tx.Status = StatusFailed

	rb := newRollback(&Emitter{})
	r1 := rb.Execute(context.Background(), tx)
	r2 := rb.Execute(context.Background(), tx)

	if len(r1.Failed) != 0 || len(r2.Failed) != 0 {
		t.Error("expected both runs to succeed on empty transaction")
	}
}

func TestRollback_UnknownOpType_Skipped(t *testing.T) {
	em := &Emitter{}

	rb := newRollback(em)

	tx := txWithOps(TxOperation{
		Sequence: 1, OperID: "GROUP-001", Type: "group_create", Target: "dso",
		Before: &GroupSnapshot{Existed: false}, After: &GroupSnapshot{Existed: true},
		Status: StatusCompleted, Reversible: true, StartedAt: time.Now(), EndedAt: time.Now(),
	})

	result := rb.Execute(context.Background(), tx)

	// Group rollback is deferred; the operation is silently skipped (no failure).
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failures for group op (silently skipped), got %d", len(result.Failed))
	}
}

// ─── Engine integration ───────────────────────────────────────────────────────

func TestEngine_Setup_FailedApply_AttachesRollbackResult(t *testing.T) {
	eng := newTestEngine(noopWizard)
	eng.applier = &stubApplier{err: errors.New("apply failed")}

	result, err := eng.Setup(context.Background(), SetupOptions{})
	if err == nil {
		t.Fatal("expected error from failed apply")
	}
	if result.Rollback == nil {
		t.Error("expected RollbackResult attached to SetupResult on apply failure")
	}
}

func TestEngine_Setup_SuccessfulApply_NoRollback(t *testing.T) {
	eng := newTestEngine(noopWizard)

	result, err := eng.Setup(context.Background(), SetupOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rollback != nil {
		t.Error("RollbackResult must be nil on successful apply")
	}
}

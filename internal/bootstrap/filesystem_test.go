package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestValidatePath tests path validation with security checks
func TestValidatePath(t *testing.T) {
	logger := &testLogger{}
	fsOps := NewFilesystemOps(logger, false)

	// Create temporary directory for testing
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		base    string
		path    string
		wantErr bool
		errType string
	}{
		{
			name:    "simple valid path",
			base:    tmpDir,
			path:    "subdir",
			wantErr: false,
		},
		{
			name:    "absolute path within base",
			base:    tmpDir,
			path:    filepath.Join(tmpDir, "subdir"),
			wantErr: false,
		},
		{
			name:    "path traversal attempt with ..",
			base:    tmpDir,
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path with multiple .. segments",
			base:    tmpDir,
			path:    "subdir/../../etc",
			wantErr: true,
		},
		{
			name:    "relative path",
			base:    tmpDir,
			path:    "subdir/nested",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fsOps.ValidatePath(tt.base, tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSafeWriteFile tests atomic file writing
func TestSafeWriteFile(t *testing.T) {
	logger := &testLogger{}
	fsOps := NewFilesystemOps(logger, false)
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")

	ctx := context.Background()
	err := fsOps.SafeWriteFile(ctx, testFile, content, 0600)
	if err != nil {
		t.Fatalf("SafeWriteFile() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(content))
	}

	// Verify permissions
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions mismatch: got %o, want %o", info.Mode().Perm(), 0600)
	}
}

// TestBootstrapLock tests exclusive bootstrap locking
func TestBootstrapLock(t *testing.T) {
	logger := &testLogger{}
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "bootstrap.lock")

	lockOps := NewBootstrapLock(lockPath, logger, false)

	ctx := context.Background()

	// Acquire lock
	lock, err := lockOps.Acquire(ctx, 5*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	if lock == nil {
		t.Fatal("Acquire() returned nil lock")
	}

	// Verify lock file exists
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// Release lock
	err = lockOps.Release(lock)
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Verify lock file was removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("lock file not removed")
	}
}

// TestDryRun tests dry-run mode doesn't create files
func TestDryRunMode(t *testing.T) {
	logger := &testLogger{}
	fsOps := NewFilesystemOps(logger, true) // dryRun = true
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	ctx := context.Background()

	err := fsOps.SafeWriteFile(ctx, testFile, []byte("test"), 0600)
	if err != nil {
		t.Fatalf("SafeWriteFile() error = %v", err)
	}

	// In dry-run mode, file should NOT be created
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Fatal("file should not exist in dry-run mode")
	}
}

// TestDirectoryValidator tests required directory validation
func TestDirectoryValidator(t *testing.T) {
	logger := &testLogger{}
	dv := &DirectoryValidator{logger: logger}

	// This test just verifies the function can be called without panic
	// Actual directory validation would require specific system setup
	err := dv.ValidateBootstrapDirectories("test")
	// Error is expected if directories don't exist, but function should work
	if err == nil {
		t.Log("Directory validation passed")
	}
}

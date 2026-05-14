package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// FilesystemOps manages safe filesystem operations
type FilesystemOps struct {
	logger Logger
	dryRun bool
}

// Logger interface for structured logging
type Logger interface {
	Info(msg string, args ...interface{})
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// NewFilesystemOps creates a new filesystem operations manager
func NewFilesystemOps(logger Logger, dryRun bool) *FilesystemOps {
	return &FilesystemOps{
		logger: logger,
		dryRun: dryRun,
	}
}

// ValidatePath ensures path doesn't escape baseDir and isn't a symlink
func (fs *FilesystemOps) ValidatePath(baseDir, path string) (string, error) {
	// Normalize paths
	cleanBase := filepath.Clean(baseDir)
	cleanPath := filepath.Clean(path)

	// If path is absolute, use it directly; if relative, join with base
	var fullPath string
	if filepath.IsAbs(cleanPath) {
		fullPath = cleanPath
	} else {
		fullPath = filepath.Join(cleanBase, cleanPath)
	}

	// Resolve symlinks
	realPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// EvalSymlinks fails if any component is a symlink
		if os.IsNotExist(err) {
			// Path doesn't exist yet, check the nearest existing parent
			parent := fullPath
			for {
				parent = filepath.Dir(parent)
				realParent, err := filepath.EvalSymlinks(parent)
				if err == nil {
					// Found an existing parent, use its real path
					realPath = filepath.Join(realParent, fullPath[len(parent):])
					break
				}
				if parent == "/" || parent == "." || parent == filepath.Dir(parent) {
					// Reached the root or same point, cannot resolve further
					return "", ErrPathValidation("filesystem", fullPath, "cannot resolve parent directory")
				}
			}
		} else {
			// Symlink detected in path
			return "", ErrSymlinkDetected("filesystem", fullPath)
		}
	}

	// Verify we're still within baseDir
	prefix := cleanBase
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}

	if !strings.HasPrefix(realPath, prefix) && realPath != cleanBase {
		return "", ErrPathTraversal("filesystem", realPath)
	}

	return realPath, nil
}

// SafeWriteFile writes content safely with validation
func (fs *FilesystemOps) SafeWriteFile(ctx context.Context, path string, content []byte, perm os.FileMode) error {
	// Validate path
	validPath, err := fs.ValidatePath("/", path)
	if err != nil {
		return err
	}

	if fs.dryRun {
		fs.logger.Info("DRY_RUN: Would write file", "path", validPath, "size", len(content))
		return nil
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(validPath), 0755); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	// Write to temporary file first
	tmpFile, err := os.CreateTemp(filepath.Dir(validPath), ".tmp-*")
	if err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return ErrFileWrite("filesystem", validPath, err)
	}

	if err := tmpFile.Close(); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	// Set permissions on temp file
	if err := os.Chmod(tmpFile.Name(), perm); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	// Atomic move
	if err := os.Rename(tmpFile.Name(), validPath); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	fs.logger.Info("File written successfully", "path", validPath, "size", len(content))
	return nil
}

// SafeCreateDirectory creates a directory with proper permissions
func (fs *FilesystemOps) SafeCreateDirectory(ctx context.Context, path string, perm os.FileMode, owner, group int) error {
	// Validate path
	validPath, err := fs.ValidatePath("/", path)
	if err != nil {
		return err
	}

	if fs.dryRun {
		fs.logger.Info("DRY_RUN: Would create directory", "path", validPath, "perm", fmt.Sprintf("%o", perm))
		return nil
	}

	// Create directory
	if err := os.MkdirAll(validPath, perm); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	// Set ownership if needed
	if owner >= 0 && group >= 0 {
		if err := os.Chown(validPath, owner, group); err != nil {
			// Log but don't fail - may not have permission
			fs.logger.Warn("Could not change ownership", "path", validPath, "error", err.Error())
		}
	}

	fs.logger.Info("Directory created successfully", "path", validPath)
	return nil
}

// SafeRemove removes a file or directory safely
func (fs *FilesystemOps) SafeRemove(ctx context.Context, path string) error {
	// Validate path
	validPath, err := fs.ValidatePath("/", path)
	if err != nil {
		return err
	}

	if fs.dryRun {
		fs.logger.Info("DRY_RUN: Would remove", "path", validPath)
		return nil
	}

	if err := os.RemoveAll(validPath); err != nil {
		return ErrFileWrite("filesystem", validPath, err)
	}

	fs.logger.Info("Removed successfully", "path", validPath)
	return nil
}

// ValidateFileOwnership checks if file is owned by expected user/group
func (fs *FilesystemOps) ValidateFileOwnership(path string, expectedUID, expectedGID int) error {
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot stat file: %w", err)
	}

	// Get unix stat info
	sysStat := stat.Sys()
	if sysStat == nil {
		return fmt.Errorf("cannot get unix stat info for file: %s", path)
	}

	// Cast to unix Stat_t to access UID/GID
	unixStat, ok := sysStat.(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot access unix stat fields for file: %s", path)
	}

	// Check UID matches
	if int(unixStat.Uid) != expectedUID {
		fs.logger.Warn("File owner mismatch",
			"path", path,
			"expected_uid", expectedUID,
			"actual_uid", unixStat.Uid)
		return fmt.Errorf("file owner mismatch for %s: expected UID %d, got %d", path, expectedUID, unixStat.Uid)
	}

	// Check GID matches
	if int(unixStat.Gid) != expectedGID {
		fs.logger.Warn("File group mismatch",
			"path", path,
			"expected_gid", expectedGID,
			"actual_gid", unixStat.Gid)
		return fmt.Errorf("file group mismatch for %s: expected GID %d, got %d", path, expectedGID, unixStat.Gid)
	}

	fs.logger.Info("File ownership validated", "path", path, "uid", expectedUID, "gid", expectedGID)
	return nil
}

// BootstrapLockOps manages bootstrap locks
type BootstrapLockOps struct {
	lockPath string
	logger   Logger
	dryRun   bool
}

// NewBootstrapLock creates a new bootstrap lock
func NewBootstrapLock(lockPath string, logger Logger, dryRun bool) *BootstrapLockOps {
	return &BootstrapLockOps{
		lockPath: lockPath,
		logger:   logger,
		dryRun:   dryRun,
	}
}

// Acquire attempts to acquire an exclusive lock
func (b *BootstrapLockOps) Acquire(ctx context.Context, timeout time.Duration) (*BootstrapLock, error) {
	deadline := time.Now().Add(timeout)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			if time.Now().After(deadline) {
				return nil, ErrLockAcquisition("filesystem", fmt.Errorf("timeout waiting for lock"))
			}
		}

		// Try to create lock file exclusively
		if b.dryRun {
			b.logger.Info("DRY_RUN: Would acquire lock", "path", b.lockPath)
			return &BootstrapLock{
				Path:       b.lockPath,
				AcquiredAt: time.Now(),
				owner:      "dry-run",
			}, nil
		}

		file, err := os.OpenFile(b.lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// Write PID to lock file
			fmt.Fprintf(file, "%d", os.Getpid())
			file.Close()

			b.logger.Info("Lock acquired", "path", b.lockPath)
			return &BootstrapLock{
				Path:       b.lockPath,
				AcquiredAt: time.Now(),
				owner:      fmt.Sprintf("pid-%d", os.Getpid()),
			}, nil
		}

		// Lock file exists, wait a bit and retry
		time.Sleep(100 * time.Millisecond)
	}
}

// Release releases the lock
func (b *BootstrapLockOps) Release(lock *BootstrapLock) error {
	if b.dryRun {
		b.logger.Info("DRY_RUN: Would release lock", "path", lock.Path)
		return nil
	}

	if err := os.Remove(lock.Path); err != nil && !os.IsNotExist(err) {
		return ErrLockAcquisition("filesystem", fmt.Errorf("failed to release lock: %w", err))
	}

	b.logger.Info("Lock released", "path", lock.Path)
	return nil
}

// DirectoryValidator checks filesystem requirements
type DirectoryValidator struct {
	logger Logger
}

// ValidateBootstrapDirectories checks that all required directories exist or can be created
func (dv *DirectoryValidator) ValidateBootstrapDirectories(phase string) error {
	dirs := []struct {
		path string
		perm os.FileMode
	}{
		{"/etc/dso", 0750},
		{"/var/lib/dso", 0750},
		{"/var/run/dso", 0755},
		{"/var/log/dso", 0750},
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.perm); err != nil {
			return ErrPathValidation(phase, d.path, fmt.Sprintf("cannot create: %v", err))
		}
	}

	return nil
}

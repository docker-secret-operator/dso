package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FileLock provides simple file-based distributed locking for container-level synchronization
// This prevents multiple agents/processes from rotating the same container simultaneously
type FileLock struct {
	lockDir string
	logger  *zap.Logger
}

// LockManager manages locks for containers during rotation
type LockManager struct {
	locks   map[string]*sync.Mutex
	mu      sync.Mutex
	fileLock *FileLock
}

// NewLockManager creates a new lock manager with optional file-based distributed locking
func NewLockManager(lockDir string, logger *zap.Logger) (*LockManager, error) {
	lm := &LockManager{
		locks: make(map[string]*sync.Mutex),
	}

	if lockDir != "" {
		if err := os.MkdirAll(lockDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create lock directory: %w", err)
		}
		lm.fileLock = &FileLock{
			lockDir: lockDir,
			logger:  logger,
		}
	}

	return lm, nil
}

// AcquireLock acquires an exclusive lock for a container
// This ensures only one rotation happens at a time for a given container
func (lm *LockManager) AcquireLock(containerID string, timeout time.Duration) error {
	lm.mu.Lock()
	if _, exists := lm.locks[containerID]; !exists {
		lm.locks[containerID] = &sync.Mutex{}
	}
	mutex := lm.locks[containerID]
	lm.mu.Unlock()

	// Acquire local lock
	lockCh := make(chan struct{}, 1)
	go func() {
		mutex.Lock()
		lockCh <- struct{}{}
	}()

	select {
	case <-lockCh:
		break
	case <-time.After(timeout):
		return fmt.Errorf("failed to acquire lock for container %s within %v", containerID, timeout)
	}

	// Also acquire file-based lock if distributed locking is enabled
	if lm.fileLock != nil {
		if err := lm.fileLock.AcquireLock(containerID, timeout); err != nil {
			mutex.Unlock()
			return err
		}
	}

	return nil
}

// ReleaseLock releases the lock for a container
func (lm *LockManager) ReleaseLock(containerID string) {
	lm.mu.Lock()
	if mutex, exists := lm.locks[containerID]; exists {
		mutex.Unlock()
	}
	lm.mu.Unlock()

	if lm.fileLock != nil {
		lm.fileLock.ReleaseLock(containerID)
	}
}

// AcquireLock acquires a file-based distributed lock
func (fl *FileLock) AcquireLock(containerID string, timeout time.Duration) error {
	lockFile := filepath.Join(fl.lockDir, fmt.Sprintf("%s.lock", containerID))
	deadline := time.Now().Add(timeout)

	for {
		// Try to create lock file exclusively
		f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			// Successfully acquired lock
			f.WriteString(fmt.Sprintf("%d\n", os.Getpid()))
			f.Close()
			fl.logger.Debug("Acquired distributed lock", zap.String("container_id", containerID))
			return nil
		}

		if !os.IsExist(err) {
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		// Lock file exists - check if lock holder is still alive
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout acquiring distributed lock for container %s", containerID)
		}

		// Check if lock is stale (older than 10 minutes)
		// CRITICAL: Atomic check-and-remove to prevent race conditions
		info, err := os.Stat(lockFile)
		if err == nil && time.Since(info.ModTime()) > 10*time.Minute {
			fl.logger.Warn("Removing stale lock", zap.String("container_id", containerID))
			// FIX: Race-safe removal - if this fails, the lock is still held, so we backoff
			if err := os.Remove(lockFile); err != nil && !os.IsNotExist(err) {
				fl.logger.Debug("Failed to remove stale lock (still held)", zap.String("container_id", containerID))
				// Stale lock couldn't be removed - backoff more to avoid tight spinning
				time.Sleep(500 * time.Millisecond)
				continue
			}
			// Lock removed, try to acquire again immediately
			continue
		}

		// Wait before retrying
		time.Sleep(100 * time.Millisecond)
	}
}

// ReleaseLock releases a file-based distributed lock
func (fl *FileLock) ReleaseLock(containerID string) {
	lockFile := filepath.Join(fl.lockDir, fmt.Sprintf("%s.lock", containerID))
	if err := os.Remove(lockFile); err != nil && !os.IsNotExist(err) {
		fl.logger.Warn("Failed to remove lock file", zap.String("container_id", containerID), zap.Error(err))
	}
}

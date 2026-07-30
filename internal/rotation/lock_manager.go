package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

// FileLock provides file-based advisory locking for container-level synchronization
// across processes. It uses flock(2) so locking is atomic and the kernel releases
// the lock automatically if the holding process dies — there is no stat()+remove()
// stale-lock window (CQ-H6).
type FileLock struct {
	lockDir string
	logger  *zap.Logger

	mu  sync.Mutex
	fds map[string]*os.File // logical lock name -> open file holding the flock
}

// lockEntry pairs a key's mutex with a reference count of goroutines
// currently holding or waiting on it (REL-4). The count is what makes it safe
// to ever remove a key from LockManager.locks: without it, a goroutine that
// already read this *lockEntry out of the map (and is polling entry.mu) could
// still lock it after a concurrent ReleaseLock deletes the map entry, while a
// third goroutine simultaneously creates a brand-new entry for the same key
// — two goroutines then hold "the lock" for the same key at once. Deleting
// only when count reaches zero (no outstanding references) rules that out.
type lockEntry struct {
	mu    sync.Mutex
	count int
}

// LockManager manages locks for containers during rotation.
type LockManager struct {
	locks    map[string]*lockEntry
	acquired map[string]bool // tracks which keys are currently locked
	mu       sync.Mutex
	fileLock *FileLock
}

// NewLockManager creates a new lock manager with optional file-based distributed locking.
func NewLockManager(lockDir string, logger *zap.Logger) (*LockManager, error) {
	lm := &LockManager{
		locks:    make(map[string]*lockEntry),
		acquired: make(map[string]bool),
	}

	if lockDir != "" {
		if err := os.MkdirAll(lockDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create lock directory: %w", err)
		}
		lm.fileLock = &FileLock{
			lockDir: lockDir,
			logger:  logger,
			fds:     make(map[string]*os.File),
		}
	}

	return lm, nil
}

// AcquireLock acquires an exclusive lock for a container (or secret) so that only
// one rotation happens at a time for a given key.
//
// CQ-C1: lock acquisition uses sync.Mutex.TryLock polled against a deadline. The
// previous implementation spawned a goroutine that called mutex.Lock() and raced
// it against time.After — on timeout that goroutine leaked forever, blocked on a
// mutex that was never unlocked. TryLock never blocks, so nothing is leaked.
func (lm *LockManager) AcquireLock(containerID string, timeout time.Duration) error {
	lm.mu.Lock()
	entry, exists := lm.locks[containerID]
	if !exists {
		entry = &lockEntry{}
		lm.locks[containerID] = entry
	}
	entry.count++
	lm.mu.Unlock()

	if !tryLockWithTimeout(&entry.mu, timeout) {
		lm.releaseEntryRef(containerID, entry)
		return fmt.Errorf("failed to acquire lock for container %s within %v", containerID, timeout)
	}

	lm.mu.Lock()
	lm.acquired[containerID] = true
	lm.mu.Unlock()

	// Also acquire the cross-process file lock if distributed locking is enabled.
	if lm.fileLock != nil {
		if err := lm.fileLock.AcquireLock(containerID, timeout); err != nil {
			lm.mu.Lock()
			delete(lm.acquired, containerID)
			lm.mu.Unlock()
			entry.mu.Unlock()
			lm.releaseEntryRef(containerID, entry)
			return err
		}
	}

	return nil
}

// releaseEntryRef decrements entry's reference count and removes it from
// lm.locks once nobody else references it (REL-4). Guarded by identity
// comparison so it never deletes a different, newer entry that has since
// replaced this one under the same key.
func (lm *LockManager) releaseEntryRef(containerID string, entry *lockEntry) {
	lm.mu.Lock()
	entry.count--
	if entry.count == 0 {
		if cur, ok := lm.locks[containerID]; ok && cur == entry {
			delete(lm.locks, containerID)
		}
	}
	lm.mu.Unlock()
}

// tryLockWithTimeout attempts to lock mu, polling with a bounded backoff until the
// timeout elapses. It runs entirely in the caller's goroutine and never leaks one.
func tryLockWithTimeout(mu *sync.Mutex, timeout time.Duration) bool {
	if mu.TryLock() {
		return true
	}
	if timeout <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	backoff := time.Millisecond
	for time.Now().Before(deadline) {
		time.Sleep(backoff)
		if mu.TryLock() {
			return true
		}
		if backoff < 25*time.Millisecond {
			backoff *= 2
		}
	}
	return false
}

// ReleaseLock releases the lock for a container. Safe to call even if the lock
// was never acquired (e.g. in a defer after a failed AcquireLock).
func (lm *LockManager) ReleaseLock(containerID string) {
	lm.mu.Lock()
	wasAcquired := lm.acquired[containerID]
	var entry *lockEntry
	if wasAcquired {
		entry = lm.locks[containerID]
		delete(lm.acquired, containerID)
	}
	lm.mu.Unlock()

	if entry != nil {
		entry.mu.Unlock()
	}

	if wasAcquired && lm.fileLock != nil {
		lm.fileLock.ReleaseLock(containerID)
	}

	if entry != nil {
		// Drop the entry once nobody else references it, otherwise lm.locks
		// grows for every distinct key ever locked over the daemon's
		// lifetime and is never trimmed (REL-4). Ref-counted rather than an
		// unconditional delete: a concurrent AcquireLock may already hold a
		// reference to this same entry and be waiting on entry.mu, so
		// removing it from the map while that reference is still live would
		// let a third goroutine allocate a *different* lockEntry for the
		// same key and acquire it concurrently with the waiter — breaking
		// mutual exclusion.
		lm.releaseEntryRef(containerID, entry)
	}
}

// sanitizeLockName makes an arbitrary lock key (container ID or secret name) safe
// to use as a filename, preventing path traversal via separators in the key.
func sanitizeLockName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", string(os.PathSeparator), "_", "..", "_")
	return replacer.Replace(name)
}

// AcquireLock acquires a cross-process advisory lock using flock(2).
//
// CQ-H6: flock is atomic and held on the open file descriptor, so there is no
// check-then-act window between detecting a stale lock and removing it. If the
// holder crashes, the kernel drops the lock automatically and the next acquirer
// proceeds — no manual staleness heuristic is required. The lock file is never
// deleted on release, which avoids the inode-reuse race that file removal
// introduces with flock.
func (fl *FileLock) AcquireLock(containerID string, timeout time.Duration) error {
	lockFile := filepath.Join(fl.lockDir, fmt.Sprintf("%s.lock", sanitizeLockName(containerID)))

	// #nosec G304 -- containerID is already sanitized by sanitizeLockName
	// above (strips "/", "\", the OS path separator, and ".."), so lockFile
	// cannot escape fl.lockDir
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		// #nosec G115 -- f.Fd() is a real OS file descriptor (always a small,
		// non-negative int in practice); unix.Flock requires an int argument
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			// Acquired. Record the holding PID for operator visibility.
			_ = f.Truncate(0)
			if _, werr := f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0); werr != nil {
				fl.logger.Debug("Failed to write PID to lock file", zap.String("container_id", containerID), zap.Error(werr))
			}
			fl.mu.Lock()
			fl.fds[containerID] = f
			fl.mu.Unlock()
			fl.logger.Debug("Acquired distributed lock", zap.String("container_id", containerID))
			return nil
		}

		if err != unix.EWOULDBLOCK {
			_ = f.Close()
			return fmt.Errorf("failed to flock lock file for container %s: %w", containerID, err)
		}

		// Lock is held by another process.
		if time.Now().After(deadline) {
			_ = f.Close()
			return fmt.Errorf("timeout acquiring distributed lock for container %s", containerID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ReleaseLock releases a file-based advisory lock.
func (fl *FileLock) ReleaseLock(containerID string) {
	fl.mu.Lock()
	f, ok := fl.fds[containerID]
	delete(fl.fds, containerID)
	fl.mu.Unlock()

	if !ok {
		return
	}

	// #nosec G115 -- f.Fd() is a real OS file descriptor, see comment above
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		fl.logger.Warn("Failed to release flock", zap.String("container_id", containerID), zap.Error(err))
	}
	if err := f.Close(); err != nil {
		fl.logger.Warn("Failed to close lock file", zap.String("container_id", containerID), zap.Error(err))
	}
}

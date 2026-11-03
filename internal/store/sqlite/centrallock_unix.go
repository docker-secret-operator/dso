//go:build unix

package sqlite

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// acquireCentralLock opens (creating if needed) the sidecar lock file for
// dbPath and takes an flock(2) in the requested mode, polling LOCK_NB
// rather than blocking indefinitely so a busy lock fails within a bounded
// time instead of hanging the caller forever. timeout<=0 uses
// centralLockDefaultTimeout.
func acquireCentralLock(dbPath string, mode LockMode, timeout time.Duration) (*centralLock, error) {
	if timeout <= 0 {
		timeout = centralLockDefaultTimeout
	}
	path := centralLockPath(dbPath)
	// O_NOFOLLOW: dbPath's directory is already confirmed safe by Open's
	// own prepareFile/securefs.EnsureDir before this runs (same directory
	// as the database file), but the sidecar's own open call should not
	// itself follow a symlink planted at the exact lock path -- matching
	// the O_NOFOLLOW discipline every other sensitive file in this package
	// (securefs.OpenPrivateFile, securefs.CreateExclusive) already applies.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600) // #nosec G304 -- path is derived from the caller-configured database path, never user input; O_NOFOLLOW above
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("sqlite: central lock file %s is a symbolic link; refusing to follow it", path)
		}
		return nil, fmt.Errorf("sqlite: opening central lock file %s: %w", path, err)
	}
	if fi, statErr := f.Stat(); statErr != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		if statErr != nil {
			return nil, fmt.Errorf("sqlite: inspecting central lock file %s: %w", path, statErr)
		}
		return nil, fmt.Errorf("sqlite: central lock file %s is not a regular file", path)
	}

	op := unix.LOCK_SH
	if mode == LockExclusive {
		op = unix.LOCK_EX
	}

	deadline := time.Now().Add(timeout)
	for {
		flockErr := unix.Flock(int(f.Fd()), op|unix.LOCK_NB)
		if flockErr == nil {
			break
		}
		if flockErr != unix.EWOULDBLOCK && flockErr != unix.EAGAIN {
			_ = f.Close()
			return nil, fmt.Errorf("sqlite: taking %s lock on %s: %w", mode, path, flockErr)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w: waited %s for a %s lock on %s", ErrCentralLockBusy, timeout, mode, path)
		}
		time.Sleep(centralLockPollInterval)
	}

	return &centralLock{release: func() error {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return f.Close()
	}}, nil
}

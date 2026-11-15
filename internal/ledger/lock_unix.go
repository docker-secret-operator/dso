//go:build unix

package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockDir takes the process-lifetime exclusive flock on ledger.lock in dir
// (design §8.1). The lock file is opened without following a final symlink and
// must be a regular file. A lock held elsewhere returns ErrLocked.
func lockDir(dir string) (func() error, error) {
	path := filepath.Join(dir, "ledger.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600) // #nosec G304 -- fixed name inside the chain-checked state directory; O_NOFOLLOW
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("ledger: lock file %s is a symbolic link; refusing to follow it", path)
		}
		return nil, fmt.Errorf("ledger: opening lock file: %w", err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("ledger: lock file %s is not a regular file", path)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { // #nosec G115 -- file descriptors are small OS-provided values
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("ledger: locking %s: %w", path, err)
	}
	return func() error {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) // #nosec G115 -- see above
		return f.Close()
	}, nil
}

//go:build unix

package vault

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// lockPollInterval is how often a blocked acquirer retries a non-blocking
// flock. Polling (rather than a blocking flock) lets acquisition time out
// instead of hanging a CLI forever behind a stuck process.
const lockPollInterval = 25 * time.Millisecond

// acquireFileLock takes an exclusive advisory flock(2) on path (creating it
// 0600 if needed) and returns a release function. flock is per open file
// description, so it serializes separate processes AND separate Vault values
// inside one process. The kernel drops the lock if the holder dies, so a
// crashed writer cannot leave the vault permanently locked.
//
// This is advisory: it only coordinates writers that use this package. It is
// not a defense against a hostile local user, and flock semantics on network
// filesystems (NFS) vary -- do not place ~/.dso on one.
func acquireFileLock(path string, timeout time.Duration) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path is derived from the vault path under ~/.dso.
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) // #nosec G115 -- file descriptors are small OS-provided values.
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("timed out after %s waiting for another dso process holding %s", timeout, path)
		}
		time.Sleep(lockPollInterval)
	}
	return func() error {
		// Closing the descriptor releases the lock; the explicit unlock makes
		// the intent obvious and surfaces errors.
		unlockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN) // #nosec G115 -- see above.
		closeErr := f.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}, nil
}

// syncDirectory fsyncs dir so a just-completed rename is durable. Filesystems
// that cannot fsync a directory report EINVAL/ENOTSUP; that is treated as
// "not supported here", every other error is returned.
func syncDirectory(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- dir is the vault directory.
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil && !errors.Is(syncErr, syscall.EINVAL) && !errors.Is(syncErr, errors.ErrUnsupported) {
		return syncErr
	}
	return closeErr
}

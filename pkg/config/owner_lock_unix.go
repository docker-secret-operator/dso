//go:build unix

package config

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// lockOwnerID takes an exclusive advisory flock(2) on path (created 0600) and
// returns a release function. flock is per open file description, so it
// serialises separate processes AND separate goroutines; the kernel drops it if
// the holder dies. It is advisory (it coordinates DSO processes, not a hostile
// local user) and unreliable on some network filesystems -- keep /etc/dso local.
func lockOwnerID(path string, timeout time.Duration) (func() error, error) {
	// O_NOFOLLOW: a symlink at the lock path must not make us open (or create)
	// its target. The descriptor is then checked to be an ordinary file, so a
	// directory, FIFO or device at that path fails closed too.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0o600) // #nosec G304 -- path is the lock file beside the validated config.
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = errors.New("owner lock path is not a regular file")
		}
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
			return nil, errors.New("timed out waiting for another DSO process initialising this config")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() error {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) // #nosec G115 -- see above.
		return f.Close()
	}, nil
}

// syncDir fsyncs dir so a just-completed rename is durable; filesystems that
// cannot fsync a directory are treated as "not supported here".
func syncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- the config directory.
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if err := ignoreUnsupportedSync(syncErr); err != nil {
		return err
	}
	return closeErr
}

// ignoreUnsupportedSync maps "this filesystem cannot fsync a directory"
// (EINVAL / errors.ErrUnsupported) to nil; every other failure (EIO, ENOSPC,
// ...) is a real durability failure and is returned.
func ignoreUnsupportedSync(err error) error {
	if err != nil && !errors.Is(err, syscall.EINVAL) && !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	return nil
}

// preserveFileOwner gives the replacement file the original's owner and group.
// It is skipped when they already match (the common non-root case). If they
// differ and cannot be set (a non-root user that is not a member of the file's
// group), the update is refused instead of silently changing the group.
func preserveFileOwner(tmp *os.File, orig os.FileInfo) error {
	want, ok := orig.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	have, err := tmp.Stat()
	if err != nil {
		return err
	}
	if got, ok := have.Sys().(*syscall.Stat_t); ok && got.Uid == want.Uid && got.Gid == want.Gid {
		return nil
	}
	return tmp.Chown(int(want.Uid), int(want.Gid)) // #nosec G115 -- uid/gid from the OS.
}

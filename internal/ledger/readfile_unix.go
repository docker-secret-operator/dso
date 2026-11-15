//go:build unix

package ledger

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// readLedgerFile reads the ledger through hardened steps: the directory chain
// is checked, the file is opened without following a final symlink and without
// blocking (O_NONBLOCK, so a FIFO planted at the ledger path cannot hang the
// agent), and the open descriptor is verified to be a regular file owned by the
// current user with no group or other access before one bounded read.
func readLedgerFile(path string, limit int64) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := securefs.CheckDirChain(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) // #nosec G304 -- chain checked above; no-follow, non-blocking; the descriptor is verified below
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("ledger: %s is a symbolic link; refusing to follow it", abs)
		}
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only descriptor; a close error cannot affect the data already read
	if err := securefs.CheckOpenFile(f, abs); err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, fi.Size(), limit)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%w: limit %d", ErrTooLarge, limit)
	}
	return b, nil
}

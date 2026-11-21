package ledger

import (
	"os"
	"path/filepath"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// tmpFile is the part of an open temporary file the write algorithm uses.
type tmpFile interface {
	Write(p []byte) (int, error)
	Sync() error
	Close() error
}

// fileOps is the fault-injection seam of the store: every filesystem effect
// goes through it, so tests can fail any step without touching a real fault.
type fileOps struct {
	ensureDir       func(dir string) error
	lock            func(dir string) (release func() error, err error)
	readFile        func(path string, limit int64) ([]byte, error)
	createTemp      func(path string) (tmpFile, error)
	rename          func(oldp, newp string) error
	renameNoReplace func(oldp, newp string) error
	syncDir         func(dir string) error
	lstat           func(path string) (os.FileInfo, error)
	readDir         func(dir string) ([]os.DirEntry, error)
	remove          func(path string) error
	ownerUID        func(fi os.FileInfo) (uint32, bool)
	euid            func() int
}

func realOps() *fileOps {
	return &fileOps{
		ensureDir: securefs.EnsureDir,
		lock:      lockDir,
		readFile:  readLedgerFile,
		createTemp: func(path string) (tmpFile, error) {
			f, err := securefs.CreateExclusive(path, 0o600)
			if err != nil {
				return nil, err
			}
			return f, nil
		},
		rename:          os.Rename,
		renameNoReplace: renameNoReplace,
		syncDir:         syncDirectory,
		lstat:           os.Lstat,
		readDir:         os.ReadDir,
		remove:          os.Remove,
		ownerUID:        ownerUID,
		euid:            os.Geteuid,
	}
}

// syncDirectory fsyncs a directory so a rename inside it is durable.
func syncDirectory(dir string) error {
	d, err := os.Open(filepath.Clean(dir)) // #nosec G304 -- directory already chain-checked by Open
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

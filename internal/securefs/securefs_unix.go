//go:build unix

// Package securefs verifies that files holding key material or the central
// database sit in locations an unprivileged local user cannot tamper with.
//
// File-mode checks alone are not enough: a 0400 key file in a directory that
// another user can write to can simply be unlinked and replaced by theirs.
// These helpers therefore also walk the directory chain.
//
// Guarantees and limits (unix only):
//   - every directory traversed on the way to the file must be owned by the
//     current user or root and must not be writable by group or others; a
//     sticky world-writable directory (like /tmp) is accepted only as an
//     ANCESTOR, never as the immediate parent;
//   - symlinks in the path are followed one at a time, and the directory that
//     CONTAINS each link is checked as well as the target chain, so a link
//     planted in a writable directory is rejected;
//   - the leaf file is opened with O_NOFOLLOW and judged on the open file
//     descriptor (fstat), not on the path;
//   - the checks are made before use; a directory owner (or root) can still
//     race the check. That residual is inherent to path-based APIs, and SQLite
//     itself opens its database by path.
//
// Different directories are NOT different volumes and nothing here tests for
// separate storage.
package securefs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Supported reports whether this platform can perform the checks. Where it is
// false callers must fail closed.
const Supported = true

// currentUID is overridden by tests to simulate directories owned by someone else.
var currentUID = os.Geteuid

const maxSymlinkDepth = 16

func trustedOwner(uid uint32) bool {
	return uid == 0 || int(uid) == currentUID()
}

// CheckDirChain verifies every directory from the root down to dir (inclusive).
// dir is the immediate parent of the sensitive file and is held to the strict
// rule; its ancestors may be sticky and world-writable.
func CheckDirChain(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("securefs: resolving %s: %w", dir, err)
	}
	return walk(filepath.Clean(abs), 0)
}

func walk(path string, depth int) error {
	if depth > maxSymlinkDepth {
		return fmt.Errorf("securefs: too many symbolic links resolving %s", path)
	}
	parts := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	cur := string(filepath.Separator)
	for i, part := range parts {
		if part == "" {
			continue
		}
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if err != nil {
			return fmt.Errorf("securefs: %w", err)
		}
		last := i == len(parts)-1
		if fi.Mode()&os.ModeSymlink != 0 {
			// The directory holding the link has already been verified (we walk
			// top-down). Follow the link and verify the resulting chain.
			target, err := os.Readlink(next)
			if err != nil {
				return fmt.Errorf("securefs: %w", err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(cur, target)
			}
			rest := filepath.Join(parts[i+1:]...)
			return walk(filepath.Join(filepath.Clean(target), rest), depth+1)
		}
		if !fi.IsDir() {
			return fmt.Errorf("securefs: %s is not a directory", next)
		}
		if err := checkDir(next, fi, last); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

func checkDir(path string, fi os.FileInfo, strict bool) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("securefs: cannot determine ownership of %s", path)
	}
	if !trustedOwner(st.Uid) {
		return fmt.Errorf("securefs: directory %s is owned by uid %d, which is neither root nor the current user; "+
			"another account could replace files in it", path, st.Uid)
	}
	mode := fi.Mode()
	if mode.Perm()&0o022 != 0 {
		sticky := mode&os.ModeSticky != 0
		if strict || !sticky {
			return fmt.Errorf("securefs: directory %s is writable by group or others (mode %04o); files in it could be replaced "+
				"(fix: chmod go-w %s)", path, mode.Perm(), path)
		}
	}
	return nil
}

// OpenPrivateFile checks the directory chain of path, then opens it read-only
// without following a final symlink and verifies, on the open descriptor, that
// it is a regular file owned by the current user with no group/other access.
func OpenPrivateFile(path string) (*os.File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := CheckDirChain(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G304 -- this is the hardened opener: directory chain checked above, O_NOFOLLOW, and ownership/mode verified on the descriptor below
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("securefs: %s is a symbolic link; refusing to follow it", abs)
		}
		return nil, err
	}
	if err := CheckOpenFile(f, abs); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// CheckOpenFile applies the file rules to an already-open descriptor.
func CheckOpenFile(f *os.File, path string) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("securefs: %s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("securefs: %s has mode %04o; it must not be accessible by group or others (chmod 600 %s)",
			path, fi.Mode().Perm(), path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != currentUID() {
		return fmt.Errorf("securefs: %s is not owned by the current user", path)
	}
	return nil
}

// ReadPrivateFile is OpenPrivateFile plus a bounded read.
func ReadPrivateFile(path string, limit int64) ([]byte, error) {
	f, err := OpenPrivateFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("securefs: %s is larger than %d bytes", path, limit)
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

// EnsureDir creates dir (and parents) with mode 0700 when missing, then
// verifies the whole chain. It never changes the mode of an existing directory.
func EnsureDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return fmt.Errorf("creating directory %s: %w", abs, err)
	}
	return CheckDirChain(abs)
}

// CreateExclusive creates path with the given mode, failing if anything
// (including a symbolic link) already exists there.
func CreateExclusive(path string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, mode) // #nosec G304 -- exclusive create with O_NOFOLLOW; callers pass operator-configured paths
}

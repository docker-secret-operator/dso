//go:build darwin

package sqlite

import "golang.org/x/sys/unix"

// renameNoReplace renames oldpath to newpath, atomically refusing to replace
// newpath if anything already exists there: renamex_np(2) with RENAME_EXCL, so
// the existence check and the rename are one kernel operation.
//
// It returns the raw errno; publishDirWith interprets it. EEXIST means the
// destination exists; ENOTSUP means the filesystem does not support RENAME_EXCL
// and fails closed (no fallback to a check followed by an ordinary rename). EINTR
// is returned untouched for publishDirWith to resolve.
//
// This file is compile-checked from other platforms but has not been run on
// macOS by this project's tests.
func renameNoReplace(oldpath, newpath string) error {
	return unix.RenamexNp(oldpath, newpath, unix.RENAME_EXCL)
}

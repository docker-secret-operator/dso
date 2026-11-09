//go:build linux

package sqlite

import "golang.org/x/sys/unix"

// renameNoReplace renames oldpath to newpath, atomically refusing to replace
// newpath if anything already exists there (a file, a directory even if empty,
// a link): renameat2(2) with RENAME_NOREPLACE, so the existence check and the
// rename are one kernel operation.
//
// It returns the raw errno; publishDirWith interprets it. EEXIST means the
// destination exists. EINVAL or ENOSYS mean the kernel or the filesystem does not
// support RENAME_NOREPLACE, which is treated as "unsupported" and fails closed:
// there is deliberately no fallback to a check followed by an ordinary rename.
// EINTR is returned untouched, because after an interrupted call the rename may
// or may not have happened; publishDirWith establishes which.
func renameNoReplace(oldpath, newpath string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
}

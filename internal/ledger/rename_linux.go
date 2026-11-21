//go:build linux

package ledger

import "golang.org/x/sys/unix"

// renameNoReplace is renameat2(2) with RENAME_NOREPLACE: the existence check
// and the rename are one kernel operation (owner decision D-S2-4). It returns
// the raw errno; EEXIST means the destination exists, EINVAL or ENOSYS mean the
// kernel or filesystem lacks support, and EINTR is left for the caller to
// resolve by file identity. There is deliberately no fallback to a check
// followed by an ordinary rename.
func renameNoReplace(oldpath, newpath string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
}

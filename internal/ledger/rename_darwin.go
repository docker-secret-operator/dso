//go:build darwin

package ledger

import "golang.org/x/sys/unix"

// renameNoReplace is renamex_np(2) with RENAME_EXCL (owner decision D-S2-4).
// This file is compile-checked from other platforms; it has not been run on
// macOS by this project's tests.
func renameNoReplace(oldpath, newpath string) error {
	return unix.RenamexNp(oldpath, newpath, unix.RENAME_EXCL)
}

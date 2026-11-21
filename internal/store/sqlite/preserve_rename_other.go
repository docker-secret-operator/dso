//go:build !linux && !darwin

package sqlite

import "fmt"

// renameNoReplace is the fail-closed implementation for platforms without an
// atomic no-replace rename that this package uses. It never renames anything:
// publishing a preservation is refused rather than done with a check followed by
// an ordinary rename, which could replace an existing directory.
func renameNoReplace(oldpath, newpath string) error {
	return fmt.Errorf("%w: atomic no-replace rename is not implemented on this platform (renaming %s)", ErrPreserveNoReplaceUnsupported, oldpath)
}

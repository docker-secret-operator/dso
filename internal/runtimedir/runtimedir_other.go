//go:build !linux

package runtimedir

import (
	"fmt"
	"os"
)

// Dir is a placeholder on platforms without descriptor-based enforcement.
type Dir struct{}

// Path returns an empty string: no directory can be opened on this platform.
func (d *Dir) Path() string { return "" }

// Close is a no-op.
func (d *Dir) Close() error { return nil }

// Ensure always fails with ErrUnsupported. It never reports success, so a
// caller cannot mistake an unenforced directory for an enforced one.
func Ensure(path string, _ Policy) (*Dir, error) {
	return nil, fmt.Errorf("runtimedir: cannot establish %s: %w", path, ErrUnsupported)
}

// OpenRegular always fails with ErrUnsupported.
func (d *Dir) OpenRegular(name string, _ int, _ os.FileMode, _ FilePolicy) (*os.File, error) {
	return nil, fmt.Errorf("runtimedir: cannot open %s: %w", name, ErrUnsupported)
}

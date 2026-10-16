// Package securefstest provides test helpers for code that is checked by
// package securefs.
package securefstest

import (
	"os"
	"testing"
)

// TempDir is t.TempDir() with mode 0700. t.TempDir creates its directory with
// the process umask applied (0775 under the common umask 002), which securefs
// correctly refuses as group-writable, so tests that exercise secured paths
// must use this instead.
func TempDir(t testing.TB) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

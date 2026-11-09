//go:build !unix

package vault

import "time"

// acquireFileLock is a NO-OP on platforms without flock(2): it returns
// immediately and provides NO mutual exclusion -- not between dso processes and
// not between separate Vault values in one process. Concurrent writers can
// therefore still lose each other's updates here (only goroutines sharing a
// single *Vault are serialized, by Vault.mu). The reload-before-write and
// atomic-rename steps still run, but they narrow the race window rather than
// close it. The supported production platform is Linux (see
// internal/agent/peercred_other.go for the same stance).
func acquireFileLock(path string, timeout time.Duration) (func() error, error) {
	return func() error { return nil }, nil
}

// syncDirectory is a no-op where directory fsync is not available.
func syncDirectory(dir string) error { return nil }

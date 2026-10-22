//go:build !unix

package config

import (
	"os"
	"time"
)

// lockOwnerID is a NO-OP where flock(2) is unavailable: it provides no mutual
// exclusion between DSO processes (goroutines in one process are still
// serialised by ownerInitMu). Concurrent first-run initialisation by SEPARATE
// processes can therefore still produce competing ids here; the supported
// production platform is Linux (same stance as pkg/vault's lock_other.go).
func lockOwnerID(path string, timeout time.Duration) (func() error, error) {
	return func() error { return nil }, nil
}

func syncDir(dir string) error { return nil }

// preserveFileOwner cannot be implemented portably; the replacement keeps the
// platform default owner.
func preserveFileOwner(tmp *os.File, orig os.FileInfo) error { return nil }

//go:build !unix

package sqlite

import (
	"fmt"
	"time"
)

// acquireCentralLock fails closed on platforms without the flock(2) support
// implemented here, matching internal/securefs's own fail-closed stance for
// unsupported platforms: no lock at all is strictly safer than silently
// proceeding as if one were held.
func acquireCentralLock(dbPath string, mode LockMode, _ time.Duration) (*centralLock, error) {
	return nil, fmt.Errorf("sqlite: central database locking (%s, %s) is not supported on this platform", mode, dbPath)
}

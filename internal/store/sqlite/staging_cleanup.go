package sqlite

import (
	"errors"
	"os"
	"sync"
)

// Lifecycle of staging directories. Every directory created by
// CopyCandidateForValidation is registered here and unregistered when its
// cleanup function runs, so a graceful-shutdown handler can remove exactly the
// directories THIS process created and has not yet cleaned up -- never
// anything merely matching a name pattern.
//
// Limits: a signal handler cannot run after SIGKILL, an out-of-memory kill, a
// machine failure or power loss; a staging directory (mode 0700, holding a
// 0600 copy of the database) can remain in the temporary directory in those
// cases and must be removed manually.
var (
	stagingMu   sync.Mutex
	stagingDirs = map[string]struct{}{}
)

func registerStagingDir(dir string) {
	stagingMu.Lock()
	stagingDirs[dir] = struct{}{}
	stagingMu.Unlock()
}

// removeStagingDir removes dir and forgets it. Safe to call more than once.
func removeStagingDir(dir string) {
	stagingMu.Lock()
	delete(stagingDirs, dir)
	stagingMu.Unlock()
	_ = os.RemoveAll(dir)
}

// removeRegisteredStagingDirs removes every staging directory this process
// created and has not yet cleaned up.
func removeRegisteredStagingDirs() {
	stagingMu.Lock()
	dirs := make([]string, 0, len(stagingDirs))
	for d := range stagingDirs {
		dirs = append(dirs, d)
	}
	stagingDirs = map[string]struct{}{}
	stagingMu.Unlock()
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
}

// Kinds of staging failure, for callers that need a stable, non-sensitive
// classification. The wrapped error's own text is unchanged.
var (
	ErrStageUnsafe      = errors.New("source is not safe to use")
	ErrStageUnreadable  = errors.New("source cannot be read")
	ErrStageEmpty       = errors.New("source is empty")
	ErrStageNotDatabase = errors.New("source is not a valid SQLite database")
	ErrStageSidecar     = errors.New("unsafe -wal/-shm companion file")
)

type stageError struct{ kind, err error }

func (e *stageError) Error() string   { return e.err.Error() }
func (e *stageError) Unwrap() []error { return []error{e.kind, e.err} }

func stageErr(kind, err error) error { return &stageError{kind: kind, err: err} }

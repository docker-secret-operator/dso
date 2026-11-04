package sqlite

// Cross-process coordination for DSO Central database access (restore
// concurrency fix; approved design: docs/security/restore-concurrency-design.md).
//
// Every sqlite.Open call -- and Restore's own exclusive critical section --
// takes an advisory flock(2) on a STABLE SIDECAR file (<dbPath>.central.lock),
// never the database file or its inode. A sidecar, not the db file itself,
// is deliberate: Restore's replaceDatabaseFile atomically renames a NEW
// inode onto dbPath, so a lock tied to the database file's inode would be
// left on the OLD, about-to-be-discarded inode the moment the rename
// happens. The sidecar path never changes across a restore, so the lock's
// identity is "whoever is coordinating access to THIS PATH right now",
// independent of which file currently exists there.
//
// Assumptions and limits, documented rather than silently assumed:
//
//   - Advisory only, and only within this codebase. Verified (grep) that
//     the only callers of sqlite.Open anywhere in this repository are
//     internal/cli/central.go and internal/centralsvc/restore.go -- every
//     one of them goes through this lock. A process that opens the sidecar,
//     the database file, or its -wal/-shm companions directly, bypassing
//     this package (e.g. the sqlite3 CLI, or an ad hoc script), is not
//     coordinated with and can still race; a filesystem lock cannot compel
//     participation from code that never asks for it.
//   - Single host only. flock(2) is a local, same-kernel primitive. It does
//     not coordinate across hosts, and is unreliable or unsupported on some
//     network filesystems (notably older NFS without properly configured
//     lock support). DSO Central's database is expected to live on local
//     storage; this is not re-verified at runtime.
//   - The lock sidecar file is never deleted or truncated by this package,
//     under any circumstance, including on the theory that it looks stale.
//     flock is held on an open file descriptor, not derived from the
//     sidecar's content, size, or mtime, and the kernel releases it
//     automatically when the holding process's last reference to that fd
//     closes -- including on crash or kill -9. There is no "stale lock"
//     state for the sidecar's bytes to represent, so there is nothing to
//     clean up, and removing it while another process still holds its fd
//     open would not release anything early; it would only let a THIRD
//     process create a fresh file at the same path while the second
//     process's fd is still flocked on the now-unlinked inode, reintroducing
//     exactly the kind of identity confusion this design avoids.
//
// Blocking/timeout: acquireCentralLock polls LOCK_NB with a short, fixed
// backoff up to a timeout (centralLockDefaultTimeout unless overridden)
// rather than blocking indefinitely, so a stuck or not-yet-reaped holder
// cannot hang every other `dso central` invocation forever. On timeout it
// returns ErrCentralLockBusy. There is no fallback path that proceeds
// without the lock on any error; every acquisition failure is fatal to the
// caller's operation.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// LockMode selects what kind of central lock a Store (or Restore's own
// explicit critical section) requires.
type LockMode int

const (
	// LockShared is compatible with any number of other LockShared holders
	// but not with a LockExclusive holder. This is Open's default: ordinary
	// reads and writes (SQLite's own transaction locking, via _txlock=
	// immediate, already serializes writers against each other at the SQL
	// level -- this sidecar lock only needs to keep those operations from
	// overlapping an exclusive database REPLACEMENT), and Backup (VACUUM
	// INTO reads the live database through its own consistent snapshot; it
	// does not need to exclude other shared holders, including another
	// concurrent backup).
	LockShared LockMode = iota
	// LockExclusive excludes every other holder, shared or exclusive. Only
	// Restore's own top-level critical section uses this (via
	// AcquireExclusiveCentralLock), for the entire span from just after the
	// candidate has been staged and validated (a dry run returns before the
	// lock is requested) and before the confirmation prompt, through the
	// final reopen -- covering the prompt, the mandatory safety snapshot and
	// the replacement, not merely the filesystem replace step -- because the
	// TOCTOU gap this protocol closes is "something else opens the database
	// while Restore is partway through", not only "something else opens it
	// during the literal rename instant".
	LockExclusive
)

func (m LockMode) String() string {
	if m == LockExclusive {
		return "exclusive"
	}
	return "shared"
}

// ErrCentralLockBusy is returned when a central lock could not be acquired
// before its timeout elapsed: another dso central process holds an
// incompatible lock on the same database. Callers must treat this as a
// hard failure.
var ErrCentralLockBusy = errors.New("sqlite: central database is locked by another dso central process")

const centralLockDefaultTimeout = 5 * time.Second
const centralLockPollInterval = 50 * time.Millisecond

// centralLockPath is the stable sidecar path for dbPath's lock: a fixed
// sibling name, independent of whatever inode currently exists at dbPath.
func centralLockPath(dbPath string) string {
	return dbPath + ".central.lock"
}

// absPathForLock resolves path to the identity this package's lock is keyed
// on: an absolute path with symlinks resolved, so two different symlinked
// routes to the SAME real database (e.g. a symlinked parent directory)
// produce the SAME lock sidecar, not two different ones that fail to
// coordinate with each other at all. filepath.Abs alone is not enough --
// it normalizes ".."/"." components but never follows a symlink.
//
// The leaf file may not exist yet (e.g. Restore's target when there is no
// active database, or a fresh Open): EvalSymlinks on the full path fails in
// that case, so this falls back to resolving the PARENT directory (which
// Open's own prepareFile/securefs.EnsureDir guarantees exists by the time
// this runs) and rejoins the leaf name, which still correctly resolves a
// symlinked parent directory even before the file itself is created.
func absPathForLock(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("sqlite: resolving %s for central lock identity: %w", path, err)
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// centralLock is a held advisory lock plus how to release it. The zero
// value (nil *centralLock) means "no lock held"; Close is a safe no-op on
// it. Close is also idempotent on a non-nil, already-released lock
// (a second call returns nil rather than re-invoking release, which would
// otherwise surface a spurious "already closed" error from the underlying
// fd) -- Store.Close and a caller that closes the same Store more than
// once (tests sometimes do, deliberately or not) must not fail because of
// that alone.
type centralLock struct {
	release func() error
	closed  bool
}

func (l *centralLock) Close() error {
	if l == nil || l.release == nil || l.closed {
		return nil
	}
	l.closed = true
	return l.release()
}

// RestoreLock is the exclusive central lock, held for the lifetime of a
// single Restore critical section. It is the ONLY way to open a Store
// without this package taking a fresh (and, for the same path in the same
// process, self-conflicting) central lock of its own: there is no field on
// Options a caller can set to request that -- skipCentralLock is
// unexported and can only be set by (*RestoreLock).Open, below, after that
// method has itself verified the requested path matches the one this lock
// was acquired for. Possessing a *RestoreLock for the exact path is the
// only credential this package accepts; a copy-pasted call site elsewhere
// cannot forge one.
type RestoreLock struct {
	path string
	lock *centralLock
}

// AcquireExclusiveCentralLock takes the top-level exclusive central lock for
// dbPath, for a caller (Restore) whose protected operation spans more than
// one Store's own Open/Close lifetime. Call (*RestoreLock).Close exactly
// once when the critical section ends.
func AcquireExclusiveCentralLock(dbPath string, timeout time.Duration) (*RestoreLock, error) {
	abs, err := absPathForLock(dbPath)
	if err != nil {
		return nil, err
	}
	l, err := acquireCentralLock(abs, LockExclusive, timeout)
	if err != nil {
		return nil, err
	}
	return &RestoreLock{path: abs, lock: l}, nil
}

// Close releases the exclusive lock. Safe to call on a nil *RestoreLock and
// safe to call more than once.
func (r *RestoreLock) Close() error {
	if r == nil {
		return nil
	}
	return r.lock.Close()
}

// Open opens path exactly like the package-level Open, but without taking a
// new central lock, because r already holds the exclusive one. It refuses
// to open any path other than the exact one r was acquired for -- a
// RestoreLock for one database can never be used, by accident or
// otherwise, to bypass locking on a different one.
func (r *RestoreLock) Open(ctx context.Context, path string, opts Options) (*Store, error) {
	if r == nil {
		return nil, errors.New("sqlite: (*RestoreLock).Open called on a nil lock")
	}
	// Use-after-release guard: without this, calling Close and then Open on
	// the same *RestoreLock would still succeed and skip locking entirely
	// (opts.skipCentralLock below trusts THIS check, not the underlying
	// fd's actual state) -- the exact same class of footgun the earlier
	// exported SkipCentralLock field was removed for, just one level
	// deeper. r.lock.closed is this package's own bookkeeping of whether
	// release has already run; it is authoritative regardless of whether
	// the kernel-level flock happens to still be held by some other
	// reference.
	if r.lock == nil || r.lock.closed {
		return nil, errors.New("sqlite: (*RestoreLock).Open called after the lock was already released")
	}
	abs, err := absPathForLock(path)
	if err != nil {
		return nil, err
	}
	if abs != r.path {
		return nil, fmt.Errorf("sqlite: restore lock held for %s cannot be used to open %s", r.path, abs)
	}
	opts.skipCentralLock = true
	return openStore(ctx, path, opts)
}

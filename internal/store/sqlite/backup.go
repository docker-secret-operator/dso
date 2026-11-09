package sqlite

// On-demand backup (Phase 2 Step 5.4, approved design/decision-resolution).
//
// This reuses the exact consistency and verification mechanism the existing
// migration-time backup (sqlite.go's backupBeforeMigration/verifyBackup)
// already relies on -- VACUUM INTO for a single-point-in-time, WAL-free
// snapshot, then PRAGMA integrity_check plus a schema_migrations version
// check before the snapshot is ever reported as usable -- rather than
// inventing a second backup format. backupBeforeMigration itself is left
// untouched: this is a separate, operator-triggered path with its own,
// caller-supplied destination, not a replacement for the automatic
// pre-migration backup.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs"
)

// ErrBackupDestinationExists is returned by Backup when something already
// exists at the requested destination. Backup never overwrites an existing
// file, symlink, or directory.
var ErrBackupDestinationExists = errors.New("sqlite: backup destination already exists")

// ErrBackupDestinationInvalid and ErrBackupVerificationFailed are category
// markers attached (via errors.Is) to the corresponding Backup failures so a
// caller can classify them without parsing text. They never change the
// failure's message.
var (
	ErrBackupDestinationInvalid = errors.New("sqlite: backup destination invalid")
	ErrBackupVerificationFailed = errors.New("sqlite: backup verification failed")
)

// taggedError keeps err's message unchanged while also matching tag via
// errors.Is.
type taggedError struct{ tag, err error }

func (e *taggedError) Error() string   { return e.err.Error() }
func (e *taggedError) Unwrap() []error { return []error{e.err, e.tag} }

// Backup writes a complete, verified, self-contained snapshot of the current
// database to destPath and returns the resolved absolute path on success.
//
// Consistency: the snapshot is produced by VACUUM INTO, which SQLite
// documents as reading the live database through its own read transaction --
// the result reflects a single point in time and needs no companion -wal/
// -shm file, even if a write transaction is in flight when Backup runs.
//
// Safety: destPath's parent directory is created (mode 0700) if missing and
// checked with the same directory-chain/symlink discipline already applied
// to the database and key files (internal/securefs); destPath itself is
// created exclusively at mode 0600 -- a pre-existing file, directory, or
// symlink at destPath causes ErrBackupDestinationExists (wrapping the
// underlying os.ErrExist) and Backup never follows or overwrites it.
//
// Verification: before returning success, the written file is independently
// re-opened read-only and checked for PRAGMA integrity_check = "ok" and that
// its own schema_migrations matches the live database's current schema
// version (the same verifyBackup helper backupBeforeMigration already uses).
// On any failure -- including a failure partway through writing, verifying,
// or syncing -- the partially-written destination file is removed; Backup
// never leaves an unverified or partial file at destPath.
//
// Content and limits: the backup contains every table, including the full
// audit_events hash chain -- no table is excluded. It never contains the
// root key or passphrase: those live entirely outside the database (see
// docs/central-store-m1.md, "Key hierarchy, binding and lifecycle") and are
// the operator's own responsibility to preserve separately; a backup without
// the matching root key cannot be used to recover any secret value.
//
// Backup never prunes or deletes any other backup file; retention is
// entirely operator-managed (Step 5.4 decision 4).
func (s *Store) Backup(ctx context.Context, destPath string) (string, error) {
	if err := s.guard(ctx); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(destPath)
	if err != nil {
		return "", &taggedError{ErrBackupDestinationInvalid, fmt.Errorf("resolving backup destination %s: %w", destPath, err)}
	}
	dir := filepath.Dir(abs)
	if err := securefs.EnsureDir(dir); err != nil {
		return "", &taggedError{ErrBackupDestinationInvalid, fmt.Errorf("backup destination directory is not safe: %w", err)}
	}
	wantVersion, err := s.SchemaVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("reading schema version: %w", err)
	}

	f, err := securefs.CreateExclusive(abs, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s", ErrBackupDestinationExists, abs)
		}
		return "", fmt.Errorf("creating backup destination %s: %w", abs, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(abs) // our own just-created, empty file
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = os.Remove(abs) // best-effort: our own just-created, incomplete file
		return "", err
	}

	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, abs); err != nil {
		return fail(fmt.Errorf("writing backup: %w", err))
	}
	if err := verifyBackup(ctx, abs, wantVersion); err != nil {
		return fail(&taggedError{ErrBackupVerificationFailed, fmt.Errorf("backup failed verification: %w", err)})
	}
	if bf, err := os.OpenFile(abs, os.O_RDWR, 0); err == nil { // #nosec G304 -- abs is the backup file this function just created, at a destination already validated by securefs above
		err = bf.Sync()
		if cerr := bf.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fail(err)
		}
	}
	// Sync the destination directory too, so the backup file's directory
	// entry itself survives a crash, not just its content. Unlike the
	// file-level sync above, a failure here does NOT remove or invalidate
	// the backup: the file itself is already written, synced, and passed
	// verifyBackup, so it is a correct, usable backup regardless. But
	// silently discarding this error (as earlier versions of this function
	// did) is inconsistent with the fail-closed posture of every other step
	// here, and it would let Backup report unqualified success when a real
	// durability guarantee was not actually confirmed. Report it via a
	// typed, non-fatal error instead so callers can decide (see
	// BackupDirectorySyncError and SafetySnapshot).
	dirSyncErr := syncBackupDir(dir)
	if err := os.Chmod(abs, 0o600); err != nil {
		return fail(err)
	}
	if dirSyncErr != nil {
		return abs, &BackupDirectorySyncError{dirSyncErr}
	}
	return abs, nil
}

// syncBackupDir fsyncs dir so a backup file's directory entry is durable
// across a crash. It is a package-level variable, not a direct os.Open/Sync
// call inlined into Backup, purely so tests can simulate a directory-sync
// failure deterministically -- OS-level permission failures are unreliable
// to provoke across platforms and CI sandboxes, the same reason
// internal/centralsvc.RestoreOptions' own test* seams exist.
var syncBackupDir = func(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- directory of the backup file created above, already validated by securefs.EnsureDir
	if err != nil {
		return fmt.Errorf("opening backup directory %s to sync: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("syncing backup directory %s: %w", dir, err)
	}
	return nil
}

// BackupDirectorySyncError is returned by Backup only AFTER the backup file
// itself has been fully written, fsynced, and independently verified
// (verifyBackup): the backup is correct and usable, but fsyncing its parent
// directory -- the step that makes the backup's directory entry itself
// durable across a crash, as distinct from its content -- failed or could
// not be attempted. Backup still returns the (valid) destination path
// alongside this error; callers that treat any non-nil error as "no backup
// was produced" must check for this type first (SafetySnapshot does).
type BackupDirectorySyncError struct{ err error }

func (e *BackupDirectorySyncError) Error() string { return e.err.Error() }
func (e *BackupDirectorySyncError) Unwrap() error { return e.err }

// NewBackupDirectorySyncErrorForTesting constructs a *BackupDirectorySyncError
// wrapping err. BackupDirectorySyncError's own field is deliberately
// unexported (callers should only ever inspect it via errors.As/Is), which
// means a test in another package that needs to exercise a caller's
// handling of this specific error type -- without reaching into Backup's
// own unexported syncBackupDir seam -- has no other way to construct one.
func NewBackupDirectorySyncErrorForTesting(err error) error {
	return &BackupDirectorySyncError{err}
}

// ErrCheckpointIncomplete is returned by CheckpointTruncate when SQLite
// reports the checkpoint did not fully complete (busy != 0), meaning the
// -wal file may still hold frames not yet folded into the main database
// file.
var ErrCheckpointIncomplete = errors.New("sqlite: WAL checkpoint did not complete (another connection is holding a conflicting lock)")

// CheckpointTruncate runs PRAGMA wal_checkpoint(TRUNCATE) and verifies, from
// the pragma's own result row, that it actually completed -- unlike
// Store.Close's own best-effort checkpoint, whose error is intentionally
// discarded there (that discard is safe for an ordinary Close because the
// store remains usable either way; it is NOT safe for the restore path,
// which needs a verified guarantee before it removes the previous
// database's WAL/SHM companions -- see Restore in internal/centralsvc).
//
// PRAGMA wal_checkpoint(TRUNCATE) returns one row of (busy, log,
// checkpointed): busy != 0 means some other connection held a lock that
// prevented a full checkpoint, in which case the -wal file may still
// contain frames that are not yet reflected in the main database file.
// CheckpointTruncate treats that as ErrCheckpointIncomplete rather than
// silently reporting success.
func (s *Store) CheckpointTruncate(ctx context.Context) error {
	var busy, log, checkpointed int
	row := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if err := row.Scan(&busy, &log, &checkpointed); err != nil {
		return fmt.Errorf("running WAL checkpoint: %w", err)
	}
	if busy != 0 {
		return ErrCheckpointIncomplete
	}
	return nil
}

// SafetySnapshot writes a mandatory pre-restore safety snapshot of the
// currently active database to a new, automatically-named file beside
// dbPath, using Backup's exact mechanism and guarantees, and returns its
// path. The naming convention ("<dbPath>.pre-restore-<UTC time>-<random
// hex>.bak") mirrors the existing migration-time backup's own naming scheme
// so a pre-restore snapshot is never mistaken for a pre-migration one and two
// concurrent attempts cannot collide.
//
// This is the step the Step 5.4 design calls its single strongest
// recommendation for making restore recoverable from its own failure: it
// must succeed, and be independently verified, before anything about the
// active database is touched by a restore.
//
// A *BackupDirectorySyncError from the underlying Backup call is NOT treated
// as a failed snapshot here: the snapshot file itself is valid and usable
// (Backup only returns one once the file is written, synced, and
// independently re-verified), so retrying the whole snapshot on that basis
// would be wrong -- it would also exhaust SafetySnapshot's own
// ErrBackupDestinationExists retry budget for no reason. SafetySnapshot
// instead returns the valid path AND that error together, exactly as Backup
// does, so the caller (Restore) can decide how to surface a durability
// warning without losing the snapshot it depends on for recovery.
func (s *Store) SafetySnapshot(ctx context.Context, dbPath string) (string, error) {
	for attempt := 0; ; attempt++ {
		var rnd [4]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return "", err
		}
		dst := fmt.Sprintf("%s.pre-restore-%s-%s.bak", dbPath,
			time.Now().UTC().Format("20060102T150405Z"), hex.EncodeToString(rnd[:]))
		path, err := s.Backup(ctx, dst)
		if err == nil {
			return path, nil
		}
		var dirSyncErr *BackupDirectorySyncError
		if errors.As(err, &dirSyncErr) {
			return path, err
		}
		if errors.Is(err, ErrBackupDestinationExists) && attempt < 5 {
			continue
		}
		return "", err
	}
}

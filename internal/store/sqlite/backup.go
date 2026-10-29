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
		return "", fmt.Errorf("resolving backup destination %s: %w", destPath, err)
	}
	dir := filepath.Dir(abs)
	if err := securefs.EnsureDir(dir); err != nil {
		return "", fmt.Errorf("backup destination directory is not safe: %w", err)
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
		return fail(fmt.Errorf("backup failed verification: %w", err))
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
	if d, err := os.Open(dir); err == nil { // #nosec G304 -- directory of the backup file created above, already validated by securefs.EnsureDir
		_ = d.Sync()
		_ = d.Close()
	}
	if err := os.Chmod(abs, 0o600); err != nil {
		return fail(err)
	}
	return abs, nil
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
		if errors.Is(err, ErrBackupDestinationExists) && attempt < 5 {
			continue
		}
		return "", err
	}
}

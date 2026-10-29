package sqlite

// Restore-candidate validation (Phase 2 Step 5.4, approved design/decision-
// resolution). This file covers every validation check that belongs at the
// store layer -- path safety, SQLite structural integrity, schema/migration-
// version compatibility, and full audit-chain self-consistency -- and never
// opens the candidate for write, migrates it, or otherwise modifies it: the
// source backup must never be altered by validation. Root-key binding
// verification is a service-layer concern (it needs a crypto.KeyProvider)
// and lives in internal/centralsvc instead; see centralsvc.ValidateCandidateKey.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs"
	"github.com/docker-secret-operator/dso/internal/store"
)

// OpenReadOnly opens path as a read-only SQLite connection (PRAGMA
// query_only), without creating it, applying migrations, or otherwise
// modifying it. It is used to validate a restore candidate, and to inspect a
// backup, without touching the file itself. Callers must Close() the
// returned *sql.DB. It does not perform any of the path-safety checks
// ValidateCandidate applies; callers that need those must call them
// separately (ValidateCandidate does).
func OpenReadOnly(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "query_only(1)")
	dsn := (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// LatestSchemaVersion returns the highest schema version this binary's
// embedded migrations support -- the same value Open's own migrate() treats
// as the ceiling for ErrSchemaTooNew. Restore validation rejects a candidate
// newer than this, exactly as Open already rejects a live database newer
// than this.
func LatestSchemaVersion() (int, error) {
	migs, err := EmbeddedMigrations()
	if err != nil {
		return 0, err
	}
	if len(migs) == 0 {
		return 0, nil
	}
	return migs[len(migs)-1].Version, nil
}

// CopyCandidateForValidation copies path's main file, and any -wal/-shm
// companions that actually exist beside it, into a fresh private temporary
// directory, using only plain file reads/writes -- never the SQLite driver
// -- so the original candidate is never passed to anything that could
// checkpoint, recover, or otherwise modify it (see ValidateCandidate's own
// doc comment for why this is necessary even for a nominally read-only
// SQLite connection). It returns the copy's main file path and a cleanup
// function that removes the whole temporary directory; callers must call
// cleanup() when done. Exported so centralsvc.ValidateCandidateKey -- which
// needs the identical protection for its own, separate read of the
// candidate -- can reuse it instead of duplicating it.
func CopyCandidateForValidation(path string) (workPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "dso-restore-validate-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temporary validation directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	workPath = filepath.Join(tmpDir, "candidate.db")
	if err := copyFile(path, workPath); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copying %s: %w", path, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		src := path + suffix
		if _, statErr := os.Lstat(src); statErr != nil {
			continue // most candidates (anything from this package's own Backup) have no companions at all
		}
		if err := copyFile(src, workPath+suffix); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("copying %s: %w", src, err)
		}
	}
	return workPath, cleanup, nil
}

// copyFile copies src to dst with a plain, non-exclusive read/write (dst is
// inside a temporary directory this process just created and owns
// exclusively, so O_EXCL is unnecessary there), and fsyncs dst before
// returning.
func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- src is either the already securefs-checked restore candidate, or its -wal/-shm companion in the same already-checked directory
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- dst is inside a temp directory this function just created
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// CandidateInfo summarizes a restore candidate's validated state. It carries
// no secret value or key material -- only metadata already safe to show an
// operator before they confirm a restore.
type CandidateInfo struct {
	SchemaVersion int
	EventCount    int
	// LastEvent is the candidate's own audit-chain tip (the event
	// PrevHash/EventHash linkage identifies), or nil if the chain is empty
	// (including a candidate from before the audit_events table existed).
	LastEvent *store.AuditEvent
}

// ValidateCandidate performs every restore-candidate check the approved Step
// 5.4 design requires at the store layer, BEFORE the active database is
// touched:
//
//  1. path safety: the exact internal/securefs regular-file/permission/
//     directory-chain discipline already applied to the database and key
//     files (securefs.OpenPrivateFile) -- applied to the ORIGINAL candidate
//     path, before anything else;
//  2. SQLite structural integrity (PRAGMA integrity_check);
//  3. schema/migration-version compatibility: a candidate from a newer
//     schema than maxSupportedSchema is rejected with store.ErrSchemaTooNew,
//     exactly like Open's own migrate() check for a live database;
//  4. full audit-chain self-consistency (VerifyAuditChain over every event
//     present in the candidate, if the candidate has an audit_events table
//     at all -- a candidate predating that table has an empty, trivially
//     valid chain).
//
// Candidate protection: SQLite's WAL subsystem can checkpoint, and thereby
// modify or delete, a database's -wal/-shm companion files merely by opening
// it, even through a PRAGMA query_only connection, whenever the file was
// ever put in WAL mode and still has pending frames -- confirmed by direct
// experiment during this implementation, not assumed: opening such a file
// through OpenReadOnly measurably removed its -wal/-shm files. A candidate
// produced by this package's own Backup (always non-WAL, via VACUUM INTO)
// never has this exposure, but an externally supplied candidate might. To
// guarantee the ORIGINAL candidate path is genuinely never modified --
// regardless of its journal mode -- every check below runs against a
// private, disposable COPY of the candidate (main file plus any -wal/-shm
// companions actually present), made with plain, non-SQLite file reads/
// writes before SQLite ever opens anything. The original is opened only
// once, by securefs.OpenPrivateFile, purely to check its safety and to read
// its bytes for that copy -- never by the SQLite driver.
//
// This also avoids the opposite failure mode of simply opening the original
// with SQLite's own "immutable=1"/"mode=ro" connection flags: those make
// SQLite skip WAL recovery entirely, which would silently validate only the
// main file's stale, pre-WAL content for a candidate with real pending
// frames, rather than its true, complete logical state. Validating a
// disposable copy lets SQLite checkpoint and recover normally -- so the
// complete, correct data is what gets validated -- without that ever
// touching the original.
//
// A clean return proves the candidate's own internal consistency; per the
// approved design, it does NOT prove the candidate is the newest legitimate
// state (whole-database rollback via an older, internally valid backup is
// not detected by chain verification alone -- see
// docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md §7)
// and does NOT prove every secret version's ciphertext is decryptable (that
// is a key-binding and, separately, an AEAD-authentication question -- see
// centralsvc.ValidateCandidateKey and the Step 5.4 decision-resolution §4).
func ValidateCandidate(ctx context.Context, path string, maxSupportedSchema int) (CandidateInfo, error) {
	f, err := securefs.OpenPrivateFile(path)
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("restore candidate is not safe to use: %w", err)
	}
	fi, statErr := f.Stat()
	_ = f.Close()
	if statErr != nil {
		return CandidateInfo{}, fmt.Errorf("inspecting restore candidate %s: %w", path, statErr)
	}
	if fi.Size() == 0 {
		return CandidateInfo{}, fmt.Errorf("restore candidate %s is empty", path)
	}

	workPath, cleanup, err := CopyCandidateForValidation(path)
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("staging a private copy of restore candidate %s for validation: %w", path, err)
	}
	defer cleanup()

	db, err := OpenReadOnly(workPath)
	if err != nil {
		return CandidateInfo{}, fmt.Errorf("restore candidate %s is not a valid SQLite database: %w", path, err)
	}
	defer db.Close()

	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return CandidateInfo{}, fmt.Errorf("restore candidate %s failed integrity_check (result %q, error %v)", path, integrity, err)
	}

	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return CandidateInfo{}, fmt.Errorf("restore candidate %s has no readable schema_migrations table: %w", path, err)
	}
	schemaVersion := int(v.Int64)
	if schemaVersion > maxSupportedSchema {
		return CandidateInfo{}, fmt.Errorf("%w: restore candidate %s is at schema %d, this binary supports up to %d",
			store.ErrSchemaTooNew, path, schemaVersion, maxSupportedSchema)
	}

	var hasAuditTable int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_events'`).Scan(&hasAuditTable); err != nil {
		return CandidateInfo{}, fmt.Errorf("restore candidate %s: checking for audit_events table: %w", path, err)
	}

	var events []store.AuditEvent
	if hasAuditTable == 1 {
		o := ops{q: db, now: time.Now}
		events, err = o.ListAuditEvents(ctx)
		if err != nil {
			return CandidateInfo{}, fmt.Errorf("restore candidate %s: audit history is not readable: %w", path, err)
		}
		if err := VerifyAuditChain(events); err != nil {
			return CandidateInfo{}, fmt.Errorf("restore candidate %s failed audit-chain verification: %w", path, err)
		}
	}

	info := CandidateInfo{SchemaVersion: schemaVersion, EventCount: len(events)}
	if len(events) > 0 {
		last := events[len(events)-1]
		info.LastEvent = &last
	}
	return info, nil
}

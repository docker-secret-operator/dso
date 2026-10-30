package centralsvc

// Backup/restore orchestration (Phase 2 Step 5.4, approved design/decision-
// resolution). This file implements:
//   - root-key binding verification against a restore candidate file
//     (ValidateCandidateKey), mirroring binding.go's verifyBinding but
//     applied to a standalone file instead of this Service's own live
//     Store -- Bind itself is tied to a mutable Store and would attempt to
//     CREATE a binding on an unbound database, which is never the right
//     behaviour for a read-only restore candidate;
//   - the full restore state machine (Restore): validate -> mandatory
//     safety snapshot of the active store -> close -> replace -> reopen ->
//     append the forward-only restore audit event.
//
// Explicitly NOT implemented here, per the approved scope: an external
// rollback-detection anchor, Step 5.2 socket authentication, automatic
// backup/audit retention, and bulk secret decryption during restore. See
// docs/reviews/2026-10-25-central-store-phase2-backup-restore-design.md and
// docs/reviews/2026-10-29-central-store-phase2-backup-restore-decision-resolution.md
// (historical references: neither document is present in this checkout or its
// git history, so the decisions they record could not be re-verified here).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// ErrRestoreNotConfirmed is returned by Restore when RestoreOptions.Confirm
// is set and returns false. The active database is left untouched.
var ErrRestoreNotConfirmed = errors.New("centralsvc: restore was not confirmed")

// ValidateCandidateKey verifies that kp is the root key the restore candidate
// at path is bound to, without opening the candidate for write, migrating
// it, or otherwise modifying it -- the source backup must never be altered
// by validation.
//
// This mirrors Service.Bind's own canary-unwrap verification (binding.go's
// verifyBinding): it reads the candidate's binding.* store_meta rows and
// asks kp to unwrap the stored canary under kp's own id, exactly as Bind
// does for a live database. It does not call Bind itself, because Bind is
// tied to a mutable Store and, for an unbound database, would attempt to
// CREATE a binding -- never correct for a read-only restore candidate, which
// is by construction already bound if it was ever written to by this
// system.
//
// Limitation, stated explicitly per the approved decision resolution
// (decision 2): a successful return proves the configured root key matches
// the one this candidate is bound to. It does NOT prove every secret
// version's ciphertext in the candidate will successfully decrypt --
// SQLite-level structural validity (checked separately by
// sqlite.ValidateCandidate) and AES-GCM-level ciphertext authenticity are
// independent properties, and this function does not perform bulk
// decryption to check the latter.
//
// Like sqlite.ValidateCandidate, this reads a private, disposable copy of
// the candidate (sqlite.CopyCandidateForValidation), never the original
// path -- opening the original directly, even through a nominally
// read-only connection, can checkpoint and modify its -wal/-shm companions
// (confirmed by direct experiment; see sqlite.ValidateCandidate's own doc
// comment for the full explanation).
func ValidateCandidateKey(ctx context.Context, path string, kp crypto.KeyProvider) error {
	staged, err := sqlite.StageCandidate(ctx, path)
	if err != nil {
		return fmt.Errorf("staging a private copy of restore candidate %s for key validation: %w", path, err)
	}
	defer staged.Close()
	return validateStagedCandidateKey(ctx, staged, path, kp)
}

// validateStagedCandidateKey is ValidateCandidateKey against an already-
// staged snapshot, so Restore validates the exact artifact it later activates.
func validateStagedCandidateKey(ctx context.Context, staged *sqlite.StagedCandidate, path string, kp crypto.KeyProvider) error {
	workPath := staged.Path
	db, err := sqlite.OpenReadOnly(workPath)
	if err != nil {
		return fmt.Errorf("opening restore candidate for key validation: %w", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT key, value FROM store_meta WHERE key LIKE 'binding.%'`)
	if err != nil {
		return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
	}
	var version, keyID, canary []byte
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
		}
		switch k {
		case metaBindingVersion:
			version = v
		case metaBindingKeyID:
			keyID = v
		case metaBindingCanary:
			canary = v
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading restore candidate key-binding metadata: %w", err)
	}
	if version == nil || keyID == nil || canary == nil {
		return fmt.Errorf("%w: restore candidate has no key-binding metadata", ErrBindingCorrupt)
	}
	if string(version) != bindingFormat {
		return fmt.Errorf("%w (unsupported binding format)", ErrBindingCorrupt)
	}

	id := kp.ID()
	bindAAD, err := crypto.BindingAAD(id)
	if err != nil {
		return fmt.Errorf("building key-binding context: %w", err)
	}
	unwrapped, err := kp.Unwrap(ctx, canary, bindAAD)
	if err == nil && len(unwrapped) == crypto.KeySize {
		crypto.Zero(unwrapped)
		return nil
	}
	crypto.Zero(unwrapped)
	if string(keyID) == id {
		// The metadata names this very key but the canary does not open: the
		// canary was altered or damaged, not a different key presented.
		return ErrBindingCorrupt
	}
	return fmt.Errorf("%w: restore candidate is bound to root key %q but the configured key is %q",
		ErrWrongRootKey, string(keyID), id)
}

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// CandidatePath is the backup file to restore from. It is never
	// modified, deleted, or overwritten by Restore.
	CandidatePath string
	// DBPath is the active database's file path. On a non-dry-run restore,
	// this file is replaced.
	DBPath string
	// KeyProvider is the configured root key, verified against the
	// candidate's own binding before anything else is touched.
	KeyProvider crypto.KeyProvider
	// DryRun performs every validation check and returns, without touching
	// the active database, creating a safety snapshot, replacing any file,
	// or appending a restore event.
	DryRun bool
	// Actor is the caller-supplied attribution string recorded on the
	// restore event's ActorLabel, exactly like every other operation's actor
	// parameter -- attribution only, never authenticated identity.
	Actor string
	// Confirm, if non-nil, is called once validation succeeds, before the
	// active database is touched (DryRun skips this entirely: it never
	// reaches this call). It receives the validated candidate's info so the
	// caller can show it to the operator before asking for confirmation.
	// Returning false aborts the restore with ErrRestoreNotConfirmed and
	// leaves the active database completely untouched.
	Confirm func(info sqlite.CandidateInfo) bool

	// testReplaceDatabaseFile, testReopen and testAppendRestoreEvent, when
	// non-nil, replace the real replaceDatabaseFile/sqlite.Open/
	// AppendAuditEvent calls for this one Restore call. Unexported: a
	// caller outside this package cannot set them. This is the same
	// dependency-injection test-seam pattern internal/cli/central.go
	// already uses (centralAuditLoggerOverride) for the same reason: OS-
	// level filesystem-permission failures are unreliable to provoke
	// deterministically (they behave differently as root or in some CI
	// sandboxes), so tests inject a failure at an exact point in the state
	// machine instead of trying to break real file permissions.
	testReplaceDatabaseFile func(dbPath, candidatePath string) error
	testReopen              func(ctx context.Context, path string) (*sqlite.Store, error)
	testAppendRestoreEvent  func(ctx context.Context, st *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error)
}

// RestoreResult reports what Restore did (or, for a dry run or an aborted
// confirmation, would have done).
type RestoreResult struct {
	CandidateInfo  sqlite.CandidateInfo
	SafetySnapshot string // path of the mandatory pre-restore safety snapshot; "" if none was needed/taken
	RestoreEventID string // "" on dry run, a declined confirmation, or failure before the event could be recorded
	DryRun         bool
}

// Restore implements the approved Step 5.4 restore state machine:
//
//  1. Validate the candidate completely (sqlite.ValidateCandidate +
//     ValidateCandidateKey) -- nothing about the active database is touched
//     by this step.
//  2. If DryRun, return here: no safety snapshot, no replacement, no event.
//  3. If Confirm is set, call it with the validated candidate info; a false
//     return aborts with ErrRestoreNotConfirmed, active database untouched.
//  4. If an active database exists at DBPath, open it and take a mandatory,
//     independently verified safety snapshot (Store.SafetySnapshot) before
//     anything else -- this is the recovery path if any later step fails.
//  5. Verify, not merely attempt, a full WAL checkpoint (Store.
//     CheckpointTruncate) -- aborting here, with the active database still
//     untouched, if it cannot complete -- then close the active database.
//  6. Replace the active database file with the candidate's content,
//     without ever modifying or deleting the candidate itself. The
//     PREVIOUS database's -wal/-shm companions are removed before this
//     step (not after), which step 5's verified checkpoint is what makes
//     safe: see replaceDatabaseFile.
//  7. Reopen the restored file through the normal sqlite.Open path (the same
//     validation and, if needed, forward migration every other database
//     open already goes through).
//  8. Append the forward-only restore audit event to the restored chain.
//
// Restore never claims success before step 7 (reopen) has actually
// succeeded: every error returned before that point leaves the result's
// RestoreEventID empty and is accompanied, where a safety snapshot exists,
// by that snapshot's path so the operator can recover the previous usable
// store. Restore never claims that the restored database is the newest
// legitimate state solely because its audit chain verified -- that chain
// verification is exactly the check sqlite.ValidateCandidate already
// performed in step 1, and the approved design is explicit that it cannot
// detect whole-database rollback (see the design document §7).
//
// Concurrency limitation (not mitigated by code): there is no cross-process
// lock. The verified checkpoint in step 5 only proves no conflicting
// connection blocked it at that instant; another process that has the
// database open could still commit between that checkpoint and the removal of
// the -wal/-shm companions in step 6, and that commit would be lost (and
// removing a live -shm out from under a process is unsafe). Run restore only
// while no other process (e.g. a running `dso` server) has the database open.
// Same-UID file permissions do not provide locking.
//
// Restore does not implement an external rollback anchor, does not decrypt
// any secret value, and has no caller-specific authorization: anyone able to
// run `dso central` as the owning OS user can call this, identically to
// every other `dso central` operation today (Step 5.2's Option B design is
// not implemented; see the Step 5.4 decision resolution, decision 5).
func Restore(ctx context.Context, opts RestoreOptions) (RestoreResult, error) {
	absCandidate, err := filepath.Abs(opts.CandidatePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolving restore candidate path: %w", err)
	}
	absDB, err := filepath.Abs(opts.DBPath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolving active database path: %w", err)
	}
	if absCandidate == absDB {
		return RestoreResult{}, fmt.Errorf("restore candidate and active database must not be the same file (%s)", absDB)
	}

	maxSchema, err := sqlite.LatestSchemaVersion()
	if err != nil {
		return RestoreResult{}, fmt.Errorf("determining supported schema version: %w", err)
	}
	// Stage ONE private, self-contained snapshot of the candidate (any -wal
	// frames folded into its main file) and use that single artifact for every
	// validation check AND as the source of the activated database. The state
	// validated is therefore, by construction, the state activated, and the
	// original candidate files are never opened by SQLite or modified. The
	// staged copy lives until Restore returns.
	staged, err := sqlite.StageCandidate(ctx, absCandidate)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed validation (rejected before any change; active database untouched): %w", err)
	}
	defer staged.Close()
	info, err := sqlite.ValidateStagedCandidate(ctx, staged, absCandidate, maxSchema)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed validation (rejected before any change; active database untouched): %w", err)
	}
	if err := validateStagedCandidateKey(ctx, staged, absCandidate, opts.KeyProvider); err != nil {
		return RestoreResult{}, fmt.Errorf("restore candidate failed key validation (rejected before any change; active database untouched): %w", err)
	}

	result := RestoreResult{CandidateInfo: info, DryRun: opts.DryRun}
	if opts.DryRun {
		return result, nil
	}

	if opts.Confirm != nil && !opts.Confirm(info) {
		return result, ErrRestoreNotConfirmed
	}

	if fileExists(absDB) {
		active, err := sqlite.Open(ctx, absDB, sqlite.Options{})
		if err != nil {
			return result, fmt.Errorf("opening active database for safety snapshot: %w", err)
		}
		snapshotPath, err := active.SafetySnapshot(ctx, absDB)
		if err != nil {
			_ = active.Close()
			return result, fmt.Errorf("creating mandatory safety snapshot: %w (restore aborted; active database untouched)", err)
		}
		result.SafetySnapshot = snapshotPath
		// Verified checkpoint, not Close's own best-effort one (whose error
		// is intentionally discarded there -- see CheckpointTruncate's doc
		// comment): replaceDatabaseFile below removes the active database's
		// -wal/-shm companions BEFORE it is replaced, which is only safe to
		// do once we have confirmed, not merely hoped, that no frame in
		// those files is still the only copy of a committed write. If the
		// checkpoint cannot complete, abort here -- before anything about
		// the active database has changed -- rather than proceeding on an
		// unverified assumption.
		if err := active.CheckpointTruncate(ctx); err != nil {
			_ = active.Close()
			return result, fmt.Errorf("checkpointing active database before replacement: %w "+
				"(restore aborted; active database untouched; safety snapshot at %s)", err, snapshotPath)
		}
		if err := active.Close(); err != nil {
			return result, fmt.Errorf("closing active database before replacement: %w (safety snapshot at %s; the active database has not been modified yet)",
				err, snapshotPath)
		}
	}

	replaceFn := replaceDatabaseFile
	if opts.testReplaceDatabaseFile != nil {
		replaceFn = opts.testReplaceDatabaseFile
	}
	var durabilityErr error
	if err := replaceFn(absDB, staged.Path); err != nil {
		var ue *unconfirmedDurabilityError
		if errors.As(err, &ue) {
			// The rename already happened: the candidate IS the active
			// database. Carry on (reopen, audit) and report this at the end.
			durabilityErr = ue
		} else {
			if result.SafetySnapshot != "" {
				return result, fmt.Errorf("replacing active database failed before activation: %w "+
					"(the active database at %s was not replaced and is unchanged; the safety snapshot at %s is not needed)",
					err, absDB, result.SafetySnapshot)
			}
			return result, fmt.Errorf("replacing active database failed before activation: %w (no database was created at %s)", err, absDB)
		}
	}

	reopenFn := func(ctx context.Context, path string) (*sqlite.Store, error) {
		return sqlite.Open(ctx, path, sqlite.Options{})
	}
	if opts.testReopen != nil {
		reopenFn = opts.testReopen
	}
	restored, err := reopenFn(ctx, absDB)
	if err != nil {
		if result.SafetySnapshot != "" {
			return result, fmt.Errorf("reopening restored database: %w (RECOVERY: restore the safety snapshot at %s to %s; "+
				"the replacement WAS ACTIVATED: the database at %s is now the candidate's content, which failed to reopen -- it is NOT automatically recovered; recover manually from the snapshot)",
				err, result.SafetySnapshot, absDB, absDB)
		}
		return result, fmt.Errorf("reopening restored database: %w", err)
	}
	defer restored.Close()

	evID, err := newID()
	if err != nil {
		return result, fmt.Errorf("generating restore event id: %w (restore completed and the database is usable; "+
			"the restore event could not be recorded; safety snapshot at %s)", err, result.SafetySnapshot)
	}
	appendFn := func(ctx context.Context, st *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error) {
		return st.AppendAuditEvent(ctx, ev)
	}
	if opts.testAppendRestoreEvent != nil {
		appendFn = opts.testAppendRestoreEvent
	}
	_, err = appendFn(ctx, restored, store.AuditEvent{
		ID:         evID,
		EventType:  store.AuditRestore,
		Outcome:    store.AuditSuccess,
		ActorLabel: opts.Actor,
	})
	if err != nil {
		return result, fmt.Errorf("restore completed and the database is usable, but the restore audit event could not be recorded: %w "+
			"(safety snapshot at %s)", err, result.SafetySnapshot)
	}
	result.RestoreEventID = evID
	if durabilityErr != nil {
		return result, fmt.Errorf("restore completed and the restore event was recorded, but durability of the replacement could not be confirmed: %w "+
			"(safety snapshot at %s)", durabilityErr, result.SafetySnapshot)
	}
	return result, nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// replaceDatabaseFile copies candidatePath's content into a new temporary
// file beside dbPath, fsyncs it, and atomically renames it onto dbPath
// (os.Rename replaces an existing destination atomically on the same
// filesystem -- there is never a window with no file at dbPath at all).
// candidatePath (Restore passes the validated staged snapshot, never the
// original candidate) is only ever opened read-only: it is never modified or
// deleted.
//
// dbPath's directory is checked with the same internal/securefs directory-
// chain/symlink discipline the database and key files already use,
// regardless of whether an active database previously existed there --
// Restore's caller only re-verifies this incidentally (via sqlite.Open) when
// a prior active database existed; the no-prior-database path has nothing
// else to check it, so this function checks it itself rather than relying
// on that incidental coverage.
//
// Any -wal/-shm companion of the PREVIOUS database at dbPath is removed
// BEFORE the rename, not after: by the time this function runs, Restore has
// already verified (Store.CheckpointTruncate, not Store.Close's own
// best-effort one) that no frame in those files is the only copy of a
// committed write, so removing them first is not a data-loss risk, and it
// eliminates the alternative window entirely -- there is no ordering under
// which the NEW main file content and the OLD database's -wal/-shm can ever
// coexist on disk at the same time, because the old ones are already gone
// before the new main file exists at dbPath at all.
func replaceDatabaseFile(dbPath, candidatePath string) error {
	dir := filepath.Dir(dbPath)
	if err := securefs.EnsureDir(dir); err != nil {
		return fmt.Errorf("active database directory is not safe: %w", err)
	}

	// Best-effort: these may not exist (a VACUUM INTO-sourced candidate
	// never has companions of its own, and a first-ever restore with no
	// prior active database has nothing to remove). An error removing a
	// file that does exist would be surfaced by the rename step below
	// failing for the same underlying reason (e.g. a now-unwritable
	// directory), so it is not treated as fatal here on its own.
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")

	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.restoring-%s", dbPath, hex.EncodeToString(rnd[:]))

	src, err := os.Open(candidatePath) // #nosec G304 -- candidatePath was already validated read-only by sqlite.ValidateCandidate/securefs.OpenPrivateFile; opened here read-only and never written to
	if err != nil {
		return fmt.Errorf("opening restore candidate: %w", err)
	}
	defer src.Close()

	dst, err := securefs.CreateExclusive(tmp, 0o600)
	if err != nil {
		return fmt.Errorf("creating temporary restore file: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copying restore candidate: %w", err)
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("syncing restored database: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing restored database: %w", err)
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("activating restored database: %w", err)
	}
	d, err := os.Open(dir) // #nosec G304 -- directory of the database file being replaced, already checked safe above
	if err != nil {
		return &unconfirmedDurabilityError{fmt.Errorf("opening %s to sync the rename: %w", dir, err)}
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return &unconfirmedDurabilityError{fmt.Errorf("syncing directory %s after rename: %w", dir, err)}
	}
	return nil
}

// unconfirmedDurabilityError is returned by replaceDatabaseFile only AFTER the
// rename has activated the new database: the replacement is in place and
// usable, but the directory fsync that makes the rename crash-durable failed.
type unconfirmedDurabilityError struct{ err error }

func (e *unconfirmedDurabilityError) Error() string { return e.err.Error() }
func (e *unconfirmedDurabilityError) Unwrap() error { return e.err }

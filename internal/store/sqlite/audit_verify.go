package sqlite

// Read-only verification of the existing, unkeyed audit hash chain (Phase 2,
// operator-facing `dso central audit verify`). This file adds no new integrity
// model: it stages a private copy of the database, reads audit_events from the
// copy and hands the events to the existing VerifyAuditChain.
//
// Read-only strategy. Opening a WAL-mode database through SQLite -- even with
// PRAGMA query_only -- can checkpoint and delete its -wal/-shm companions when
// the last connection closes (confirmed by experiment during Step 5.4; see
// ValidateCandidate). So the live database file is never opened by SQLite
// here: StageCandidate applies the securefs safety checks to it, copies it
// (and any companions) with plain file I/O into a private temporary
// directory, and only that disposable copy is opened. The copy is retried if
// the source changed while it was being copied, so a concurrent writer yields
// "unavailable" rather than a false integrity failure.
//
// Limits, unchanged from VerifyAuditChain: an unkeyed chain cannot establish
// authenticity against anyone who can modify the database and recompute the
// chain, and cannot detect deletion of the chain's tail or replacement of the
// database by an older, internally valid copy.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// AuditVerifyStatus is the outcome class of VerifyAuditLog.
type AuditVerifyStatus string

const (
	// AuditVerified: the whole available chain passed VerifyAuditChain. An
	// empty chain (zero events) is verified, exactly as VerifyAuditChain
	// treats it.
	AuditVerified AuditVerifyStatus = "verified"
	// AuditIntegrityFailure: VerifyAuditChain found a broken link or a hash
	// that does not match its event.
	AuditIntegrityFailure AuditVerifyStatus = "integrity_failure"
	// AuditUnavailable: verification could not be completed (unreadable or
	// unsafe database, missing audit table, unsupported schema, ...). This is
	// NOT evidence of tampering.
	AuditUnavailable AuditVerifyStatus = "unavailable"
)

// AuditVerifyResult describes one verification run. Only fields that apply to
// the Status are set.
type AuditVerifyResult struct {
	Status         AuditVerifyStatus
	EventCount     int
	TipEventID     string // last event's id when Status is verified and the chain is non-empty
	FailureIndex   int    // valid when Status is AuditIntegrityFailure
	FailureEventID string
	FailureReason  string
	// Reason is a short, stable, sanitized explanation when Status is
	// AuditUnavailable: it never contains a filesystem path, a secret value,
	// key material or a remediation hint, so it is safe for machine output.
	Reason string
	// Err is the underlying diagnostic (it can contain paths); intended for
	// stderr diagnostics only, never for structured output.
	Err error
}

var errChangedWhileCopying = errors.New("the database kept changing while it was being copied for verification; retry when no process is writing to it")

const auditLogMinSchema = 3 // migration 0003 creates audit_events

type fileSig struct {
	exists bool
	size   int64
	mtime  int64
}

func sigOf(path string) fileSig {
	fi, err := os.Lstat(path)
	if err != nil {
		return fileSig{}
	}
	return fileSig{exists: true, size: fi.Size(), mtime: fi.ModTime().UnixNano()}
}

// stageStable stages path, retrying if its main file or WAL changed while the
// copy was made (a torn copy of a live database could look corrupt).
func stageStable(ctx context.Context, path string) (*StagedCandidate, error) {
	for attempt := 0; attempt < 3; attempt++ {
		beforeMain, beforeWAL := sigOf(path), sigOf(path+"-wal")
		staged, err := StageCandidate(ctx, path)
		if err != nil {
			return nil, err
		}
		if sigOf(path) == beforeMain && sigOf(path+"-wal") == beforeWAL {
			return staged, nil
		}
		staged.Close()
	}
	return nil, errChangedWhileCopying
}

// stageReason maps a staging failure to a stable, sanitized reason.
func stageReason(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, errChangedWhileCopying):
		return "the database kept changing while it was being copied; retry when no process is writing to it"
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return "verification was interrupted"
	case errors.Is(err, ErrStageSidecar):
		return "the database has an unsafe -wal/-shm companion file (it must be a regular file, not a symbolic link or special file); nothing was read from it"
	case errors.Is(err, ErrStageUnsafe):
		return "the database file or its directory is not safe to read (unexpected type, ownership or permissions); it was not opened"
	case errors.Is(err, ErrStageEmpty):
		return "the database file is empty"
	case errors.Is(err, ErrStageNotDatabase):
		return "the file is not a valid SQLite database"
	case errors.Is(err, ErrStageUnreadable):
		return "the database file could not be found or read"
	default:
		return "the database could not be safely read for verification"
	}
}

// VerifyAuditLog verifies the audit chain of the database at path without
// opening, migrating or otherwise modifying it. See the file comment.
func VerifyAuditLog(ctx context.Context, path string) AuditVerifyResult {
	unavailable := func(reason string, err error) AuditVerifyResult {
		return AuditVerifyResult{Status: AuditUnavailable, Reason: reason, Err: err}
	}

	staged, err := stageStable(ctx, path)
	if err != nil {
		return unavailable(stageReason(ctx, err), err)
	}
	defer staged.Close()

	db, err := OpenReadOnly(staged.Path)
	if err != nil {
		return unavailable("the database could not be opened for verification", err)
	}
	defer db.Close()

	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return unavailable("the database schema version could not be read", err)
	}
	latest, err := LatestSchemaVersion()
	if err != nil {
		return unavailable("this binary's supported schema version could not be determined", err)
	}
	if int(v.Int64) > latest {
		return unavailable(fmt.Sprintf("the database schema version (%d) is newer than this binary supports (%d)", v.Int64, latest),
			fmt.Errorf("database schema version %d is newer than %d", v.Int64, latest))
	}
	var hasTable int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_events'`).Scan(&hasTable); err != nil {
		return unavailable("the database could not be inspected", err)
	}
	if hasTable == 0 {
		return unavailable(fmt.Sprintf("the database has no audit log (schema version %d; the audit log needs version %d or later); nothing was created or migrated", v.Int64, auditLogMinSchema),
			errors.New("audit_events table not found"))
	}

	events, err := (ops{q: db}).ListAuditEvents(ctx)
	if err != nil {
		return unavailable("the audit events could not be read", err)
	}
	for i, ev := range events {
		if ev.SchemaVersion != supportedAuditEventSchemaVersion {
			return unavailable(fmt.Sprintf("an audit event (index %d) has audit schema version %d but this binary can only verify version %d "+
				"(it may come from a newer writer or may have been altered; verification was not attempted)",
				i, ev.SchemaVersion, supportedAuditEventSchemaVersion),
				fmt.Errorf("audit event %q has audit schema version %d", ev.ID, ev.SchemaVersion))
		}
	}

	if err := VerifyAuditChain(events); err != nil {
		var ce *ChainError
		if errors.As(err, &ce) {
			return AuditVerifyResult{Status: AuditIntegrityFailure, EventCount: len(events),
				FailureIndex: ce.Index, FailureEventID: ce.EventID, FailureReason: ce.Reason}
		}
		return unavailable("the audit chain could not be evaluated", err)
	}
	res := AuditVerifyResult{Status: AuditVerified, EventCount: len(events)}
	if n := len(events); n > 0 {
		res.TipEventID = events[n-1].ID
	}
	return res
}

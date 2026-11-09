package centralsvc

// Audit orchestration for `dso central backup`. Store.Backup only creates and
// verifies the file; recording that it happened is this layer's job, so the
// event is appended to the ACTIVE database after the backup exists and is
// never part of the backup it describes. Every append here is best-effort
// (A-2): a failure is logged at Error level and never changes the backup
// result.

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// recordAuditBestEffort appends ev (assigning it a fresh id) to st. A failure
// to generate the id or persist the event is logged at Error level and
// returned as ok=false; it never panics or returns an error to the caller.
// Only fixed fields are logged: never paths, keys or wrapped error text
// beyond the persistence error itself.
func recordAuditBestEffort(ctx context.Context, log *zap.Logger, appendFn func(context.Context, store.AuditEvent) (store.AuditEvent, error), ev store.AuditEvent) (string, bool) {
	if log == nil {
		log = zap.NewNop()
	}
	id, err := newID()
	if err != nil {
		log.Error("audit event id generation failed", zap.String("event_type", string(ev.EventType)), zap.Error(err))
		return "", false
	}
	ev.ID = id
	if _, err := appendFn(ctx, ev); err != nil {
		log.Error("audit event persistence failed; operation result is unaffected (availability-first policy)",
			zap.String("event_type", string(ev.EventType)), zap.String("outcome", string(ev.Outcome)),
			zap.String("reason_category", ev.ReasonCategory), zap.Error(err))
		return "", false
	}
	return id, true
}

// backupFailureReason maps a Store.Backup error to its fixed audit reason.
func backupFailureReason(err error) string {
	switch {
	case errors.Is(err, sqlite.ErrBackupDestinationExists):
		return store.AuditReasonDestinationExists
	case errors.Is(err, sqlite.ErrBackupDestinationInvalid):
		return store.AuditReasonDestinationInvalid
	case errors.Is(err, sqlite.ErrBackupVerificationFailed):
		return store.AuditReasonVerificationFailed
	default:
		return store.AuditReasonBackupCreationFailed
	}
}

// backupAuditEvent builds the audit event for a Store.Backup outcome. A
// *sqlite.BackupDirectorySyncError means the backup file itself is valid (see
// Backup), so it is a success with reason "sync_warning".
func backupAuditEvent(err error, actor string) store.AuditEvent {
	ev := store.AuditEvent{EventType: store.AuditBackup, ActorLabel: actor}
	var syncErr *sqlite.BackupDirectorySyncError
	switch {
	case err == nil:
		ev.Outcome = store.AuditSuccess
	case errors.As(err, &syncErr):
		ev.Outcome = store.AuditSuccess
		ev.ReasonCategory = store.AuditReasonSyncWarning
	default:
		ev.Outcome = store.AuditFailure
		ev.ReasonCategory = backupFailureReason(err)
	}
	return ev
}

// BackupWithAudit runs st.Backup and then records the outcome on st (the
// active database) best-effort. The returned path and error are exactly
// Backup's own, regardless of whether the audit append succeeds. log may be
// nil.
func BackupWithAudit(ctx context.Context, st *sqlite.Store, destPath, actor string, log *zap.Logger) (string, error) {
	path, err := st.Backup(ctx, destPath)
	recordAuditBestEffort(ctx, log, st.AppendAuditEvent, backupAuditEvent(err, actor))
	return path, err
}

package centralsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// eventsAt opens the database at path (nothing else may hold it) and returns
// its audit events in chain order.
func eventsAt(t *testing.T, path string) []store.AuditEvent {
	t.Helper()
	st, err := sqlite.Open(ctx, path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	evs, err := st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func countType(evs []store.AuditEvent, typ store.AuditEventType) int {
	n := 0
	for _, ev := range evs {
		if ev.EventType == typ {
			n++
		}
	}
	return n
}

func lastEvent(t *testing.T, evs []store.AuditEvent) store.AuditEvent {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("expected at least one audit event")
	}
	return evs[len(evs)-1]
}

func assertEvent(t *testing.T, ev store.AuditEvent, typ store.AuditEventType, outcome store.AuditOutcome, reason string) {
	t.Helper()
	if ev.EventType != typ || ev.Outcome != outcome || ev.ReasonCategory != reason {
		t.Fatalf("event = %s/%s/%q, want %s/%s/%q", ev.EventType, ev.Outcome, ev.ReasonCategory, typ, outcome, reason)
	}
}

// openReader holds a read transaction open on path so a later TRUNCATE
// checkpoint cannot complete (the same technique the existing checkpoint test
// uses).
type heldReader struct {
	db *sql.DB
	tx *sql.Tx
}

func (h *heldReader) Close() { _ = h.tx.Rollback(); _ = h.db.Close() }

func openReader(path string) (*heldReader, error) {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&n); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	return &heldReader{db: db, tx: tx}, nil
}

// ---- Backup ----

func TestBackupWithAudit_SuccessRecordsOneEventOnActiveDatabaseOnly(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	dest := filepath.Join(filepath.Dir(e.dbPath), "b.db")

	path, err := BackupWithAudit(ctx, e.st, dest, "tester", nil)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if countType(evs, store.AuditBackup) != 1 {
		t.Fatalf("expected exactly one backup event, got %d", countType(evs, store.AuditBackup))
	}
	last := lastEvent(t, evs)
	assertEvent(t, last, store.AuditBackup, store.AuditSuccess, "")
	if last.ActorLabel != "tester" {
		t.Fatalf("actor = %q", last.ActorLabel)
	}
	// The backup file must not contain an event describing its own creation.
	if got := countType(eventsAt(t, path), store.AuditBackup); got != 0 {
		t.Fatalf("the backup file must contain no backup event, found %d", got)
	}
}

func TestBackupAuditEvent_Mapping(t *testing.T) {
	syncErr := sqlite.NewBackupDirectorySyncErrorForTesting(errors.New("injected dir sync failure"))
	cases := []struct {
		name    string
		err     error
		outcome store.AuditOutcome
		reason  string
	}{
		{"success", nil, store.AuditSuccess, ""},
		{"sync_warning", syncErr, store.AuditSuccess, store.AuditReasonSyncWarning},
		{"destination_exists", fmt.Errorf("x: %w", sqlite.ErrBackupDestinationExists), store.AuditFailure, store.AuditReasonDestinationExists},
		{"destination_invalid", fmt.Errorf("x: %w", sqlite.ErrBackupDestinationInvalid), store.AuditFailure, store.AuditReasonDestinationInvalid},
		{"verification_failed", fmt.Errorf("x: %w", sqlite.ErrBackupVerificationFailed), store.AuditFailure, store.AuditReasonVerificationFailed},
		{"backup_creation_failed", errors.New("writing backup: boom /secret/path"), store.AuditFailure, store.AuditReasonBackupCreationFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := backupAuditEvent(c.err, "tester")
			assertEvent(t, ev, store.AuditBackup, c.outcome, c.reason)
			if ev.ActorLabel != "tester" {
				t.Fatalf("actor = %q", ev.ActorLabel)
			}
		})
	}
}

func TestBackupWithAudit_RealFailuresAreRecordedWithFixedReasons(t *testing.T) {
	e := setup(t)
	base := filepath.Dir(e.dbPath)
	existing := filepath.Join(base, "exists.db")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	notDir := filepath.Join(base, "afile")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, dest, reason string }{
		{"destination_exists", existing, store.AuditReasonDestinationExists},
		{"destination_invalid", filepath.Join(notDir, "b.db"), store.AuditReasonDestinationInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := BackupWithAudit(ctx, e.st, c.dest, "", nil); err == nil {
				t.Fatal("expected the backup to fail")
			}
			evs, _ := e.st.ListAuditEvents(ctx)
			assertEvent(t, lastEvent(t, evs), store.AuditBackup, store.AuditFailure, c.reason)
		})
	}
}

// TestBackupWithAudit_RealAuditPersistenceFailure uses a real SQLite trigger
// that rejects backup events, so the append genuinely fails at the
// persistence layer.
func TestBackupWithAudit_RealAuditPersistenceFailure(t *testing.T) {
	e := setup(t)
	if _, err := e.rawDB(t).Exec(`CREATE TRIGGER reject_backup_events BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'backup' BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(filepath.Dir(e.dbPath), "b.db")
	core, logs := observer.New(zap.InfoLevel)

	path, err := BackupWithAudit(ctx, e.st, dest, "tester", zap.New(core))
	if err != nil || path == "" {
		t.Fatalf("an audit-persistence failure must not change a successful backup: %q, %v", path, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("backup file must exist: %v", statErr)
	}
	entries := logs.FilterLevelExact(zap.ErrorLevel).All()
	if len(entries) != 1 {
		t.Fatalf("expected exactly one Error-level log entry, got %d", len(entries))
	}
	for k, v := range entries[0].ContextMap() {
		if k != "error" && strings.Contains(fmt.Sprint(v), dest) {
			t.Fatalf("log field %s leaks the destination path", k)
		}
	}

	// A failed backup keeps its own error when its audit append also fails.
	_, err = BackupWithAudit(ctx, e.st, dest, "", zap.New(core))
	if !errors.Is(err, sqlite.ErrBackupDestinationExists) {
		t.Fatalf("original backup error must be preserved, got %v", err)
	}
}

func TestRestore_SafetySnapshotEmitsNoBackupEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	result, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetySnapshot == "" {
		t.Fatal("expected a safety snapshot")
	}
	if n := countType(eventsAt(t, e.dbPath), store.AuditBackup); n != 0 {
		t.Fatalf("restored database must contain no backup event, found %d", n)
	}
	if n := countType(eventsAt(t, result.SafetySnapshot), store.AuditBackup); n != 0 {
		t.Fatalf("safety snapshot must contain no backup event, found %d", n)
	}
}

// ---- Restore ----

// countingAppend returns a testAppendRestoreEvent that delegates to the real
// append and records every event it is asked to write.
func countingAppend(got *[]store.AuditEvent) func(context.Context, *sqlite.Store, store.AuditEvent) (store.AuditEvent, error) {
	return func(ctx context.Context, st *sqlite.Store, ev store.AuditEvent) (store.AuditEvent, error) {
		*got = append(*got, ev)
		return st.AppendAuditEvent(ctx, ev)
	}
}

func TestRestore_SuccessEventAppendFailureIsBestEffort(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	core, logs := observer.New(zap.InfoLevel)
	result, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, Logger: zap.New(core),
		Confirm: func(sqlite.CandidateInfo) bool { return true },
		testAppendRestoreEvent: func(context.Context, *sqlite.Store, store.AuditEvent) (store.AuditEvent, error) {
			return store.AuditEvent{}, errors.New("injected: audit sink unavailable")
		},
	})
	if err != nil {
		t.Fatalf("a successful restore must stay successful when its audit event cannot be appended: %v", err)
	}
	if result.RestoreEventID != "" {
		t.Fatal("RestoreEventID must be empty when no event was recorded")
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 1 {
		t.Fatalf("expected one Error-level log entry, got %d", logs.FilterLevelExact(zap.ErrorLevel).Len())
	}
	if n := countType(eventsAt(t, e.dbPath), store.AuditRestore); n != 0 {
		t.Fatalf("no restore event should exist, found %d", n)
	}
}

func TestRestore_SuccessRecordsRestoreSuccessEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	result, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, Actor: "tester",
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	evs := eventsAt(t, e.dbPath)
	last := lastEvent(t, evs)
	assertEvent(t, last, store.AuditRestore, store.AuditSuccess, "")
	if last.ID != result.RestoreEventID || last.ActorLabel != "tester" {
		t.Fatalf("event id/actor mismatch: %+v vs %q", last, result.RestoreEventID)
	}
}

func TestRestore_SnapshotFailureRecordsEventWhileActiveOpen(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injected := errors.New("injected: snapshot failed")
	var got []store.AuditEvent
	_, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
		testSafetySnapshot: func(context.Context, *sqlite.Store, string) (string, error) {
			return "", injected
		},
		testAppendRestoreEvent: countingAppend(&got),
	})
	if !errors.Is(err, injected) {
		t.Fatalf("original error must be preserved, got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one append, got %d", len(got))
	}
	assertEvent(t, lastEvent(t, eventsAt(t, e.dbPath)), store.AuditRestore, store.AuditFailure, store.AuditReasonSnapshotFailed)
}

func TestRestore_CheckpointFailureRecordsEventWhileActiveOpen(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	reader, err := openReader(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := e.svc.CreateEnvironment(ctx, "proj", "staging", "staging"); err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	var got []store.AuditEvent
	_, err = restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm:                 func(sqlite.CandidateInfo) bool { return true },
		testReplaceDatabaseFile: func(string, string) error { t.Fatal("replacement must not run"); return nil },
		testAppendRestoreEvent:  countingAppend(&got),
	})
	if !errors.Is(err, sqlite.ErrCheckpointIncomplete) {
		t.Fatalf("expected ErrCheckpointIncomplete, got %v", err)
	}
	if len(got) != 1 || got[0].ReasonCategory != store.AuditReasonCheckpointFailed {
		t.Fatalf("expected one checkpoint_failed append, got %+v", got)
	}
	reader.Close()
	assertEvent(t, lastEvent(t, eventsAt(t, e.dbPath)), store.AuditRestore, store.AuditFailure, store.AuditReasonCheckpointFailed)
}

func TestRestore_ReplaceFailureRecordsEventAfterReopeningOriginal(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injected := errors.New("injected: replacement failed")
	var got []store.AuditEvent
	_, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm:                 func(sqlite.CandidateInfo) bool { return true },
		testReplaceDatabaseFile: func(string, string) error { return injected },
		testAppendRestoreEvent:  countingAppend(&got),
	})
	if !errors.Is(err, injected) {
		t.Fatalf("original error must be preserved, got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one append, got %d", len(got))
	}
	assertEvent(t, lastEvent(t, eventsAt(t, e.dbPath)), store.AuditRestore, store.AuditFailure, store.AuditReasonDatabaseReplaceFailed)
}

func TestRestore_ReplaceFailureWithUnreopenableOriginalWritesNothing(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injected := errors.New("injected: replacement failed")
	var got []store.AuditEvent
	_, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm:                 func(sqlite.CandidateInfo) bool { return true },
		testReplaceDatabaseFile: func(string, string) error { return injected },
		testReopenOriginal: func(context.Context, string) (*sqlite.Store, error) {
			return nil, errors.New("injected: reopen failed")
		},
		testAppendRestoreEvent: countingAppend(&got),
	})
	if !errors.Is(err, injected) {
		t.Fatalf("original error must be preserved, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("no event may be written when the original cannot be reopened, got %+v", got)
	}
	if n := countType(eventsAt(t, e.dbPath), store.AuditRestore); n != 0 {
		t.Fatalf("found %d restore events", n)
	}
}

func TestRestore_ReplaceFailureAuditAppendFailureKeepsOriginalError(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injected := errors.New("injected: replacement failed")
	core, logs := observer.New(zap.InfoLevel)
	_, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, Logger: zap.New(core),
		Confirm:                 func(sqlite.CandidateInfo) bool { return true },
		testReplaceDatabaseFile: func(string, string) error { return injected },
		testAppendRestoreEvent: func(context.Context, *sqlite.Store, store.AuditEvent) (store.AuditEvent, error) {
			return store.AuditEvent{}, errors.New("injected: audit failed")
		},
	})
	if !errors.Is(err, injected) || strings.Contains(err.Error(), "audit failed") {
		t.Fatalf("the original restore error must be returned unchanged, got %v", err)
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 1 {
		t.Fatalf("expected one Error-level entry, got %d", logs.FilterLevelExact(zap.ErrorLevel).Len())
	}
}

func TestRestore_ReopenFailureAfterReplacementWritesNoEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	var got []store.AuditEvent
	_, err := restoreT(t, e, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
		testReopen: func(context.Context, string) (*sqlite.Store, error) {
			return nil, errors.New("injected: reopen failed")
		},
		testAppendRestoreEvent: countingAppend(&got),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(got) != 0 {
		t.Fatalf("no event may be written after a failed reopen, got %+v", got)
	}
	candBytes, _ := os.ReadFile(cand)
	dbBytes, _ := os.ReadFile(e.dbPath)
	if string(candBytes) != string(dbBytes) {
		t.Fatal("the replaced database must be untouched by any audit write")
	}
}

func TestRestore_UnauditedPathsWriteNothing(t *testing.T) {
	// Each case must complete without any audit append and without changing
	// the active database's audit chain.
	type tc struct {
		name string
		run  func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error
	}
	cases := []tc{
		{"dry_run", func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error {
			kp, _ := crypto.NewFileKeyProvider(e.keyPath)
			_, err := restoreT(t, e, RestoreOptions{CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, DryRun: true,
				testAppendRestoreEvent: countingAppend(got)})
			return err
		}},
		{"confirmation_declined", func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error {
			kp, _ := crypto.NewFileKeyProvider(e.keyPath)
			_, err := restoreT(t, e, RestoreOptions{CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
				Confirm:                func(sqlite.CandidateInfo) bool { return false },
				testAppendRestoreEvent: countingAppend(got)})
			if !errors.Is(err, ErrRestoreNotConfirmed) {
				t.Fatalf("expected ErrRestoreNotConfirmed, got %v", err)
			}
			return err
		}},
		{"wrong_key", func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error {
			otherPath := filepath.Join(securefstest.TempDir(t), "other.key")
			if _, err := crypto.InstallKeyFile(otherPath); err != nil {
				t.Fatal(err)
			}
			kp, _ := crypto.NewFileKeyProvider(otherPath)
			_, err := restoreT(t, e, RestoreOptions{CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
				Confirm:                func(sqlite.CandidateInfo) bool { return true },
				testAppendRestoreEvent: countingAppend(got)})
			if !errors.Is(err, ErrWrongRootKey) {
				t.Fatalf("expected ErrWrongRootKey, got %v", err)
			}
			return err
		}},
		{"invalid_candidate", func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error {
			bad := filepath.Join(filepath.Dir(e.dbPath), "bad.db")
			if err := os.WriteFile(bad, []byte("not a database"), 0o600); err != nil {
				t.Fatal(err)
			}
			kp, _ := crypto.NewFileKeyProvider(e.keyPath)
			_, err := restoreT(t, e, RestoreOptions{CandidatePath: bad, DBPath: e.dbPath, KeyProvider: kp,
				Confirm:                func(sqlite.CandidateInfo) bool { return true },
				testAppendRestoreEvent: countingAppend(got)})
			return err
		}},
		{"lock_busy", func(t *testing.T, e *env, cand string, got *[]store.AuditEvent) error {
			if e.st != nil {
				e.st.Close()
				e.st = nil
			}
			held, err := sqlite.AcquireExclusiveCentralLock(e.dbPath, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			kp, _ := crypto.NewFileKeyProvider(e.keyPath)
			_, err = Restore(ctx, RestoreOptions{CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
				LockTimeout:            100 * time.Millisecond,
				Confirm:                func(sqlite.CandidateInfo) bool { return true },
				testAppendRestoreEvent: countingAppend(got)})
			if !errors.Is(err, sqlite.ErrCentralLockBusy) {
				t.Fatalf("expected ErrCentralLockBusy, got %v", err)
			}
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := setup(t)
			e.project(t, "proj", "prod")
			cand := backupActive(t, e, "cand.db")
			var got []store.AuditEvent
			_ = c.run(t, e, cand, &got)
			if len(got) != 0 {
				t.Fatalf("no audit append expected, got %+v", got)
			}
			evs := eventsAt(t, e.dbPath)
			if countType(evs, store.AuditRestore) != 0 || countType(evs, store.AuditBackup) != 0 {
				t.Fatalf("no backup/restore events expected, got %+v", evs)
			}
		})
	}
}

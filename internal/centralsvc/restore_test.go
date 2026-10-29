package centralsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/crypto"
	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
	"github.com/docker-secret-operator/dso/internal/store"
	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

// backupActive creates a backup of e's active database and returns its path.
func backupActive(t *testing.T, e *env, name string) string {
	t.Helper()
	dst := filepath.Join(filepath.Dir(e.dbPath), name)
	path, err := e.st.Backup(ctx, dst)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateCandidateKey_WrongKeyRejectedBeforeActiveDatabaseModified(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	otherKeyPath := filepath.Join(securefstest.TempDir(t), "other.key")
	if _, err := crypto.InstallKeyFile(otherKeyPath); err != nil {
		t.Fatal(err)
	}
	otherKP, err := crypto.NewFileKeyProvider(otherKeyPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateCandidateKey(ctx, cand, otherKP); !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("expected ErrWrongRootKey, got %v", err)
	}

	// The candidate itself must be untouched by a failed key check.
	if _, err := sqlite.ValidateCandidate(ctx, cand, 99); err != nil {
		t.Fatalf("candidate should still be structurally valid after a failed key check: %v", err)
	}
}

func TestValidateCandidateKey_CorrectKeyAccepted(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	kp, err := crypto.NewFileKeyProvider(e.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCandidateKey(ctx, cand, kp); err != nil {
		t.Fatalf("expected the correct root key to validate, got %v", err)
	}
}

func TestRestore_CandidateAndActivePathsCannotBeConfused(t *testing.T) {
	e := setup(t)
	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	_, err := Restore(ctx, RestoreOptions{CandidatePath: e.dbPath, DBPath: e.dbPath, KeyProvider: kp})
	if err == nil {
		t.Fatal("expected Restore to reject a candidate path identical to the active database path")
	}
}

func TestRestore_DryRunDoesNotMutateActiveDatabase(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	before, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	confirmCalled := false
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, DryRun: true,
		Confirm: func(sqlite.CandidateInfo) bool { confirmCalled = true; return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("expected result.DryRun to be true")
	}
	if result.SafetySnapshot != "" {
		t.Fatal("dry run must not create a safety snapshot")
	}
	if result.RestoreEventID != "" {
		t.Fatal("dry run must not append a restore event")
	}
	if confirmCalled {
		t.Fatal("dry run must never reach the confirmation callback")
	}

	after, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("dry run must not modify the active database's bytes at all")
	}
}

func TestRestore_SuccessfulRoundTripAppendsLinkedRestoreEvent(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	// Add more activity after the backup, so the restore is observably
	// replacing a database with MORE recent events than the candidate had.
	if _, err := e.svc.CreateEnvironment(ctx, "proj", "staging", "staging"); err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp, Actor: "test-actor",
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.SafetySnapshot == "" {
		t.Fatal("expected a safety snapshot to have been created")
	}
	if result.RestoreEventID == "" {
		t.Fatal("expected a restore event id")
	}

	e.reopen(t) // the on-disk file changed under the previously-open Store
	events, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least the restore event")
	}
	last := events[len(events)-1]
	if last.ID != result.RestoreEventID || last.EventType != store.AuditRestore {
		t.Fatalf("expected the chain's last event to be the restore event, got %+v", last)
	}
	if last.ActorLabel != "test-actor" {
		t.Fatalf("expected the restore event's ActorLabel to be the caller-supplied actor, got %q", last.ActorLabel)
	}
	if err := sqlite.VerifyAuditChain(events); err != nil {
		t.Fatalf("restored chain plus its new restore event must verify cleanly: %v", err)
	}

	// The restore replaced the environments: "staging" (added after the
	// backup) must be gone, confirming the restore actually took effect.
	envs, err := e.svc.ListEnvironments(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range envs {
		if en.Slug == "staging" {
			t.Fatal("expected the restored database to NOT contain environments added after the backup")
		}
	}
}

func TestRestore_OlderValidBackupPassesChainVerification_DocumentedLimitation(t *testing.T) {
	// This test actively proves, rather than merely asserting in prose, the
	// documented limitation that restoring an older, internally valid
	// backup over a database with more recent legitimate activity is NOT
	// detected: the restored chain verifies cleanly even though newer events
	// (added after the backup) are now gone.
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "older.db")
	beforeEvents, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.CreateEnvironment(ctx, "proj", "staging", "staging"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateEnvironment(ctx, "proj", "qa", "qa"); err != nil {
		t.Fatal(err)
	}
	afterEvents, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterEvents) <= len(beforeEvents) {
		t.Fatal("expected additional events after the backup, to make the limitation observable")
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatalf("restoring an older, internally valid backup must succeed (that is the limitation): %v", err)
	}
	if result.RestoreEventID == "" {
		t.Fatal("expected the restore to complete and append its event")
	}

	e.reopen(t)
	restoredEvents, err := e.st.ListAuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.VerifyAuditChain(restoredEvents); err != nil {
		t.Fatalf("the restored (older) chain plus its restore event must still verify cleanly -- this IS the documented limitation: %v", err)
	}
	// The two environments created after the backup are gone, and nothing
	// about chain verification flagged that -- exactly the documented gap.
	envs, err := e.svc.ListEnvironments(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Slug != "prod" {
		t.Fatalf("expected only the pre-backup environment to remain, got %+v", envs)
	}
}

func TestRestore_ConfirmationDeclinedLeavesActiveDatabaseUntouched(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	before, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	_, err = Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return false },
	})
	if !errors.Is(err, ErrRestoreNotConfirmed) {
		t.Fatalf("expected ErrRestoreNotConfirmed, got %v", err)
	}
	after, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a declined confirmation must leave the active database completely untouched")
	}
}

func TestRestore_SourceBackupRemainsUnchanged(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	before, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	if _, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the source backup must never be modified by a restore")
	}
}

func TestRestore_StaleWALAndSHMDoNotCorruptRestoredDatabase(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	// The active store's own -wal/-shm files, from writes not yet
	// checkpointed, are real, well-formed WAL/SHM content -- the realistic
	// "stale companion from the previous database generation" case this
	// test exercises, as opposed to arbitrary corrupt bytes (which would
	// crash SQLite's WAL recovery regardless of restore, an unrelated,
	// pre-existing driver robustness question, not what this test is for).
	walBefore, err := os.ReadFile(e.dbPath + "-wal")
	if err != nil {
		t.Fatalf("expected the active store to have a real, non-empty -wal file before restore: %v", err)
	}
	if len(walBefore) == 0 {
		t.Fatal("expected the pre-restore -wal file to contain real frames")
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	if _, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(e.dbPath + "-wal"); !os.IsNotExist(err) {
		t.Fatal("expected the stale -wal file to be removed by restore")
	}
	if _, err := os.Stat(e.dbPath + "-shm"); !os.IsNotExist(err) {
		t.Fatal("expected the stale -shm file to be removed by restore")
	}

	// And the restored database must still open and work correctly.
	restored, err := sqlite.Open(ctx, e.dbPath, sqlite.Options{})
	if err != nil {
		t.Fatalf("restored database with stale WAL/SHM present did not reopen cleanly: %v", err)
	}
	restored.Close()
}

// TestRestore_UnsafeDestinationDirectoryRejectedWithNoActiveDatabase is a
// regression test for a confirmed defect found during the Step 5.4 audit
// (F2): when no active database exists yet, nothing in the restore flow
// previously checked the destination directory's safety before writing into
// it -- the check only happened incidentally, via sqlite.Open, when an
// active database already existed. This exercises exactly the gap: no
// active database, and a destination directory that fails the same
// securefs discipline the live database directory already requires.
func TestRestore_UnsafeDestinationDirectoryRejectedWithNoActiveDatabase(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	e.st.Close()
	if err := os.Remove(e.dbPath); err != nil {
		t.Fatal(err)
	}

	unsafeDir := filepath.Join(filepath.Dir(e.dbPath), "unsafe")
	if err := os.Mkdir(unsafeDir, 0o777); err != nil { // #nosec G301 -- deliberately unsafe for this test
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeDir, 0o777); err != nil { // bypass umask, see internal/store/sqlite's identical test
		t.Fatal(err)
	}
	unsafeDBPath := filepath.Join(unsafeDir, "central.db")

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	_, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: unsafeDBPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err == nil {
		t.Fatal("expected Restore to reject an unsafe destination directory even with no active database present")
	}
	if _, statErr := os.Stat(unsafeDBPath); !os.IsNotExist(statErr) {
		t.Fatal("expected no database file to be created in the unsafe directory")
	}
}

func TestRestore_NoActiveDatabaseYetSkipsSnapshotAndStillWorks(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	e.st.Close()
	if err := os.Remove(e.dbPath); err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err != nil {
		t.Fatalf("restoring onto a missing active database should still succeed: %v", err)
	}
	if result.SafetySnapshot != "" {
		t.Fatal("expected no safety snapshot when there was no active database to protect")
	}
	if result.RestoreEventID == "" {
		t.Fatal("expected a restore event")
	}
}

func TestRestore_ContextCancellationDuringValidation(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	before, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = Restore(cctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
	})
	if err == nil {
		t.Fatal("expected restore to fail with a cancelled context")
	}
	after, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a context-cancelled restore attempt must leave the active database untouched")
	}
}

// TestRestore_AuditEventAppendFailureAfterActivation_ReportsUsableButUnrecorded
// is a regression test for audit finding F5: it forces AppendAuditEvent to
// fail AFTER the candidate has already been activated (replaced and
// reopened), using the testAppendRestoreEvent fault-injection seam, and
// asserts the exact required outcome: a non-nil error whose text says the
// database is restored and usable but the event could not be recorded, that
// the active database actually IS usable and contains the restored data,
// that this is not misrepresented as a fully successful restore (no
// RestoreEventID), and that the safety snapshot path is still reported.
func TestRestore_AuditEventAppendFailureAfterActivation_ReportsUsableButUnrecorded(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injectedErr := errors.New("injected: audit sink unavailable")
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm: func(sqlite.CandidateInfo) bool { return true },
		testAppendRestoreEvent: func(context.Context, *sqlite.Store, store.AuditEvent) (store.AuditEvent, error) {
			return store.AuditEvent{}, injectedErr
		},
	})

	if err == nil {
		t.Fatal("expected a non-nil error when the restore audit event cannot be recorded")
	}
	if !errors.Is(err, injectedErr) {
		t.Fatalf("expected the error to wrap the injected failure, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "restore completed") || !strings.Contains(msg, "usable") || !strings.Contains(msg, "could not be recorded") {
		t.Fatalf("expected the error to clearly state the database is restored/usable but the event was not recorded, got: %q", msg)
	}
	if result.RestoreEventID != "" {
		t.Fatal("must not report a RestoreEventID when the append failed -- that would misrepresent this as a fully successful restore")
	}
	if result.SafetySnapshot == "" {
		t.Fatal("expected the safety snapshot path to still be reported")
	}
	if _, statErr := os.Stat(result.SafetySnapshot); statErr != nil {
		t.Fatalf("expected the reported safety snapshot to actually exist: %v", statErr)
	}

	// The active database must genuinely be usable and contain the
	// restored (candidate's) data -- not left in some broken intermediate
	// state -- even though the append failed.
	restored, openErr := sqlite.Open(ctx, e.dbPath, sqlite.Options{})
	if openErr != nil {
		t.Fatalf("expected the active database to still be usable after the injected append failure: %v", openErr)
	}
	defer restored.Close()
	if _, getErr := restored.GetProjectBySlug(ctx, "proj"); getErr != nil {
		t.Fatalf("expected the restored project to be present and readable: %v", getErr)
	}
}

// TestRestore_ReplacementFailureBeforeActivation_LeavesActiveDatabaseUsable
// is a regression test for audit finding F6: it forces replaceDatabaseFile
// to fail via the testReplaceDatabaseFile seam (rather than relying on
// OS-level permission behavior, which differs as root or in some CI
// sandboxes) and asserts the active database is never touched, the
// candidate is never touched, the safety snapshot exists and is usable, and
// the returned error names the snapshot's recovery path exactly as
// promised.
func TestRestore_ReplacementFailureBeforeActivation_LeavesActiveDatabaseUsable(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	candBefore, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injectedErr := errors.New("injected: replacement failed")
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm:                 func(sqlite.CandidateInfo) bool { return true },
		testReplaceDatabaseFile: func(string, string) error { return injectedErr },
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("expected the error to wrap the injected failure, got %v", err)
	}
	if result.SafetySnapshot == "" {
		t.Fatal("expected the safety snapshot path to be reported")
	}
	if !strings.Contains(err.Error(), result.SafetySnapshot) {
		t.Fatalf("expected the error to name the exact recovery snapshot path %q, got: %q", result.SafetySnapshot, err.Error())
	}

	// Note: the active database's PHYSICAL bytes legitimately change here
	// (the mandatory pre-replacement WAL checkpoint folds pending frames
	// into the main file), so this asserts logical, not byte-for-byte,
	// preservation -- replaceDatabaseFile was never called with real
	// effect (it's mocked), so the file itself was never replaced.
	candAfter, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}
	if string(candBefore) != string(candAfter) {
		t.Fatal("the candidate must be untouched even though replacement failed")
	}

	// The active database, having never been touched, must still open and
	// work normally.
	active, openErr := sqlite.Open(ctx, e.dbPath, sqlite.Options{})
	if openErr != nil {
		t.Fatalf("expected the untouched active database to still open cleanly: %v", openErr)
	}
	defer active.Close()
	if _, getErr := active.GetProjectBySlug(ctx, "proj"); getErr != nil {
		t.Fatalf("expected the original project to still be present: %v", getErr)
	}

	// The reported safety snapshot must itself be a real, usable database.
	snap, openErr := sqlite.Open(ctx, result.SafetySnapshot, sqlite.Options{})
	if openErr != nil {
		t.Fatalf("expected the reported safety snapshot to open cleanly as a recovery artifact: %v", openErr)
	}
	defer snap.Close()
	if _, getErr := snap.GetProjectBySlug(ctx, "proj"); getErr != nil {
		t.Fatalf("expected the safety snapshot to contain the pre-restore data: %v", getErr)
	}
}

// TestRestore_ReopenFailureAfterReplacement_DoesNotPromiseAutomaticRecovery
// is a regression test for audit finding F6's second scenario: replacement
// succeeds but reopening the newly-replaced file fails. This uses the
// testReopen seam (the real replaceDatabaseFile runs, genuinely replacing
// the file) to force that specific failure, and asserts the error names the
// recovery snapshot but does NOT claim the active database was
// automatically recovered -- because the code does not perform automatic
// recovery, only reports where to find the snapshot.
func TestRestore_ReopenFailureAfterReplacement_DoesNotPromiseAutomaticRecovery(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")

	kp, _ := crypto.NewFileKeyProvider(e.keyPath)
	injectedErr := errors.New("injected: reopen failed")
	result, err := Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: kp,
		Confirm:    func(sqlite.CandidateInfo) bool { return true },
		testReopen: func(context.Context, string) (*sqlite.Store, error) { return nil, injectedErr },
	})
	if !errors.Is(err, injectedErr) {
		t.Fatalf("expected the error to wrap the injected failure, got %v", err)
	}
	if result.SafetySnapshot == "" {
		t.Fatal("expected the safety snapshot path to be reported")
	}
	msg := err.Error()
	if !strings.Contains(msg, result.SafetySnapshot) {
		t.Fatalf("expected the error to name the recovery snapshot path, got: %q", msg)
	}
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "not automatically recovered") {
		t.Fatalf("expected the error to explicitly disclaim automatic recovery, got: %q", msg)
	}
	if result.RestoreEventID != "" {
		t.Fatal("must not report a RestoreEventID when reopen failed")
	}

	// The file at dbPath really was replaced (the real replaceDatabaseFile
	// ran, unmocked) -- this test's job is only to verify the reopen
	// failure's reporting, not to pretend replacement itself didn't happen.
	candBytes, err := os.ReadFile(cand)
	if err != nil {
		t.Fatal(err)
	}
	dbBytes, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(candBytes) != string(dbBytes) {
		t.Fatal("expected the real replacement to have actually run before the injected reopen failure")
	}

	// Recovery is available (the snapshot exists and is usable) but is NOT
	// automatic: the active path still holds the replaced (candidate's)
	// content, not the pre-restore one, until an operator acts on the
	// reported recovery instructions.
	snap, openErr := sqlite.Open(ctx, result.SafetySnapshot, sqlite.Options{})
	if openErr != nil {
		t.Fatalf("expected the safety snapshot to be usable for manual recovery: %v", openErr)
	}
	snap.Close()
}

func TestRestore_FailedKeyValidationLeavesActiveDatabaseUntouched(t *testing.T) {
	e := setup(t)
	e.project(t, "proj", "prod")
	cand := backupActive(t, e, "cand.db")
	before, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}

	otherKeyPath := filepath.Join(securefstest.TempDir(t), "other.key")
	if _, err := crypto.InstallKeyFile(otherKeyPath); err != nil {
		t.Fatal(err)
	}
	otherKP, err := crypto.NewFileKeyProvider(otherKeyPath)
	if err != nil {
		t.Fatal(err)
	}

	confirmReached := false
	_, err = Restore(ctx, RestoreOptions{
		CandidatePath: cand, DBPath: e.dbPath, KeyProvider: otherKP,
		Confirm: func(sqlite.CandidateInfo) bool { confirmReached = true; return false },
	})
	if !errors.Is(err, ErrWrongRootKey) {
		t.Fatalf("expected ErrWrongRootKey, got %v", err)
	}
	if confirmReached {
		t.Fatal("must not reach confirmation after failed key validation")
	}
	after, err := os.ReadFile(e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a failed key validation must leave the active database untouched")
	}
}

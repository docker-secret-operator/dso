package centralsvc

// Phase D: tests for the thin orchestration layer around the recovery
// scanner. Every fixture is synthetic, under a fresh t.TempDir().

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

func TestScanActiveRecoveryArtifacts_LockSucceeds_ReportsLockAcquired(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "c.db")

	res, err := ScanActiveRecoveryArtifacts(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !res.LockAcquired {
		t.Fatalf("expected the shared lock to be acquired, note: %s", res.LockNote)
	}
	if res.LockNote == "" {
		t.Fatal("expected a non-empty lock note even on success")
	}
}

// mkAsideFixture creates a displacement aside directory beside db holding a
// "main" file -- a shape that, if scanned and classified, would normally
// reach ConfidenceFact (when a file also exists at the active path) or
// ConfidenceManual with MainDatabaseUnsafe (when it does not). Either way,
// a correct H-2 fix must never let that classification happen at all when
// the lock could not be acquired.
func mkAsideFixture(t *testing.T, db string) {
	t.Helper()
	if err := os.WriteFile(db, []byte("main-db-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	aside := db + ".replaced-20260101T000000Z-aaaa"
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aside, "main"), []byte("retained-original"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestScanActiveRecoveryArtifacts_ConcurrentExclusiveRestore_NeverScansOrClassifies
// is the H-2 regression test: unlike the superseded version of this test
// (which used an empty directory and so could not have caught the original
// defect), this one plants a real artifact that WOULD have produced
// ConfidenceFact under the pre-fix behavior, and proves three things: the
// lock failure is reported, the scan function is never invoked at all (via
// the package-private scanRecoveryArtifactsFn seam), and no artifact is
// returned.
func TestScanActiveRecoveryArtifacts_ConcurrentExclusiveRestore_NeverScansOrClassifies(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "c.db")
	mkAsideFixture(t, db)

	excl, err := sqlite.AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excl.Close()

	scanCalls := 0
	orig := scanRecoveryArtifactsFn
	scanRecoveryArtifactsFn = func(dbPath string) (sqlite.ScanResult, error) {
		scanCalls++
		return orig(dbPath)
	}
	defer func() { scanRecoveryArtifactsFn = orig }()

	res, err := ScanActiveRecoveryArtifacts(db, 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res.LockAcquired {
		t.Fatal("expected the lock acquisition to fail while an exclusive Restore holds it")
	}
	if res.LockNote == "" {
		t.Fatal("a failed lock acquisition must still produce an explicit note, never a silent unlocked scan")
	}
	if res.Scanned {
		t.Fatal("Scanned must be false when the lock could not be acquired")
	}
	if scanCalls != 0 {
		t.Fatalf("the scan function must never be called when the lock could not be acquired, got %d calls", scanCalls)
	}
	if res.Artifacts != nil {
		t.Fatalf("Artifacts must be nil when the lock could not be acquired, got %+v", res.Artifacts)
	}
	for _, a := range res.Artifacts {
		if a.Confidence == sqlite.ConfidenceFact {
			t.Fatalf("no artifact may ever reach ConfidenceFact from an unlocked attempt: %+v", a)
		}
	}
}

// TestScanActiveRecoveryArtifacts_LockSucceeds_ScansAndClassifiesNormally
// proves the success path still works exactly as before: the real artifact
// planted by mkAsideFixture, with a file present at the active path, is
// classified at ConfidenceFact.
func TestScanActiveRecoveryArtifacts_LockSucceeds_ScansAndClassifiesNormally(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "c.db")
	mkAsideFixture(t, db)

	res, err := ScanActiveRecoveryArtifacts(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !res.LockAcquired || !res.Scanned {
		t.Fatalf("expected a successful, scanned result: %+v", res)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("expected exactly one artifact, got %+v", res.Artifacts)
	}
	if res.Artifacts[0].Confidence != sqlite.ConfidenceFact {
		t.Fatalf("expected ConfidenceFact for a completed-install shape, got %s", res.Artifacts[0].Confidence)
	}
}

// TestScanActiveRecoveryArtifacts_LockReleased_ThenSucceeds proves the
// exclusive holder's release frees the shared lock for a subsequent call,
// which then scans and classifies normally.
func TestScanActiveRecoveryArtifacts_LockReleased_ThenSucceeds(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "c.db")
	mkAsideFixture(t, db)

	excl, err := sqlite.AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := ScanActiveRecoveryArtifacts(db, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Scanned {
		t.Fatal("expected Scanned to be false while the exclusive lock is held")
	}
	if err := excl.Close(); err != nil {
		t.Fatal(err)
	}

	res, err := ScanActiveRecoveryArtifacts(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !res.LockAcquired || !res.Scanned {
		t.Fatalf("expected a successful, scanned result once the exclusive holder released: %+v", res)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("expected exactly one artifact after the lock freed up, got %+v", res.Artifacts)
	}
}

package cli

// Phase D: tests for `dso central recovery-status`. Reuses the existing
// newCentralEnv/e.run CLI test harness (central_test.go); every fixture is
// synthetic, under a fresh securefstest.TempDir().

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/store/sqlite"
)

func TestCentralRecoveryStatus_NoArtifacts_ReportsNoneFound(t *testing.T) {
	e := newCentralEnv(t)
	// The database FILE need not exist (a crash can leave it missing
	// entirely), but its parent directory must, so the shared central lock
	// -- which this command now requires before scanning at all, per the
	// H-2 fix -- can resolve its sidecar path in this single-process,
	// no-contention test.
	if err := os.MkdirAll(e.dir+"/data", 0o700); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run(t, "", "recovery-status", "--db", e.db)
	if err != nil {
		t.Fatalf("recovery-status must not require an existing database: %v", err)
	}
	if !strings.Contains(out, "No recovery artifacts were found") {
		t.Fatalf("expected a clear 'none found' statement, got:\n%s", out)
	}
	if !strings.Contains(out, "Shared central lock acquired:") {
		t.Fatalf("expected the lock status to be reported, got:\n%s", out)
	}
	if !strings.Contains(out, "does not perform recovery or cleanup") {
		t.Fatalf("expected the no-recovery/no-cleanup disclosure, got:\n%s", out)
	}
}

func TestCentralRecoveryStatus_RequiresDBFlag(t *testing.T) {
	e := newCentralEnv(t)
	_, _, err := e.run(t, "", "recovery-status")
	if err == nil {
		t.Fatal("expected --db to be required")
	}
}

func TestCentralRecoveryStatus_ReportsAsideDirectoryAndUnsafeState(t *testing.T) {
	e := newCentralEnv(t)
	if err := os.MkdirAll(e.dir+"/data", 0o700); err != nil {
		t.Fatal(err)
	}
	aside := e.db + ".replaced-20260101T000000Z-aaaa"
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aside+"/main", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// e.db itself does not exist: matches the "retained main, no active file" shape.

	out, _, err := e.run(t, "", "recovery-status", "--db", e.db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "manual-intervention") {
		t.Fatalf("expected a manual-intervention classification, got:\n%s", out)
	}
	if !strings.Contains(out, "must NOT be treated as safe to open") {
		t.Fatalf("expected the unsafe-to-open warning, got:\n%s", out)
	}
}

// TestCentralRecoveryStatus_LockUnavailable_StatesScanningWasNotPerformed
// is the H-2 CLI regression test: with a real artifact present and a
// concurrent exclusive lock held, the command must say plainly that
// scanning/classification did not happen -- never silently fall back to
// reporting the artifact as though it had been examined.
func TestCentralRecoveryStatus_LockUnavailable_StatesScanningWasNotPerformed(t *testing.T) {
	e := newCentralEnv(t)
	if err := os.MkdirAll(e.dir+"/data", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.db, []byte("main"), 0o600); err != nil {
		t.Fatal(err)
	}
	aside := e.db + ".replaced-20260101T000000Z-aaaa"
	if err := os.Mkdir(aside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aside+"/main", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	excl, err := sqlite.AcquireExclusiveCentralLock(e.db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer excl.Close()

	out, _, err := e.run(t, "", "recovery-status", "--db", e.db)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Shared central lock acquired: false") {
		t.Fatalf("expected the lock failure to be reported, got:\n%s", out)
	}
	if !strings.Contains(out, "were NOT performed") {
		t.Fatalf("expected an explicit scan-not-performed statement, got:\n%s", out)
	}
	if strings.Contains(out, "Confidence:") {
		t.Fatalf("no artifact classification must appear when the lock was unavailable, got:\n%s", out)
	}
}

// TestCentralRecoveryStatus_HelpText_AccuratelyDescribesLockFailureBehavior
// is the Phase D.2 regression test: the recovery-status --help text must
// state that a lock failure skips scanning/classification entirely, never
// the pre-H-2-fix claim that filesystem facts are still reported.
func TestCentralRecoveryStatus_HelpText_AccuratelyDescribesLockFailureBehavior(t *testing.T) {
	e := newCentralEnv(t)
	out, _, err := e.run(t, "", "recovery-status", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "still reports what the") {
		t.Fatalf("help text must not claim filesystem facts are still reported on a lock failure:\n%s", out)
	}
	if !strings.Contains(out, "scanning and classification are NOT performed") {
		t.Fatalf("expected the corrected lock-failure description, got:\n%s", out)
	}
	if !strings.Contains(out, "does not exclude an external SQLite process") {
		t.Fatalf("expected the advisory-lock-scope disclosure, got:\n%s", out)
	}
}

func TestCentralRecoveryStatus_DoesNotAffectRestoreCommand(t *testing.T) {
	// Registering recovery-status must not add flags to restore or change its
	// confirmation behavior: `restore --help` must still show exactly its own
	// existing flags, with no mention of recovery-status internals.
	e := newCentralEnv(t)
	out, _, err := e.run(t, "", "restore", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "recovery-status") {
		t.Fatalf("restore --help must not reference recovery-status:\n%s", out)
	}
}

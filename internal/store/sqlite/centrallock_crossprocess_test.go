package sqlite

// Cross-process tests for the restore-concurrency fix. A goroutine-only
// test cannot exercise flock's actual cross-process behavior: within one
// process, acquireCentralLock's own fd bookkeeping and this package's Go-
// level logic are the only things under test, never the kernel's real
// per-process lock table. These tests re-exec the test binary itself
// (os.Args[0], with "-test.run=TestHelperProcess") as a genuine separate OS
// process holding a real lock, the same pattern the standard library's own
// os/exec tests use for subprocess behavior that cannot be faked.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestHelperProcess is not a real test. Its body only runs when
// DSO_LOCK_HELPER=1 is set (by startLockHelper, below); under a normal `go
// test` invocation it returns immediately and does nothing. When run as the
// helper, it acquires a central lock in the requested mode, signals
// readiness by creating readyFile, then waits for releaseFile to appear
// before releasing the lock and exiting -- or, for the kill-based test,
// never gets a chance to release at all, because the parent kills it.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("DSO_LOCK_HELPER") != "1" {
		return
	}
	var rest []string
	for i, a := range os.Args {
		if a == "--" {
			rest = os.Args[i+1:]
			break
		}
	}
	if len(rest) != 4 {
		fmt.Fprintln(os.Stderr, "lock helper: bad arguments")
		os.Exit(2)
	}
	modeArg, dbPath, readyFile, releaseFile := rest[0], rest[1], rest[2], rest[3]
	mode := LockShared
	if modeArg == "exclusive" {
		mode = LockExclusive
	}
	lock, err := acquireCentralLock(dbPath, mode, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lock helper: acquire failed:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(readyFile, []byte("ready"), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "lock helper: signalling ready failed:", err)
		os.Exit(1)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, statErr := os.Stat(releaseFile); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "lock helper: timed out waiting to be released")
			os.Exit(1)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = lock.Close()
	os.Exit(0)
}

// lockHelper is a running subprocess holding a central lock.
type lockHelper struct {
	cmd         *exec.Cmd
	releaseFile string
	// waitCalled is set to true immediately before this helper's own
	// (*exec.Cmd).Wait call -- by release or by kill, whichever reaches it
	// first -- and only then. It distinguishes "Wait has already been
	// invoked" (by either method, regardless of what Wait itself returns)
	// from every other state, including release failing before it ever
	// reaches Wait (for example its releaseFile write failing): a caller's
	// own cleanup can check this field to decide whether it is safe to call
	// kill, without ever risking a second Wait on the same *exec.Cmd.
	waitCalled bool
	// waitAttempts counts how many times release/kill have invoked
	// (*exec.Cmd).Wait. It exists purely for test observability -- proving,
	// for example, that kill reaches Wait even when Process.Kill itself
	// returned an error -- and is never used for any correctness decision;
	// waitCalled is.
	waitAttempts int
}

// startLockHelper launches the subprocess and blocks until it has actually
// acquired the lock (signalled via readyFile), so the caller never races the
// helper's own startup.
func startLockHelper(t *testing.T, mode string, dbPath string) *lockHelper {
	t.Helper()
	dir := t.TempDir()
	readyFile := filepath.Join(dir, "ready")
	releaseFile := filepath.Join(dir, "release")
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", mode, dbPath, readyFile, releaseFile) // #nosec G204 -- os.Args[0] is this test binary itself, not external input
	cmd.Env = append(os.Environ(), "DSO_LOCK_HELPER=1")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting lock helper: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("lock helper never signalled ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &lockHelper{cmd: cmd, releaseFile: releaseFile}
}

// release signals the helper to release its lock and exit cleanly, and
// waits for it to do so.
func (h *lockHelper) release(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(h.releaseFile, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.waitCalled = true
	h.waitAttempts++
	if err := h.cmd.Wait(); err != nil {
		t.Fatalf("lock helper exited uncleanly: %v", err)
	}
}

// kill terminates the helper WITHOUT letting it release its lock itself,
// simulating a crashed or killed process -- the kernel, not this package,
// must be what releases the flock. Wait is always attempted, regardless of
// what Process.Kill returns: a process that has already finished (reported
// via the well-known os.ErrProcessDone sentinel -- see convertESRCH in the
// Go standard library's os package) still needs exactly the same Wait call
// to be reaped if it has not been already, and silently never calling Wait
// on any Kill error would risk leaving a still-running child unreaped. Only
// a genuinely unexpected Kill error (anything other than os.ErrProcessDone)
// is still reported as a failure -- after the wait attempt, not instead of
// it -- so a real problem is never hidden, and a child is never abandoned
// merely because Kill reported it was already gone.
func (h *lockHelper) kill(t *testing.T) {
	t.Helper()
	killErr := h.cmd.Process.Kill()
	h.waitCalled = true
	h.waitAttempts++
	_ = h.cmd.Wait() // Wait's own error is expected and ignored here, exactly as before (e.g. "signal: killed").
	if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		t.Fatal(killErr)
	}
}

func lockTestDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "central.db")
}

// --- Two concurrent read-only operations (shared vs shared) ---

func TestCentralLock_TwoConcurrentSharedAcquisitionsAcrossProcessesSucceed(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db)
	defer h.release(t)

	// A second, independent process-local shared acquisition must succeed
	// quickly, not wait out the timeout: shared locks are compatible with
	// each other.
	lock, err := acquireCentralLock(db, LockShared, 2*time.Second)
	if err != nil {
		t.Fatalf("expected a shared lock to coexist with another shared lock held by a different process, got: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

// --- Backup vs backup (both shared) ---

func TestCentralLock_BackupVsBackup_BothSucceedConcurrently(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db) // stands in for one concurrent backup's shared lock
	defer h.release(t)

	lock, err := acquireCentralLock(db, LockShared, 2*time.Second) // stands in for a second backup
	if err != nil {
		t.Fatalf("expected two concurrent backups (both shared) to coexist, got: %v", err)
	}
	_ = lock.Close()
}

// --- Backup vs restore (shared vs exclusive): restore must wait/fail, never silently race ---

func TestCentralLock_BackupVsRestore_RestoreWaitsAndFailsWithClearError(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db) // a backup in progress
	defer h.release(t)

	_, err := acquireCentralLock(db, LockExclusive, 300*time.Millisecond) // restore's attempt
	if err == nil {
		t.Fatal("expected restore's exclusive acquisition to fail while a backup's shared lock is held")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected a clear ErrCentralLockBusy conflict, got: %v", err)
	}
}

// --- Restore vs restore (exclusive vs exclusive): the second must be serialized/rejected, never silently proceed ---

func TestCentralLock_RestoreVsRestore_SecondFailsWithClearConflict(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db) // first restore in progress
	defer h.release(t)

	_, err := acquireCentralLock(db, LockExclusive, 300*time.Millisecond) // second restore
	if err == nil {
		t.Fatal("expected a second concurrent restore to fail while the first holds the exclusive lock")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected a clear ErrCentralLockBusy conflict, got: %v", err)
	}
}

// --- Read vs restore (shared vs exclusive) ---

func TestCentralLock_ReadVsRestore_RestoreBlocksUntilReadFinishes(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db) // a read holding its shared lock

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := acquireCentralLock(db, LockExclusive, 3*time.Second)
		done <- err
	}()

	time.Sleep(200 * time.Millisecond) // let the exclusive attempt start waiting
	h.release(t)                       // the read finishes and releases

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected restore's exclusive lock to succeed once the read released, got: %v", err)
		}
		if time.Since(start) < 150*time.Millisecond {
			t.Fatal("expected restore to have actually waited for the read, not raced past it")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("restore never acquired the lock after the read released")
	}
}

// --- Write vs restore (shared vs exclusive) ---

func TestCentralLock_WriteVsRestore_RestoreBlockedWhileWriteHoldsSharedLock(t *testing.T) {
	db := lockTestDBPath(t)
	// A write still only takes LockShared at the sidecar level (SQLite's own
	// transaction locking handles write/write serialization at the SQL
	// layer; the sidecar lock's job is only to keep ANY normal operation
	// from overlapping an exclusive replacement).
	h := startLockHelper(t, "shared", db)
	defer h.release(t)

	_, err := acquireCentralLock(db, LockExclusive, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected restore's exclusive acquisition to fail while a write's shared lock is held")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected a clear ErrCentralLockBusy conflict, got: %v", err)
	}
}

// --- Lock release after normal Close ---

func TestCentralLock_ReleasedAfterHelperExitsCleanly(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	h.release(t) // helper releases its own lock and exits before we proceed

	lock, err := acquireCentralLock(db, LockExclusive, 2*time.Second)
	if err != nil {
		t.Fatalf("expected the lock to be free immediately after the helper's clean exit, got: %v", err)
	}
	_ = lock.Close()
}

// --- Lock release after process termination (crash/kill) ---

func TestCentralLock_ReleasedByKernelAfterProcessIsKilled(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	h.kill(t) // the helper never gets to call lock.Close itself

	// The kernel releases flock automatically when the last fd referencing
	// it closes, which happens as part of process teardown on kill -- there
	// is no stale-lock-file cleanup step for this package to perform (see
	// centrallock.go's package doc comment).
	lock, err := acquireCentralLock(db, LockExclusive, 2*time.Second)
	if err != nil {
		t.Fatalf("expected the lock to become available after the holding process was killed, got: %v", err)
	}
	_ = lock.Close()
}

// --- AcquireSharedCentralLock across real OS processes (L-2) ---
//
// sharedlock_test.go already proves AcquireSharedCentralLock's compatibility
// and contention rules within a single process (multiple handles, same
// goroutine tree). These three reuse this file's existing cross-process
// machinery (startLockHelper/lockHelper, which re-execs the test binary as a
// genuine separate OS process) to prove the same rules hold through the
// kernel's real per-process lock table, specifically through the public
// AcquireSharedCentralLock entry point the recovery scanner calls -- not
// just through the lower-level acquireCentralLock the other tests in this
// file already exercise.

func TestCentralLock_TwoSharedHoldersAcrossRealProcesses_BothSucceed(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db)
	defer h.release(t)

	l, err := AcquireSharedCentralLock(db, 2*time.Second)
	if err != nil {
		t.Fatalf("a second, independent-process shared holder must be compatible with the first: %v", err)
	}
	defer l.Close()
}

func TestCentralLock_ExclusiveHelperBlocksSharedAcquisitionThenSucceedsAfterRelease(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	// Registered immediately after the helper starts, so every exit path --
	// an earlier assertion failing, h.release(t) itself failing before it
	// ever reaches Wait (for example its releaseFile write failing), h.
	// release(t) reaching Wait and Wait then erroring, or h.release(t)
	// succeeding outright -- is covered. h.waitCalled (set by release/kill
	// themselves, immediately before their own Wait call, never before)
	// is the one fact that actually determines whether a second Wait would
	// be unsafe, which a locally-scoped "did we get far enough to attempt
	// release" flag cannot distinguish from "Wait was truly already called."
	t.Cleanup(func() {
		if !h.waitCalled {
			h.kill(t)
		}
	})

	_, err := AcquireSharedCentralLock(db, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected the shared acquisition to fail while another process holds the exclusive lock")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected ErrCentralLockBusy, got: %v", err)
	}

	h.release(t)

	l, err := AcquireSharedCentralLock(db, 2*time.Second)
	if err != nil {
		t.Fatalf("expected the shared lock to succeed once the exclusive holder released: %v", err)
	}
	l.Close()
}

// TestCentralLock_ExclusiveHelperCleanup_ReleaseFileWriteFailure_KillsAndReapsHelper
// exercises the row this file's own cleanup audit identified as previously
// uncovered: release()'s os.WriteFile failing before cmd.Wait is ever
// reached. It forces that failure by making the helper's own release-file
// directory temporarily unwritable, then verifies that a waitCalled-gated
// cleanup -- exactly the pattern now used by
// TestCentralLock_ExclusiveHelperBlocksSharedAcquisitionThenSucceedsAfterRelease
// -- still kills and reaps the helper, with Wait invoked exactly once, well
// inside the helper's own 10-second internal release-wait timeout.
func TestCentralLock_ExclusiveHelperCleanup_ReleaseFileWriteFailure_KillsAndReapsHelper(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)

	// This is exactly the cleanup pattern
	// TestCentralLock_ExclusiveHelperBlocksSharedAcquisitionThenSucceedsAfterRelease
	// uses: kill only if Wait has not already been invoked. It also carries
	// this test's actual verification, since it is the code that runs once
	// the forced write failure below has happened and the test body below
	// returns without itself calling release or kill.
	t.Cleanup(func() {
		if h.waitCalled {
			t.Fatal("waitCalled must be false entering cleanup: the forced write failure must never have reached Wait")
		}
		start := time.Now()
		h.kill(t)
		killDuration := time.Since(start)
		if !h.waitCalled {
			t.Fatal("expected kill to set waitCalled after calling Wait")
		}
		if h.cmd.ProcessState == nil {
			t.Fatal("expected kill's Wait to have reaped the helper process (ProcessState is nil)")
		}
		// Well inside the helper's own 10-second release-wait deadline: this
		// cleanup's own kill did the reaping, not that internal timeout.
		if killDuration >= 5*time.Second {
			t.Fatalf("kill took %s; expected cleanup to act immediately, not approach the helper's internal timeout", killDuration)
		}
	})

	releaseDir := filepath.Dir(h.releaseFile)
	// Registered after the cleanup above, so it runs first (t.Cleanup is
	// LIFO): permissions are restored before that cleanup's own kill runs
	// (kill itself needs none of this, but t.TempDir's later removal does)
	// and well before TempDir's own removal.
	t.Cleanup(func() { _ = os.Chmod(releaseDir, 0o700) })
	if err := os.Chmod(releaseDir, 0o500); err != nil {
		t.Fatalf("restricting the release directory for this test: %v", err)
	}

	// The exact operation release() performs before it could ever reach
	// Wait, called directly rather than via h.release(t) -- which would
	// itself call t.Fatal on this same failure and end the test immediately,
	// leaving no chance to report PASS once the Cleanup above confirms the
	// fix behaved correctly.
	if err := os.WriteFile(h.releaseFile, []byte("go"), 0o600); err == nil {
		t.Fatal("expected the release-file write to fail while its directory is read-only")
	}
	if h.waitCalled {
		t.Fatal("waitCalled must still be false: the write failed before Wait could ever be reached")
	}
}

// TestLockHelper_Kill_NormalSuccess_InvokesWaitAndReaps is the baseline case
// for the kill() control-flow fix: an ordinary, successful Process.Kill
// must still reach exactly one Wait call and reap the helper.
func TestLockHelper_Kill_NormalSuccess_InvokesWaitAndReaps(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	h.kill(t)
	if !h.waitCalled {
		t.Fatal("expected waitCalled to be true after a normal kill")
	}
	if h.waitAttempts != 1 {
		t.Fatalf("expected exactly one Wait attempt, got %d", h.waitAttempts)
	}
	if h.cmd.ProcessState == nil {
		t.Fatal("expected the process to be reaped (ProcessState set) after kill")
	}
}

// TestLockHelper_Kill_AfterAlreadyReaped_GetsErrProcessDoneButStillAttemptsWait
// exercises kill()'s os.ErrProcessDone branch deterministically, with no
// timing dependency, race, or fragile process manipulation: killing the
// SAME already-reaped helper a second time makes Process.Kill short-circuit
// to os.ErrProcessDone purely from Go's own internal process bookkeeping
// (confirmed against the Go standard library's os package source during
// the preceding investigation: see pidSignal/pidfdSendSignal's statusDone
// check) -- no real signal is even sent. This proves the second kill(t)
// call does not fail the test on that error, and that a second Wait
// attempt still happens (waitAttempts reaches 2) rather than being skipped.
func TestLockHelper_Kill_AfterAlreadyReaped_GetsErrProcessDoneButStillAttemptsWait(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	h.kill(t) // first kill: a real SIGKILL, reaps normally.
	if h.waitAttempts != 1 {
		t.Fatalf("expected exactly one Wait attempt after the first kill, got %d", h.waitAttempts)
	}

	h.kill(t) // second kill on the same, already-reaped helper: must not fail.
	if h.waitAttempts != 2 {
		t.Fatalf("expected a second Wait attempt even though Kill returned ErrProcessDone, got %d total", h.waitAttempts)
	}
}

func TestCentralLock_SharedAcquisitionSucceedsAfterExclusiveHelperIsKilled(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	h.kill(t) // the helper never gets to call lock.Close itself

	// As in TestCentralLock_ReleasedByKernelAfterProcessIsKilled, the kernel
	// releases the flock on process teardown; there is no stale-lock-file
	// cleanup step this package performs. This is the same guarantee,
	// verified through AcquireSharedCentralLock specifically.
	l, err := AcquireSharedCentralLock(db, 2*time.Second)
	if err != nil {
		t.Fatalf("expected the shared lock to become available after the exclusive holder was killed, got: %v", err)
	}
	l.Close()
}

// --- Lock contention and timeout: the busy error must appear within roughly the configured timeout, not hang forever ---

func TestCentralLock_TimeoutIsBounded(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	defer h.release(t)

	timeout := 200 * time.Millisecond
	start := time.Now()
	_, err := acquireCentralLock(db, LockExclusive, timeout)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected the acquisition to fail")
	}
	if elapsed > timeout+2*time.Second {
		t.Fatalf("expected the timeout to be roughly bounded by %s, took %s", timeout, elapsed)
	}
}

func isCentralLockBusy(err error) bool {
	for e := err; e != nil; {
		if e == ErrCentralLockBusy {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// --- Higher-level API: sqlite.Open (shared) vs AcquireExclusiveCentralLock, across real processes ---

func TestCentralLock_OpenBlockedWhileAnotherProcessHoldsExclusive(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "exclusive", db)
	defer h.release(t)

	_, err := Open(context.Background(), db, Options{LockTimeout: 300 * time.Millisecond})
	if err == nil {
		t.Fatal("expected Open to fail while another process holds the exclusive restore lock")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected ErrCentralLockBusy, got: %v", err)
	}
}

// --- Path normalization: a symlinked route to the same database must take the SAME lock ---
//
// This is deliberately a same-process test, not a cross-process one: it is
// testing pure path-identity logic (absPathForLock), not the kernel's real
// flock behavior, so a subprocess would add cost without adding coverage.

func TestCentralLock_SymlinkedPathToSameDatabaseSharesTheLock(t *testing.T) {
	realDir := t.TempDir()
	db := filepath.Join(realDir, "central.db")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(t.TempDir(), "via-symlink")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	viaSymlink := filepath.Join(linkDir, "central.db")

	realResolved, err := absPathForLock(db)
	if err != nil {
		t.Fatal(err)
	}
	symlinkResolved, err := absPathForLock(viaSymlink)
	if err != nil {
		t.Fatal(err)
	}
	if realResolved != symlinkResolved {
		t.Fatalf("expected the real and symlinked paths to resolve to the same lock identity, got %q vs %q", realResolved, symlinkResolved)
	}

	// Acquire exclusive via the symlinked path; a second exclusive attempt
	// via the real path must see it as busy -- if path normalization
	// didn't resolve the symlink, this would incorrectly succeed against a
	// different, uncoordinated sidecar file.
	lock, err := AcquireExclusiveCentralLock(viaSymlink, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	_, err = AcquireExclusiveCentralLock(db, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected a lock taken via a symlinked path to be visible (and conflicting) via the real path")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected ErrCentralLockBusy, got: %v", err)
	}
}

// --- Lifecycle: RestoreLock.Open must refuse use-after-release ---

func TestRestoreLock_OpenAfterCloseIsRefused(t *testing.T) {
	db := lockTestDBPath(t)
	lock, err := AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	// Without the use-after-release guard, this would succeed and silently
	// open the database with NO lock actually held at all, because Open
	// trusts skipCentralLock unconditionally once the path matches.
	_, err = lock.Open(context.Background(), db, Options{})
	if err == nil {
		t.Fatal("expected Open to refuse a RestoreLock that was already released")
	}

	// And it must not have left the real lock state inconsistent: a fresh
	// exclusive acquisition on the same path must still succeed (not
	// permanently wedged by the refused Open attempt).
	fresh, err := AcquireExclusiveCentralLock(db, time.Second)
	if err != nil {
		t.Fatalf("expected the path to still be free after the refused Open, got: %v", err)
	}
	_ = fresh.Close()
}

func TestCentralLock_AcquireExclusiveBlockedWhileAnotherProcessHasItOpen(t *testing.T) {
	db := lockTestDBPath(t)
	h := startLockHelper(t, "shared", db) // stands in for another process's ordinary Open
	defer h.release(t)

	_, err := AcquireExclusiveCentralLock(db, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected the exclusive restore lock to fail while another process has the database open")
	}
	if !isCentralLockBusy(err) {
		t.Fatalf("expected ErrCentralLockBusy, got: %v", err)
	}
}

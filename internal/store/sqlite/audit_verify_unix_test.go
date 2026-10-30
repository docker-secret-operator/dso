//go:build unix

package sqlite

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

// readableDB returns a closed, populated database in an isolated safe
// directory, so sidecar tests can place companions beside it.
func readableDB(t *testing.T) string {
	t.Helper()
	s, path := open(t)
	seedWithAudit(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// verifyWithin fails the test if VerifyAuditLog does not return in time. The
// timeout is a secondary safeguard; the fix is that sidecars are opened
// without blocking.
func verifyWithin(t *testing.T, path string) AuditVerifyResult {
	t.Helper()
	ch := make(chan AuditVerifyResult, 1)
	go func() { ch <- VerifyAuditLog(ctx, path) }()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("verification blocked (a FIFO sidecar must never be waited on)")
		return AuditVerifyResult{}
	}
}

func TestSidecars_FIFOAndSymlinkAreRejectedAsUnavailable(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run("fifo"+suffix, func(t *testing.T) {
			path := readableDB(t)
			if err := syscall.Mkfifo(path+suffix, 0o600); err != nil {
				t.Skipf("cannot create a FIFO here: %v", err)
			}
			r := verifyWithin(t, path)
			if r.Status != AuditUnavailable || !strings.Contains(r.Reason, "unsafe -wal/-shm") || !errors.Is(r.Err, ErrStageSidecar) {
				t.Fatalf("a FIFO sidecar must make verification unavailable, got %+v", r)
			}
		})
		t.Run("symlink"+suffix, func(t *testing.T) {
			path := readableDB(t)
			target := filepath.Join(filepath.Dir(path), "elsewhere")
			if err := os.WriteFile(target, []byte("not a wal"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path+suffix); err != nil {
				t.Fatal(err)
			}
			r := verifyWithin(t, path)
			if r.Status != AuditUnavailable || !errors.Is(r.Err, ErrStageSidecar) {
				t.Fatalf("a symlinked sidecar must make verification unavailable, got %+v", r)
			}
			if b, _ := os.ReadFile(target); string(b) != "not a wal" {
				t.Fatal("the symlink target was modified")
			}
		})
		t.Run("dangling-symlink"+suffix, func(t *testing.T) {
			path := readableDB(t)
			if err := os.Symlink(filepath.Join(filepath.Dir(path), "missing-target"), path+suffix); err != nil {
				t.Fatal(err)
			}
			if r := verifyWithin(t, path); r.Status != AuditUnavailable || !errors.Is(r.Err, ErrStageSidecar) {
				t.Fatalf("a dangling symlink sidecar must be rejected, got %+v", r)
			}
		})
	}
}

func TestSidecars_RejectedSidecarLeavesNoStagingDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	path := readableDB(t)
	if err := syscall.Mkfifo(path+"-wal", 0o600); err != nil {
		t.Skip(err)
	}
	if r := verifyWithin(t, path); r.Status != AuditUnavailable {
		t.Fatalf("got %+v", r)
	}
	// the same shared staging code serves restore: it rejects the sidecar too.
	if _, _, err := CopyCandidateForValidation(path); !errors.Is(err, ErrStageSidecar) {
		t.Fatalf("restore staging must reject the FIFO sidecar, got %v", err)
	}
	if m, _ := filepath.Glob(filepath.Join(tmp, "dso-restore-validate-*")); len(m) != 0 {
		t.Fatalf("staging directories leaked: %v", m)
	}
}

func TestStaging_CleanedOnSuccessAndOnReturnedErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	count := func() int {
		m, _ := filepath.Glob(filepath.Join(tmp, "dso-restore-validate-*"))
		return len(m)
	}
	path := readableDB(t)

	if r := VerifyAuditLog(ctx, path); r.Status != AuditVerified || count() != 0 {
		t.Fatalf("success must leave no staging directory (found %d): %+v", count(), r)
	}
	st, err := StageCandidate(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if count() != 1 {
		t.Fatalf("expected exactly one live staging directory, got %d", count())
	}
	st.Close()
	st.Close() // idempotent
	if count() != 0 {
		t.Fatal("Close must remove the staging directory")
	}

	garbage := filepath.Join(securefstest.TempDir(t), "g.db")
	if err := os.WriteFile(garbage, []byte("definitely not a sqlite database, only bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := VerifyAuditLog(ctx, garbage); r.Status != AuditUnavailable || count() != 0 {
		t.Fatalf("a staging error must leave no directory (found %d): %+v", count(), r)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	r := VerifyAuditLog(cctx, path)
	if r.Status != AuditUnavailable || !strings.Contains(r.Reason, "interrupted") || count() != 0 {
		t.Fatalf("a cancelled run must be unavailable/interrupted and clean up (found %d): %+v", count(), r)
	}
}

func TestStaging_RemoveRegisteredTouchesOnlyOwnDirectories(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	foreign := filepath.Join(tmp, "dso-restore-validate-foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := StageCandidate(ctx, readableDB(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(st.Path)
	removeRegisteredStagingDirs()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a registered staging directory must be removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("a directory this process did not create must never be removed, even if its name matches")
	}
	st.Close() // already gone: must not fail
}

const childEnv = "DSO_TEST_STAGING_CHILD_DB"

// TestStagingSignalHelperProcess is the child half of
// TestStaging_GracefulSIGTERMRemovesStagingAndStillTerminates; it does nothing
// unless launched by that test.
func TestStagingSignalHelperProcess(t *testing.T) {
	path := os.Getenv(childEnv)
	if path == "" {
		return
	}
	stop := InstallStagingSignalCleanup()
	defer stop()
	st, err := StageCandidate(context.Background(), path)
	if err != nil {
		os.Stdout.WriteString("ERROR " + err.Error() + "\n")
		return
	}
	os.Stdout.WriteString("STAGED " + filepath.Dir(st.Path) + "\n")
	select {} // wait to be signalled
}

func TestStaging_GracefulSIGTERMRemovesStagingAndStillTerminates(t *testing.T) {
	tmp := t.TempDir()
	path := readableDB(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestStagingSignalHelperProcess$")
	cmd.Env = append(os.Environ(), childEnv+"="+path, "TMPDIR="+tmp)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	staged := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "STAGED ") || strings.HasPrefix(line, "ERROR ") {
				staged <- line
				return
			}
		}
		staged <- ""
	}()
	var line string
	select {
	case line = <-staged:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child never reported staging")
	}
	if !strings.HasPrefix(line, "STAGED ") {
		_ = cmd.Process.Kill()
		t.Fatalf("child failed to stage: %q", line)
	}
	dir := strings.TrimPrefix(line, "STAGED ")
	if _, err := os.Stat(dir); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("expected a live staging directory: %v", err)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("expected the child to be terminated by the signal, got %v", err)
		}
		ws := ee.Sys().(syscall.WaitStatus)
		if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Fatalf("the signal must not be swallowed: the child must die by SIGTERM, got %v", ws)
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not terminate after SIGTERM")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("the staging directory must be removed on SIGTERM (stat err=%v)", err)
	}
}

func TestVerifyAuditLog_UnavailableReasonsAreSanitized(t *testing.T) {
	dir := securefstest.TempDir(t)
	loose := filepath.Join(dir, "loose.db")
	src := readableDB(t)
	b, _ := os.ReadFile(src)
	if err := os.WriteFile(loose, b, 0o644); err != nil { // #nosec G306 -- deliberately unsafe for this test
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{"missing": filepath.Join(dir, "none.db"), "loose": loose, "symlink": link} {
		r := VerifyAuditLog(ctx, p)
		if r.Status != AuditUnavailable || r.Reason == "" {
			t.Fatalf("%s: %+v", name, r)
		}
		for _, bad := range []string{dir, "/", "securefs", "chmod", "fix:"} {
			if strings.Contains(r.Reason, bad) {
				t.Fatalf("%s: sanitized reason leaks %q: %s", name, bad, r.Reason)
			}
		}
		if r.Err == nil {
			t.Fatalf("%s: the underlying diagnostic must be preserved in Err", name)
		}
	}
}

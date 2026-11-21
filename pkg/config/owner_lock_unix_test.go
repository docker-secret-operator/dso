//go:build unix

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// trackLockFiles records every lock file lockOwnerID opens, so a test can assert
// deterministically (no /proc, no process-wide descriptor counts, nothing a
// background goroutine could disturb) that each one is closed again.
func trackLockFiles(t *testing.T) *[]*os.File {
	t.Helper()
	var opened []*os.File
	old := ownerLockOpened
	ownerLockOpened = func(f *os.File) { opened = append(opened, f) }
	t.Cleanup(func() { ownerLockOpened = old })
	return &opened
}

func isClosed(f *os.File) bool {
	_, err := f.Stat()
	return errors.Is(err, os.ErrClosed)
}

// F4: a symlink at the lock path is never followed.
func TestLockOwnerID_SymlinkFailsClosedAndTargetUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(target, old, old)
	before, _ := os.Stat(target)
	lock := filepath.Join(dir, ownerIDLockFile)
	if err := os.Symlink(target, lock); err != nil {
		t.Skip(err)
	}

	if rel, err := lockOwnerID(lock, time.Second); err == nil {
		_ = rel()
		t.Fatal("a symlinked lock path was accepted")
	}
	after, _ := os.Stat(target)
	if b, _ := os.ReadFile(target); string(b) != "precious" || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		t.Error("the symlink target was opened for writing / modified")
	}

	// Through the public API: no id, config untouched.
	cfgDir := writeCfg(t, minimalCfg, 0o640)
	t.Chdir(cfgDir)
	if err := os.Symlink(target, filepath.Join(cfgDir, ownerIDLockFile)); err != nil {
		t.Skip(err)
	}
	c := &Config{}
	if err := EnsureOwnerID(c, "dso.yaml"); err == nil || c.Agent.OwnerID != "" {
		t.Fatalf("err=%v id=%q", err, c.Agent.OwnerID)
	}
	if b, _ := os.ReadFile("dso.yaml"); string(b) != minimalCfg {
		t.Error("config changed")
	}
}

func TestLockOwnerID_NonRegularPathsFailClosed(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "asdir")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "afifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	for name, p := range map[string]string{"directory": d, "fifo": fifo} {
		if rel, err := lockOwnerID(p, 200*time.Millisecond); err == nil {
			_ = rel()
			t.Errorf("%s accepted as a lock file", name)
		}
	}
}

func TestLockOwnerID_RegularFileWorksAndIsKept(t *testing.T) {
	lock := filepath.Join(t.TempDir(), ownerIDLockFile)
	rel, err := lockOwnerID(lock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(lock)
	if err != nil || st.Mode().Perm() != 0o600 || !st.Mode().IsRegular() {
		t.Fatalf("lock file: %v err=%v, want regular 0600", st, err)
	}
	if err := rel(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Error("the lock file must not be unlinked after use")
	}
	// A pre-existing regular lock file is reused.
	rel2, err := lockOwnerID(lock, time.Second)
	if err != nil {
		t.Fatalf("pre-existing lock file rejected: %v", err)
	}
	_ = rel2()
}

// Timeout behaviour, and: every lock file lockOwnerID opens is closed again on
// every path -- release, timeout, and rejection of a non-regular file.
func TestLockOwnerID_TimeoutAndDescriptorCleanup(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, ownerIDLockFile)
	opened := trackLockFiles(t)

	holder, err := lockOwnerID(lock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || isClosed((*opened)[0]) {
		t.Fatal("a held lock must keep its descriptor open")
	}
	held := (*opened)[0]

	for i := 0; i < 5; i++ { // contended: times out each time
		start := time.Now()
		rel, err := lockOwnerID(lock, 30*time.Millisecond)
		if err == nil {
			_ = rel()
			t.Fatal("acquired a held lock")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("unexpected error: %v", err)
		}
		if time.Since(start) > 2*time.Second {
			t.Fatal("timeout not honoured")
		}
		if f := (*opened)[len(*opened)-1]; !isClosed(f) {
			t.Fatal("a timed-out attempt leaked its lock-file descriptor")
		}
	}
	if isClosed(held) {
		t.Fatal("contention must not close the holder's descriptor")
	}

	if err := holder(); err != nil {
		t.Fatal(err)
	}
	if !isClosed(held) {
		t.Fatal("release did not close the lock-file descriptor")
	}

	// A non-regular file is opened, rejected, and must be closed again (a FIFO
	// opens read-write without blocking; a symlink/directory fail before an
	// descriptor exists, so there is nothing to leak).
	fifo := filepath.Join(dir, "afifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	before := len(*opened)
	if rel, err := lockOwnerID(fifo, 50*time.Millisecond); err == nil {
		_ = rel()
		t.Fatal("a FIFO was accepted as a lock file")
	}
	if len(*opened) != before+1 || !isClosed((*opened)[before]) {
		t.Fatal("a rejected non-regular lock file leaked its descriptor")
	}

	// Success again after all of that, then released and closed.
	rel, err := lockOwnerID(lock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	last := (*opened)[len(*opened)-1]
	if err := rel(); err != nil || !isClosed(last) {
		t.Fatalf("release: err=%v closed=%v", err, isClosed(last))
	}
}

// F3: only "unsupported" is tolerated.
func TestIgnoreUnsupportedSync(t *testing.T) {
	if ignoreUnsupportedSync(nil) != nil || ignoreUnsupportedSync(syscall.EINVAL) != nil || ignoreUnsupportedSync(errors.ErrUnsupported) != nil {
		t.Error("nil / EINVAL / ErrUnsupported must be tolerated (filesystem cannot fsync a directory)")
	}
	for _, e := range []error{syscall.EIO, syscall.ENOSPC, syscall.EROFS} {
		if ignoreUnsupportedSync(e) == nil {
			t.Errorf("%v must be reported as a durability failure", e)
		}
	}
	if err := syncDir(t.TempDir()); err != nil {
		t.Errorf("syncDir on a real directory: %v", err)
	}
}

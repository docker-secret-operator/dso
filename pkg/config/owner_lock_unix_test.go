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

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("/proc/self/fd unavailable: cannot count descriptors on this platform")
	}
	return len(entries)
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

// Timeout behaviour and descriptor hygiene on every path.
func TestLockOwnerID_TimeoutAndNoDescriptorLeaks(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, ownerIDLockFile)
	base := openFDs(t)

	holder, err := lockOwnerID(lock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ { // contended: times out each time
		start := time.Now()
		if rel, err := lockOwnerID(lock, 30*time.Millisecond); err == nil {
			_ = rel()
			t.Fatal("acquired a held lock")
		} else if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("unexpected error: %v", err)
		} else if time.Since(start) > 2*time.Second {
			t.Fatal("timeout not honoured")
		}
	}
	if err := holder(); err != nil {
		t.Fatal(err)
	}
	// Failing paths (symlink, directory) and success all release their descriptors.
	_ = os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "sym"))
	_ = os.Mkdir(filepath.Join(dir, "d"), 0o700)
	for i := 0; i < 20; i++ {
		for _, p := range []string{filepath.Join(dir, "sym"), filepath.Join(dir, "d")} {
			if rel, err := lockOwnerID(p, 10*time.Millisecond); err == nil {
				_ = rel()
			}
		}
		rel, err := lockOwnerID(lock, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = rel()
	}
	if got := openFDs(t); got != base {
		t.Errorf("descriptor leak: %d open before, %d after", base, got)
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

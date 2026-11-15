//go:build linux

package runtimedir

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// All tests use temporary directories only. None touches /run/dso.

func euid() uint32 { return uint32(os.Geteuid()) }

// ownGroup injects a group the test process may chown to without root: its own.
func ownGroup(string) (int, bool) { return os.Getgid(), true }

func mustMkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // defeat the umask
		t.Fatal(err)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func enforcePolicy() Policy {
	return Policy{
		OwnerUID:                 euid(),
		Enforce:                  true,
		GroupName:                "dso",
		Mode:                     0o750,
		Create:                   true,
		RequireNoGroupOtherWrite: true,
		LookupGroup:              ownGroup,
	}
}

func openFDs(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count descriptors: %v", err)
	}
	return len(ents)
}

// ---- runtime directory ----

func TestEnsure_CreatesAMissingDirectoryWithTheExactMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	d, err := Ensure(p, enforcePolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := modeOf(t, p); got != 0o750 {
		t.Fatalf("created directory has mode %04o, want 0750 (umask must not weaken it)", got)
	}
	var st unix.Stat_t
	if err := unix.Stat(p, &st); err != nil {
		t.Fatal(err)
	}
	if int(st.Gid) != os.Getgid() || st.Uid != euid() {
		t.Fatalf("owner/group = %d/%d", st.Uid, st.Gid)
	}
}

func TestEnsure_CorrectsAnExisting0775Directory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	mustMkdir(t, p, 0o775)
	d, err := Ensure(p, enforcePolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := modeOf(t, p); got != 0o750 {
		t.Fatalf("mode %04o after Ensure, want 0750", got)
	}
}

func TestEnsure_ClearsExtraModeBits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	mustMkdir(t, p, 0o750)
	if err := os.Chmod(p, 0o750|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	d, err := Ensure(p, enforcePolicy())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fi, _ := os.Lstat(p)
	if fi.Mode()&os.ModeSetgid != 0 || fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode %v after Ensure, want plain 0750", fi.Mode())
	}
}

func TestEnsure_RefusesAWrongOwnerWithoutModifyingIt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	mustMkdir(t, p, 0o775)
	pol := enforcePolicy()
	pol.OwnerUID = euid() + 1 // the directory is owned by someone else, as far as the policy is concerned
	d, err := Ensure(p, pol)
	if err == nil {
		d.Close()
		t.Fatal("a directory with an unexpected owner must be refused")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("want ErrUnsafe, got %v", err)
	}
	if got := modeOf(t, p); got != 0o775 {
		t.Fatalf("a refused directory was modified: mode %04o", got)
	}
}

func TestEnsure_RefusesANonDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := Ensure(p, enforcePolicy()); err == nil || !errors.Is(err, ErrUnsafe) {
		if d != nil {
			d.Close()
		}
		t.Fatalf("want ErrUnsafe for a regular file, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatal("the non-directory object was modified")
	}
}

func TestEnsure_RefusesASymlinkedDirectoryAndLeavesTheTargetAlone(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	mustMkdir(t, target, 0o775)
	link := filepath.Join(root, "dso")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	d, err := Ensure(link, enforcePolicy())
	if err == nil {
		d.Close()
		t.Fatal("a symlinked runtime directory must be refused")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("want ErrUnsafe, got %v", err)
	}
	if got := modeOf(t, target); got != 0o775 {
		t.Fatalf("the symlink target was modified through the link: mode %04o", got)
	}
}

func TestEnsure_VerifyOnlyRejectsGroupOrOtherWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dso")
	mustMkdir(t, p, 0o775)
	pol := Policy{OwnerUID: euid(), RequireNoGroupOtherWrite: true}
	d, err := Ensure(p, pol)
	if err == nil {
		d.Close()
		t.Fatal("0775 must be rejected in verify-only mode")
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("want ErrUnsafe, got %v", err)
	}
	if got := modeOf(t, p); got != 0o775 {
		t.Fatalf("verify-only mode changed the directory: %04o", got)
	}

	mustMkdir(t, p+"-ok", 0o750)
	d, err = Ensure(p+"-ok", pol)
	if err != nil {
		t.Fatalf("0750 owned by the caller must be accepted: %v", err)
	}
	d.Close()
}

func TestEnsure_VerifyOnlyDoesNotCreateWhenCreateIsFalse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing")
	_, err := Ensure(p, Policy{OwnerUID: euid()})
	if err == nil {
		t.Fatal("a missing directory must be an error when Create is false")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist in the chain, got %v", err)
	}
	if _, serr := os.Lstat(p); serr == nil {
		t.Fatal("Ensure created a directory it was not allowed to create")
	}
}

func TestEnsure_FailureToSetTheGroupIsAnErrorNotAWideningAndNotSilent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can chown to gid 0; this asserts the unprivileged failure path")
	}
	p := filepath.Join(t.TempDir(), "dso")
	mustMkdir(t, p, 0o770)
	pol := enforcePolicy()
	pol.GroupName = "" // wants gid 0, which an unprivileged process cannot set
	pol.LookupGroup = nil
	d, err := Ensure(p, pol)
	if err == nil {
		d.Close()
		t.Fatal("being unable to establish the group must fail, not be ignored")
	}
	if got := modeOf(t, p); got != 0o770 {
		t.Fatalf("a failed Ensure changed the mode to %04o", got)
	}
}

func TestEnsure_RequiresAnAbsolutePath(t *testing.T) {
	if _, err := Ensure("relative/dso", enforcePolicy()); err == nil {
		t.Fatal("a relative path must be rejected")
	}
	if _, err := Ensure("", enforcePolicy()); err == nil {
		t.Fatal("an empty path must be rejected")
	}
}

func TestDir_CloseIsIdempotentAndBlocksFurtherUse(t *testing.T) {
	d, err := Ensure(t.TempDir(), Policy{OwnerUID: euid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := d.OpenRegular("f", os.O_RDONLY, 0, FilePolicy{OwnerUID: euid(), MaxMode: 0o600}); err == nil {
		t.Fatal("OpenRegular on a closed Dir must fail")
	}
}

func TestDescriptorsAreNotLeaked(t *testing.T) {
	dir := t.TempDir()
	fp := FilePolicy{OwnerUID: euid(), MaxMode: 0o600}
	before := openFDs(t)
	for i := 0; i < 300; i++ {
		d, err := Ensure(dir, Policy{OwnerUID: euid()})
		if err != nil {
			t.Fatal(err)
		}
		f, err := d.OpenRegular("log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, fp)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		// failing opens must not leak either
		_ = os.Symlink("/nonexistent", filepath.Join(dir, "link"))
		_, _ = d.OpenRegular("link", os.O_RDONLY, 0, fp)
		_ = os.Remove(filepath.Join(dir, "link"))
		_ = d.Close()
	}
	if after := openFDs(t); after > before+2 {
		t.Fatalf("descriptor leak: %d open before, %d after", before, after)
	}
}

// ---- files in the directory ----

func newDir(t *testing.T) (*Dir, string) {
	t.Helper()
	root := t.TempDir()
	d, err := Ensure(root, Policy{OwnerUID: euid()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, root
}

var testFP = func() FilePolicy { return FilePolicy{OwnerUID: euid(), MaxMode: 0o600} }

func wantUnsafe(t *testing.T, f *os.File, err error, what string) {
	t.Helper()
	if f != nil {
		f.Close()
	}
	if err == nil {
		t.Fatalf("%s: expected the file to be refused", what)
	}
	if !errors.Is(err, ErrUnsafe) {
		t.Fatalf("%s: want ErrUnsafe, got %v", what, err)
	}
}

func TestOpenRegular_RefusesASymlinkOnReadAndWrite(t *testing.T) {
	d, root := newDir(t)
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("ORIGINAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "events.jsonl")); err != nil {
		t.Fatal(err)
	}
	f, err := d.OpenRegular("events.jsonl", os.O_RDONLY, 0, testFP())
	wantUnsafe(t, f, err, "read through symlink")
	f, err = d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
	wantUnsafe(t, f, err, "write through symlink")
	if b, _ := os.ReadFile(victim); string(b) != "ORIGINAL\n" {
		t.Fatalf("the symlink target was modified: %q", b)
	}
}

func TestOpenRegular_RefusesAFIFOWithoutHanging(t *testing.T) {
	d, root := newDir(t)
	if err := syscall.Mkfifo(filepath.Join(root, "events.jsonl"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f, err := d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
		wantUnsafe(t, f, err, "write-open of a FIFO")
		f, err = d.OpenRegular("events.jsonl", os.O_RDONLY, 0, testFP())
		wantUnsafe(t, f, err, "read-open of a FIFO")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("opening a FIFO blocked: O_NONBLOCK must prevent a startup hang")
	}
	fi, _ := os.Lstat(filepath.Join(root, "events.jsonl"))
	if fi.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("the FIFO was replaced or removed")
	}
}

func TestOpenRegular_RefusesADirectory(t *testing.T) {
	d, root := newDir(t)
	if err := os.Mkdir(filepath.Join(root, "events.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := d.OpenRegular("events.jsonl", os.O_RDONLY, 0, testFP())
	wantUnsafe(t, f, err, "read-open of a directory")
	f, err = d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
	wantUnsafe(t, f, err, "write-open of a directory")
	if fi, serr := os.Lstat(filepath.Join(root, "events.jsonl")); serr != nil || !fi.IsDir() {
		t.Fatal("the directory was removed or replaced")
	}
}

func TestOpenRegular_RefusesADeviceFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("mknod needs root; the classification itself is covered by TestFileProblem")
	}
	d, root := newDir(t)
	if err := unix.Mknod(filepath.Join(root, "events.jsonl"), unix.S_IFCHR|0o600, int(unix.Mkdev(1, 3))); err != nil { // /dev/null numbers
		t.Skipf("cannot create a device node here: %v", err)
	}
	f, err := d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND, 0, FilePolicy{OwnerUID: 0, MaxMode: 0o600})
	wantUnsafe(t, f, err, "device file")
}

func TestFileProblem_ClassifiesEveryObjectType(t *testing.T) {
	fp := FilePolicy{OwnerUID: 1000, MaxMode: 0o600}
	cases := []struct {
		name string
		st   unix.Stat_t
		bad  bool
	}{
		{"regular ok", unix.Stat_t{Mode: unix.S_IFREG | 0o600, Uid: 1000, Nlink: 1}, false},
		{"regular stricter mode ok", unix.Stat_t{Mode: unix.S_IFREG | 0o400, Uid: 1000, Nlink: 1}, false},
		{"fifo", unix.Stat_t{Mode: unix.S_IFIFO | 0o600, Uid: 1000, Nlink: 1}, true},
		{"directory", unix.Stat_t{Mode: unix.S_IFDIR | 0o700, Uid: 1000, Nlink: 1}, true},
		{"char device", unix.Stat_t{Mode: unix.S_IFCHR | 0o600, Uid: 1000, Nlink: 1}, true},
		{"block device", unix.Stat_t{Mode: unix.S_IFBLK | 0o600, Uid: 1000, Nlink: 1}, true},
		{"symlink", unix.Stat_t{Mode: unix.S_IFLNK | 0o777, Uid: 1000, Nlink: 1}, true},
		{"socket", unix.Stat_t{Mode: unix.S_IFSOCK | 0o600, Uid: 1000, Nlink: 1}, true},
		{"wrong owner", unix.Stat_t{Mode: unix.S_IFREG | 0o600, Uid: 1001, Nlink: 1}, true},
		{"hard linked", unix.Stat_t{Mode: unix.S_IFREG | 0o600, Uid: 1000, Nlink: 2}, true},
		{"group readable", unix.Stat_t{Mode: unix.S_IFREG | 0o640, Uid: 1000, Nlink: 1}, true},
		{"other readable", unix.Stat_t{Mode: unix.S_IFREG | 0o604, Uid: 1000, Nlink: 1}, true},
		{"owner execute exceeds", unix.Stat_t{Mode: unix.S_IFREG | 0o700, Uid: 1000, Nlink: 1}, true},
		{"setuid", unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISUID | 0o600, Uid: 1000, Nlink: 1}, true},
		{"setgid", unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISGID | 0o600, Uid: 1000, Nlink: 1}, true},
		{"sticky", unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISVTX | 0o600, Uid: 1000, Nlink: 1}, true},
		{"setuid+setgid+sticky", unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISUID | unix.S_ISGID | unix.S_ISVTX | 0o600, Uid: 1000, Nlink: 1}, true},
		{"setuid on a stricter base mode", unix.Stat_t{Mode: unix.S_IFREG | unix.S_ISUID | 0o400, Uid: 1000, Nlink: 1}, true},
	}
	for _, c := range cases {
		st := c.st
		if got := fileProblem(&st, fp); (got != "") != c.bad {
			t.Errorf("%s: problem=%q, want bad=%v", c.name, got, c.bad)
		}
	}
}

func TestOpenRegular_RefusesAHardLinkedFile(t *testing.T) {
	d, root := newDir(t)
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(victim, filepath.Join(root, "events.jsonl")); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}
	f, err := d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
	wantUnsafe(t, f, err, "hard-linked file")
	if b, _ := os.ReadFile(victim); string(b) != "v" {
		t.Fatal("the hard-linked file was written to")
	}
}

func TestOpenRegular_RefusesAWrongOwnerAndExtraPermissions(t *testing.T) {
	d, root := newDir(t)
	p := filepath.Join(root, "events.jsonl")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := d.OpenRegular("events.jsonl", os.O_RDONLY, 0, FilePolicy{OwnerUID: euid() + 1, MaxMode: 0o600})
	wantUnsafe(t, f, err, "wrong owner")

	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err = d.OpenRegular("events.jsonl", os.O_RDONLY, 0, testFP())
	wantUnsafe(t, f, err, "group/other readable file")
	if got := modeOf(t, p); got != 0o644 {
		t.Fatalf("a refused file was chmod'ed to %04o", got)
	}
}

func TestOpenRegular_AcceptsAValidFileAndKeepsAppendSemantics(t *testing.T) {
	d, root := newDir(t)
	flags := os.O_WRONLY | os.O_APPEND | os.O_CREATE

	f1, err := d.OpenRegular("events.jsonl", flags, 0o600, testFP()) // missing: created
	if err != nil {
		t.Fatal(err)
	}
	if got := modeOf(t, filepath.Join(root, "events.jsonl")); got&0o077 != 0 {
		t.Fatalf("created file has mode %04o", got)
	}
	f2, err := d.OpenRegular("events.jsonl", flags, 0o600, testFP()) // existing: reused
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f1.WriteString("one\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := f2.WriteString("two\n"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*os.File{f1, f2} {
		fl, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
		if err != nil {
			t.Fatal(err)
		}
		if fl&unix.O_NONBLOCK != 0 {
			t.Fatal("O_NONBLOCK must be cleared on a validated regular file")
		}
		if fl&unix.O_APPEND == 0 {
			t.Fatal("O_APPEND must be preserved")
		}
		fd := int(f.Fd())
		if ff, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil || ff&unix.FD_CLOEXEC == 0 {
			t.Fatalf("descriptor is not close-on-exec (%v)", err)
		}
		_ = f.Close()
	}
	if b, _ := os.ReadFile(filepath.Join(root, "events.jsonl")); string(b) != "one\ntwo\n" {
		t.Fatalf("appends were not preserved: %q", b)
	}
}

func TestOpenRegular_MissingFileIsErrNotExist(t *testing.T) {
	d, _ := newDir(t)
	_, err := d.OpenRegular("absent", os.O_RDONLY, 0, testFP())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	if errors.Is(err, ErrUnsafe) {
		t.Fatal("a missing file is not unsafe")
	}
}

func TestOpenRegular_RejectsNonPlainNames(t *testing.T) {
	d, _ := newDir(t)
	for _, name := range []string{"", ".", "..", "a/b", "../x", "/etc/passwd"} {
		if f, err := d.OpenRegular(name, os.O_RDONLY, 0, testFP()); err == nil {
			f.Close()
			t.Errorf("name %q must be rejected", name)
		}
	}
}

// A planted symlink in a PARENT component cannot redirect the open: the file
// is resolved relative to the pinned descriptor, so swapping the directory's
// path afterwards does not change what is opened.
func TestOpenRegular_ParentReplacementAfterPinningDoesNotRedirect(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "dso")
	mustMkdir(t, real, 0o750)
	d, err := Ensure(real, Policy{OwnerUID: euid()})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Swap the path for a symlink to an attacker directory after pinning.
	attacker := filepath.Join(root, "attacker")
	mustMkdir(t, attacker, 0o700)
	moved := filepath.Join(root, "dso-moved")
	if err := os.Rename(real, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(attacker, real); err != nil {
		t.Fatal(err)
	}

	f, err := d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := os.Lstat(filepath.Join(attacker, "events.jsonl")); err == nil {
		t.Fatal("the file was created in the attacker's directory: the path was re-resolved")
	}
	if _, err := os.Lstat(filepath.Join(moved, "events.jsonl")); err != nil {
		t.Fatalf("the file must be created in the originally pinned directory: %v", err)
	}
}

func TestOpenRegular_ConcurrentReplacementNeverRedirectsOrHangs(t *testing.T) {
	d, root := newDir(t)
	victim := filepath.Join(root, "victim")
	if err := os.WriteFile(victim, []byte("ORIGINAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "events.jsonl")

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // the attacker: keeps swapping what sits at the path
		defer wg.Done()
		i := 0
		for !stop.Load() {
			_ = os.Remove(target)
			// A hard link to the victim is deliberately NOT churned here: the
			// link-count check is made on the opened descriptor and a link that
			// is added and removed within that window cannot be caught by any
			// open-time check. Hard links are covered deterministically by
			// TestOpenRegular_RefusesAHardLinkedFile (a link that stays in
			// place is always refused); against churn the protection is the
			// 0750 directory plus the kernel's protected_hardlinks.
			switch i % 3 {
			case 0:
				_ = os.Symlink(victim, target)
			case 1:
				_ = syscall.Mkfifo(target, 0o600)
			case 2:
				_ = os.WriteFile(target, nil, 0o600)
			}
			i++
		}
	}()

	deadline := time.Now().Add(10 * time.Second)
	opened, refused := 0, 0
	for i := 0; i < 3000 && time.Now().Before(deadline); i++ {
		f, err := d.OpenRegular("events.jsonl", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
		if err != nil {
			refused++
			continue
		}
		// Whatever we got was validated on the descriptor itself. The attacker
		// may unlink it right after the open (link count 0), but it can never
		// be a different kind of object or gain extra links.
		var st unix.Stat_t
		if err := unix.Fstat(int(f.Fd()), &st); err != nil {
			t.Fatal(err)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink > 1 {
			t.Fatalf("an unvalidated object was returned: mode %#o nlink %d", st.Mode, st.Nlink)
		}
		opened++
		_ = f.Close()
	}
	stop.Store(true)
	wg.Wait()
	if time.Now().After(deadline) {
		t.Fatal("the loop did not finish in time: an open hung")
	}
	if b, _ := os.ReadFile(victim); string(b) != "ORIGINAL\n" {
		t.Fatalf("the victim file was modified through a planted link: %q", b)
	}
	t.Logf("opened %d validated files, refused %d unsafe objects", opened, refused)
}

func TestOpenRegular_ReadWriteConcurrency(t *testing.T) {
	d, _ := newDir(t)
	w, err := d.OpenRegular("log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600, testFP())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	const writers, perWriter = 4, 200
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if _, err := w.WriteString("line\n"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { // readers open and read while writers append
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r, err := d.OpenRegular("log", os.O_RDONLY, 0, testFP())
				if err != nil {
					t.Error(err)
					return
				}
				buf := make([]byte, 4096)
				_, _ = r.Read(buf)
				_ = r.Close()
			}
		}()
	}
	wg.Wait()
	fi, err := w.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != int64(writers*perWriter*len("line\n")) {
		t.Fatalf("size %d, want %d", fi.Size(), writers*perWriter*len("line\n"))
	}
}

// ---- special permission bits ----

func TestOpenRegular_RefusesSetuidSetgidAndStickyBits(t *testing.T) {
	cases := []struct {
		name string
		bits os.FileMode
	}{
		{"setuid", os.ModeSetuid},
		{"setgid", os.ModeSetgid},
		{"sticky", os.ModeSticky},
		{"setuid+setgid+sticky", os.ModeSetuid | os.ModeSetgid | os.ModeSticky},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, root := newDir(t)
			p := filepath.Join(root, "events.jsonl")
			if err := os.WriteFile(p, []byte("keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, 0o600|c.bits); err != nil {
				t.Skipf("cannot set %s on this filesystem/user: %v", c.name, err)
			}
			if fi, err := os.Lstat(p); err != nil || fi.Mode()&c.bits != c.bits {
				t.Skipf("the filesystem did not retain %s (mode %v): this case could not be exercised here", c.name, fi.Mode())
			}

			for _, flags := range []int{os.O_RDONLY, os.O_WRONLY | os.O_APPEND | os.O_CREATE} {
				f, err := d.OpenRegular("events.jsonl", flags, 0o600, testFP())
				wantUnsafe(t, f, err, c.name)
			}
			// the refused file is untouched: same content, same bits
			if b, _ := os.ReadFile(p); string(b) != "keep\n" {
				t.Fatalf("a refused file was modified: %q", b)
			}
			if fi, _ := os.Lstat(p); fi.Mode()&c.bits != c.bits || fi.Mode().Perm() != 0o600 {
				t.Fatalf("a refused file's mode was changed to %v", fi.Mode())
			}
		})
	}
}

func TestOpenRegular_OrdinaryRestrictiveModesStillWork(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400, 0o200} {
		d, root := newDir(t)
		p := filepath.Join(root, "events.jsonl")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		// open read-only for modes that allow it, otherwise just validate via a readable mode
		flags := os.O_RDONLY
		if mode&0o400 == 0 {
			flags = os.O_WRONLY | os.O_APPEND
		}
		if os.Geteuid() == 0 || mode&0o600 != 0 {
			f, err := d.OpenRegular("events.jsonl", flags, 0, testFP())
			if err != nil {
				t.Fatalf("mode %04o must be accepted: %v", mode, err)
			}
			_ = f.Close()
		}
	}
}

//go:build unix

package securefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/securefs/securefstest"
)

func mkdir(t *testing.T, parent, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // bypass umask
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	_ = os.Remove(p)
	if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err, substr)
	}
}

func TestCheckDirChain_SafeDirectoryIsAccepted(t *testing.T) {
	d := mkdir(t, securefstest.TempDir(t), "safe", 0o700)
	if err := CheckDirChain(d); err != nil {
		t.Fatal(err)
	}
	d2 := mkdir(t, securefstest.TempDir(t), "readable", 0o755) // readable by others but not writable: fine
	if err := CheckDirChain(d2); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDirChain_WritableDirectoriesAreRejected(t *testing.T) {
	for _, m := range []os.FileMode{0o777, 0o775, 0o770, 0o757, 0o722} {
		d := mkdir(t, securefstest.TempDir(t), "unsafe", m)
		wantErr(t, CheckDirChain(d), "writable by group or others")
	}
}

func TestCheckDirChain_UnsafeAncestorIsRejected(t *testing.T) {
	top := mkdir(t, securefstest.TempDir(t), "top", 0o777) // anyone can rename "inner" away and substitute their own
	inner := mkdir(t, top, "inner", 0o700)
	wantErr(t, CheckDirChain(inner), "writable by group or others")
}

func TestCheckDirChain_StickyAncestorOkButStickyParentIsNot(t *testing.T) {
	top := mkdir(t, securefstest.TempDir(t), "sticky", 0o777)
	if err := os.Chmod(top, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	inner := mkdir(t, top, "inner", 0o700)
	if err := CheckDirChain(inner); err != nil {
		t.Fatalf("sticky ancestor (like /tmp) should be accepted: %v", err)
	}
	// As the IMMEDIATE parent, a sticky world-writable directory still lets
	// others pre-create sibling files (e.g. SQLite -wal/-shm), so it is refused.
	wantErr(t, CheckDirChain(top), "writable by group or others")
}

func TestCheckDirChain_ForeignOwnerIsRejected(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: every directory is trusted-owned")
	}
	d := mkdir(t, securefstest.TempDir(t), "mine", 0o700)
	old := currentUID
	currentUID = func() int { return os.Geteuid() + 4242 } // pretend to be someone else
	defer func() { currentUID = old }()
	wantErr(t, CheckDirChain(d), "neither root nor the current user")
}

func TestCheckDirChain_Symlinks(t *testing.T) {
	root := securefstest.TempDir(t)
	real := mkdir(t, root, "real", 0o700)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	// A link to a safe directory, held in a safe directory: accepted.
	if err := CheckDirChain(link); err != nil {
		t.Fatalf("safe symlink rejected: %v", err)
	}
	// A link held in a group/other-writable directory could be re-pointed by
	// anyone: rejected even though its target is safe.
	open := mkdir(t, root, "open", 0o777)
	evil := filepath.Join(open, "evil")
	if err := os.Symlink(real, evil); err != nil {
		t.Fatal(err)
	}
	wantErr(t, CheckDirChain(evil), "writable by group or others")
	// A link to an unsafe target is rejected.
	badTarget := mkdir(t, root, "badtarget", 0o777)
	l2 := filepath.Join(root, "l2")
	if err := os.Symlink(badTarget, l2); err != nil {
		t.Fatal(err)
	}
	wantErr(t, CheckDirChain(l2), "writable by group or others")
	// Relative link, and a link in the middle of the path.
	if err := os.Symlink("real", filepath.Join(root, "rel")); err != nil {
		t.Fatal(err)
	}
	sub := mkdir(t, real, "sub", 0o700)
	_ = sub
	if err := CheckDirChain(filepath.Join(root, "rel", "sub")); err != nil {
		t.Fatalf("relative/mid-path symlink: %v", err)
	}
	// Loop.
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	os.Symlink(b, a)
	os.Symlink(a, b)
	wantErr(t, CheckDirChain(a), "too many symbolic links")
}

func TestCheckDirChain_MissingAndNonDirectory(t *testing.T) {
	d := securefstest.TempDir(t)
	if err := CheckDirChain(filepath.Join(d, "nope")); err == nil {
		t.Fatal("missing directory accepted")
	}
	f := writeFile(t, d, "f", 0o600)
	wantErr(t, CheckDirChain(filepath.Join(f, "x")), "not a directory")
}

func TestOpenPrivateFile(t *testing.T) {
	d := mkdir(t, securefstest.TempDir(t), "keys", 0o700)
	for _, m := range []os.FileMode{0o600, 0o400} {
		f, err := OpenPrivateFile(writeFile(t, d, "ok", m))
		if err != nil {
			t.Fatalf("mode %04o: %v", m, err)
		}
		f.Close()
	}
	for _, m := range []os.FileMode{0o640, 0o644, 0o604, 0o660, 0o666, 0o440} {
		_, err := OpenPrivateFile(writeFile(t, d, "bad", m))
		wantErr(t, err, "accessible by group or others")
	}
	if _, err := OpenPrivateFile(filepath.Join(d, "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := OpenPrivateFile(d); err == nil {
		t.Fatal("directory accepted as file")
	}
}

func TestOpenPrivateFile_SymlinkLeafIsRefused(t *testing.T) {
	d := mkdir(t, securefstest.TempDir(t), "keys", 0o700)
	target := writeFile(t, d, "target", 0o600)
	link := filepath.Join(d, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	_, err := OpenPrivateFile(link)
	wantErr(t, err, "symbolic link")
}

func TestOpenPrivateFile_ReplacementAfterSetupIsDetected(t *testing.T) {
	root := securefstest.TempDir(t)
	d := mkdir(t, root, "keys", 0o700)
	p := writeFile(t, d, "k", 0o400)
	f, err := OpenPrivateFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	// The directory later becomes writable by others (misconfiguration or an
	// attacker with the right to chmod): the very next open must refuse.
	if err := os.Chmod(d, 0o777); err != nil {
		t.Fatal(err)
	}
	_, err = OpenPrivateFile(p)
	wantErr(t, err, "writable by group or others")
	// ...and a file replaced by a looser one is refused too.
	if err := os.Chmod(d, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(p)
	writeFile(t, d, "k", 0o644)
	_, err = OpenPrivateFile(p)
	wantErr(t, err, "accessible by group or others")
}

func TestOpenPrivateFile_ForeignOwnerFileIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	d := mkdir(t, securefstest.TempDir(t), "keys", 0o700)
	p := writeFile(t, d, "k", 0o600)
	old := currentUID
	currentUID = func() int { return os.Geteuid() + 4242 }
	defer func() { currentUID = old }()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	wantErr(t, CheckOpenFile(f, p), "not owned by the current user")
}

func TestReadPrivateFileBounds(t *testing.T) {
	d := mkdir(t, securefstest.TempDir(t), "keys", 0o700)
	p := writeFile(t, d, "k", 0o600)
	b, err := ReadPrivateFile(p, 1024)
	if err != nil || string(b) != "data" {
		t.Fatalf("%q %v", b, err)
	}
	wantErr(t, func() error { _, err := ReadPrivateFile(p, 2); return err }(), "larger than")
}

func TestEnsureDir(t *testing.T) {
	root := mkdir(t, securefstest.TempDir(t), "root", 0o700)
	p := filepath.Join(root, "a", "b")
	if err := EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(root, "a"), p} {
		fi, _ := os.Stat(d)
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s created with mode %04o, want 0700", d, fi.Mode().Perm())
		}
	}
	// an existing unsafe directory is rejected, not silently chmod-ed
	bad := mkdir(t, root, "bad", 0o777)
	wantErr(t, EnsureDir(bad), "writable by group or others")
	if fi, _ := os.Stat(bad); fi.Mode().Perm() != 0o777 {
		t.Error("EnsureDir changed the mode of an existing directory")
	}
}

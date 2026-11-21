//go:build linux

package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// trustedDir returns a 0755 temp dir owned by the test user (a trusted owner).
func trustedDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func writePlugin(t *testing.T, dir, name string, content []byte, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // defeat umask
		t.Fatal(err)
	}
	return p
}

func writeMan(t *testing.T, dir, body string, mode os.FileMode) string {
	t.Helper()
	m := filepath.Join(dir, "hashes.txt")
	if err := os.WriteFile(m, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(m, mode)
	return m
}

func wantErr(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("want error containing %q, got %v", sub, err)
	}
}

// The core G012 property: replacing the file on disk after verification must
// not change what is executed. The launch descriptor still yields the verified
// bytes, and executing it runs the ORIGINAL binary.
func TestPrepareVerifiedLaunch_TamperAfterVerifyDoesNotAffectExecutedBytes(t *testing.T) {
	good, err := os.ReadFile("/bin/true")
	if err != nil {
		t.Skip("no /bin/true")
	}
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", good, 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha(good)+"\n", 0o644)

	afterVerifyHook = func() {
		// attacker replaces the path (rename over it) after verification
		evil := writePlugin(t, dir, "evil", []byte("#!/bin/sh\nexit 42\n"), 0o755)
		if err := os.Rename(evil, p); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { afterVerifyHook = nil }()

	tg, err := prepareVerifiedLaunch(m, p)
	if err != nil {
		t.Skipf("memfd launch unavailable here: %v", err)
	}
	defer tg.cleanup()

	on, _ := os.ReadFile(p)
	if string(on) == string(good) {
		t.Fatal("test setup: path was not tampered")
	}
	if _, err := tg.extraFiles[0].Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	held, _ := io.ReadAll(tg.extraFiles[0])
	if sha(held) != sha(good) {
		t.Fatal("launch descriptor does not hold the verified bytes")
	}
	cmd := exec.Command(tg.path)
	cmd.ExtraFiles = tg.extraFiles
	if err := cmd.Run(); err != nil {
		t.Fatalf("executing verified descriptor (original /bin/true) failed: %v", err)
	}
}

func TestPrepareVerifiedLaunch_SealedAgainstWrites(t *testing.T) {
	good := []byte("\x7fELFpayload")
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", good, 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha(good)+"\n", 0o644)
	tg, err := prepareVerifiedLaunch(m, p)
	if err != nil {
		t.Skipf("memfd launch unavailable here: %v", err)
	}
	defer tg.cleanup()
	// the child-visible descriptor cannot be re-opened for writing
	w, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", tg.extraFiles[0].Fd()), os.O_WRONLY, 0)
	if err == nil {
		_, werr := w.Write([]byte("x"))
		_ = w.Close()
		if werr == nil {
			t.Fatal("sealed memfd accepted a write")
		}
	}
}

func TestPrepareVerifiedLaunch_HashMismatch(t *testing.T) {
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("real"), 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("other"))+"\n", 0o644)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "hash mismatch")
}

func TestPrepareVerifiedLaunch_RejectsSymlinkBinary(t *testing.T) {
	dir := trustedDir(t)
	real := writePlugin(t, dir, "real", []byte("b"), 0o755)
	link := filepath.Join(dir, "dso-provider-x")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	_, err := prepareVerifiedLaunch(m, link)
	if err == nil {
		t.Fatal("symlink binary accepted")
	}
}

func TestPrepareVerifiedLaunch_RejectsHardlinkedBinary(t *testing.T) {
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	if err := os.Link(p, filepath.Join(dir, "alias")); err != nil {
		t.Skip("hard links unsupported")
	}
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "hard links")
}

func TestPrepareVerifiedLaunch_RejectsWritableBinary(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o757} {
		dir := trustedDir(t)
		p := writePlugin(t, dir, "dso-provider-x", []byte("b"), mode)
		m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
		_, err := prepareVerifiedLaunch(m, p)
		wantErr(t, err, "writable")
	}
}

func TestPrepareVerifiedLaunch_RejectsWritableManifest(t *testing.T) {
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o666)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "writable")
}

func TestPrepareVerifiedLaunch_RejectsSymlinkManifest(t *testing.T) {
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	real := filepath.Join(dir, "real.txt")
	_ = os.WriteFile(real, []byte("dso-provider-x="+sha([]byte("b"))+"\n"), 0o644)
	link := filepath.Join(dir, "hashes.txt")
	_ = os.Symlink(real, link)
	_, err := prepareVerifiedLaunch(link, p)
	if err == nil {
		t.Fatal("symlink manifest accepted")
	}
}

func TestPrepareVerifiedLaunch_RejectsWritablePluginDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "writable")
}

func TestPrepareVerifiedLaunch_RejectsWritableAncestor(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "plugins")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outer, 0o775); err != nil { // non-sticky group-writable ancestor
		t.Fatal(err)
	}
	p := writePlugin(t, inner, "dso-provider-x", []byte("b"), 0o755)
	m := writeMan(t, inner, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "writable")
}

func TestPrepareVerifiedLaunch_RejectsUntrustedOwner(t *testing.T) {
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	old := trustedOwner
	trustedOwner = func(uint32) bool { return false }
	defer func() { trustedOwner = old }()
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "owned by uid")
}

func TestPrepareVerifiedLaunch_RejectsNonRegularAndNonExecutable(t *testing.T) {
	dir := trustedDir(t)
	m := writeMan(t, dir, "dso-provider-x="+sha([]byte("b"))+"\n", 0o644)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o644)
	_, err := prepareVerifiedLaunch(m, p)
	wantErr(t, err, "executable")

	d2 := filepath.Join(dir, "dso-provider-y")
	_ = os.Mkdir(d2, 0o755)
	_, err = prepareVerifiedLaunch(m, d2)
	if err == nil {
		t.Fatal("directory accepted as plugin")
	}
}

func TestPrepareVerifiedLaunch_ManifestEntryProblems(t *testing.T) {
	good := sha([]byte("b"))
	cases := map[string]string{
		"duplicate": "dso-provider-x=" + good + "\ndso-provider-x=" + good + "\n",
		"missing":   "dso-provider-other=" + good + "\n",
		"malformed": "dso-provider-x=nothex\n",
		"uppercase": "dso-provider-x=" + strings.ToUpper(good) + "\n",
		"empty":     "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := trustedDir(t)
			p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
			m := writeMan(t, dir, body, 0o644)
			if _, err := prepareVerifiedLaunch(m, p); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestPrepareLaunch_SkipVerifyUsesPlainPath(t *testing.T) {
	t.Setenv("DSO_PLUGIN_SKIP_HASH_VERIFY", "1")
	tg, err := prepareLaunch("/nonexistent", "x", "dso-provider-x", "/nonexistent/dso-provider-x")
	if err != nil || tg.path != "/nonexistent/dso-provider-x" || len(tg.extraFiles) != 0 {
		t.Fatalf("skip path wrong: %+v %v", tg, err)
	}
}

func TestPrepareLaunch_FailureMentionsFix(t *testing.T) {
	t.Setenv("DSO_PLUGIN_SKIP_HASH_VERIFY", "")
	t.Setenv("DSO_PLUGIN_HASH_MANIFEST", "")
	dir := trustedDir(t)
	p := writePlugin(t, dir, "dso-provider-x", []byte("b"), 0o755)
	_, err := prepareLaunch(dir, "x", "dso-provider-x", p)
	wantErr(t, err, "system setup")
}

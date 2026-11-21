//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func withProcStatus(t *testing.T, path string) {
	t.Helper()
	old := procStatusPath
	procStatusPath = path
	t.Cleanup(func() { procStatusPath = old })
}

func writeStatus(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "status")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// procUmask: every unusable /proc/self/status reports ok=false (never a guess).
func TestProcUmask_FailureModesReportNotOK(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"file missing (no procfs / masked)":  filepath.Join(dir, "absent"),
		"path is a directory (read fails)":   dir,
		"Umask field missing (kernel < 4.7)": writeStatus(t, "Name:\tdso\nState:\tR\n"),
		"Umask malformed":                    writeStatus(t, "Umask:\tzz\n"),
		"Umask empty":                        writeStatus(t, "Umask:\t\n"),
		"Umask out of range":                 writeStatus(t, "Umask:\t01000\n"),
		"Umask decimal-looking invalid":      writeStatus(t, "Umask:\t0098\n"),
		"empty file":                         writeStatus(t, ""),
	}
	for name, p := range cases {
		withProcStatus(t, p)
		if v, ok := procUmask(); ok {
			t.Errorf("%s: procUmask = (%#o, true), want not ok", name, v)
		}
	}
	withProcStatus(t, writeStatus(t, "Name:\tx\nUmask:\t0027\nThreads:\t1\n"))
	if v, ok := procUmask(); !ok || v != 0o027 {
		t.Errorf("well-formed: got (%#o, %v), want (027, true)", v, ok)
	}
}

// With procfs unusable, withUmask uses the single atomic Umask(mask) and re-applies
// previous|mask: inside the scope the mask is NEVER looser than previous|077, it is
// never a 0777 probe value, and the previous mask is restored exactly (also on panic).
func TestWithUmask_ProcfsUnavailable_FallbackIsMonotonicAndRestores(t *testing.T) {
	orig := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(orig) })
	dir := t.TempDir()
	broken := map[string]string{
		"missing":   filepath.Join(dir, "absent"),
		"directory": dir,
		"no field":  writeStatus(t, "Name:\tx\n"),
		"malformed": writeStatus(t, "Umask:\tnope\n"),
	}
	prevs := []int{0o000, 0o022, 0o077, 0o277, 0o207}
	for name, p := range broken {
		withProcStatus(t, p)
		for _, prev := range prevs {
			unix.Umask(prev)
			want := prev | 0o077
			var inside int
			withUmask(0o077, func() {
				inside = unix.Umask(0o777) // test-only read; restored on the next line
				unix.Umask(inside)
			})
			if inside != want {
				t.Errorf("%s prev=%#o: mask inside scope = %#o, want %#o", name, prev, inside, want)
			}
			if got := unix.Umask(prev); got != prev {
				t.Errorf("%s prev=%#o: mask after scope = %#o, want restored", name, prev, got)
			}
		}
		// panic safety on the fallback path
		unix.Umask(0o277)
		func() {
			defer func() { _ = recover() }()
			withUmask(0o077, func() { panic("boom") })
		}()
		if got := unix.Umask(0o277); got != 0o277 {
			t.Errorf("%s: mask after panic = %#o, want 0277", name, got)
		}
		unix.Umask(0o022)
	}
}

// The fallback path still creates a socket with no group/other bits.
func TestListenUnixRestricted_ProcfsUnavailable(t *testing.T) {
	orig := unix.Umask(0o000)
	t.Cleanup(func() { unix.Umask(orig) })
	withProcStatus(t, filepath.Join(t.TempDir(), "absent"))
	p := filepath.Join(t.TempDir(), "s.sock")
	l, err := listenUnixRestricted(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	fi, _ := os.Stat(p)
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket mode %v has group/other bits", fi.Mode().Perm())
	}
	if got := unix.Umask(0o000); got != 0o000 {
		t.Fatalf("umask after = %#o", got)
	}
}

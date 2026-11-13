package injector

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// These tests execute the exact script that runs inside the container, against
// a local directory, so the file-delivery guarantees are verified for real
// (permissions, atomic replace, cleanup) rather than by string matching.

func needTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"sh", "mktemp", "base64", "sha256sum", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("tool %s unavailable", tool)
		}
	}
}

// runScript runs the injection script with the given stdin and returns the exit code.
func runScript(t *testing.T, dir, name string, uid, gid int, tmpfs bool, stdin string) (int, string) {
	t.Helper()
	argv := injectArgv(dir, name, uid, gid, tmpfs)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stderr = &out
	err := cmd.Run()
	if err == nil {
		return 0, out.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String()
	}
	t.Fatalf("run: %v", err)
	return -1, ""
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return n
}

func TestInjectScript_DeliversFileWith0400AndNoTemp(t *testing.T) {
	needTools(t)
	dir := filepath.Join(t.TempDir(), "dso")
	me := os.Getuid()
	code, msg := runScript(t, dir, "db_password", me, os.Getgid(), false, stdinPayload("s3cr3t-value"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, msg)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "db_password"))
	if string(b) != "s3cr3t-value" {
		t.Fatalf("content mismatch")
	}
	fi, _ := os.Stat(filepath.Join(dir, "db_password"))
	if fi.Mode().Perm() != 0o400 {
		t.Fatalf("mode %v, want 0400", fi.Mode().Perm())
	}
	if got := listDir(t, dir); len(got) != 1 {
		t.Fatalf("leftover files: %v", got)
	}
}

func TestInjectScript_EmptySecretIsDelivered(t *testing.T) {
	needTools(t)
	dir := filepath.Join(t.TempDir(), "dso")
	if code, msg := runScript(t, dir, "empty", os.Getuid(), os.Getgid(), false, stdinPayload("")); code != 0 {
		t.Fatalf("exit %d: %s", code, msg)
	}
	if fi, err := os.Stat(filepath.Join(dir, "empty")); err != nil || fi.Size() != 0 {
		t.Fatalf("empty secret not delivered: %v", err)
	}
}

func TestInjectScript_FailedDeliveryKeepsPreviousSecretAndLeavesNoTemp(t *testing.T) {
	needTools(t)
	dir := filepath.Join(t.TempDir(), "dso")
	me, mg := os.Getuid(), os.Getgid()
	if code, msg := runScript(t, dir, "tok", me, mg, false, stdinPayload("OLD")); code != 0 {
		t.Fatalf("%d %s", code, msg)
	}
	good := stdinPayload("NEW")
	lines := strings.SplitN(good, "\n", 3)
	cases := map[string]struct {
		stdin string
		want  int
	}{
		"checksum mismatch":    {lines[0] + "\n" + strings.Repeat("0", 64) + "\n", 69},
		"truncated, no sum":    {lines[0] + "\n", 68},
		"no input":             {"", 68},
		"invalid base64":       {"!!!notbase64!!!\n" + lines[1] + "\n", 1},
		"partial secret bytes": {lines[0][:len(lines[0])/2] + "\n" + lines[1] + "\n", -1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, _ := runScript(t, dir, "tok", me, mg, false, c.stdin)
			if code == 0 {
				t.Fatal("delivery must fail")
			}
			if c.want > 0 && code != c.want {
				t.Fatalf("exit %d, want %d", code, c.want)
			}
			b, _ := os.ReadFile(filepath.Join(dir, "tok"))
			if string(b) != "OLD" {
				t.Fatalf("previous secret was damaged: %q", b)
			}
			if got := listDir(t, dir); len(got) != 1 {
				t.Fatalf("temporary file left behind: %v", got)
			}
		})
	}
}

func TestInjectScript_RefusesUnsafeTargets(t *testing.T) {
	needTools(t)
	me, mg := os.Getuid(), os.Getgid()
	t.Run("destination is a directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "dso")
		_ = os.MkdirAll(filepath.Join(dir, "x"), 0o700)
		if code, _ := runScript(t, dir, "x", me, mg, false, stdinPayload("v")); code != 67 {
			t.Fatalf("exit %d, want 67", code)
		}
	})
	t.Run("secret directory is a symlink", func(t *testing.T) {
		base := t.TempDir()
		real := filepath.Join(base, "real")
		_ = os.MkdirAll(real, 0o700)
		link := filepath.Join(base, "dso")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		if code, _ := runScript(t, link, "x", me, mg, false, stdinPayload("v")); code != 65 {
			t.Fatalf("exit %d, want 65", code)
		}
		if got := listDir(t, real); len(got) != 0 {
			t.Fatalf("wrote through symlink: %v", got)
		}
	})
	t.Run("destination symlink is replaced, not followed", func(t *testing.T) {
		base := t.TempDir()
		dir := filepath.Join(base, "dso")
		_ = os.MkdirAll(dir, 0o700)
		victim := filepath.Join(base, "victim")
		_ = os.WriteFile(victim, []byte("untouched"), 0o600)
		_ = os.Symlink(victim, filepath.Join(dir, "x"))
		if code, msg := runScript(t, dir, "x", me, mg, false, stdinPayload("v")); code != 0 {
			t.Fatalf("exit %d %s", code, msg)
		}
		if b, _ := os.ReadFile(victim); string(b) != "untouched" {
			t.Fatalf("symlink target was overwritten: %q", b)
		}
	})
	for _, bad := range []string{"", ".", "..", "a/b", "../x", "x;touch pwned", "x$(id)", "x y", "x\ny", "-rf"} {
		bad := bad
		t.Run(fmt.Sprintf("name %q", bad), func(t *testing.T) {
			if bad == "-rf" { // a leading dash is a valid file name; it must still be written inside dir
				dir := filepath.Join(t.TempDir(), "dso")
				if code, msg := runScript(t, dir, bad, me, mg, false, stdinPayload("v")); code != 0 {
					t.Fatalf("%d %s", code, msg)
				}
				return
			}
			dir := filepath.Join(t.TempDir(), "dso")
			if code, _ := runScript(t, dir, bad, me, mg, false, stdinPayload("v")); code != 64 {
				t.Fatalf("exit %d, want 64", code)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "pwned")); err == nil {
				t.Fatal("shell injection executed")
			}
		})
	}
}

func TestInjectScript_RequireTmpfs(t *testing.T) {
	needTools(t)
	me, mg := os.Getuid(), os.Getgid()
	// A directory on a regular filesystem must be refused when tmpfs is required.
	disk, err := os.MkdirTemp(".", "inj")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(disk) }()
	abs, _ := filepath.Abs(disk)
	if fsIsTmpfs(abs) {
		t.Skip("working directory is on tmpfs; cannot test the non-tmpfs refusal here")
	}
	if code, _ := runScript(t, filepath.Join(abs, "dso"), "x", me, mg, true, stdinPayload("v")); code != 66 {
		t.Fatalf("non-tmpfs: exit %d, want 66", code)
	}
	if got := listDir(t, abs); len(got) > 1 { // only the (empty) dso dir may exist
		t.Fatalf("plaintext written to non-tmpfs: %v", got)
	}
	// On a tmpfs it succeeds.
	if !fsIsTmpfs("/dev/shm") {
		t.Skip("/dev/shm is not tmpfs")
	}
	shm, err := os.MkdirTemp("/dev/shm", "dsoinj")
	if err != nil {
		t.Skipf("cannot use /dev/shm: %v", err)
	}
	defer func() { _ = os.RemoveAll(shm) }()
	if code, msg := runScript(t, filepath.Join(shm, "dso"), "x", me, mg, true, stdinPayload("v")); code != 0 {
		t.Fatalf("tmpfs: exit %d %s", code, msg)
	}
}

func fsIsTmpfs(path string) bool {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	best, typ := -1, ""
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		m := f[1]
		if (path == m || strings.HasPrefix(path, strings.TrimSuffix(m, "/")+"/")) && len(m) > best {
			best, typ = len(m), f[2]
		}
	}
	return typ == "tmpfs"
}

func TestInjectScript_ConcurrentInjectionNeverCorrupts(t *testing.T) {
	needTools(t)
	dir := filepath.Join(t.TempDir(), "dso")
	me, mg := os.Getuid(), os.Getgid()
	if code, msg := runScript(t, dir, "k", me, mg, false, stdinPayload("v0")); code != 0 {
		t.Fatalf("%d %s", code, msg)
	}
	valid := map[string]bool{}
	var wg sync.WaitGroup
	for i := 1; i <= 24; i++ {
		v := strings.Repeat(fmt.Sprintf("value-%02d-", i), 200)
		valid[v] = true
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			if code, msg := runScript(t, dir, "k", me, mg, false, stdinPayload(v)); code != 0 {
				t.Errorf("exit %d %s", code, msg)
			}
		}(v)
	}
	wg.Wait()
	b, _ := os.ReadFile(filepath.Join(dir, "k"))
	if !valid[string(b)] {
		t.Fatalf("final content is not one complete written value (len %d)", len(b))
	}
	if got := listDir(t, dir); len(got) != 1 {
		t.Fatalf("temporary files left: %v", got)
	}
}

func TestInjectScript_SecretNeverInArgvAndModeSetBeforePublish(t *testing.T) {
	argv := strings.Join(injectArgv(secretDir, "f", 1000, 2000, true), " ")
	if strings.Contains(argv, "TOPSECRET") {
		t.Fatal("argv must not carry content")
	}
	sc := injectScript
	iu, im, ic, imv := strings.Index(sc, "umask 077"), strings.Index(sc, "mktemp"), strings.Index(sc, "chmod 0400"), strings.Index(sc, "mv -f")
	if iu < 0 || iu >= im || im >= ic || ic >= imv {
		t.Fatalf("order must be umask < mktemp < chmod < mv (got %d %d %d %d)", iu, im, ic, imv)
	}
	if ich := strings.Index(sc, `chown "$uid:$gid"`); ich <= 0 || ich >= imv {
		t.Fatal("chown must happen before publication")
	}
}

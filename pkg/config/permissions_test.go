//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermissionAdvisory(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		_ = os.Remove(p)
		if err := os.WriteFile(p, []byte("x: 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, m := range []os.FileMode{0o600, 0o400, 0o640, 0o440} {
		if got := PermissionAdvisory(mk("ok", m)); got != "" {
			t.Errorf("mode %04o should be acceptable, got advisory: %s", m, got)
		}
	}
	for _, m := range []os.FileMode{0o644, 0o664, 0o604, 0o666, 0o660, 0o620} {
		p := mk("bad", m)
		got := PermissionAdvisory(p)
		if got == "" {
			t.Errorf("mode %04o must produce an advisory", m)
			continue
		}
		if !strings.Contains(got, p) || !strings.Contains(got, "chmod") || !strings.Contains(got, "does not change") {
			t.Errorf("advisory lacks path/remediation: %s", got)
		}
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != m {
			t.Errorf("PermissionAdvisory changed the file mode: %04o -> %04o", m, fi.Mode().Perm())
		}
	}
	if PermissionAdvisory(filepath.Join(dir, "missing")) != "" {
		t.Error("missing file must not warn")
	}
	if PermissionAdvisory(dir) != "" {
		t.Error("directory must not warn")
	}
}

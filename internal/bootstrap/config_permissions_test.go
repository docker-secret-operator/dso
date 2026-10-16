package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// The config file can hold credentials (webhook hmac_secret / auth_token, the
// WebUI password_hash, provider auth parameters). These tests pin the
// permission policy so it cannot silently regress to world-readable.

func newConfigOps(t *testing.T) *BootstrapOperations {
	t.Helper()
	logger := &testLogger{}
	return NewBootstrapOperations(logger, NewFilesystemOps(logger, false), NewSystemdManager(logger, true), NewPermissionManager(logger, true))
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestTightenMode(t *testing.T) {
	cases := []struct {
		name         string
		current, lim os.FileMode
		want         os.FileMode
	}{
		{"group-writable world-readable (old default)", 0664, 0640, 0640},
		{"world-readable", 0644, 0640, 0640},
		{"group rw only", 0660, 0640, 0640},
		{"wide open", 0666, 0640, 0640},
		{"already at limit", 0640, 0640, 0640},
		{"stricter owner-only stays owner-only (never widened)", 0600, 0640, 0600},
		{"read-only owner stays", 0400, 0640, 0400},
		{"world-read only collapses to owner bits", 0604, 0640, 0600},
		{"setuid-style high bits are not carried", os.ModeSetuid | 0755, 0640, 0640 & 0755},
	}
	for _, tc := range cases {
		if got := tightenMode(tc.current, tc.lim); got != tc.want {
			t.Errorf("%s: tightenMode(%04o, %04o) = %04o, want %04o", tc.name, tc.current, tc.lim, got, tc.want)
		}
		// Invariant: the result never grants a bit that current lacks.
		if got := tightenMode(tc.current, tc.lim); got&^tc.current.Perm() != 0 {
			t.Errorf("%s: result %04o widens %04o", tc.name, got, tc.current.Perm())
		}
	}
}

func TestConfigModeTooPermissive_MatchesDoctorStandard(t *testing.T) {
	// Must agree with DSO-DOCTOR-009: ok = 0600/0640 (and stricter); not ok =
	// any world bit or any group bit other than read.
	for mode, tooPermissive := range map[os.FileMode]bool{
		0600: false, 0640: false, 0400: false, 0440: false, 0000: false,
		0644: true, 0664: true, 0660: true, 0604: true, 0666: true, 0750: true, 0620: true,
	} {
		if got := configModeTooPermissive(mode); got != tooPermissive {
			t.Errorf("configModeTooPermissive(%04o) = %v, want %v", mode, got, tooPermissive)
		}
	}
}

func TestDSOConfigFileModeConstantIsNotWorldAccessible(t *testing.T) {
	if dsoConfigFileMode&0007 != 0 {
		t.Fatalf("dsoConfigFileMode %04o grants world access", dsoConfigFileMode)
	}
	if dsoConfigFileMode&0020 != 0 {
		t.Fatalf("dsoConfigFileMode %04o grants group write", dsoConfigFileMode)
	}
}

// Fresh install: the new config is created without world or group-write bits,
// whatever the process umask is.
func TestWriteConfigOp_NewConfigIsNotWorldReadableOrGroupWritable(t *testing.T) {
	old := syscall.Umask(0) // most permissive umask: only the explicit mode can restrict
	defer syscall.Umask(old)

	configPath := filepath.Join(t.TempDir(), "dso.yaml")
	op, _ := newConfigOps(t).WriteConfigOp(configPath, []byte("agent:\n  watch:\n    webhook:\n      hmac_secret: not-a-real-secret\n"))
	if err := op.Execute(context.Background()); err != nil {
		t.Fatalf("op.Execute: %v", err)
	}
	if got := modeOf(t, configPath); got != 0640 {
		t.Errorf("new config mode = %04o, want 0640", got)
	}
}

// Upgrade/re-run: rewriting an existing 0664 or 0644 config tightens it.
func TestWriteConfigOp_RewritingExistingLooseConfigTightensIt(t *testing.T) {
	for _, prior := range []os.FileMode{0664, 0644, 0666} {
		configPath := filepath.Join(t.TempDir(), "dso.yaml")
		if err := os.WriteFile(configPath, []byte("old: true\n"), prior); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(configPath, prior); err != nil { // defeat umask
			t.Fatal(err)
		}
		op, _ := newConfigOps(t).WriteConfigOp(configPath, []byte("new: true\n"))
		if err := op.Execute(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := modeOf(t, configPath); got&^0640 != 0 {
			t.Errorf("rewrite of a %04o config left mode %04o (must be <= 0640)", prior, got)
		}
	}
}

// Rollback restores the previous content but must not re-expose a file that
// holds credentials, and must not widen a deliberately strict mode.
func TestWriteConfigOp_RollbackNeverRestoresLooseOrWidensStrictMode(t *testing.T) {
	cases := []struct {
		prior os.FileMode
		want  os.FileMode
	}{
		{0664, 0640}, {0644, 0640}, {0666, 0640}, // loose prior mode is NOT restored
		{0640, 0640}, {0600, 0600}, // already strict: kept exactly (0600 is not widened to 0640)
	}
	for _, tc := range cases {
		configPath := filepath.Join(t.TempDir(), "dso.yaml")
		original := []byte("original: true\n")
		if err := os.WriteFile(configPath, original, tc.prior); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(configPath, tc.prior); err != nil {
			t.Fatal(err)
		}
		op, rollback := newConfigOps(t).WriteConfigOp(configPath, []byte("new: true\n"))
		ctx := context.Background()
		if err := op.Execute(ctx); err != nil {
			t.Fatal(err)
		}
		if err := rollback(ctx); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(configPath)
		if string(b) != string(original) {
			t.Errorf("prior %04o: content not restored: %q", tc.prior, b)
		}
		if got := modeOf(t, configPath); got != tc.want {
			t.Errorf("prior %04o: rollback mode = %04o, want %04o", tc.prior, got, tc.want)
		}
	}
}

// The write is still atomic: no temp file is left next to the config, and a
// failed write leaves the previous file untouched.
func TestWriteConfigOp_LeavesNoTempFilesAndIsAtomic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "dso.yaml")
	op, _ := newConfigOps(t).WriteConfigOp(configPath, []byte("a: 1\n"))
	if err := op.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != "dso.yaml" {
		t.Errorf("unexpected files next to config: %v", ents)
	}
}

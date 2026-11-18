package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Re-running `dso bootstrap` on a configured host used to overwrite its dso.yaml (the old
// content was kept only in memory for rollback on failure). Force was set to true and never
// read, so nothing stopped it. Replacing an existing configuration now needs the explicit
// ReplaceExistingConfig opt-in, and takes a backup first.

func localBootstrapAt(t *testing.T, path string, replace bool) error {
	t.Helper()
	isolateHome(t)
	opts := &BootstrapOptions{
		Mode: ModeLocal, Provider: "vault", NonInteractive: true, VaultAddress: "https://vault.local",
		ReplaceExistingConfig: replace,
		Context:               context.WithValue(context.Background(), ConfigPathContextKey, path),
	}
	_, err := NewLocalBootstrapper(&MockLogger{}, opts).Bootstrap(context.Background(), opts)
	return err
}

const precious = "agent:\n  owner_id: dso-owner-keep-me\nproviders:\n  prod-vault:\n    type: vault\n"

func TestLocalBootstrap_ExistingConfig_IsNotOverwrittenWithoutTheOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dso.yaml")
	if err := os.WriteFile(path, []byte(precious), 0o600); err != nil {
		t.Fatal(err)
	}
	err := localBootstrapAt(t, path, false)
	if err == nil {
		t.Fatal("bootstrap must refuse to overwrite an existing configuration")
	}
	if !strings.Contains(err.Error(), "--replace-config") || !strings.Contains(err.Error(), path) {
		t.Fatalf("the refusal must name the file and the opt-in, got: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != precious {
		t.Fatalf("the existing configuration was modified:\n%s", got)
	}
	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Fatalf("a refusal must not leave backups behind: %v", matches)
	}
}

func TestLocalBootstrap_ExistingConfig_OptInBacksUpThenReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dso.yaml")
	if err := os.WriteFile(path, []byte(precious), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := localBootstrapAt(t, path, true); err != nil {
		t.Fatalf("an opted-in replacement must succeed: %v", err)
	}
	backups, _ := filepath.Glob(path + ".bak-*")
	if len(backups) != 1 {
		t.Fatalf("want exactly one backup next to the config, got %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != precious {
		t.Fatalf("the backup must hold the previous bytes, got:\n%s", b)
	}
	if info, err := os.Stat(backups[0]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the backup may hold credentials: want mode 0600, got %v (%v)", info.Mode().Perm(), err)
	}
	if now, _ := os.ReadFile(path); string(now) == precious {
		t.Fatal("the configuration was not replaced")
	}
}

func TestLocalBootstrap_FirstTime_IsUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dso.yaml")
	if err := localBootstrapAt(t, path, false); err != nil {
		t.Fatalf("a first-time bootstrap must succeed: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if matches, _ := filepath.Glob(path + ".bak-*"); len(matches) != 0 {
		t.Fatalf("a first-time bootstrap has nothing to back up: %v", matches)
	}
}

// The shared helper: edge cases that the end-to-end tests do not reach (the agent flow needs
// systemd, so its early refusal and pre-transaction backup are pinned here).
func TestRefuseExistingConfig_Cases(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "none.yaml")
	if err := refuseExistingConfig(missing, false); err != nil {
		t.Fatalf("a missing file is a first-time install: %v", err)
	}
	file := filepath.Join(dir, "dso.yaml")
	if err := os.WriteFile(file, []byte("x: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := refuseExistingConfig(file, false); !errors.Is(err, ErrExistingConfig) {
		t.Fatalf("an existing file without the opt-in must be refused with ErrExistingConfig, got %v", err)
	}
	if err := refuseExistingConfig(file, true); err != nil {
		t.Fatalf("the opt-in allows replacing a regular file: %v", err)
	}
	// Never write through a symlink or over a directory, opt-in or not.
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if refuseExistingConfig(link, true) == nil {
		t.Fatal("a symlink at the config path must be refused even with the opt-in")
	}
	if refuseExistingConfig(dir, true) == nil {
		t.Fatal("a directory at the config path must be refused even with the opt-in")
	}
}

func TestBackupExistingConfig_NeverOverwritesAnEarlierBackup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dso.yaml")
	if err := os.WriteFile(file, []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b1, err := backupExistingConfig(file)
	if err != nil || b1 == "" {
		t.Fatalf("first backup: %q %v", b1, err)
	}
	if err := os.WriteFile(file, []byte("v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b2, err := backupExistingConfig(file) // same second: the name must not collide
	if err != nil || b2 == "" || b2 == b1 {
		t.Fatalf("second backup must get its own name: %q vs %q (%v)", b2, b1, err)
	}
	if got, _ := os.ReadFile(b1); string(got) != "v1\n" {
		t.Fatalf("the earlier backup was overwritten: %q", got)
	}
	if b, err := backupExistingConfig(filepath.Join(t.TempDir(), "absent.yaml")); b != "" || err != nil {
		t.Fatalf("no file means no backup and no error: %q %v", b, err)
	}
}

// Force only skips prompts; the non-interactive paths even set it unconditionally. It must
// never authorise replacing an existing configuration.
func TestLocalBootstrap_ForceAndNonInteractive_DoNotAuthoriseReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dso.yaml")
	if err := os.WriteFile(path, []byte(precious), 0o600); err != nil {
		t.Fatal(err)
	}
	isolateHome(t)
	opts := &BootstrapOptions{
		Mode: ModeLocal, Provider: "vault", NonInteractive: true, Force: true, VaultAddress: "https://vault.local",
		Context: context.WithValue(context.Background(), ConfigPathContextKey, path),
	}
	_, err := NewLocalBootstrapper(&MockLogger{}, opts).Bootstrap(context.Background(), opts)
	if !errors.Is(err, ErrExistingConfig) && (err == nil || !strings.Contains(err.Error(), "--replace-config")) {
		t.Fatalf("Force + NonInteractive must still be refused, got: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != precious {
		t.Fatalf("the existing configuration was modified despite Force:\n%s", got)
	}
}

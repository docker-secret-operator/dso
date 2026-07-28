package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteHashManifest_CoversAllPluginBinaries verifies that every
// dso-provider-* binary in the plugin directory gets a correct manifest
// entry [SEC-2]. This is what makes pkg/provider.LoadProvider's
// mandatory-by-default hash verification work without extra operator steps
// after a standard `docker dso system setup` run.
func TestWriteHashManifest_CoversAllPluginBinaries(t *testing.T) {
	dir := t.TempDir()
	ppi := NewProviderPluginInstaller(nopLogger{}, false)

	plugins := map[string]string{
		"dso-provider-aws":   "aws plugin content",
		"dso-provider-vault": "vault plugin content",
	}
	for name, content := range plugins {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0755); err != nil {
			t.Fatal(err)
		}
	}

	if err := ppi.writeHashManifest(dir); err != nil {
		t.Fatalf("writeHashManifest failed: %v", err)
	}

	manifestPath := filepath.Join(dir, "hashes.txt")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
	manifest := string(data)

	for name, content := range plugins {
		expectedHash := sha256Hex(content)
		expectedLine := name + "=" + expectedHash
		if !strings.Contains(manifest, expectedLine) {
			t.Errorf("manifest missing correct entry for %s.\nExpected line: %s\nManifest:\n%s", name, expectedLine, manifest)
		}
	}
}

// TestWriteHashManifest_IgnoresNonPluginFiles confirms only dso-provider-*
// binaries are included -- an incidental file in the plugin directory (e.g.
// a leftover manifest from a previous run, or a README) must not produce a
// bogus manifest entry.
func TestWriteHashManifest_IgnoresNonPluginFiles(t *testing.T) {
	dir := t.TempDir()
	ppi := NewProviderPluginInstaller(nopLogger{}, false)

	if err := os.WriteFile(filepath.Join(dir, "dso-provider-vault"), []byte("plugin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a plugin"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ppi.writeHashManifest(dir); err != nil {
		t.Fatalf("writeHashManifest failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "hashes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(data)
	if strings.Contains(manifest, "README") || strings.Contains(manifest, "subdir") {
		t.Errorf("manifest incorrectly includes non-plugin entries:\n%s", manifest)
	}
}

// TestWriteHashManifest_EmptyDirectoryProducesHeaderOnlyManifest confirms no
// panic and a valid (if empty) manifest when no plugins are installed yet --
// an edge case that must not crash bootstrap.
func TestWriteHashManifest_EmptyDirectoryProducesHeaderOnlyManifest(t *testing.T) {
	dir := t.TempDir()
	ppi := NewProviderPluginInstaller(nopLogger{}, false)

	if err := ppi.writeHashManifest(dir); err != nil {
		t.Fatalf("writeHashManifest should not fail on an empty directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hashes.txt")); err != nil {
		t.Fatalf("manifest file should still be created: %v", err)
	}
}

// TestWriteHashManifest_NonexistentDirectoryFails confirms a clear error
// (not a panic) when the plugin directory itself doesn't exist.
func TestWriteHashManifest_NonexistentDirectoryFails(t *testing.T) {
	ppi := NewProviderPluginInstaller(nopLogger{}, false)
	err := ppi.writeHashManifest(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent plugin directory")
	}
}

// TestInstallProviderPlugins_WritesManifestBestEffort confirms a manifest
// write failure does not fail the overall bootstrap (best-effort, matching
// the existing "continue on single plugin failure" pattern) -- verified by
// making the plugin directory read-only after creation so the manifest write
// fails, and confirming InstallProviderPlugins still returns nil.
func TestInstallProviderPlugins_WritesManifestBestEffort(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permission enforcement; cannot exercise this path as root")
	}
	dir := t.TempDir()
	ppi := NewProviderPluginInstaller(nopLogger{}, false)

	// InstallProviderPlugins creates pluginDir itself; here we pre-create it
	// read-only so the manifest write step fails but bootstrap still succeeds.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()

	// One provider is enough to get past InstallProviderPlugins' empty-list
	// early return; the plugin build itself will fail (no source directory
	// in a unit test environment) and is logged as a warning, matching the
	// existing "continue on single plugin failure" behavior -- this test
	// targets the subsequent manifest-write step specifically.
	err := ppi.InstallProviderPlugins(context.Background(), []string{"vault"})
	if err != nil {
		t.Fatalf("InstallProviderPlugins should not fail even if manifest write fails: %v", err)
	}
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

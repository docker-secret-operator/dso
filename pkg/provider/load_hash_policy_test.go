package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupPluginDir writes a fake plugin binary at <dir>/<pluginName> and
// returns dir. The content is never actually executed by these tests --
// enforceHashVerification only hashes it.
func setupPluginDir(t *testing.T, pluginName, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, pluginName), []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestEnforceHashVerification_MandatoryByDefault is the core SEC-2 regression
// test: with no DSO_PLUGIN_HASH_MANIFEST set and no manifest file present at
// the default location, verification must FAIL (fail closed), not silently
// pass. The pre-SEC-2 implementation gated verification entirely behind
// DSO_PLUGIN_HASH_MANIFEST being set -- this exact scenario (env var unset,
// no manifest anywhere) would have returned nil (success) under the old
// code, which is precisely the gap SEC-2 closes.
func TestEnforceHashVerification_MandatoryByDefault(t *testing.T) {
	dir := setupPluginDir(t, "dso-provider-test", "binary content")
	pluginPath := filepath.Join(dir, "dso-provider-test")

	// No DSO_PLUGIN_HASH_MANIFEST set, no hashes.txt written -- this is
	// exactly today's real-world default state for any deployment that
	// hasn't manually configured verification.
	err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath)
	if err == nil {
		t.Fatal("SEC-2 REGRESSION: hash verification passed with no manifest present and no env var set -- " +
			"verification must fail closed by default")
	}
	if !strings.Contains(err.Error(), "hash verification") {
		t.Errorf("expected a hash verification error, got: %v", err)
	}
}

// TestEnforceHashVerification_DefaultManifestPath confirms verification
// succeeds when a valid manifest exists at <pluginDir>/hashes.txt with no
// DSO_PLUGIN_HASH_MANIFEST override -- proving the new default-path
// resolution works, not just that missing-manifest correctly fails.
func TestEnforceHashVerification_DefaultManifestPath(t *testing.T) {
	content := "binary content"
	dir := setupPluginDir(t, "dso-provider-test", content)
	pluginPath := filepath.Join(dir, "dso-provider-test")
	hash := hashBytes([]byte(content))

	manifestPath := filepath.Join(dir, defaultManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("dso-provider-test="+hash+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath); err != nil {
		t.Fatalf("expected success with valid manifest at default path, got: %v", err)
	}
}

// TestEnforceHashVerification_EnvVarOverridesDefaultPath confirms
// DSO_PLUGIN_HASH_MANIFEST still works as a path override, not as an on/off
// switch -- an env var pointing to a valid manifest elsewhere must succeed
// even though no manifest exists at the default <pluginDir>/hashes.txt path.
func TestEnforceHashVerification_EnvVarOverridesDefaultPath(t *testing.T) {
	content := "binary content"
	dir := setupPluginDir(t, "dso-provider-test", content)
	pluginPath := filepath.Join(dir, "dso-provider-test")
	hash := hashBytes([]byte(content))

	elsewhere := t.TempDir()
	manifestPath := filepath.Join(elsewhere, "custom-manifest.txt")
	if err := os.WriteFile(manifestPath, []byte("dso-provider-test="+hash+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSO_PLUGIN_HASH_MANIFEST", manifestPath)

	if err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath); err != nil {
		t.Fatalf("expected success with env-var-overridden manifest path, got: %v", err)
	}
}

// TestEnforceHashVerification_HashMismatchStillFails confirms a tampered
// binary is rejected even with a manifest present at the default path --
// mandatory-by-default must not weaken the existing mismatch detection.
func TestEnforceHashVerification_HashMismatchStillFails(t *testing.T) {
	dir := setupPluginDir(t, "dso-provider-test", "real content")
	pluginPath := filepath.Join(dir, "dso-provider-test")
	wrongHash := hashBytes([]byte("different content"))

	manifestPath := filepath.Join(dir, defaultManifestFileName)
	if err := os.WriteFile(manifestPath, []byte("dso-provider-test="+wrongHash+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath)
	if err == nil {
		t.Fatal("expected hash mismatch to be rejected")
	}
}

// TestEnforceHashVerification_SkipEscapeHatch confirms
// DSO_PLUGIN_SKIP_HASH_VERIFY bypasses verification entirely, including when
// no manifest exists at all -- this is the explicit, auditable escape hatch,
// distinct from the old implicit "unset env var = skip" default.
func TestEnforceHashVerification_SkipEscapeHatch(t *testing.T) {
	dir := setupPluginDir(t, "dso-provider-test", "binary content")
	pluginPath := filepath.Join(dir, "dso-provider-test")

	for _, val := range []string{"1", "true", "TRUE", "yes", "Yes"} {
		t.Run("value_"+val, func(t *testing.T) {
			t.Setenv("DSO_PLUGIN_SKIP_HASH_VERIFY", val)
			if err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath); err != nil {
				t.Errorf("expected skip escape hatch (%q) to bypass verification, got: %v", val, err)
			}
		})
	}
}

// TestEnforceHashVerification_SkipEscapeHatchRejectsGarbageValues confirms
// only recognized truthy values activate the skip -- an unset or malformed
// value must NOT silently bypass verification, preserving fail-closed
// behavior for anything other than a deliberate, correctly-spelled opt-out.
func TestEnforceHashVerification_SkipEscapeHatchRejectsGarbageValues(t *testing.T) {
	dir := setupPluginDir(t, "dso-provider-test", "binary content")
	pluginPath := filepath.Join(dir, "dso-provider-test")

	for _, val := range []string{"", "0", "false", "no", "enabled", " "} {
		t.Run("value_"+val, func(t *testing.T) {
			t.Setenv("DSO_PLUGIN_SKIP_HASH_VERIFY", val)
			err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath)
			if err == nil {
				t.Errorf("value %q must NOT bypass verification (no manifest present), but it did", val)
			}
		})
	}
}

// TestEnforceHashVerification_ErrorMentionsEscapeHatchAndFix confirms the
// failure message is actionable: it must point at both the fix (regenerate
// via setup) and the escape hatch, matching this codebase's established
// error-message convention (see the "not installed" error in LoadProvider).
func TestEnforceHashVerification_ErrorMentionsEscapeHatchAndFix(t *testing.T) {
	dir := setupPluginDir(t, "dso-provider-test", "binary content")
	pluginPath := filepath.Join(dir, "dso-provider-test")

	err := enforceHashVerification(dir, "test", "dso-provider-test", pluginPath)
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "docker dso system setup") {
		t.Errorf("error should mention the setup fix command: %s", msg)
	}
	if !strings.Contains(msg, "DSO_PLUGIN_SKIP_HASH_VERIFY") {
		t.Errorf("error should mention the escape hatch: %s", msg)
	}
}

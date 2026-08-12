package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyFixture copies a testdata/migrate/<name> fixture directory's files
// into dir, so tests can chdir into an isolated working copy without ever
// risking a write to the checked-in fixtures themselves.
func copyFixture(t *testing.T, name, dir string) {
	t.Helper()
	src := filepath.Join("testdata", "migrate", name)
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", src, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name())) // #nosec G304 -- fixed testdata path
		if err != nil {
			t.Fatalf("read fixture file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0600); err != nil {
			t.Fatalf("write fixture copy: %v", err)
		}
	}
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = orig }()

	go func() {
		_, _ = w.WriteString(input)
		_ = w.Close()
	}()
	fn()
}

func setupIsolatedProject(t *testing.T, fixture string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	copyFixture(t, fixture, dir)
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir()) // isolate vault; never touches the real ~/.dso
	return dir
}

// ── the critical invariant: dry-run mutates nothing ─────────────────────

func TestRunMigrate_DryRun_ZeroMutation(t *testing.T) {
	dir := setupIsolatedProject(t, "basic")

	envBefore, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeBefore, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))

	captured := captureStdout(t, func() {
		err := runMigrate(migrateOptions{dryRun: true, envFileFlag: ".env"})
		if err != nil {
			t.Fatalf("dry-run returned an unexpected error: %v", err)
		}
	})

	envAfter, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeAfter, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if string(envBefore) != string(envAfter) {
		t.Fatal("dry-run must never modify .env")
	}
	if string(composeBefore) != string(composeAfter) {
		t.Fatal("dry-run must never modify docker-compose.yml")
	}
	if _, err := os.Stat(filepath.Join(dir, migratedComposeFilename)); err == nil {
		t.Fatal("dry-run must never create docker-compose.dso.yml")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".dso")); err == nil {
		t.Fatal("dry-run must never initialize or touch the vault directory")
	}
	if !strings.Contains(captured, "No files changed.") || !strings.Contains(captured, "No secrets imported.") {
		t.Errorf("expected dry-run to explicitly state nothing changed, got:\n%s", captured)
	}
}

// ── declining the interactive prompt is equivalent to dry-run in effect ──

func TestRunMigrate_InteractiveDecline_NoMutation(t *testing.T) {
	dir := setupIsolatedProject(t, "basic")

	var err error
	withStdin(t, "n\n", func() {
		captureStdout(t, func() {
			err = runMigrate(migrateOptions{envFileFlag: ".env"})
		})
	})
	if err != nil {
		t.Fatalf("declining must not be treated as an error: %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(dir, migratedComposeFilename)); statErr == nil {
		t.Fatal("declining the prompt must not create docker-compose.dso.yml")
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("HOME"), ".dso")); statErr == nil {
		t.Fatal("declining the prompt must not initialize the vault")
	}
}

// ── --confirm actually performs the migration ────────────────────────────

func TestRunMigrate_Confirm_PerformsMigration(t *testing.T) {
	dir := setupIsolatedProject(t, "basic")

	// Capture the working copy's content *before* migrating -- not the
	// checked-in testdata fixture, since the test process has already
	// t.Chdir'd away from the package root by this point.
	envBefore, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeBefore, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))

	captured := captureStdout(t, func() {
		err := runMigrate(migrateOptions{confirm: true, envFileFlag: ".env"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	out, err := os.ReadFile(filepath.Join(dir, migratedComposeFilename))
	if err != nil {
		t.Fatalf("expected %s to be created: %v", migratedComposeFilename, err)
	}
	if !strings.Contains(string(out), "dso://") {
		t.Fatalf("expected a dso:// reference in the generated compose file, got:\n%s", out)
	}

	envAfter, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeAfter, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if string(envAfter) != string(envBefore) {
		t.Error("the original .env must never be modified, even on a successful migration")
	}
	if string(composeAfter) != string(composeBefore) {
		t.Error("the original docker-compose.yml must never be modified, even on a successful migration")
	}

	if !strings.Contains(captured, "✓ Imported: 1") {
		t.Errorf("expected the summary to report 1 import, got:\n%s", captured)
	}
}

// Partial-import failure semantics (one key fails, others still succeed,
// no secret value leaks into the error) are covered directly and more
// precisely at the applySecrets layer -- see
// TestApplySecrets_PartialFailure_OthersStillProcessed in
// migrate_apply_test.go. An end-to-end equivalent isn't meaningfully
// constructible here: every per-key failure mode vault.Set exposes
// (empty key, "..", oversized value) is already rejected earlier by
// parseDotEnv itself, so the key never reaches applySecrets in the first
// place -- which is itself a consistency property of the pipeline worth
// noting, not a gap in coverage.

// ── security invariant: no secret value ever printed ─────────────────────

func TestRunMigrate_NoSecretValueEverPrinted(t *testing.T) {
	setupIsolatedProject(t, "basic")

	captured := captureStdout(t, func() {
		_ = runMigrate(migrateOptions{confirm: true, envFileFlag: ".env"})
	})

	if strings.Contains(captured, "hunter2") {
		t.Fatalf("the .env secret value must never appear in migrate output:\n%s", captured)
	}
}

// ── clear diagnostics for missing inputs ──────────────────────────────────

func TestRunMigrate_MissingEnv_ClearError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("docker-compose.yml", []byte("services: {}\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := runMigrate(migrateOptions{envFileFlag: ".env"})
	if err == nil {
		t.Fatal("expected an error when .env is missing")
	}
	if !strings.Contains(err.Error(), ".env") {
		t.Errorf("expected the error to mention .env, got: %v", err)
	}
}

func TestRunMigrate_MissingCompose_ClearError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(".env", []byte("DB_PASSWORD=hunter2\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := runMigrate(migrateOptions{envFileFlag: ".env"})
	if err == nil {
		t.Fatal("expected an error when no compose file is found")
	}
}

// ── environment-isolation regression test ─────────────────────────────────
//
// A manual (non-test) smoke run of `docker dso migrate --confirm` during
// this feature's development wrote real files to the operator's actual
// $HOME/.dso -- an `export HOME=...` set in one shell invocation did not
// carry over to the next command invocation, so the confirm run fell back
// to the real, ambient $HOME instead of the intended isolated one. No real
// secret was exposed (the vault held only fake fixture values), but it was
// a genuine environment-integrity failure: a secret-management tool must
// never touch state outside the location it was explicitly pointed at.
//
// This test is the permanent guard against a regression of that class of
// bug specifically -- not just "vault operations are isolatable in tests"
// (already implied by every other test using t.Setenv), but the explicit,
// named invariant: running a full migration must provably leave whatever
// $HOME/.dso existed *before* the test untouched. It captures real $HOME's
// .dso state (if any) before isolating, runs a full confirmed migration
// against an isolated HOME, and then asserts the real HOME's .dso
// directory is byte-for-byte unchanged -- failing loudly, not silently,
// if a future change ever causes migration to fall back to ambient state.
func TestRunMigrate_RealHomeDirectoryNeverTouched(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine real HOME to guard: %v", err)
	}
	realDSODir := filepath.Join(realHome, ".dso")
	before := snapshotDir(t, realDSODir)

	setupIsolatedProject(t, "basic")
	isolatedHome := os.Getenv("HOME")
	if isolatedHome == "" || isolatedHome == realHome {
		t.Fatal("test setup did not actually isolate HOME -- refusing to run a mutating migration")
	}

	captureStdout(t, func() {
		if err := runMigrate(migrateOptions{confirm: true, envFileFlag: ".env"}); err != nil {
			t.Fatalf("migrate failed: %v", err)
		}
	})

	// The isolated HOME must have received the vault -- proves the
	// migration actually ran and wrote *somewhere*, not that it silently
	// no-op'd in a way that would make the "real HOME untouched" assertion
	// meaningless.
	if _, err := os.Stat(filepath.Join(isolatedHome, ".dso", "vault.enc")); err != nil {
		t.Fatalf("expected the isolated HOME to receive the new vault, got: %v", err)
	}

	after := snapshotDir(t, realDSODir)
	if before != after {
		t.Fatalf("REGRESSION: the real $HOME/.dso (%s) changed during an isolated migrate run.\nBefore:\n%s\nAfter:\n%s",
			realDSODir, before, after)
	}
}

// snapshotDir returns a stable, comparable description of dir's immediate
// contents (name, size, mtime per entry) or "<absent>" if dir doesn't
// exist. Good enough to detect any file added, removed, or modified by a
// test run, without needing to hash file contents.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "<absent>"
	}
	var b strings.Builder
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			fmt.Fprintf(&b, "%s: <stat error: %v>\n", e.Name(), err)
			continue
		}
		fmt.Fprintf(&b, "%s size=%d mtime=%s\n", e.Name(), info.Size(), info.ModTime())
	}
	return b.String()
}

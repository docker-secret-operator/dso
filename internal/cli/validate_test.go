package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── command shape ─────────────────────────────────────────────────────────

func TestNewValidateCmd_Flags(t *testing.T) {
	cmd := NewValidateCmd()
	if cmd.Use != "validate" {
		t.Fatalf("expected 'validate', got %q", cmd.Use)
	}
	if cmd.Flag("json") == nil {
		t.Fatal("expected --json flag")
	}
	if cmd.Flag("file") == nil {
		t.Fatal("expected --file flag")
	}
}

// ── exit codes ────────────────────────────────────────────────────────────

// assertNewCategoriesPass checks that validate's own new check categories
// (Compose/References/Secrets -- what Step 3 actually built) contain no
// failures in output, without asserting on the full command exit code.
// The full aggregate also includes Configuration/Provider checks reused
// from doctor's engine (Step 1, already tested there), whose result
// legitimately depends on whatever DSO configuration exists on the host
// running the test -- not something these tests should be sensitive to.
func assertNewCategoriesPass(t *testing.T, output string) {
	t.Helper()
	for _, section := range []string{"Compose", "References", "Secrets"} {
		idx := strings.Index(output, "\n"+section+"\n")
		if idx == -1 {
			continue // section absent (e.g. no references) is fine
		}
		rest := output[idx+len(section)+2:]
		if end := strings.Index(rest, "\n\n"); end != -1 {
			rest = rest[:end]
		}
		if strings.Contains(rest, "✗") {
			t.Errorf("expected no failing checks in the %s section, got:\n%s", section, rest)
		}
	}
}

func TestRunValidate_ValidProject_NewChecksPass(t *testing.T) {
	setupIsolatedProject(t, "existing-dso-references")
	v := newTestVault(t)
	// The fixture's dso:// URIs hardcode project "myapp" explicitly
	// (dso://myapp/db_password), not the working-directory-derived
	// project name -- an explicit project segment always wins.
	if err := v.Set("myapp", "db_password", "hunter2"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// NEW_SECRET is referenced via ${NEW_SECRET} interpolation, not a
	// dso:// URI in this fixture -- migrate would map it under the
	// project name derived from the working directory, not "myapp". It's
	// only relevant to migrate's own tests, not validate's, so it's not
	// seeded here.

	out := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})
	assertNewCategoriesPass(t, out)
}

func TestRunValidate_MalformedCompose_ExitNonZero(t *testing.T) {
	setupIsolatedProject(t, "malformed-compose")

	captureStdout(t, func() {
		err := runValidate(context.Background(), false, "")
		if err == nil {
			t.Error("expected a non-nil error for malformed Compose YAML")
		}
	})
}

func TestRunValidate_MalformedDSOReference_ExitNonZero(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())
	if err := os.WriteFile("docker-compose.yml", []byte("services:\n  app:\n    environment:\n      DB_PASSWORD: dso://\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	captureStdout(t, func() {
		err := runValidate(context.Background(), false, "")
		if err == nil {
			t.Error("expected a non-nil error for a malformed dso:// reference")
		}
	})
}

func TestRunValidate_MissingSecret_ExitNonZero(t *testing.T) {
	setupIsolatedProject(t, "existing-dso-references")
	newTestVault(t) // initialized but empty -- neither referenced secret exists

	var captured string
	captureStdout(t, func() {
		err := runValidate(context.Background(), false, "")
		if err == nil {
			t.Error("expected a non-nil error when referenced secrets are missing from the vault")
		}
	})
	_ = captured
}

// ── JSON output ───────────────────────────────────────────────────────────

func TestRunValidate_JSON_WellFormed(t *testing.T) {
	setupIsolatedProject(t, "basic")

	out := captureStdout(t, func() {
		_ = runValidate(context.Background(), true, "")
	})

	if !json.Valid([]byte(out)) {
		t.Fatalf("expected valid JSON, got:\n%s", out)
	}

	var parsed struct {
		OverallStatus string `json:"overall_status"`
		Checks        []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
			Status   string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("failed to unmarshal into expected shape: %v", err)
	}
	if parsed.OverallStatus == "" {
		t.Error("expected a non-empty overall_status")
	}
	if len(parsed.Checks) == 0 {
		t.Error("expected at least one check in the JSON output")
	}
}

// ── read-only invariant ───────────────────────────────────────────────────

func TestRunValidate_NeverMutatesProjectFiles(t *testing.T) {
	dir := setupIsolatedProject(t, "existing-dso-references")

	envBefore, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeBefore, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))

	captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
		_ = runValidate(context.Background(), true, "")
	})

	envAfter, _ := os.ReadFile(filepath.Join(dir, ".env"))
	composeAfter, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if string(envBefore) != string(envAfter) {
		t.Error("validate must never modify .env")
	}
	if string(composeBefore) != string(composeAfter) {
		t.Error("validate must never modify docker-compose.yml")
	}
	if _, err := os.Stat(filepath.Join(dir, migratedComposeFilename)); err == nil {
		t.Error("validate must never create docker-compose.dso.yml")
	}
}

func TestRunValidate_NeverInitializesVault(t *testing.T) {
	dir := setupIsolatedProject(t, "basic")

	captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})

	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".dso")); err == nil {
		t.Error("validate must never initialize the vault as a side effect -- an absent vault should be reported NOT CHECKED")
	}
	_ = dir
}

// ── secret safety: mandatory sentinel test ────────────────────────────────

const validationTestSentinel = "VALIDATION_TEST_SECRET_9f82b7"

func TestRunValidate_SentinelSecretNeverLeaked(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())

	if err := os.WriteFile(".env", []byte("DB_PASSWORD="+validationTestSentinel+"\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile("docker-compose.yml", []byte(
		"services:\n  app:\n    environment:\n      DB_PASSWORD: dso://myproj/db_password\n",
	), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	v := newTestVault(t)
	if err := v.Set("myproj", "db_password", validationTestSentinel); err != nil {
		t.Fatalf("seed vault: %v", err)
	}

	// text mode
	textOut := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})
	if strings.Contains(textOut, validationTestSentinel) {
		t.Fatalf("sentinel leaked in text output:\n%s", textOut)
	}

	// json mode
	jsonOut := captureStdout(t, func() {
		_ = runValidate(context.Background(), true, "")
	})
	if strings.Contains(jsonOut, validationTestSentinel) {
		t.Fatalf("sentinel leaked in JSON output:\n%s", jsonOut)
	}

	// error path: force a provider/compose error and check it too
	if err := os.WriteFile("docker-compose.yml", []byte("not: [valid, yaml"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	errOut := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})
	if strings.Contains(errOut, validationTestSentinel) {
		t.Fatalf("sentinel leaked in an error-path output:\n%s", errOut)
	}
}

// ── dual-configuration stdout purity regression test ─────────────────────
//
// detectMode (internal/cli/up.go) prints a diagnostic when both a local
// vault and a cloud config are present on the same host. That print used
// to go to stdout via fmt.Println -- fine for `docker dso up`'s
// interactive narrative, but detectMode is also called by `validate` and
// `doctor`, both of which treat stdout as a machine-readable contract
// under --json. A real manual test run hit exactly this: the warning line
// prepended itself to the JSON payload and broke `jq`/json.Valid parsing.
// This test pins the fix: the diagnostic must go to stderr, and stdout
// must remain valid, uncontaminated JSON even when both conditions are
// simultaneously true.
func TestRunValidate_JSON_ValidEvenWithDualConfiguration(t *testing.T) {
	dir := setupIsolatedProject(t, "basic")

	// Trigger detectMode's dual-configuration branch: a cloud-style
	// project-local dso.yaml alongside an initialized local vault.
	if err := os.WriteFile(filepath.Join(dir, "dso.yaml"), []byte("providers: {}\nsecrets: []\n"), 0600); err != nil {
		t.Fatalf("write dso.yaml fixture: %v", err)
	}
	newTestVault(t) // initializes ~/.dso/vault.enc under the already-isolated HOME

	stdout, stderr := captureStdoutAndStderr(t, func() {
		_ = runValidate(context.Background(), true, "")
	})

	if !json.Valid([]byte(stdout)) {
		t.Fatalf("REGRESSION: stdout is not valid JSON when both local vault and cloud config are present:\n%s", stdout)
	}
	if strings.Contains(stdout, "Both local vault and cloud configuration") {
		t.Error("the dual-configuration diagnostic must not appear on stdout")
	}
	if !strings.Contains(stderr, "Both local vault and cloud configuration") {
		t.Error("expected the dual-configuration diagnostic to appear on stderr")
	}
}

// ── migrate -> validate integration ────────────────────────────────────────

func TestMigrateValidateIntegration_ValidMigration_Passes(t *testing.T) {
	dir := setupIsolatedProject(t, "multiple-services")

	captureStdout(t, func() {
		if err := runMigrate(migrateOptions{confirm: true, envFileFlag: ".env"}); err != nil {
			t.Fatalf("migrate failed: %v", err)
		}
	})

	dsoComposePath := filepath.Join(dir, migratedComposeFilename)
	if _, err := os.Stat(dsoComposePath); err != nil {
		t.Fatalf("expected %s to exist after migrate: %v", migratedComposeFilename, err)
	}

	out := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, migratedComposeFilename)
	})
	assertNewCategoriesPass(t, out)
}

func TestMigrateValidateIntegration_DsofileReference_Validates(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", t.TempDir())

	v := newTestVault(t)
	if err := v.Set("myproj", "db_password", "hunter2"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile("docker-compose.yml", []byte(
		"services:\n  app:\n    environment:\n      DB_PASSWORD_FILE: dsofile://myproj/db_password\n",
	), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})
	assertNewCategoriesPass(t, out)
	_ = dir
}

func TestMigrateValidateIntegration_NonDSOVars_NoFalseFailure(t *testing.T) {
	setupIsolatedProject(t, "compose-interpolation")

	out := captureStdout(t, func() {
		_ = runValidate(context.Background(), false, "")
	})
	assertNewCategoriesPass(t, out)
}

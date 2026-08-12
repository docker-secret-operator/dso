package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/vault"
)

// newTestVault isolates pkg/vault's default (~/.dso-rooted) storage to a
// temp directory for the duration of the test, by redirecting HOME --
// reusing the real vault package rather than introducing a mock/second
// storage abstraction, matching the pattern already used in
// pkg/provider/load_env_test.go for HOME redirection.
func newTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := vault.InitDefault(); err != nil {
		t.Fatalf("vault.InitDefault: %v", err)
	}
	v, err := vault.LoadDefault()
	if err != nil {
		t.Fatalf("vault.LoadDefault: %v", err)
	}
	return v
}

// ── applySecrets ──────────────────────────────────────────────────────────

func TestApplySecrets_ImportsSelectedOnly(t *testing.T) {
	v := newTestVault(t)
	plan := &MigrationPlan{
		ProjectName: "myproj",
		envValues:   map[string]string{"DB_PASSWORD": "hunter2", "PORT": "8080"},
		Candidates: []MigrationCandidate{
			{Key: "DB_PASSWORD", LooksSecret: true, Selected: true},
			{Key: "PORT", LooksSecret: false, Selected: false},
		},
	}

	summary := applySecrets(v, plan, false)

	if len(summary.Imported) != 1 || summary.Imported[0] != "DB_PASSWORD" {
		t.Fatalf("expected Imported=[DB_PASSWORD], got %v", summary.Imported)
	}
	sec, err := v.Get("myproj", "DB_PASSWORD")
	if err != nil || sec.Value != "hunter2" {
		t.Fatalf("expected DB_PASSWORD=hunter2 in vault, got %+v err=%v", sec, err)
	}
	if _, err := v.Get("myproj", "PORT"); err == nil {
		t.Error("PORT was not selected -- it must not have been imported")
	}
}

func TestApplySecrets_IdenticalExisting_NotReimported(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("myproj", "DB_PASSWORD", "hunter2"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	plan := &MigrationPlan{
		ProjectName: "myproj",
		envValues:   map[string]string{"DB_PASSWORD": "hunter2"},
		Candidates: []MigrationCandidate{
			{Key: "DB_PASSWORD", Selected: true, ExistsInVault: true, VaultDiffers: false},
		},
	}

	summary := applySecrets(v, plan, false)

	if len(summary.AlreadyExisted) != 1 || summary.AlreadyExisted[0] != "DB_PASSWORD" {
		t.Fatalf("expected AlreadyExisted=[DB_PASSWORD], got %v", summary)
	}
	if len(summary.Imported) != 0 {
		t.Error("an identical existing value must not be counted as freshly imported")
	}
}

func TestApplySecrets_DifferingExisting_SkippedWithoutOverwrite(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("myproj", "DB_PASSWORD", "old-value"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	plan := &MigrationPlan{
		ProjectName: "myproj",
		envValues:   map[string]string{"DB_PASSWORD": "new-value"},
		Candidates: []MigrationCandidate{
			{Key: "DB_PASSWORD", Selected: true, ExistsInVault: true, VaultDiffers: true},
		},
	}

	summary := applySecrets(v, plan, false)

	if len(summary.Skipped) != 1 || summary.Skipped[0] != "DB_PASSWORD" {
		t.Fatalf("expected Skipped=[DB_PASSWORD] (no overwrite requested), got %+v", summary)
	}
	sec, _ := v.Get("myproj", "DB_PASSWORD")
	if sec.Value != "old-value" {
		t.Fatalf("must never silently overwrite without explicit overwrite=true; vault now has %q", sec.Value)
	}
}

func TestApplySecrets_DifferingExisting_OverwrittenWhenRequested(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("myproj", "DB_PASSWORD", "old-value"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	plan := &MigrationPlan{
		ProjectName: "myproj",
		envValues:   map[string]string{"DB_PASSWORD": "new-value"},
		Candidates: []MigrationCandidate{
			{Key: "DB_PASSWORD", Selected: true, ExistsInVault: true, VaultDiffers: true},
		},
	}

	summary := applySecrets(v, plan, true)

	if len(summary.Imported) != 1 || summary.Imported[0] != "DB_PASSWORD" {
		t.Fatalf("expected Imported=[DB_PASSWORD] with overwrite=true, got %+v", summary)
	}
	sec, _ := v.Get("myproj", "DB_PASSWORD")
	if sec.Value != "new-value" {
		t.Fatalf("expected vault value updated to new-value, got %q", sec.Value)
	}
}

func TestApplySecrets_PartialFailure_OthersStillProcessed(t *testing.T) {
	v := newTestVault(t)
	plan := &MigrationPlan{
		ProjectName: "in/valid", // vault rejects ".." but this exercises an
		// independently-failing key without relying on vault internals --
		// see the next assertion for the actual failure mechanism used.
		envValues: map[string]string{
			"GOOD_ONE": "value1",
			"BAD_ONE":  "value2",
			"GOOD_TWO": "value3",
		},
		Candidates: []MigrationCandidate{
			{Key: "GOOD_ONE", Selected: true},
			{Key: "BAD_ONE", Selected: true},
			{Key: "GOOD_TWO", Selected: true},
		},
	}
	// Use a project name vault.Set will reject outright (contains ".."),
	// isolated to prove ALL keys fail together only because the *project*
	// is invalid -- so instead, test per-key failure by giving one
	// candidate an invalid key name (containing "..") while others are valid.
	plan.ProjectName = "myproj"
	plan.Candidates[1].Key = "BAD..ONE"
	plan.envValues["BAD..ONE"] = plan.envValues["BAD_ONE"]

	summary := applySecrets(v, plan, false)

	if len(summary.Imported) != 2 {
		t.Errorf("expected the 2 valid keys to still be imported despite one failure, got Imported=%v", summary.Imported)
	}
	if _, failed := summary.Failed["BAD..ONE"]; !failed {
		t.Errorf("expected BAD..ONE to be recorded as Failed, got %+v", summary)
	}
	for _, errMsg := range summary.Failed {
		if strings.Contains(errMsg, "value2") {
			t.Error("Failed error message must never contain the secret value")
		}
	}
}

// ── writeMigratedCompose ──────────────────────────────────────────────────

func TestWriteMigratedCompose_OriginalFileUntouched(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	original := "services:\n  app:\n    image: myapp\n    environment:\n      DB_PASSWORD: ${DB_PASSWORD}\n"
	if err := os.WriteFile(composePath, []byte(original), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outputPath := filepath.Join(dir, "docker-compose.dso.yml")
	changes := []ComposeChange{{Service: "app", EnvKey: "DB_PASSWORD", OldValue: "${DB_PASSWORD}", NewURI: "dso://myproj/DB_PASSWORD"}}

	if err := writeMigratedCompose(composePath, outputPath, changes); err != nil {
		t.Fatalf("writeMigratedCompose: %v", err)
	}

	after, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read original after migrate: %v", err)
	}
	if string(after) != original {
		t.Fatal("the original compose file must never be modified by writeMigratedCompose")
	}

	out, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(out), "dso://myproj/DB_PASSWORD") {
		t.Fatalf("expected output to contain the new dso:// reference, got:\n%s", out)
	}
	if strings.Contains(string(out), "${DB_PASSWORD}") {
		t.Fatalf("expected the old interpolation to be replaced, got:\n%s", out)
	}
}

func TestWriteMigratedCompose_ListForm(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	original := "services:\n  app:\n    image: myapp\n    environment:\n      - DB_PASSWORD=${DB_PASSWORD}\n      - LOG_LEVEL=info\n"
	if err := os.WriteFile(composePath, []byte(original), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outputPath := filepath.Join(dir, "docker-compose.dso.yml")
	changes := []ComposeChange{{Service: "app", EnvKey: "DB_PASSWORD", OldValue: "${DB_PASSWORD}", NewURI: "dso://myproj/DB_PASSWORD"}}

	if err := writeMigratedCompose(composePath, outputPath, changes); err != nil {
		t.Fatalf("writeMigratedCompose: %v", err)
	}

	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), "DB_PASSWORD=dso://myproj/DB_PASSWORD") {
		t.Fatalf("expected list-form substitution, got:\n%s", out)
	}
	if !strings.Contains(string(out), "LOG_LEVEL=info") {
		t.Fatalf("expected unrelated list entries to be preserved, got:\n%s", out)
	}
}

func TestWriteMigratedCompose_OnlyTargetedServiceChanges(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	original := "services:\n  db:\n    image: postgres\n    environment:\n      DB_PASSWORD: ${DB_PASSWORD}\n  web:\n    image: nginx\n    ports:\n      - \"80:80\"\n"
	if err := os.WriteFile(composePath, []byte(original), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outputPath := filepath.Join(dir, "docker-compose.dso.yml")
	changes := []ComposeChange{{Service: "db", EnvKey: "DB_PASSWORD", OldValue: "${DB_PASSWORD}", NewURI: "dso://myproj/DB_PASSWORD"}}

	if err := writeMigratedCompose(composePath, outputPath, changes); err != nil {
		t.Fatalf("writeMigratedCompose: %v", err)
	}

	out, _ := os.ReadFile(outputPath)
	if !strings.Contains(string(out), `"80:80"`) {
		t.Fatalf("expected unrelated web service (ports, image, etc.) to be fully preserved, got:\n%s", out)
	}
}

package cli

import (
	"errors"
	"path/filepath"
	"sort"
	"testing"
)

func fixturePaths(name string) (envPath, composePath string) {
	dir := filepath.Join("testdata", "migrate", name)
	return filepath.Join(dir, ".env"), filepath.Join(dir, "docker-compose.yml")
}

func TestPlanMigration_Basic(t *testing.T) {
	envPath, composePath := fixturePaths("basic")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var candidateKeys []string
	for _, c := range plan.Candidates {
		candidateKeys = append(candidateKeys, c.Key)
	}
	if len(candidateKeys) != 1 || candidateKeys[0] != "DB_PASSWORD" {
		t.Fatalf("expected candidates=[DB_PASSWORD], got %v", candidateKeys)
	}

	sort.Strings(plan.NonSecretVars)
	wantNonSecret := []string{"LOG_LEVEL", "PORT"}
	if len(plan.NonSecretVars) != 2 || plan.NonSecretVars[0] != wantNonSecret[0] || plan.NonSecretVars[1] != wantNonSecret[1] {
		t.Fatalf("expected non-secret vars %v, got %v", wantNonSecret, plan.NonSecretVars)
	}

	if len(plan.SelectedComposeChanges()) != 1 {
		t.Fatalf("expected 1 selected compose change (DB_PASSWORD is a default-selected candidate), got %d", len(plan.SelectedComposeChanges()))
	}
}

func TestPlanMigration_ComposeInterpolation_OnlyMatchesReferencedVars(t *testing.T) {
	envPath, composePath := fixturePaths("compose-interpolation")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// DB_PASSWORD is referenced twice (${DB_PASSWORD} and the
	// ${DB_PASSWORD:-fallback} default form) -- both must resolve to the
	// same variable and produce compose changes.
	var dbChanges int
	for _, c := range plan.ComposeChanges {
		if c.EnvKey == "DB_PASSWORD" || c.EnvKey == "DB_PASSWORD_DEFAULTED" {
			dbChanges++
		}
	}
	if dbChanges != 2 {
		t.Errorf("expected 2 compose changes for DB_PASSWORD variants, got %d: %+v", dbChanges, plan.ComposeChanges)
	}

	// SOME_UNRELATED_HOST_VAR is not in .env at all -- must not produce a
	// change referencing a variable DSO knows nothing about.
	for _, c := range plan.ComposeChanges {
		if c.EnvKey == "SOME_OTHER_INTERPOLATION" {
			t.Errorf("did not expect a compose change for a variable absent from .env: %+v", c)
		}
	}

	// UNRELATED_VAR exists in .env but is never referenced by compose --
	// it must still appear as a plain env var (non-secret bucket, since it
	// doesn't match a secret-name heuristic), but propose no compose change.
	found := false
	for _, v := range plan.NonSecretVars {
		if v == "UNRELATED_VAR" {
			found = true
		}
	}
	if !found {
		t.Error("expected UNRELATED_VAR to appear in the plan even though compose never references it")
	}
}

func TestPlanMigration_ExistingDSOReferences_NeverDoubleConverted(t *testing.T) {
	envPath, composePath := fixturePaths("existing-dso-references")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.AlreadyMigrated) != 2 {
		t.Fatalf("expected 2 already-migrated keys (env+file form), got %v", plan.AlreadyMigrated)
	}

	for _, c := range plan.Candidates {
		if c.Key == "DB_PASSWORD" {
			t.Error("DB_PASSWORD is already referenced via dso:// in compose -- it must not be offered as a fresh migration candidate")
		}
	}
	for _, c := range plan.ComposeChanges {
		if c.EnvKey == "DB_PASSWORD" || c.EnvKey == "DB_PASSWORD_FILE" {
			t.Error("must not propose a compose change for a value that is already a dso:// / dsofile:// reference")
		}
	}

	// NEW_SECRET is a genuinely new candidate referenced via interpolation.
	foundNewSecret := false
	for _, c := range plan.Candidates {
		if c.Key == "NEW_SECRET" {
			foundNewSecret = true
		}
	}
	if !foundNewSecret {
		t.Error("expected NEW_SECRET to be offered as a migration candidate")
	}
}

func TestPlanMigration_EnvFileDirective_FlaggedForManualReview(t *testing.T) {
	envPath, composePath := fixturePaths("env-file-directive")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.ManualReview) != 1 || plan.ManualReview[0].Service != "app" {
		t.Fatalf("expected service 'app' flagged for manual review (env_file:), got %+v", plan.ManualReview)
	}
	if len(plan.ComposeChanges) != 0 {
		t.Errorf("expected no automatic compose changes for a service using env_file:, got %+v", plan.ComposeChanges)
	}
}

func TestPlanMigration_MultipleServices_OnlyRelevantReferencesChange(t *testing.T) {
	envPath, composePath := fixturePaths("multiple-services")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	changesByService := map[string]string{}
	for _, c := range plan.ComposeChanges {
		changesByService[c.Service] = c.EnvKey
	}
	if changesByService["db"] != "DB_PASSWORD" {
		t.Errorf("expected db service to get a DB_PASSWORD change, got %v", changesByService)
	}
	if changesByService["cache"] != "REDIS_PASSWORD" {
		t.Errorf("expected cache service to get a REDIS_PASSWORD change, got %v", changesByService)
	}
	if _, ok := changesByService["web"]; ok {
		t.Error("web service has no secret-referencing environment -- it must not appear in ComposeChanges")
	}
}

func TestPlanMigration_MalformedCompose_ReturnsError(t *testing.T) {
	envPath, composePath := fixturePaths("malformed-compose")
	_, err := planMigration(envPath, composePath, "myproj", nil)
	if err == nil {
		t.Fatal("expected an error for malformed Compose YAML, got nil")
	}
}

func TestPlanMigration_MissingEnv_ReturnsClearError(t *testing.T) {
	_, composePath := fixturePaths("missing-env")
	_, err := planMigration(filepath.Join("testdata", "migrate", "missing-env", ".env"), composePath, "myproj", nil)
	if err == nil {
		t.Fatal("expected an error when .env is missing, got nil")
	}
}

func TestPlanMigration_DuplicateVars_Deterministic(t *testing.T) {
	envPath, _ := fixturePaths("duplicate-vars")
	// duplicate-vars fixture has no compose file; reuse basic's compose to
	// isolate the .env-duplicate-handling behavior being tested here.
	_, composePath := fixturePaths("basic")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.DuplicateKeys) != 1 || plan.DuplicateKeys[0] != "DB_PASSWORD" {
		t.Fatalf("expected DuplicateKeys=[DB_PASSWORD], got %v", plan.DuplicateKeys)
	}
}

// ── conflict detection ───────────────────────────────────────────────────

func TestPlanMigration_ConflictDetection(t *testing.T) {
	envPath, composePath := fixturePaths("basic")

	lookup := func(project, key, candidateValue string) (exists bool, differs bool, err error) {
		switch key {
		case "DB_PASSWORD":
			// Simulate the vault already holding a DIFFERENT value.
			return true, candidateValue != "already-imported-value", nil
		}
		return false, false, nil
	}

	plan, err := planMigration(envPath, composePath, "myproj", lookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var dbCandidate *MigrationCandidate
	for i := range plan.Candidates {
		if plan.Candidates[i].Key == "DB_PASSWORD" {
			dbCandidate = &plan.Candidates[i]
		}
	}
	if dbCandidate == nil {
		t.Fatal("expected DB_PASSWORD candidate")
	}
	if !dbCandidate.ExistsInVault || !dbCandidate.VaultDiffers {
		t.Errorf("expected DB_PASSWORD to be flagged as an existing, differing vault entry, got %+v", dbCandidate)
	}
}

func TestPlanMigration_ConflictLookupFailure_BecomesWarningNotFatal(t *testing.T) {
	envPath, composePath := fixturePaths("basic")

	lookup := func(project, key, candidateValue string) (bool, bool, error) {
		return false, false, errors.New("vault unavailable")
	}

	plan, err := planMigration(envPath, composePath, "myproj", lookup)
	if err != nil {
		t.Fatalf("a vault lookup failure must not fail plan generation entirely, got error: %v", err)
	}
	if len(plan.Warnings) == 0 {
		t.Error("expected a warning to be recorded for the failed vault lookup")
	}
}

// ── security invariant: no secret values ever appear in the plan's display surfaces ──

func TestPlanMigration_NoSecretValuesInDisplaySurfaces(t *testing.T) {
	envPath, composePath := fixturePaths("basic")
	plan, err := planMigration(envPath, composePath, "myproj", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const secretValue = "hunter2"
	checkNoLeak := func(label, s string) {
		if contains(s, secretValue) {
			t.Errorf("%s leaked the secret value %q: %q", label, secretValue, s)
		}
	}
	for _, w := range plan.Warnings {
		checkNoLeak("Warnings", w)
	}
	for _, c := range plan.ComposeChanges {
		checkNoLeak("ComposeChange.OldValue", c.OldValue)
		checkNoLeak("ComposeChange.NewURI", c.NewURI)
	}
	for _, m := range plan.ManualReview {
		checkNoLeak("ManualReview.Reason", m.Reason)
	}
}

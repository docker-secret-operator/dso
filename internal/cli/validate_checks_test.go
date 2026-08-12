package cli

import (
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/setup"
)

// ── parseDSOReference ─────────────────────────────────────────────────────

func TestParseDSOReference(t *testing.T) {
	cases := []struct {
		uri, fallback string
		wantScheme    string
		wantProject   string
		wantPath      string
		wantOK        bool
	}{
		{"dso://myproj/db_password", "fallback", "dso", "myproj", "db_password", true},
		{"dsofile://myproj/db_password", "fallback", "dsofile", "myproj", "db_password", true},
		{"dso://db_password", "fallback", "dso", "fallback", "db_password", true},
		{"dso://", "fallback", "dso", "", "", false},
		{"dsofile://", "fallback", "dsofile", "", "", false},
		{"not-a-dso-uri", "fallback", "", "", "", false},
		{"dso:///empty-project/path", "fallback", "dso", "", "path", false},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			scheme, project, path, ok := parseDSOReference(tc.uri, tc.fallback)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (scheme=%q project=%q path=%q)", ok, tc.wantOK, scheme, project, path)
			}
			if ok {
				if scheme != tc.wantScheme || project != tc.wantProject || path != tc.wantPath {
					t.Fatalf("got scheme=%q project=%q path=%q, want scheme=%q project=%q path=%q",
						scheme, project, path, tc.wantScheme, tc.wantProject, tc.wantPath)
				}
			}
		})
	}
}

// ── checkComposeStructure ────────────────────────────────────────────────

func TestCheckComposeStructure_Valid(t *testing.T) {
	root, checks := checkComposeStructure(joinFixture("basic", "docker-compose.yml"))
	if root == nil {
		t.Fatal("expected a parsed root node for valid compose")
	}
	assertNoFail(t, checks)
}

func TestCheckComposeStructure_Malformed(t *testing.T) {
	_, checks := checkComposeStructure(joinFixture("malformed-compose", "docker-compose.yml"))
	assertHasFail(t, checks)
}

func TestCheckComposeStructure_MissingFile(t *testing.T) {
	_, checks := checkComposeStructure("testdata/migrate/does-not-exist/docker-compose.yml")
	assertHasFail(t, checks)
}

// ── collectDSOReferences ─────────────────────────────────────────────────

func TestCollectDSOReferences_ExistingDSOReferences(t *testing.T) {
	root, composeChecks := checkComposeStructure(joinFixture("existing-dso-references", "docker-compose.yml"))
	assertNoFail(t, composeChecks)

	refs, checks := collectDSOReferences(root, "fallback")
	if len(refs) != 2 {
		t.Fatalf("expected 2 dso references (env + file form), got %d: %+v", len(refs), refs)
	}
	var sawDso, sawDsofile bool
	for _, r := range refs {
		if r.Scheme == "dso" {
			sawDso = true
		}
		if r.Scheme == "dsofile" {
			sawDsofile = true
		}
	}
	if !sawDso || !sawDsofile {
		t.Errorf("expected both dso:// and dsofile:// references, got %+v", refs)
	}
	assertNoFail(t, checks)
}

func TestCollectDSOReferences_NoReferences_NotAFailure(t *testing.T) {
	root, _ := checkComposeStructure(joinFixture("compose-interpolation", "docker-compose.yml"))
	refs, checks := collectDSOReferences(root, "fallback")
	if len(refs) != 0 {
		t.Fatalf("expected 0 references (this fixture uses ${VAR} interpolation, not dso://), got %d", len(refs))
	}
	assertNoFail(t, checks)
}

func TestCollectDSOReferences_Malformed(t *testing.T) {
	yamlText := `
services:
  app:
    environment:
      DB_PASSWORD: dso://
`
	root := parseCompose(t, yamlText)
	_, checks := collectDSOReferences(root, "fallback")
	assertHasFail(t, checks)
}

func TestCheckReferenceConsistency_InconsistentMapping(t *testing.T) {
	refs := []dsoReference{
		{Service: "a", EnvKey: "DB_PASSWORD", Project: "proj", Path: "db_password"},
		{Service: "b", EnvKey: "DB_PASSWORD", Project: "proj", Path: "other_password"},
	}
	c := checkReferenceConsistency(refs)
	if c.Status != setup.DoctorWarn {
		t.Fatalf("expected DoctorWarn for inconsistent key mapping, got %v", c.Status)
	}
}

func TestCheckReferenceConsistency_ConsistentMapping_Pass(t *testing.T) {
	refs := []dsoReference{
		{Service: "a", EnvKey: "DB_PASSWORD", Project: "proj", Path: "db_password"},
		{Service: "b", EnvKey: "DB_PASSWORD", Project: "proj", Path: "db_password"},
	}
	c := checkReferenceConsistency(refs)
	if c.Status != setup.DoctorPass {
		t.Fatalf("expected DoctorPass for consistent key mapping, got %v", c.Status)
	}
}

// ── checkSecretExistence ─────────────────────────────────────────────────

func TestCheckSecretExistence_NoRefs_NoChecks(t *testing.T) {
	checks := checkSecretExistence(nil)
	if len(checks) != 0 {
		t.Fatalf("expected no checks when there are no references, got %+v", checks)
	}
}

func TestCheckSecretExistence_VaultUnavailable_NotCheckedNotFailed(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no vault initialized here

	refs := []dsoReference{{Project: "proj", Path: "db_password"}}
	checks := checkSecretExistence(refs)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status == setup.DoctorFail {
		t.Fatal("an unavailable vault must be reported as NOT CHECKED, not a validation failure")
	}
	if !strings.Contains(checks[0].Detail, "NOT CHECKED") {
		t.Errorf("expected detail to explicitly say NOT CHECKED, got %q", checks[0].Detail)
	}
}

func TestCheckSecretExistence_MissingSecret_Fails(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("proj", "present_secret", "value"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	refs := []dsoReference{
		{Project: "proj", Path: "present_secret"},
		{Project: "proj", Path: "missing_secret"},
	}
	checks := checkSecretExistence(refs)
	if len(checks) != 1 || checks[0].Status != setup.DoctorFail {
		t.Fatalf("expected a single FAIL check for the missing secret, got %+v", checks)
	}
	if !strings.Contains(checks[0].Detail, "missing_secret") {
		t.Errorf("expected the detail to name the missing secret, got %q", checks[0].Detail)
	}
	if strings.Contains(checks[0].Detail, "value") {
		t.Fatal("must never include the secret value, even for the one that exists")
	}
}

func TestCheckSecretExistence_AllPresent_Pass(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("proj", "db_password", "value"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	refs := []dsoReference{{Project: "proj", Path: "db_password"}}
	checks := checkSecretExistence(refs)
	if len(checks) != 1 || checks[0].Status != setup.DoctorPass {
		t.Fatalf("expected PASS, got %+v", checks)
	}
}

func TestCheckSecretExistence_Deduplicates(t *testing.T) {
	v := newTestVault(t)
	if err := v.Set("proj", "db_password", "value"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Same (project, path) referenced by two different services --
	// existence should be reported once, not once per reference.
	refs := []dsoReference{
		{Service: "a", EnvKey: "DB_PASSWORD", Project: "proj", Path: "db_password"},
		{Service: "b", EnvKey: "DB_PASSWORD", Project: "proj", Path: "db_password"},
	}
	checks := checkSecretExistence(refs)
	if len(checks) != 1 || !strings.Contains(checks[0].Detail, "1 referenced secret") {
		t.Fatalf("expected deduplication to report 1 unique secret, got %+v", checks)
	}
}

// ── test helpers ──────────────────────────────────────────────────────────

func joinFixture(name, file string) string {
	return "testdata/migrate/" + name + "/" + file
}

func assertNoFail(t *testing.T, checks []setup.DoctorCheck) {
	t.Helper()
	for _, c := range checks {
		if c.Status == setup.DoctorFail {
			t.Errorf("unexpected FAIL check: %+v", c)
		}
	}
}

func assertHasFail(t *testing.T, checks []setup.DoctorCheck) {
	t.Helper()
	for _, c := range checks {
		if c.Status == setup.DoctorFail {
			return
		}
	}
	t.Fatalf("expected at least one FAIL check, got %+v", checks)
}

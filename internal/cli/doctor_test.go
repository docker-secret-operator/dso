package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/setup"
	"gopkg.in/yaml.v3"
)

// ── Command shape ────────────────────────────────────────────────────────

func TestNewDoctorCmd(t *testing.T) {
	cmd := NewDoctorCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "doctor" {
		t.Fatalf("expected 'doctor', got '%s'", cmd.Use)
	}
}

func TestDoctorCmd_Flags(t *testing.T) {
	cmd := NewDoctorCmd()

	if cmd.Flag("level") == nil {
		t.Fatal("expected 'level' flag")
	}
	if cmd.Flag("json") == nil {
		t.Fatal("expected 'json' flag")
	}
}

func TestDoctorCmd_HelpText(t *testing.T) {
	cmd := NewDoctorCmd()
	if cmd.Long == "" {
		t.Fatal("expected help text")
	}
	if !contains(cmd.Long, "Docker") || !contains(cmd.Long, "environment") {
		t.Fatal("help text missing key content")
	}
}

// ── findComposeFile ──────────────────────────────────────────────────────

func TestFindComposeFile_None(t *testing.T) {
	t.Chdir(t.TempDir())

	if _, found := findComposeFile(); found {
		t.Fatal("expected no compose file to be found in an empty directory")
	}
}

func TestFindComposeFile_YmlPreferred(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile("docker-compose.yml", []byte("services: {}\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	name, found := findComposeFile()
	if !found || name != "docker-compose.yml" {
		t.Fatalf("expected docker-compose.yml, got %q found=%v", name, found)
	}
}

func TestFindComposeFile_YamlFallback(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile("docker-compose.yaml", []byte("services: {}\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	name, found := findComposeFile()
	if !found || name != "docker-compose.yaml" {
		t.Fatalf("expected docker-compose.yaml, got %q found=%v", name, found)
	}
}

// ── checkDotEnvPresence ──────────────────────────────────────────────────

func TestCheckDotEnvPresence_Absent(t *testing.T) {
	t.Chdir(t.TempDir())

	c := checkDotEnvPresence()
	if c.Status != setup.DoctorInfo {
		t.Fatalf("expected DoctorInfo when .env is absent, got %v", c.Status)
	}
}

func TestCheckDotEnvPresence_Present(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Deliberately includes a fake secret-shaped value to verify the check
	// never reads or surfaces the value itself -- only presence.
	if err := os.WriteFile(".env", []byte("DB_PASSWORD=super-secret-value\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	c := checkDotEnvPresence()
	if c.Status != setup.DoctorWarn {
		t.Fatalf("expected DoctorWarn when .env is present, got %v", c.Status)
	}
	if strings.Contains(c.Detail, "super-secret-value") || strings.Contains(c.RootCause, "super-secret-value") {
		t.Fatal("checkDotEnvPresence must never surface .env content")
	}
}

// ── checkDSOReferences ───────────────────────────────────────────────────

func parseCompose(t *testing.T, yamlText string) *yaml.Node {
	t.Helper()
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(yamlText), &root); err != nil {
		t.Fatalf("failed to parse fixture YAML: %v", err)
	}
	return &root
}

func TestCheckDSOReferences_None(t *testing.T) {
	root := parseCompose(t, `
services:
  app:
    image: myapp:latest
    environment:
      PORT: "8080"
`)
	c := checkDSOReferences(root)
	if c.Status != setup.DoctorInfo {
		t.Fatalf("expected DoctorInfo with no references, got %v: %s", c.Status, c.Detail)
	}
}

func TestCheckDSOReferences_WellFormed_MappingForm(t *testing.T) {
	root := parseCompose(t, `
services:
  db:
    image: postgres:15
    environment:
      DB_PASSWORD: dso://myapp/db_password
      DB_FILE: dsofile://myapp/db_password
`)
	c := checkDSOReferences(root)
	if c.Status != setup.DoctorPass {
		t.Fatalf("expected DoctorPass, got %v: %s", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, "2 reference(s)") {
		t.Fatalf("expected detail to report 2 references, got %q", c.Detail)
	}
}

func TestCheckDSOReferences_WellFormed_ListForm(t *testing.T) {
	root := parseCompose(t, `
services:
  db:
    image: postgres:15
    environment:
      - DB_PASSWORD=dso://myapp/db_password
      - LOG_LEVEL=info
`)
	c := checkDSOReferences(root)
	if c.Status != setup.DoctorPass {
		t.Fatalf("expected DoctorPass, got %v: %s", c.Status, c.Detail)
	}
	if !strings.Contains(c.Detail, "1 reference(s)") {
		t.Fatalf("expected detail to report 1 reference, got %q", c.Detail)
	}
}

func TestCheckDSOReferences_Malformed(t *testing.T) {
	root := parseCompose(t, `
services:
  db:
    image: postgres:15
    environment:
      DB_PASSWORD: dso://
`)
	c := checkDSOReferences(root)
	if c.Status != setup.DoctorFail {
		t.Fatalf("expected DoctorFail for empty dso:// path, got %v: %s", c.Status, c.Detail)
	}
	if c.Severity != setup.DoctorHigh {
		t.Fatalf("expected DoctorHigh severity, got %v", c.Severity)
	}
	if len(c.Recovery) == 0 {
		t.Fatal("expected recovery guidance for a malformed reference")
	}
}

func TestCheckDSOReferences_NoSecretValueLeaked(t *testing.T) {
	root := parseCompose(t, `
services:
  db:
    environment:
      DB_PASSWORD: dso://myapp/db_password
      UNRELATED: plain-value-not-a-reference
`)
	c := checkDSOReferences(root)
	if strings.Contains(c.Detail, "plain-value-not-a-reference") {
		t.Fatal("checkDSOReferences must not echo unrelated environment values into its output")
	}
}

// ── buildDoctorResult ────────────────────────────────────────────────────

func TestBuildDoctorResult_OverallStatus(t *testing.T) {
	cases := []struct {
		name   string
		checks []setup.DoctorCheck
		want   setup.DoctorStatus
	}{
		{"all pass", []setup.DoctorCheck{{Status: setup.DoctorPass}, {Status: setup.DoctorInfo}}, setup.DoctorPass},
		{"has warn", []setup.DoctorCheck{{Status: setup.DoctorPass}, {Status: setup.DoctorWarn}}, setup.DoctorWarn},
		{"has fail beats warn", []setup.DoctorCheck{{Status: setup.DoctorWarn}, {Status: setup.DoctorFail}}, setup.DoctorFail},
		{"empty", nil, setup.DoctorPass},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := buildDoctorResult(tc.checks)
			if result.OverallStatus != tc.want {
				t.Fatalf("expected overall status %v, got %v", tc.want, result.OverallStatus)
			}
			if result.Summary.Total != len(tc.checks) {
				t.Fatalf("expected total %d, got %d", len(tc.checks), result.Summary.Total)
			}
		})
	}
}

// ── configuredProviderNames ──────────────────────────────────────────────

func TestConfiguredProviderNames_MissingConfig(t *testing.T) {
	names := configuredProviderNames(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if names != nil {
		t.Fatalf("expected nil for missing config, got %v", names)
	}
}

func TestConfiguredProviderNames_Sorted(t *testing.T) {
	// config.LoadConfig's IsSafePath rejects arbitrary absolute paths by
	// design (only /etc/dso/ and similar system dirs are allowlisted), so
	// this must use a relative path, matching how ResolveConfig() itself
	// resolves a project-local "dso.yaml".
	t.Chdir(t.TempDir())
	cfgYAML := `version: v1.0.0
mode: agent
providers:
  vault:
    type: vault
  aws:
    type: aws
    region: us-east-1
secrets: []
`
	if err := os.WriteFile("dso.yaml", []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	names := configuredProviderNames("dso.yaml")
	if len(names) != 2 || names[0] != "aws" || names[1] != "vault" {
		t.Fatalf("expected sorted [aws vault], got %v", names)
	}
}

// ── rendering ─────────────────────────────────────────────────────────────

func TestRenderDoctorSections_NoPanicAllStatuses(t *testing.T) {
	checks := []setup.DoctorCheck{
		{ID: "1", Category: setup.DoctorCatDocker, Status: setup.DoctorPass, Name: "a", Detail: "ok"},
		{ID: "2", Category: setup.DoctorCatConfiguration, Status: setup.DoctorWarn, Name: "b", Detail: "warn", RootCause: "rc", Recovery: []string{"fix"}},
		{ID: "3", Category: setup.DoctorCatProvider, Status: setup.DoctorFail, Name: "c", Detail: "fail", RootCause: "rc2", Recovery: []string{"fix2"}},
		{ID: "4", Category: doctorCatProject, Status: setup.DoctorInfo, Name: "d", Detail: "info"},
	}
	result := buildDoctorResult(checks)

	out := renderSectionedResult("DSO Doctor", result, doctorSections, true)
	for _, want := range []string{"Environment", "Configuration", "Provider", "Project", "Result"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected rendered output to contain section %q", want)
		}
	}
}

func TestDoctorStatusSymbol(t *testing.T) {
	tests := []struct {
		status   setup.DoctorStatus
		expected string
	}{
		{setup.DoctorPass, "✓"},
		{setup.DoctorFail, "✗"},
		{setup.DoctorWarn, "⚠"},
		{setup.DoctorInfo, "-"},
		{setup.DoctorStatus("unknown"), "?"},
	}
	for _, tt := range tests {
		if got := doctorStatusSymbol(tt.status); got != tt.expected {
			t.Errorf("doctorStatusSymbol(%q) = %q, want %q", tt.status, got, tt.expected)
		}
	}
}

// ── end-to-end smoke test ────────────────────────────────────────────────

// TestRunDoctor_EmptyProject_JSON_NoSecretLeak exercises the full runDoctor
// path against a directory containing only a .env file with a fabricated
// secret-shaped value. Docker/provider results are environment-dependent
// (sandboxed CI may or may not have a reachable daemon), so this doesn't
// assert on overall pass/fail -- it asserts the command completes without
// panicking, produces well-formed JSON, and -- the actual point of the
// test -- that the fabricated secret value never appears in stdout.
func TestRunDoctor_EmptyProject_JSON_NoSecretLeak(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Isolate HOME so this test's result doesn't depend on whatever real
	// ~/.dso state happens to exist on the machine running it (e.g. a
	// locally-configured cloud provider triggers unrelated FAIL checks,
	// or -- as briefly happened on this machine during manual smoke
	// testing -- a stray real vault breaking JSON-validity assumptions
	// via an unrelated warning line on stdout).
	t.Setenv("HOME", t.TempDir())

	const marker = "leaked-if-this-appears-in-output"
	if err := os.WriteFile(".env", []byte("DB_PASSWORD="+marker+"\n"), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	captured := captureStdout(t, func() {
		// runDoctor may return a non-nil error if checks fail (e.g. no
		// Docker in the test sandbox) -- that's expected and fine.
		_ = runDoctor(context.Background(), "full", true)
	})

	if strings.Contains(captured, marker) {
		t.Fatalf("doctor output must never contain .env values, but found the fixture secret in:\n%s", captured)
	}
	if !json.Valid([]byte(captured)) {
		t.Fatalf("expected valid JSON output with --json, got:\n%s", captured)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdout, _ := captureStdoutAndStderr(t, fn)
	return stdout
}

// captureStdoutAndStderr redirects both streams independently, so a test
// can assert not just what was printed, but which stream it went to --
// the property that matters for commands whose stdout is a machine-
// readable contract (e.g. `validate --json`).
func captureStdoutAndStderr(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	fn()

	_ = outW.Close()
	_ = errW.Close()
	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("failed to read captured stderr: %v", err)
	}
	return string(outBytes), string(errBytes)
}

package cli

import (
	"testing"
)

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
	
	// Check for level flag
	levelFlag := cmd.Flag("level")
	if levelFlag == nil {
		t.Fatal("expected 'level' flag")
	}
	
	// Check for json flag
	jsonFlag := cmd.Flag("json")
	if jsonFlag == nil {
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

func TestDiagnosticsAddCheck(t *testing.T) {
	diag := &Diagnostics{
		Checks: []Check{},
	}

	diag.addCheck("Test", "healthy", "message", false)

	if len(diag.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(diag.Checks))
	}

	check := diag.Checks[0]
	if check.Name != "Test" || check.Status != "healthy" || check.Message != "message" {
		t.Fatal("check not added correctly")
	}
}

func TestStatusSymbol(t *testing.T) {
	tests := []struct {
		status   string
		expected string
	}{
		{"healthy", "✓"},
		{"unhealthy", "✗"},
		{"warning", "⚠"},
		{"disabled", "-"},
		{"unknown", "?"},
	}

	for _, tt := range tests {
		result := statusSymbol(tt.status)
		if result != tt.expected {
			t.Fatalf("statusSymbol(%q) = %q, want %q", tt.status, result, tt.expected)
		}
	}
}

func TestCheckDockerConnectivity(t *testing.T) {
	diag := &Diagnostics{
		Checks: []Check{},
	}

	diag.checkDockerConnectivity()

	if len(diag.Checks) == 0 {
		t.Fatal("expected at least one check")
	}

	// The first check should be about Docker socket
	if diag.Checks[0].Name != "Docker socket" {
		t.Fatalf("expected 'Docker socket' check, got %s", diag.Checks[0].Name)
	}
}

func TestCheckRuntimeEnvironment(t *testing.T) {
	diag := &Diagnostics{
		Checks: []Check{},
	}

	diag.checkRuntimeEnvironment()

	if len(diag.Checks) == 0 {
		t.Fatal("expected checks")
	}

	// Should have checks about vault and config
	foundVault := false
	foundConfig := false
	for _, check := range diag.Checks {
		if check.Name == "Local vault" {
			foundVault = true
		}
		if check.Name == "Configuration" {
			foundConfig = true
		}
	}

	if !foundVault {
		t.Fatal("expected 'Local vault' check")
	}
	if !foundConfig {
		t.Fatal("expected 'Configuration' check")
	}
}

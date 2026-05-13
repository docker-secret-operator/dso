package cli

import (
	"testing"
)

func TestNewBootstrapCmd(t *testing.T) {
	cmd := NewBootstrapCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "bootstrap [local|agent]" {
		t.Fatalf("expected 'bootstrap [local|agent]', got '%s'", cmd.Use)
	}
}

func TestBootstrapCmd_HelpText(t *testing.T) {
	cmd := NewBootstrapCmd()
	help := cmd.Long
	if help == "" {
		t.Fatal("expected help text")
	}
	if !contains(help, "local development") && !contains(help, "production") {
		t.Fatal("help text missing mode descriptions")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s); i++ {
		if len(s[i:]) < len(substr) {
			return false
		}
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

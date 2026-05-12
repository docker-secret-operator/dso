package cli

import (
	"bytes"
	"testing"
)

func TestNewRootCmd(t *testing.T) {
	cmd := NewRootCmd()
	if cmd == nil {
		t.Fatal("Expected non-nil root command")
	}

	if cmd.Use != "dso" {
		t.Fatalf("Expected Use to be 'dso', got %s", cmd.Use)
	}

	expectedMinCommands := 10
	if len(cmd.Commands()) < expectedMinCommands {
		t.Errorf("Expected at least %d commands, got %d", expectedMinCommands, len(cmd.Commands()))
	}
}

func TestResolveConfig(t *testing.T) {
	// Simple test, assumes dso.yaml is default
	CfgFile = ""
	cfg := ResolveConfig()
	if cfg != "dso.yaml" && cfg != "/etc/dso/dso.yaml" {
		t.Errorf("Unexpected default config path: %s", cfg)
	}

	CfgFile = "custom.yaml"
	cfg = ResolveConfig()
	if cfg != "custom.yaml" {
		t.Errorf("Expected custom.yaml, got: %s", cfg)
	}
}

func TestExecuteHelp(t *testing.T) {
	cmd := NewRootCmd()

	b := bytes.NewBufferString("")
	cmd.SetOut(b)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Unexpected error executing help: %v", err)
	}

	out := b.String()
	if len(out) == 0 {
		t.Fatal("Expected help output, got empty string")
	}
}

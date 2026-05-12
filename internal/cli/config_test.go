package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewConfigCmd(t *testing.T) {
	cmd := NewConfigCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "config" {
		t.Fatalf("expected 'config', got '%s'", cmd.Use)
	}
}

func TestConfigCmd_Subcommands(t *testing.T) {
	cmd := NewConfigCmd()
	
	// Should have show, edit, validate subcommands
	subcommands := map[string]bool{
		"show":     false,
		"edit":     false,
		"validate": false,
	}

	for _, subcmd := range cmd.Commands() {
		if _, ok := subcommands[subcmd.Name()]; ok {
			subcommands[subcmd.Name()] = true
		}
	}

	for name, found := range subcommands {
		if !found {
			t.Fatalf("expected '%s' subcommand", name)
		}
	}
}

func TestValidateConfigFile_ValidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create valid config
	validConfig := `version: v1alpha1

runtime:
  mode: local
  log_level: info

providers:
  local:
    type: file
    enabled: true
    path: ~/.dso/vault

agent:
  cache:
    ttl: 1h
    max_size: 100Mi
`

	err := os.WriteFile(configPath, []byte(validConfig), 0644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err = validateConfigFile(configPath)
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateConfigFile_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidConfig := `version: v1alpha1

runtime:
  mode: invalid
  log_level: info
`

	err := os.WriteFile(configPath, []byte(invalidConfig), 0644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err = validateConfigFile(configPath)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !contains(err.Error(), "invalid") {
		t.Fatalf("expected error about invalid mode, got: %v", err)
	}
}

func TestValidateConfigFile_MissingVersion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	missingVersion := `runtime:
  mode: local
`

	err := os.WriteFile(configPath, []byte(missingVersion), 0644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err = validateConfigFile(configPath)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if !contains(err.Error(), "version") {
		t.Fatalf("expected error about missing version, got: %v", err)
	}
}

func TestIsValidSize(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"100Mi", true},
		{"1Gi", true},
		{"500MB", true},
		{"100", false},
		{"invalid", false},
		{"100XYZ", false},
	}

	for _, tt := range tests {
		result := isValidSize(tt.input)
		if result != tt.expected {
			t.Fatalf("isValidSize(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestGetConfigPath(t *testing.T) {
	path := getConfigPath()
	if path == "" {
		t.Fatal("expected config path")
	}
	// Should be ~/.dso/config.yaml or similar
	if !contains(path, ".dso") && !contains(path, "/etc/dso") {
		t.Fatalf("unexpected config path: %s", path)
	}
}

package cli

import (
	"os"
	"path/filepath"
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

func TestCreateLocalDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	err := createLocalDirectoryStructure(tmpDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Check if directories were created
	dirs := []string{"vault", "state", "cache", "logs", "plugins"}
	for _, dir := range dirs {
		path := filepath.Join(tmpDir, dir)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected directory %s to exist", path)
		}
	}
}

func TestCreateAgentDirectoryStructure(t *testing.T) {
	// Skip if not running as root
	if os.Geteuid() != 0 {
		t.Skip("skipping agent directory test (requires root)")
	}

	// This would create system directories, so we skip in normal tests
	t.Skip("skipping agent directory test (creates system directories)")
}

func TestGenerateLocalConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	err := generateLocalConfig(configPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Check if file was created
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to exist, got %v", err)
	}

	// Check content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected to read config, got %v", err)
	}

	if !contains(string(content), "version:") {
		t.Fatal("config missing version field")
	}
	if !contains(string(content), "mode: local") {
		t.Fatal("config missing or incorrect mode")
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		input    string
		length   int
		expected string
	}{
		{"hello", 5, "hello "},
		{"hi", 5, "hi    "},
		{"toolong", 5, "toolong "},
	}

	for _, tt := range tests {
		result := padRight(tt.input, tt.length)
		if result != tt.expected {
			t.Fatalf("padRight(%q, %d) = %q, want %q", tt.input, tt.length, result, tt.expected)
		}
	}
}

func TestValidateDockerConnectivity(t *testing.T) {
	err := validateDockerConnectivity()
	// We expect this to succeed or fail gracefully
	// The actual Docker socket may not exist in test environment
	if err != nil && !contains(err.Error(), "Docker") {
		t.Fatalf("unexpected error: %v", err)
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

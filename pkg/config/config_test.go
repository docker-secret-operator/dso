package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary directory
	dir := t.TempDir()
	tempFile := filepath.Join(dir, "dso.yaml")

	yamlContext := `
provider: aws
config:
  region: us-west-2
agent:
  cache: true
secrets:
  - name: test/secret
    inject: env
    rotation: true
    mappings:
      password: DB_PASSWORD
`
	err := os.WriteFile(tempFile, []byte(yamlContext), 0644)
	if err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	// Change working directory to temp dir so we can use relative paths
	// This respects the new security rule that forbids absolute paths
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Failed to change working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cfg, err := LoadConfig("dso.yaml")
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Provider != "aws" {
		t.Errorf("Expected provider 'aws', got '%s'", cfg.Provider)
	}
	if cfg.Config["region"] != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", cfg.Config["region"])
	}
	if len(cfg.Secrets) != 1 {
		t.Fatalf("Expected 1 secret, got %d", len(cfg.Secrets))
	}
	if cfg.Secrets[0].Name != "test/secret" {
		t.Errorf("Expected secret name 'test/secret', got '%s'", cfg.Secrets[0].Name)
	}
    if !cfg.Agent.Cache {
        t.Errorf("Expected agent cache to be true")
    }
}

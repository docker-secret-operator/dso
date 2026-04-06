package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigV1(t *testing.T) {
	dir := t.TempDir()
	tempFile := filepath.Join(dir, "dso_v1.yaml")

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

	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	cfg, err := LoadConfig("dso_v1.yaml")
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if _, ok := cfg.Providers["aws"]; !ok {
		t.Errorf("Expected provider 'aws' to be mapped from legacy field")
	}
	if cfg.Providers["aws"].Config["region"] != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", cfg.Providers["aws"].Config["region"])
	}
	if cfg.Secrets[0].Inject.Type != "env" {
		t.Errorf("Expected legacy inject 'env', got '%s'", cfg.Secrets[0].Inject.Type)
	}
	if !cfg.Secrets[0].Rotation.Enabled {
		t.Errorf("Expected legacy rotation enabled")
	}
}

func TestLoadConfigV2(t *testing.T) {
	dir := t.TempDir()
	tempFile := filepath.Join(dir, "dso_v2.yaml")

	yamlContext := `
providers:
  my-aws:
    type: aws
    region: us-east-1
    retry:
      attempts: 5
      backoff: 10s
defaults:
  inject:
    type: file
    uid: 1000
    gid: 1000
  rotation:
    enabled: true
    strategy: restart
logging:
  level: debug
  format: json
secrets:
  - name: prod/db
    provider: my-aws
    inject:
      path: "/run/secrets/db"
    mappings:
      apiKey: API_KEY
`
	err := os.WriteFile(tempFile, []byte(yamlContext), 0644)
	if err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	oldWd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(oldWd)

	cfg, err := LoadConfig("dso_v2.yaml")
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if cfg.Providers["my-aws"].Retry.Attempts != 5 {
		t.Errorf("Expected 5 attempts, got %d", cfg.Providers["my-aws"].Retry.Attempts)
	}
	if cfg.Secrets[0].Inject.Type != "file" {
		t.Errorf("Expected defaulted inject 'file', got '%s'", cfg.Secrets[0].Inject.Type)
	}
	if cfg.Secrets[0].Inject.UID != 1000 {
		t.Errorf("Expected defaulted UID 1000, got %d", cfg.Secrets[0].Inject.UID)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected logging level debug, got %s", cfg.Logging.Level)
	}
}

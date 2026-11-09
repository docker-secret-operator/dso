package config

import (
	"os"
	"strings"
	"testing"
)

func TestWebUIConfig_Parse(t *testing.T) {
	t.Chdir(t.TempDir())
	cfgYAML := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
webui:
  enabled: true
  listen_address: 127.0.0.1:8472
  username: operator
  password_hash: $2a$10$/aGs0AIVE2e29Moe7alAb./7dKJPB2iIv4aqPKzYCZ5DVD5TG251q
  session_idle_timeout: 15m
secrets: []
`
	if err := os.WriteFile("dso.yaml", []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := LoadConfig("dso.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	w := cfg.WebUI
	if !w.Enabled {
		t.Fatal("expected webui.enabled=true")
	}
	if w.ListenAddress != "127.0.0.1:8472" {
		t.Fatalf("unexpected listen_address: %q", w.ListenAddress)
	}
	if w.Username != "operator" {
		t.Fatalf("unexpected username: %q", w.Username)
	}
	if w.PasswordHash == "" {
		t.Fatal("expected password_hash to be parsed")
	}
	if w.SessionIdleTimeout != "15m" {
		t.Fatalf("unexpected session_idle_timeout: %q", w.SessionIdleTimeout)
	}
}

func TestWebUIConfig_AbsentMeansDisabled(t *testing.T) {
	t.Chdir(t.TempDir())
	cfgYAML := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
secrets: []
`
	if err := os.WriteFile("dso.yaml", []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := LoadConfig("dso.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// Compatibility guarantee: an existing config with no webui block
	// must not start a browser-facing dashboard.
	if cfg.WebUI.Enabled {
		t.Fatalf("expected webui disabled by default, got %+v", cfg.WebUI)
	}
}

func writeWebUIFixture(t *testing.T, webuiBlock string) error {
	t.Helper()
	t.Chdir(t.TempDir())
	cfgYAML := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
secrets: []
` + webuiBlock

	if err := os.WriteFile("dso.yaml", []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadConfig("dso.yaml")
	return err
}

func TestValidate_WebUIDisabled_NoError(t *testing.T) {
	if err := writeWebUIFixture(t, ""); err != nil {
		t.Fatalf("expected no error for disabled webui, got %v", err)
	}
}

func TestValidate_WebUIEnabled_Valid_NoError(t *testing.T) {
	block := `webui:
  enabled: true
  username: operator
  password_hash: $2a$10$/aGs0AIVE2e29Moe7alAb./7dKJPB2iIv4aqPKzYCZ5DVD5TG251q
`
	if err := writeWebUIFixture(t, block); err != nil {
		t.Fatalf("expected no error for valid enabled webui, got %v", err)
	}
}

func TestValidate_WebUIEnabled_MissingUsername_Error(t *testing.T) {
	block := `webui:
  enabled: true
  password_hash: $2a$10$/aGs0AIVE2e29Moe7alAb./7dKJPB2iIv4aqPKzYCZ5DVD5TG251q
`
	err := writeWebUIFixture(t, block)
	if err == nil {
		t.Fatal("expected error for missing webui.username")
	}
}

func TestValidate_WebUIEnabled_MissingPasswordHash_Error(t *testing.T) {
	block := `webui:
  enabled: true
  username: operator
`
	err := writeWebUIFixture(t, block)
	if err == nil {
		t.Fatal("expected error for missing webui.password_hash")
	}
}

func TestValidate_WebUIEnabled_MalformedPasswordHash_Error(t *testing.T) {
	block := `webui:
  enabled: true
  username: operator
  password_hash: not-a-bcrypt-hash
`
	err := writeWebUIFixture(t, block)
	if err == nil {
		t.Fatal("expected error for malformed webui.password_hash")
	}
	// The error must not leak the actual (malformed) hash value.
	if strings.Contains(err.Error(), "not-a-bcrypt-hash") {
		t.Fatalf("error message must not include the raw password hash value: %v", err)
	}
}

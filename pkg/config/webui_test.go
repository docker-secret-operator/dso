package config

import (
	"os"
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
  password_hash: $2a$10$exampleexampleexampleexampleexampleexampleexample
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

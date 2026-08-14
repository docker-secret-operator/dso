package config

import (
	"os"
	"testing"
)

func TestNotificationsConfig_Parse(t *testing.T) {
	t.Chdir(t.TempDir())
	cfgYAML := `version: v1.0.0
mode: agent
providers:
  main:
    type: aws
    region: us-east-1
notifications:
  enabled: true
  webhooks:
    - url: https://hooks.example.com/dso
      timeout: 5s
      max_retries: 3
      events: [rotation_failed, recovery_failed]
    - url: http://internal.monitoring:9090/hook
      allow_insecure_http: true
secrets: []
`
	if err := os.WriteFile("dso.yaml", []byte(cfgYAML), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := LoadConfig("dso.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	n := cfg.Notifications
	if !n.Enabled {
		t.Fatal("expected notifications.enabled=true")
	}
	if len(n.Webhooks) != 2 {
		t.Fatalf("expected 2 webhooks, got %d", len(n.Webhooks))
	}
	w := n.Webhooks[0]
	if w.URL != "https://hooks.example.com/dso" || w.Timeout != "5s" || w.MaxRetries != 3 {
		t.Fatalf("first webhook parsed wrong: %+v", w)
	}
	if len(w.Events) != 2 || w.Events[0] != "rotation_failed" {
		t.Fatalf("events filter parsed wrong: %+v", w.Events)
	}
	if !n.Webhooks[1].AllowInsecureHTTP {
		t.Fatal("expected allow_insecure_http=true on second webhook")
	}
}

func TestNotificationsConfig_AbsentMeansDisabled(t *testing.T) {
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
	// The compatibility guarantee: an existing config with no
	// notifications block must not enable any outbound delivery.
	if cfg.Notifications.Enabled || len(cfg.Notifications.Webhooks) != 0 {
		t.Fatalf("expected notifications fully disabled by default, got %+v", cfg.Notifications)
	}
}

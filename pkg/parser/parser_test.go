package parser_test

import (
	"testing"

	"github.com/docker-secret-operator/dso/pkg/parser"
)

// validYAML is a minimal but complete dso-compose.yml that should parse cleanly.
const validYAML = `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: backend-app1
        ports:
          - "3005:3000"
      - name: mysql-db
        ports:
          - "8080:3306"
  mysql-db:
    image: mysql:8.0
  backend-app1:
    image: email-service:latest
`

func TestParse_Valid(t *testing.T) {
	cfg, err := parser.Parse([]byte(validYAML))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Version != "3.9" {
		t.Errorf("expected version 3.9, got %q", cfg.Version)
	}
	if len(cfg.Services) != 2 {
		t.Errorf("expected 2 backing services, got %d", len(cfg.Services))
	}
	if len(cfg.DSO.Containers) != 2 {
		t.Errorf("expected 2 proxy targets, got %d", len(cfg.DSO.Containers))
	}
}

func TestParse_MissingServices(t *testing.T) {
	_, err := parser.Parse([]byte(`version: "3.9"`))
	if err == nil {
		t.Fatal("expected error for missing services block, got nil")
	}
}

func TestParse_ProxyReferencesUnknownService(t *testing.T) {
	yaml := `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: ghost-service
        ports:
          - "9000:80"
  real-service:
    image: nginx:latest
`
	_, err := parser.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for proxy referencing unknown service, got nil")
	}
}

func TestParse_ProxyTargetMissingPorts(t *testing.T) {
	yaml := `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: backend-app1
        ports: []
  backend-app1:
    image: email-service:latest
`
	_, err := parser.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for proxy target with no ports, got nil")
	}
}

func TestParse_InvalidPortFormat(t *testing.T) {
	yaml := `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: backend-app1
        ports:
          - "3000"
  backend-app1:
    image: email-service:latest
`
	_, err := parser.Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for port missing ':' separator, got nil")
	}
}

func TestParse_NoDSOProxyStanza(t *testing.T) {
	// A plain compose-like file with no proxy block should parse fine.
	yaml := `
version: "3.9"
services:
  backend:
    image: nginx:latest
`
	cfg, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("expected no error for file without proxy stanza, got: %v", err)
	}
	if len(cfg.DSO.Containers) != 0 {
		t.Errorf("expected 0 proxy targets, got %d", len(cfg.DSO.Containers))
	}
}

package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/pkg/parser"
)

// Ensure fmt and strings are used (used in database subtests and legacy tests).
var _ = fmt.Sprintf
var _ = strings.Contains

// ── Standard compose auto-detection ──────────────────────────────────────────

func TestParse_ServiceWithPorts_IsEligible(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}
	svc := cfg.Services[0]
	if svc.Name != "api" {
		t.Errorf("expected service name 'api', got %q", svc.Name)
	}
	if !svc.IsEligible {
		t.Error("expected service with ports to be eligible for proxy injection")
	}
	if len(svc.Ports) != 1 || svc.Ports[0] != "3000:3000" {
		t.Errorf("expected ports [3000:3000], got %v", svc.Ports)
	}
}

func TestParse_ServiceWithoutPorts_NotEligible(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  worker:
    image: myworker:latest
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Services[0].IsEligible {
		t.Error("service without ports must not be eligible")
	}
}

// ── Database auto-exclusion ───────────────────────────────────────────────────

func TestParse_DatabaseImage_NotEligible(t *testing.T) {
	cases := []struct {
		name  string
		image string
	}{
		{"mysql latest", "mysql:latest"},
		{"mysql 8.0", "mysql:8.0"},
		{"postgres", "postgres:16"},
		{"redis", "redis:7-alpine"},
		{"mongo full registry", "docker.io/library/mongo:6"},
		{"elasticsearch", "elasticsearch:8.0.0"},
		{"rabbitmq", "rabbitmq:3-management"},
	}

	tmpl := `
version: "3.9"
services:
  db:
    image: %s
    ports:
      - "5432:5432"
`
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(fmt.Sprintf(tmpl, tc.image))
			cfg, _, err := parser.Parse(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Services[0].IsEligible {
				t.Errorf("database image %q must not be eligible for proxy injection", tc.image)
			}
		})
	}
}

// ── x-dso extension field ─────────────────────────────────────────────────────

func TestParse_XDso_ExplicitEnable_OverridesDatabase(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  db:
    image: mysql:8.0
    ports:
      - "3306:3306"
    x-dso:
      enabled: true
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Services[0].IsEligible {
		t.Error("x-dso.enabled=true must force eligibility even for database images")
	}
}

func TestParse_XDso_ExplicitDisable_OverridesAutoDetect(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
    x-dso:
      enabled: false
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Services[0].IsEligible {
		t.Error("x-dso.enabled=false must prevent proxy injection even with ports declared")
	}
}

func TestParse_XDso_StrategyPreserved(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
    x-dso:
      enabled: true
      strategy: rolling
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Services[0].DSO.Strategy != "rolling" {
		t.Errorf("expected strategy 'rolling', got %q", cfg.Services[0].DSO.Strategy)
	}
}

func TestParse_XDso_FieldStrippedFromRawFields(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
    x-dso:
      enabled: true
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Services[0].RawFields["x-dso"]; ok {
		t.Error("x-dso key must not appear in RawFields (it is a DSO-managed field)")
	}
}

// ── Field stripping ──────────────────────────────────────────────────────────

func TestParse_ContainerName_Stripped(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    container_name: should-be-gone
    ports:
      - "3000:3000"
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Services[0].RawFields["container_name"]; ok {
		t.Error("container_name must be stripped from RawFields")
	}
}

func TestParse_Ports_StrippedFromRawFields(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Services[0].RawFields["ports"]; ok {
		t.Error("ports must be extracted from RawFields into the Ports slice")
	}
}

// ── Error cases ───────────────────────────────────────────────────────────────

func TestParse_EmptyFile_ReturnsError(t *testing.T) {
	_, _, err := parser.Parse([]byte(`version: "3.9"`))
	if err == nil {
		t.Fatal("expected error for file with no services, got nil")
	}
}

func TestParse_InvalidYAML_ReturnsError(t *testing.T) {
	_, _, err := parser.Parse([]byte(`{invalid: [yaml`))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ── Mixed file (multiple services) ───────────────────────────────────────────

func TestParse_MixedFile_CorrectEligibility(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  api:
    image: myapp:latest
    ports:
      - "3000:3000"
  mysql-db:
    image: mysql:8.0
    ports:
      - "3306:3306"
  worker:
    image: myworker:latest
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(cfg.Services))
	}

	eligibility := make(map[string]bool)
	for _, svc := range cfg.Services {
		eligibility[svc.Name] = svc.IsEligible
	}

	if !eligibility["api"] {
		t.Error("api (app with ports) must be eligible")
	}
	if eligibility["mysql-db"] {
		t.Error("mysql-db (database) must NOT be eligible")
	}
	if eligibility["worker"] {
		t.Error("worker (no ports) must NOT be eligible")
	}
}

// ── Services are alphabetically ordered ──────────────────────────────────────

func TestParse_ServicesAlphabeticalOrder(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  zebra:
    image: z:latest
    ports:
      - "9000:9000"
  alpha:
    image: a:latest
    ports:
      - "1000:1000"
  middle:
    image: m:latest
    ports:
      - "5000:5000"
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(cfg.Services))
	}

	names := []string{cfg.Services[0].Name, cfg.Services[1].Name, cfg.Services[2].Name}
	expected := []string{"alpha", "middle", "zebra"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("position %d: expected %q, got %q", i, want, names[i])
		}
	}
}

// ── Legacy dso-proxy backward compat ─────────────────────────────────────────

func TestParse_LegacyDSOProxy_ProducesDeprecationWarning(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: api
        ports:
          - "3005:3000"
  api:
    image: myapp:latest
`
	_, warnings, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one deprecation warning when dso-proxy block is present")
	}
}

func TestParse_LegacyDSOProxy_ServiceStillEligible(t *testing.T) {
	const yaml = `
version: "3.9"
services:
  dso-proxy:
    containers:
      - name: api
        ports:
          - "3005:3000"
  api:
    image: myapp:latest
`
	cfg, _, err := parser.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var apiSvc *struct{ IsEligible bool }
	for _, svc := range cfg.Services {
		if svc.Name == "api" {
			apiSvc = &struct{ IsEligible bool }{svc.IsEligible}
		}
	}
	if apiSvc == nil {
		t.Fatal("api service not found in parsed output")
	}
	if !apiSvc.IsEligible {
		t.Error("api service referenced by legacy dso-proxy block must be eligible")
	}
}

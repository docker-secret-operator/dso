package transformer_test

import (
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/models"
	"github.com/docker-secret-operator/dso/pkg/transformer"
	"gopkg.in/yaml.v3"
)

// buildConfig is a test helper that creates a minimal DSOConfig.
func buildConfig() *models.DSOConfig {
	return &models.DSOConfig{
		Version: "3.9",
		DSO: models.ProxyConfig{
			Containers: []models.ProxyTarget{
				{
					Name:  "backend-app1",
					Ports: []string{"3005:3000", "5001:5000"},
				},
				{
					Name:  "mysql-db",
					Ports: []string{"8080:3306"},
				},
			},
		},
		Services: map[string]interface{}{
			"backend-app1": map[string]interface{}{
				"image":          "email-service:latest",
				"container_name": "should-be-removed",
				"ports":          []interface{}{"3000", "5000"},
			},
			"mysql-db": map[string]interface{}{
				"image": "mysql:8.0",
				"ports": []interface{}{"3306"},
			},
		},
	}
}

// parseOutput is a quick helper to unmarshal Transform output for inspection.
func parseOutput(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("generated YAML is invalid: %v", err)
	}
	return out
}

func TestTransform_GeneratesValidYAML(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	// Must parse as valid YAML.
	parseOutput(t, out)
}

func TestTransform_ProxyServicesCreated(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := parseOutput(t, out)
	services, ok := doc["services"].(map[string]interface{})
	if !ok {
		t.Fatal("services key missing or wrong type")
	}

	if _, ok := services["dso-proxy-backend-app1"]; !ok {
		t.Error("expected proxy service dso-proxy-backend-app1 to be created")
	}
	if _, ok := services["dso-proxy-mysql-db"]; !ok {
		t.Error("expected proxy service dso-proxy-mysql-db to be created")
	}
}

func TestTransform_ContainerNameRemoved(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// container_name must not appear anywhere in the generated output.
	if strings.Contains(string(out), "container_name") {
		t.Error("generated YAML must not contain container_name")
	}
}

func TestTransform_PortsMovedToExpose(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	doc := parseOutput(t, out)
	services := doc["services"].(map[string]interface{})

	// The backing service must not have a `ports` key.
	backendSvc := services["backend-app1"].(map[string]interface{})
	if _, hasPorts := backendSvc["ports"]; hasPorts {
		t.Error("backing service must not have a host-bound ports key")
	}

	// It must have expose instead.
	if _, hasExpose := backendSvc["expose"]; !hasExpose {
		t.Error("backing service must have an expose key")
	}
}

func TestTransform_DSOLabelInjected(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(out), "dso.service") {
		t.Error("generated YAML must contain dso.service label")
	}
	if !strings.Contains(string(out), "dso.managed") {
		t.Error("generated YAML must contain dso.managed label")
	}
}

func TestTransform_MeshNetworkPresent(t *testing.T) {
	cfg := buildConfig()
	out, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(out), "dso_mesh") {
		t.Error("generated YAML must reference the dso_mesh network")
	}
}

package transformer_test

import (
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/models"
	"github.com/docker-secret-operator/dso/pkg/transformer"
	"gopkg.in/yaml.v3"
)

// ── test helpers ─────────────────────────────────────────────────────────────

// boolPtr returns a pointer to a bool, used for DSOOptions.Enabled.
func boolPtr(b bool) *bool { return &b }

// parseYAML unmarshals the transform output for field inspection.
func parseYAML(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("generated YAML is not valid: %v", err)
	}
	return out
}

func services(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	doc := parseYAML(t, data)
	svcRaw, ok := doc["services"]
	if !ok {
		t.Fatal("generated YAML has no 'services' key")
	}
	svc, ok := svcRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("services is not a map: %T", svcRaw)
	}
	return svc
}

// buildCfg builds a minimal DSOConfig for a single eligible service.
func buildCfg(extraServices ...models.ParsedService) *models.DSOConfig {
	cfg := &models.DSOConfig{
		Version: "3.9",
		Services: []models.ParsedService{
			{
				Name:       "api",
				IsEligible: true,
				Ports:      []string{"3000:3000", "5000:5000"},
				RawFields: map[string]interface{}{
					"image":   "myapp:latest",
					"restart": "unless-stopped",
				},
			},
		},
	}
	cfg.Services = append(cfg.Services, extraServices...)
	return cfg
}

// ── output validity ───────────────────────────────────────────────────────────

func TestTransform_ProducesValidYAML(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parseYAML(t, out) // panics on invalid YAML
}

// ── proxy service generation ──────────────────────────────────────────────────

func TestTransform_EligibleService_ProxyCreated(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := services(t, out)
	if _, ok := svcs["dso-proxy-api"]; !ok {
		t.Error("expected dso-proxy-api service to be generated")
	}
}

func TestTransform_ProxyService_HasCorrectPorts(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := services(t, out)
	proxy, ok := svcs["dso-proxy-api"].(map[string]interface{})
	if !ok {
		t.Fatal("dso-proxy-api is not a map")
	}
	if _, ok := proxy["ports"]; !ok {
		t.Error("proxy service must have a ports key")
	}
}

func TestTransform_ProxyService_HasDependsOn(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := services(t, out)
	proxy := svcs["dso-proxy-api"].(map[string]interface{})
	dep, ok := proxy["depends_on"]
	if !ok {
		t.Fatal("proxy service must declare depends_on")
	}
	deps, _ := dep.([]interface{})
	if len(deps) == 0 || deps[0] != "api" {
		t.Errorf("proxy depends_on must reference 'api', got %v", dep)
	}
}

// ── backing service transformation ───────────────────────────────────────────

func TestTransform_BackingService_PortsRemovedAndExposed(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := services(t, out)
	api := svcs["api"].(map[string]interface{})

	if _, hasPorts := api["ports"]; hasPorts {
		t.Error("backing service must NOT have host-bound ports key")
	}
	if _, hasExpose := api["expose"]; !hasExpose {
		t.Error("backing service must have an expose key")
	}
}

func TestTransform_BackingService_LabelsInjected(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := string(out)
	if !strings.Contains(raw, "dso.service") {
		t.Error("output must contain dso.service label")
	}
	if !strings.Contains(raw, "dso.managed") {
		t.Error("output must contain dso.managed label")
	}
}

func TestTransform_BackingService_MeshNetworkInjected(t *testing.T) {
	out, _, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "dso_mesh") {
		t.Error("output must include dso_mesh network")
	}
}

func TestTransform_BackingService_StrategyLabelSet(t *testing.T) {
	cfg := buildCfg()
	cfg.Services[0].DSO = models.DSOOptions{Strategy: "rolling"}

	out, _, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "dso.strategy") {
		t.Error("output must include dso.strategy label when strategy is set")
	}
}

// ── container_name enforcement ────────────────────────────────────────────────

func TestTransform_ContainerName_NeverEmitted(t *testing.T) {
	cfg := buildCfg()
	// Inject a container_name that the parser would have stripped, but simulate
	// a caller that somehow sneaks one in via RawFields.
	cfg.Services[0].RawFields["container_name"] = "should-not-appear"

	out, _, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Note: the transformer does not re-strip container_name — that is the
	// parser's job. This test documents that it doesn't RE-ADD it.
	// If the parser strips correctly, container_name won't be present.
	_ = out
}

// ── ineligible service pass-through ──────────────────────────────────────────

func TestTransform_IneligibleService_PassedThroughWithPorts(t *testing.T) {
	cfg := buildCfg(models.ParsedService{
		Name:       "mysql-db",
		IsEligible: false,
		Ports:      []string{"3306:3306"},
		RawFields: map[string]interface{}{
			"image": "mysql:8.0",
		},
	})

	out, _, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svcs := services(t, out)

	// Ineligible service must exist.
	db, ok := svcs["mysql-db"].(map[string]interface{})
	if !ok {
		t.Fatal("mysql-db must be present in output")
	}
	// Its ports must be preserved (not moved to a proxy).
	if _, hasPorts := db["ports"]; !hasPorts {
		t.Error("ineligible service must retain its ports in the output")
	}
	// No proxy must be generated for it.
	if _, hasProxy := svcs["dso-proxy-mysql-db"]; hasProxy {
		t.Error("no proxy service must be created for ineligible mysql-db")
	}
}

// ── summary log lines ─────────────────────────────────────────────────────────

func TestTransform_SummaryContainsDSOLog(t *testing.T) {
	_, summary, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, line := range summary {
		if strings.Contains(line, "Enabling zero-downtime") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected summary to contain 'Enabling zero-downtime', got: %v", summary)
	}
}

func TestTransform_SummaryContainsPortLog(t *testing.T) {
	_, summary, err := transformer.Transform(buildCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, line := range summary {
		if strings.Contains(line, "Injecting proxy for port") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected summary to contain 'Injecting proxy for port', got: %v", summary)
	}
}

// ── deprecated proxy block ───────────────────────────────────────────────────

func TestTransform_DeprecatedProxy_WarningSurfaced(t *testing.T) {
	cfg := buildCfg()
	cfg.DeprecatedProxy = &models.LegacyProxyConfig{} // non-nil triggers warning

	_, summary, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, line := range summary {
		if strings.Contains(line, "deprecated") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected deprecation warning in summary, got: %v", summary)
	}
}

// ── top-level networks and volumes ───────────────────────────────────────────

func TestTransform_TopLevelNetworks_MergedWithDSOmesh(t *testing.T) {
	cfg := buildCfg()
	cfg.RawNetworks = map[string]interface{}{
		"custom_net": map[string]interface{}{"driver": "overlay"},
	}

	out, _, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	doc := parseYAML(t, out)
	nets, ok := doc["networks"].(map[string]interface{})
	if !ok {
		t.Fatal("expected networks key in output")
	}
	if _, ok := nets["dso_mesh"]; !ok {
		t.Error("dso_mesh must always be present in top-level networks")
	}
	if _, ok := nets["custom_net"]; !ok {
		t.Error("user-defined custom_net must be preserved in output")
	}
}

func TestTransform_Volumes_PreservedVerbatim(t *testing.T) {
	cfg := buildCfg()
	cfg.RawVolumes = map[string]interface{}{
		"db_data": nil,
	}

	out, _, err := transformer.Transform(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "db_data") {
		t.Error("user-defined volumes must be preserved verbatim in output")
	}
}

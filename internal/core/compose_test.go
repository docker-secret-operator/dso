package core

import (
	"testing"

	"github.com/docker-secret-operator/dso/pkg/config"
)

func TestParsePortEntry(t *testing.T) {
	tests := []struct {
		input         string
		expectedHost  string
		expectedCont  string
	}{
		{"3306", "", "3306"},
		{"3306/tcp", "", "3306"},
		{"8080:80", "8080", "80"},
		{"8080:80/udp", "8080", "80"},
		{"0.0.0.0:8080:80", "8080", "80"},
		{"127.0.0.1:5432:5432/tcp", "5432", "5432"},
	}

	for _, tc := range tests {
		h, c := parsePortEntry(tc.input)
		if h != tc.expectedHost || c != tc.expectedCont {
			t.Errorf("parsePortEntry(%q) = (%q, %q), expected (%q, %q)", tc.input, h, c, tc.expectedHost, tc.expectedCont)
		}
	}
}

func TestStripAndLabelHostPorts(t *testing.T) {
	svc := map[string]interface{}{
		"ports": []interface{}{
			"8080:80",
			"3306",
			"0.0.0.0:443:443/tcp",
		},
		"image": "nginx:latest",
	}

	result := stripAndLabelHostPorts(svc)

	// Ports key should be removed
	if _, ok := result["ports"]; ok {
		t.Errorf("Expected 'ports' key to be removed")
	}

	// Image should remain
	if result["image"] != "nginx:latest" {
		t.Errorf("Expected image to remain intact")
	}

	// Expose should have 80, 3306, 443
	exposeRaw, ok := result["expose"]
	if !ok {
		t.Fatalf("Expected 'expose' key")
	}
	exposeList, ok := exposeRaw.([]interface{})
	if !ok {
		t.Fatalf("'expose' key is not a slice")
	}

	expectedExpose := map[string]bool{"80": true, "3306": true, "443": true}
	for _, e := range exposeList {
		if !expectedExpose[e.(string)] {
			t.Errorf("Unexpected expose port: %v", e)
		}
		delete(expectedExpose, e.(string))
	}
	if len(expectedExpose) > 0 {
		t.Errorf("Missing expose ports: %v", expectedExpose)
	}

	// Labels should have dso.host_ports
	labelsRaw, ok := result["labels"]
	if !ok {
		t.Fatalf("Expected 'labels' key")
	}
	labels, ok := labelsRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("'labels' key is not a map")
	}

	if labels["dso.host_ports"] != "8080:80,443:443" {
		t.Errorf("Unexpected dso.host_ports label: %v", labels["dso.host_ports"])
	}
}

func TestServiceIsTarget(t *testing.T) {
	sec := config.SecretMapping{
		Name: "test_secret",
		Mappings: map[string]string{
			"DB_PASS": "DB_PASS",
		},
	}

	// Test 1: Container targeting
	sec.Targets.Containers = []string{"db"}
	if !serviceIsTarget("db", map[string]interface{}{}, sec) {
		t.Errorf("Expected serviceIsTarget to match container name")
	}
	if serviceIsTarget("api", map[string]interface{}{}, sec) {
		t.Errorf("Expected serviceIsTarget to not match wrong container name")
	}

	// Test 2: Label targeting
	sec.Targets.Containers = nil
	sec.Targets.Labels = map[string]string{"env": "prod"}
	svcWithLabel := map[string]interface{}{
		"labels": map[string]interface{}{"env": "prod"},
	}
	if !serviceIsTarget("api", svcWithLabel, sec) {
		t.Errorf("Expected serviceIsTarget to match labels")
	}

	// Test 3: Env fallback (consumes secret)
	sec.Targets.Labels = nil
	svcWithEnv := map[string]interface{}{
		"environment": map[string]interface{}{
			"DB_PASS": "placeholder",
		},
	}
	if !serviceIsTarget("api", svcWithEnv, sec) {
		t.Errorf("Expected serviceIsTarget to match environment variable")
	}
}

func TestServiceConsumesSecret(t *testing.T) {
	sec := config.SecretMapping{
		Mappings: map[string]string{
			"API_KEY": "API_KEY",
		},
	}

	svcMap := map[string]interface{}{
		"environment": map[string]interface{}{
			"API_KEY": "some-val",
		},
	}
	if !serviceConsumesSecret(svcMap, sec) {
		t.Errorf("Expected true for map environment matching API_KEY")
	}

	svcSlice := map[string]interface{}{
		"environment": []interface{}{
			"API_KEY=some-val",
		},
	}
	if !serviceConsumesSecret(svcSlice, sec) {
		t.Errorf("Expected true for slice environment matching API_KEY")
	}

	svcDsoPrefix := map[string]interface{}{
		"environment": []interface{}{
			"OTHER_VAR=dso://test_secret",
		},
	}
	if !serviceConsumesSecret(svcDsoPrefix, sec) {
		t.Errorf("Expected true for slice environment matching dso:// prefix")
	}
}

func TestSetDebug(t *testing.T) {
	SetDebug(true)
	if !debugMode {
		t.Errorf("Expected debugMode to be true")
	}
	SetDebug(false)
	if debugMode {
		t.Errorf("Expected debugMode to be false")
	}
}

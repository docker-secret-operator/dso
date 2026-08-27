package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	dsoConfig "github.com/docker-secret-operator/dso/pkg/config"
)

func driftContainersHandler(t *testing.T, containers []map[string]interface{}) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			w.WriteHeader(http.StatusOK)
			b, err := json.Marshal(containers)
			if err != nil {
				t.Fatalf("failed to marshal fixture containers: %v", err)
			}
			_, _ = w.Write(b)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})
}

func secretWithTarget(name string, targets ...string) dsoConfig.SecretMapping {
	return dsoConfig.SecretMapping{
		Name:    name,
		Targets: dsoConfig.TargetConfig{Containers: targets},
	}
}

// Test 1 — all expected containers/mappings exist -> 0 findings.
func TestComputeDrift_AllInSync_NoFindings(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{
			"Id":     "c1",
			"Names":  []string{"/backend"},
			"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "database-password"},
		},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	summary, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %+v", findings)
	}
	if summary.Total != 1 || summary.InSync != 1 || summary.Drifted != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

// Test 2 — expected container missing -> MISSING_CONTAINER.
func TestComputeDrift_MissingContainer(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	summary, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	if len(findings) != 1 || findings[0].DriftType != DriftMissingContainer {
		t.Fatalf("expected exactly 1 MISSING_CONTAINER finding, got %+v", findings)
	}
	if summary.Total != 1 || summary.Drifted != 1 || summary.InSync != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

// Test 3 — container exists but expected label is missing -> MISSING_MAPPING.
func TestComputeDrift_MissingMapping(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{
			"Id":     "c1",
			"Names":  []string{"/backend"},
			"Labels": map[string]string{},
		},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	_, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	if len(findings) != 1 || findings[0].DriftType != DriftMissingMapping {
		t.Fatalf("expected exactly 1 MISSING_MAPPING finding, got %+v", findings)
	}
}

// Test 4 — wrong mapping (labels point elsewhere) -> MAPPING_MISMATCH.
func TestComputeDrift_MappingMismatch(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{
			"Id":     "c1",
			"Names":  []string{"/backend"},
			"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "some-other-secret"},
		},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	_, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	if len(findings) != 1 || findings[0].DriftType != DriftMappingMismatch {
		t.Fatalf("expected exactly 1 MAPPING_MISMATCH finding, got %+v", findings)
	}
}

// Test 5 — similar-looking unrelated container must NOT produce a false
// positive: exact name/ID match only, no substring matching.
func TestComputeDrift_SimilarNamedContainer_NoFalsePositive(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{
			"Id":     "c1",
			"Names":  []string{"/backend-canary"}, // similar to "backend" but not equal
			"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "database-password"},
		},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	_, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	// "backend" has no exact match -> MISSING_CONTAINER is the correct,
	// honest finding here (the declared target genuinely isn't running) --
	// what must NOT happen is treating "backend-canary" as a fuzzy match
	// for "backend" and reporting IN_SYNC or a mismatch against it.
	if len(findings) != 1 || findings[0].DriftType != DriftMissingContainer || findings[0].Container != "" {
		t.Fatalf("expected a MISSING_CONTAINER finding with no container attribution (no fuzzy match), got %+v", findings)
	}
}

// Test 6 — no configured targets -> empty/healthy response.
func TestComputeDrift_NoConfiguredTargets_EmptyHealthy(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{"Id": "c1", "Names": []string{"/unrelated"}, "Labels": map[string]string{}},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		{Name: "no-explicit-target"}, // Targets.Containers is empty -> skipped entirely
	}}

	summary, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings when no secret declares explicit targets, got %+v", findings)
	}
	if summary.Total != 0 {
		t.Fatalf("expected summary.Total == 0, got %+v", summary)
	}
}

// Test 7 — the serialized response must never contain secret values.
func TestComputeDrift_ResponseContainsNoSecretValues(t *testing.T) {
	handler := driftContainersHandler(t, []map[string]interface{}{
		{
			"Id":     "c1",
			"Names":  []string{"/backend"},
			"Labels": map[string]string{"dso.reloader": "true", "dso.secrets": "some-other-secret"},
		},
	})
	rc := newMockController(t, handler)
	rc.Config = &dsoConfig.Config{Secrets: []dsoConfig.SecretMapping{
		secretWithTarget("database-password", "backend"),
	}}

	summary, findings, err := rc.ComputeDrift(context.Background())
	if err != nil {
		t.Fatalf("ComputeDrift error: %v", err)
	}
	blob, err := json.Marshal(map[string]interface{}{"summary": summary, "findings": findings})
	if err != nil {
		t.Fatalf("failed to marshal drift response: %v", err)
	}
	serialized := string(blob)
	for _, forbidden := range []string{"password", "secret_value", "token", "credential"} {
		if strings.Contains(strings.ToLower(serialized), forbidden) && forbidden != "password" {
			t.Fatalf("drift response unexpectedly contains %q: %s", forbidden, serialized)
		}
	}
	// "password" is expected to appear only as part of the SECRET NAME
	// ("database-password"), never as a field holding an actual value --
	// confirm the response has no separate value-shaped field.
	if strings.Contains(serialized, "\"value\"") || strings.Contains(serialized, "\"actual_value\"") {
		t.Fatalf("drift response contains a value-shaped field: %s", serialized)
	}
}

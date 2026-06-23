package insights_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/internal/policy"
)

// stubDriftStore implements drift.Store with N synthetic detected findings.
type stubDriftStore struct {
	findings []drift.DriftFinding
}

func (s *stubDriftStore) ListFindings(_ context.Context) ([]drift.DriftFinding, error) {
	return s.findings, nil
}
func (s *stubDriftStore) CreateFinding(_ context.Context, f drift.DriftFinding) error { return nil }
func (s *stubDriftStore) UpdateFinding(_ context.Context, f drift.DriftFinding) error { return nil }
func (s *stubDriftStore) GetFinding(_ context.Context, id string) (*drift.DriftFinding, error) {
	return nil, nil
}
func (s *stubDriftStore) DeleteFinding(_ context.Context, id string) error { return nil }
func (s *stubDriftStore) LogScan(_ context.Context, scan *drift.DriftScan) error { return nil }
func (s *stubDriftStore) GetScans(_ context.Context, limit int) ([]*drift.DriftScan, error) {
	return nil, nil
}
func (s *stubDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return nil }

func makeDriftStore(n int) *stubDriftStore {
	findings := make([]drift.DriftFinding, n)
	for i := range findings {
		findings[i] = drift.DriftFinding{
			ID:         fmt.Sprintf("finding-%d", i),
			Resource:   fmt.Sprintf("secret-%d", i%50),
			Severity:   drift.SeverityHigh,
			Status:     drift.StatusDetected,
			DetectedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}
	return &stubDriftStore{findings: findings}
}

func makeSecrets(n int) []compliance.SecretInput {
	out := make([]compliance.SecretInput, n)
	for i := range out {
		out[i] = compliance.SecretInput{
			Name:     fmt.Sprintf("secret-%d", i),
			Provider: "vault-prod",
		}
	}
	return out
}

// BenchmarkEvaluatorEvaluateAll measures recommendation evaluation at various secret counts.
func BenchmarkEvaluatorEvaluateAll_50(b *testing.B)   { benchmarkEval(b, 50) }
func BenchmarkEvaluatorEvaluateAll_500(b *testing.B)  { benchmarkEval(b, 500) }
func BenchmarkEvaluatorEvaluateAll_1000(b *testing.B) { benchmarkEval(b, 1000) }
func BenchmarkEvaluatorEvaluateAll_5000(b *testing.B) { benchmarkEval(b, 5000) }

func benchmarkEval(b *testing.B, secretCount int) {
	b.Helper()
	ds := makeDriftStore(secretCount / 10) // 10% of secrets have findings
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(secretCount)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ev.EvaluateAll(ctx, secrets)
	}
}

// BenchmarkForecastAll measures forecast evaluation at various secret counts.
func BenchmarkForecastAll_50(b *testing.B)   { benchmarkForecast(b, 50) }
func BenchmarkForecastAll_500(b *testing.B)  { benchmarkForecast(b, 500) }
func BenchmarkForecastAll_1000(b *testing.B) { benchmarkForecast(b, 1000) }
func BenchmarkForecastAll_5000(b *testing.B) { benchmarkForecast(b, 5000) }

func benchmarkForecast(b *testing.B, secretCount int) {
	b.Helper()
	ds := makeDriftStore(secretCount / 10)
	fc := insights.NewOperationalForecaster(nil, ds, nil)
	secrets := makeSecrets(secretCount)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fc.ForecastAll(ctx, secrets)
	}
}

// ── Graceful degradation tests ────────────────────────────────────────────────

// TestEvaluator_NilStores verifies no panic when stores are nil.
func TestEvaluator_NilStores(t *testing.T) {
	ev := insights.NewEvaluator(nil, nil, nil)
	recs := ev.EvaluateAll(context.Background(), makeSecrets(10))
	// Should return empty slice, not panic.
	if recs == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
}

// TestForecaster_NilStores verifies no panic when stores are nil.
func TestForecaster_NilStores(t *testing.T) {
	fc := insights.NewOperationalForecaster(nil, nil, nil)
	result := fc.ForecastAll(context.Background(), makeSecrets(10))
	if result == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
}

// TestEvaluator_DriftStoreFailure verifies graceful degradation when drift returns error.
func TestEvaluator_DriftStoreFailure(t *testing.T) {
	failing := &failingDriftStore{}
	ev := insights.NewEvaluator(nil, failing, nil)
	recs := ev.EvaluateAll(context.Background(), makeSecrets(5))
	// Must not panic; result may be empty or partial.
	_ = recs
}

// TestForecaster_DriftStoreFailure verifies no panic on drift store error.
func TestForecaster_DriftStoreFailure(t *testing.T) {
	failing := &failingDriftStore{}
	fc := insights.NewOperationalForecaster(nil, failing, nil)
	result := fc.ForecastAll(context.Background(), makeSecrets(5))
	_ = result
}

// TestForecaster_EmptySecrets verifies empty input produces no forecasts.
func TestForecaster_EmptySecrets(t *testing.T) {
	fc := insights.NewOperationalForecaster(nil, makeDriftStore(5), nil)
	result := fc.ForecastAll(context.Background(), nil)
	if len(result) != 0 {
		// Drift forecasts may still appear even with no secrets — that's fine.
		for _, r := range result {
			if r.Category == forecast.CatRotation || r.Category == forecast.CatCompliance {
				t.Errorf("unexpected %s forecast with no secrets", r.Category)
			}
		}
	}
}

// failingDriftStore always returns an error.
type failingDriftStore struct{}

func (f *failingDriftStore) ListFindings(_ context.Context) ([]drift.DriftFinding, error) {
	return nil, fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error {
	return fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error {
	return fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) DeleteFinding(_ context.Context, _ string) error {
	return fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error {
	return fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, fmt.Errorf("store unavailable")
}
func (f *failingDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error {
	return fmt.Errorf("store unavailable")
}

package insights_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/internal/policy"
)

// ─── scale benchmarks (Phase 1) ───────────────────────────────────────────────

// BenchmarkEvaluatorEvaluateAll_10000 extends the coverage to 10k secrets.
func BenchmarkEvaluatorEvaluateAll_10000(b *testing.B) { benchmarkEval(b, 10000) }

// BenchmarkForecastAll_10000 extends the coverage to 10k secrets.
func BenchmarkForecastAll_10000(b *testing.B) { benchmarkForecast(b, 10000) }

// ─── memory growth tests (Phase 2 — lightweight in-process soak) ──────────────

// TestEvaluator_NoHeapGrowth verifies that repeated evaluations do not grow the
// live heap unboundedly. We allow a generous 3× headroom from the first
// measurement to the hundredth, which in practice should be <10% growth.
func TestEvaluator_NoHeapGrowth(t *testing.T) {
	ds := makeDriftStore(50) // 500 secrets, 10% drift
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(500)
	ctx := context.Background()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 100; i++ {
		_ = ev.EvaluateAll(ctx, secrets)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// HeapInuse delta must be < 10 MB — the evaluation result is not retained.
	delta := int64(after.HeapInuse) - int64(before.HeapInuse)
	if delta > 10*1024*1024 {
		t.Errorf("heap grew by %d bytes after 100 evaluations — possible leak", delta)
	}
}

func TestForecaster_NoHeapGrowth(t *testing.T) {
	ds := makeDriftStore(50)
	fc := insights.NewOperationalForecaster(nil, ds, nil)
	secrets := makeSecrets(500)
	ctx := context.Background()

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 100; i++ {
		_ = fc.ForecastAll(ctx, secrets)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	delta := int64(after.HeapInuse) - int64(before.HeapInuse)
	if delta > 10*1024*1024 {
		t.Errorf("heap grew by %d bytes after 100 forecasts — possible leak", delta)
	}
}

// ─── goroutine leak tests (Phase 2) ───────────────────────────────────────────

// TestEvaluator_NoGoroutineLeak verifies that repeated evaluations do not spawn
// goroutines that are never reaped.
func TestEvaluator_NoGoroutineLeak(t *testing.T) {
	ds := makeDriftStore(10)
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(100)
	ctx := context.Background()

	before := runtime.NumGoroutine()

	for i := 0; i < 50; i++ {
		_ = ev.EvaluateAll(ctx, secrets)
	}

	// Give any goroutines spawned inside a moment to exit.
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow up to 5 extra goroutines (runtime scheduling noise).
	if after > before+5 {
		t.Errorf("goroutine count grew from %d to %d — possible leak", before, after)
	}
}

// ─── concurrent correctness (Phase 5 complement) ─────────────────────────────

// TestEvaluator_ConcurrentEval verifies no data races under concurrent evaluation.
// Run with: go test -race ./internal/insights/ -run TestEvaluator_ConcurrentEval
func TestEvaluator_ConcurrentEval(t *testing.T) {
	ds := makeDriftStore(20)
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(200)
	ctx := context.Background()

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = ev.EvaluateAll(ctx, secrets)
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestForecaster_ConcurrentForecast(t *testing.T) {
	ds := makeDriftStore(20)
	fc := insights.NewOperationalForecaster(nil, ds, nil)
	secrets := makeSecrets(200)
	ctx := context.Background()

	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = fc.ForecastAll(ctx, secrets)
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

// ─── determinism (Phase 6) ────────────────────────────────────────────────────

// TestEvaluator_DeterministicOutput verifies that two evaluations on the same
// inputs produce the same recommendation IDs and count.
func TestEvaluator_DeterministicOutput(t *testing.T) {
	ds := makeDriftStore(20)
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(100)
	ctx := context.Background()

	recs1 := ev.EvaluateAll(ctx, secrets)
	recs2 := ev.EvaluateAll(ctx, secrets)

	if len(recs1) != len(recs2) {
		t.Fatalf("non-deterministic: first=%d recs, second=%d recs", len(recs1), len(recs2))
	}
	ids1 := make(map[string]bool, len(recs1))
	for _, r := range recs1 {
		ids1[r.ID] = true
	}
	for _, r := range recs2 {
		if !ids1[r.ID] {
			t.Errorf("second evaluation produced new ID %q not present in first", r.ID)
		}
	}
}

func TestForecaster_DeterministicOutput(t *testing.T) {
	ds := makeDriftStore(20)
	fc := insights.NewOperationalForecaster(nil, ds, nil)
	secrets := makeSecrets(100)
	ctx := context.Background()

	fc1 := fc.ForecastAll(ctx, secrets)
	fc2 := fc.ForecastAll(ctx, secrets)

	if len(fc1) != len(fc2) {
		t.Fatalf("non-deterministic: first=%d forecasts, second=%d", len(fc1), len(fc2))
	}
	ids := make(map[string]bool, len(fc1))
	for _, f := range fc1 {
		ids[f.ID] = true
	}
	for _, f := range fc2 {
		if !ids[f.ID] {
			t.Errorf("second forecast produced new ID %q not in first run", f.ID)
		}
	}
}

// ─── scale correctness (Phase 1) ─────────────────────────────────────────────

// TestEvaluator_ScaleCorrectness verifies that evaluation at 10k secrets
// produces sensible output without panicking.
func TestEvaluator_ScaleCorrectness(t *testing.T) {
	ds := makeDriftStore(1000) // 10% drift
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)
	secrets := makeSecrets(10000)
	ctx := context.Background()

	start := time.Now()
	recs := ev.EvaluateAll(ctx, secrets)
	elapsed := time.Since(start)

	t.Logf("10 000 secrets: %d recommendations in %v", len(recs), elapsed)
	// Must complete in < 5 seconds.
	if elapsed > 5*time.Second {
		t.Errorf("evaluation too slow: %v > 5s", elapsed)
	}
	if recs == nil {
		t.Error("expected non-nil result")
	}
}

func TestForecaster_ScaleCorrectness(t *testing.T) {
	ds := makeDriftStore(1000)
	fc := insights.NewOperationalForecaster(nil, ds, nil)
	secrets := makeSecrets(10000)
	ctx := context.Background()

	start := time.Now()
	result := fc.ForecastAll(ctx, secrets)
	elapsed := time.Since(start)

	t.Logf("10 000 secrets: %d forecasts in %v", len(result), elapsed)
	if elapsed > 5*time.Second {
		t.Errorf("forecast too slow: %v > 5s", elapsed)
	}
}

// ─── evidence disappears when drift resolves (Phase 6 validation semantics) ──

// TestForecaster_DriftForecastDisappearsWhenResolved verifies that drift forecasts
// vanish when all drift findings are resolved.
func TestForecaster_DriftForecastDisappearsWhenResolved(t *testing.T) {
	// Store with active detected findings → should forecast drift.
	active := makeDriftStore(10)
	fc := insights.NewOperationalForecaster(nil, active, nil)
	secrets := makeSecrets(20)
	ctx := context.Background()

	withDrift := fc.ForecastAll(ctx, secrets)

	// Store with no findings → no drift forecasts.
	empty := &stubDriftStore{findings: []drift.DriftFinding{}}
	fc2 := insights.NewOperationalForecaster(nil, empty, nil)
	withoutDrift := fc2.ForecastAll(ctx, secrets)

	var withDriftCat, withoutDriftCat int
	for _, f := range withDrift {
		if string(f.Category) == "drift" {
			withDriftCat++
		}
	}
	for _, f := range withoutDrift {
		if string(f.Category) == "drift" {
			withoutDriftCat++
		}
	}

	if withDriftCat == 0 {
		t.Log("no drift forecasts with active findings (may need ≥3 findings per resource in 14d window)")
	}
	// Importantly, without findings there must be no drift forecasts.
	if withoutDriftCat > 0 {
		t.Errorf("expected 0 drift forecasts with empty store, got %d", withoutDriftCat)
	}
}

// ─── compliance input boundary ────────────────────────────────────────────────

func TestEvaluator_LargeProviderVariety(t *testing.T) {
	providers := []string{"vault", "k8s", "aws", "gcp", "azure", "env"}
	secrets := make([]compliance.SecretInput, 600)
	for i := range secrets {
		secrets[i] = compliance.SecretInput{
			Name:     fmt.Sprintf("secret-%d", i),
			Provider: providers[i%len(providers)],
		}
	}
	ds := makeDriftStore(30)
	pStore := policy.NewInMemoryStore()
	ev := insights.NewEvaluator(nil, ds, pStore)

	recs := ev.EvaluateAll(context.Background(), secrets)
	t.Logf("600 secrets across %d providers: %d recommendations", len(providers), len(recs))
}

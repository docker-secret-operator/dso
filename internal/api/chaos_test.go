package api_test

// Phase 4 — Chaos Testing
// Phase 6 — Recovery Guarantees
// Phase 7 — Security Review
// Phase 8 — Observability Validation
//
// Every test in this file must pass with: go test -race ./internal/api/

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/recommendation"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// ─── helpers shared with degradation_test.go ─────────────────────────────────

func chaosAdminCtx() context.Context {
	return auth.WithAuthenticatedUser(context.Background(),
		&storage.User{Username: "admin", Role: "admin"})
}

func chaosViewerCtx() context.Context {
	return auth.WithAuthenticatedUser(context.Background(),
		&storage.User{Username: "viewer", Role: "viewer"})
}

func chaosUnauthCtx() context.Context { return context.Background() }

func chaosGet(h http.Handler, path string, ctx context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func chaosPost(h http.Handler, path string, ctx context.Context) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// ─── stubDriftStore returns a fixed set of findings ──────────────────────────

type stubDriftStore struct {
	findings []drift.DriftFinding
}

func (s *stubDriftStore) ListFindings(_ context.Context) ([]drift.DriftFinding, error) {
	return s.findings, nil
}
func (s *stubDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (s *stubDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (s *stubDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, nil
}
func (s *stubDriftStore) DeleteFinding(_ context.Context, _ string) error { return nil }
func (s *stubDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error { return nil }
func (s *stubDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, nil
}
func (s *stubDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return nil }

// ─── slowDriftStore simulates a slow dependency ───────────────────────────────

type slowDriftStore struct {
	delay time.Duration
	inner *stubDriftStore
}

func (s *slowDriftStore) ListFindings(ctx context.Context) ([]drift.DriftFinding, error) {
	select {
	case <-time.After(s.delay):
		return s.inner.ListFindings(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (s *slowDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (s *slowDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, nil
}
func (s *slowDriftStore) DeleteFinding(_ context.Context, _ string) error { return nil }
func (s *slowDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error { return nil }
func (s *slowDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, nil
}
func (s *slowDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return nil }

// ─── errDriftStore returns a specific error ───────────────────────────────────

type errDriftStore struct{ err error }

func (e *errDriftStore) ListFindings(_ context.Context) ([]drift.DriftFinding, error) {
	return nil, e.err
}
func (e *errDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error  { return e.err }
func (e *errDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error  { return e.err }
func (e *errDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, e.err
}
func (e *errDriftStore) DeleteFinding(_ context.Context, _ string) error              { return e.err }
func (e *errDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error          { return e.err }
func (e *errDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, e.err
}
func (e *errDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return e.err }

// ─── flakeDriftStore alternates between success and failure ──────────────────

type flakeDriftStore struct {
	mu    sync.Mutex
	calls int
	inner *stubDriftStore
}

func (f *flakeDriftStore) ListFindings(ctx context.Context) ([]drift.DriftFinding, error) {
	f.mu.Lock()
	call := f.calls
	f.calls++
	f.mu.Unlock()
	if call%2 == 1 {
		return nil, fmt.Errorf("intermittent failure on call %d", call)
	}
	return f.inner.ListFindings(ctx)
}
func (f *flakeDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (f *flakeDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (f *flakeDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, nil
}
func (f *flakeDriftStore) DeleteFinding(_ context.Context, _ string) error             { return nil }
func (f *flakeDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error         { return nil }
func (f *flakeDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, nil
}
func (f *flakeDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return nil }

// ─── Phase 4: Chaos — slow dependency ────────────────────────────────────────

func TestRecommendationHandler_SlowDrift_NoPanic(t *testing.T) {
	slow := &slowDriftStore{delay: 5 * time.Millisecond, inner: &stubDriftStore{}}
	ev := insights.NewEvaluator(nil, slow, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestForecastHandler_SlowDrift_NoPanic(t *testing.T) {
	slow := &slowDriftStore{delay: 5 * time.Millisecond, inner: &stubDriftStore{}}
	op := insights.NewOperationalForecaster(nil, slow, nil)
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	h.WithOperationalForecaster(op, minConfig())

	w := chaosGet(h, "/api/forecasts", chaosAdminCtx())
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ─── Phase 4: Chaos — flaky dependency (alternates success/failure) ───────────

func TestRecommendationHandler_FlakeDrift_ConsistentResponses(t *testing.T) {
	flake := &flakeDriftStore{inner: &stubDriftStore{}}
	ev := insights.NewEvaluator(nil, flake, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	for i := 0; i < 10; i++ {
		h.InvalidateCache() // force re-evaluation every call
		w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
		// Must always return HTTP 200 — never 500.
		if w.Code != http.StatusOK {
			t.Errorf("call %d: expected 200, got %d", i, w.Code)
		}
	}
}

// ─── Phase 4: Chaos — specific error messages ─────────────────────────────────

func TestRecommendationHandler_StorageError_HTTP200(t *testing.T) {
	// Evaluator is designed to return empty results on store errors, not 500.
	err := &errDriftStore{err: fmt.Errorf("ECONNREFUSED")}
	ev := insights.NewEvaluator(nil, err, nil)
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 on store error, got %d: %s", w.Code, w.Body.String())
	}
}

// ─── Phase 4: Chaos — concurrent cache invalidation ──────────────────────────

func TestRecommendationHandler_ConcurrentInvalidateAndRead(t *testing.T) {
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			h.InvalidateCache()
		}()
		go func() {
			defer wg.Done()
			w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

func TestForecastHandler_ConcurrentInvalidateAndRead(t *testing.T) {
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	h.WithOperationalForecaster(op, minConfig())

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			h.InvalidateCache()
		}()
		go func() {
			defer wg.Done()
			w := chaosGet(h, "/api/forecasts", chaosAdminCtx())
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

// ─── Phase 7: Security — RBAC on recommendations ─────────────────────────────

func TestRecommendationHandler_RBAC_UnauthRejected(t *testing.T) {
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	w := chaosGet(h, "/api/recommendations", chaosUnauthCtx())
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unauthenticated, got %d", w.Code)
	}
}

func TestRecommendationHandler_RBAC_ViewerCanRead(t *testing.T) {
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	w := chaosGet(h, "/api/recommendations", chaosViewerCtx())
	// Viewers can list (GET) but not mutate.
	if w.Code != http.StatusOK {
		t.Errorf("viewer GET: expected 200, got %d", w.Code)
	}
}

func TestRecommendationHandler_RBAC_ViewerCannotAcknowledge(t *testing.T) {
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	w := chaosPost(h, "/api/recommendations/rec-1/acknowledge", chaosViewerCtx())
	if w.Code != http.StatusForbidden {
		t.Errorf("viewer POST: expected 403, got %d", w.Code)
	}
}

func TestRecommendationHandler_RBAC_AdminCanAcknowledge(t *testing.T) {
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	// ID doesn't exist in the engine → 500 from engine.Acknowledge, not 403.
	// Important thing: not rejected at the auth layer.
	w := chaosPost(h, "/api/recommendations/nonexistent/acknowledge", chaosAdminCtx())
	if w.Code == http.StatusForbidden {
		t.Errorf("admin should not be 403, got %d", w.Code)
	}
}

// ─── Phase 7: Security — RBAC on forecasts ────────────────────────────────────

func TestForecastHandler_RBAC_UnauthRejected(t *testing.T) {
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	w := chaosGet(h, "/api/forecasts", chaosUnauthCtx())
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for unauthenticated, got %d", w.Code)
	}
}

func TestForecastHandler_RBAC_ViewerCanRead(t *testing.T) {
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	w := chaosGet(h, "/api/forecasts", chaosViewerCtx())
	if w.Code != http.StatusOK {
		t.Errorf("viewer GET: expected 200, got %d", w.Code)
	}
}

// ─── Phase 7: Security — no secret values in responses ───────────────────────

func TestRecommendationHandler_NoSecretValuesInResponse(t *testing.T) {
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "db-password"},
			{Name: "api-key"},
		},
	})

	w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
	body := w.Body.String()

	// Secret names are ok, but values like "password123" must not appear.
	// We check that no "value" field exists in the recommendation response.
	if strings.Contains(body, `"value"`) {
		t.Errorf("response contains 'value' field — possible secret leakage: %s", body)
	}
}

func TestForecastHandler_NoSecretValuesInResponse(t *testing.T) {
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	h.WithOperationalForecaster(op, minConfig())

	w := chaosGet(h, "/api/forecasts", chaosAdminCtx())
	body := w.Body.String()
	if strings.Contains(body, `"value"`) {
		t.Errorf("forecast response contains 'value' field: %s", body)
	}
}

// ─── Phase 8: Observability — EvalStatus populated after query ────────────────

func TestRecommendationHandler_EvalStatusPopulatedAfterQuery(t *testing.T) {
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	// Before any query the status may have zero last-eval time.
	// After a query it must be non-zero.
	chaosGet(h, "/api/recommendations", chaosAdminCtx())

	status := h.EvalStatus()
	if status == nil {
		t.Fatal("EvalStatus is nil")
	}
	snap := status.Snapshot()
	if snap.LastRecommendationEval.IsZero() {
		t.Error("last_recommendation_eval is still zero after a query — staleness tracker not updated")
	}
}

func TestForecastHandler_EvalStatusPopulatedAfterQuery(t *testing.T) {
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h := api.NewForecastHandler(forecast.NewEngine(nil, nil))
	h.WithOperationalForecaster(op, minConfig())

	chaosGet(h, "/api/forecasts", chaosAdminCtx())

	status := h.EvalStatus()
	if status == nil {
		t.Fatal("EvalStatus is nil")
	}
	snap := status.Snapshot()
	if snap.LastForecastEval.IsZero() {
		t.Error("last_forecast_eval is still zero after a query")
	}
}

// ─── Phase 6: Recovery — repeated invalidation+query stabilises ───────────────

func TestRecommendationHandler_RepeatedRestartSimulation(t *testing.T) {
	// Simulate: query → invalidate (restart) → query → check consistency.
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	var prevCount int
	for round := 0; round < 10; round++ {
		h.InvalidateCache() // simulate restart / cache flush

		w := chaosGet(h, "/api/recommendations", chaosAdminCtx())
		if w.Code != http.StatusOK {
			t.Fatalf("round %d: expected 200, got %d", round, w.Code)
		}

		body := w.Body.String()
		var count int
		fmt.Sscanf(extractJSON(body, `"count":`), "%d", &count)

		if round > 0 && count != prevCount {
			t.Errorf("round %d: count changed from %d to %d — non-deterministic", round, prevCount, count)
		}
		prevCount = count
	}
}

// extractJSON is a tiny helper to pull an integer after a JSON key.
func extractJSON(body, key string) string {
	idx := strings.Index(body, key)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(key):]
	end := strings.IndexAny(rest, ",}")
	if end < 0 {
		return rest
	}
	return strings.TrimSpace(rest[:end])
}

// ─── Phase 6: Recovery — invalidation does not corrupt filter results ─────────

func TestRecommendationHandler_FilterAfterInvalidation(t *testing.T) {
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h := api.NewRecommendationHandler(recommendation.NewEngine(nil, nil))
	h.WithEvaluator(ev, minConfig())

	// Prime the cache.
	chaosGet(h, "/api/recommendations", chaosAdminCtx())

	// Invalidate, then filter.
	h.InvalidateCache()

	w := chaosGet(h, "/api/recommendations?severity=critical", chaosAdminCtx())
	if w.Code != http.StatusOK {
		t.Errorf("filter after invalidation: expected 200, got %d", w.Code)
	}
}

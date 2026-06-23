package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/recommendation"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func adminCtx() context.Context {
	u := &storage.User{Username: "admin", Role: "admin"}
	return auth.WithAuthenticatedUser(context.Background(), u)
}

func getWithAdmin(handler http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = r.WithContext(adminCtx())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func minConfig() *config.Config {
	return &config.Config{
		Secrets: []config.SecretMapping{
			{Name: "db-password"},
			{Name: "api-key"},
		},
	}
}

// ─── nilDriftStore ────────────────────────────────────────────────────────────

type nilDriftStore struct{}

func (n *nilDriftStore) CreateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (n *nilDriftStore) UpdateFinding(_ context.Context, _ drift.DriftFinding) error { return nil }
func (n *nilDriftStore) GetFinding(_ context.Context, _ string) (*drift.DriftFinding, error) {
	return nil, nil
}
func (n *nilDriftStore) ListFindings(_ context.Context) ([]drift.DriftFinding, error) {
	return nil, nil
}
func (n *nilDriftStore) DeleteFinding(_ context.Context, _ string) error { return nil }
func (n *nilDriftStore) LogScan(_ context.Context, _ *drift.DriftScan) error { return nil }
func (n *nilDriftStore) GetScans(_ context.Context, _ int) ([]*drift.DriftScan, error) {
	return nil, nil
}
func (n *nilDriftStore) CleanupOldFindings(_ context.Context, _ time.Time) error { return nil }

// ─── RecommendationHandler degradation ───────────────────────────────────────

func TestRecommendationHandler_NilEvaluator_ReturnsEmpty(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)

	w := getWithAdmin(h, "/api/recommendations")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRecommendationHandler_WithEvaluator_NilDrift_NoError(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)

	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h.WithEvaluator(ev, minConfig())

	w := getWithAdmin(h, "/api/recommendations")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRecommendationHandler_InvalidateCache_NoPanic(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)
	ev := insights.NewEvaluator(nil, nil, nil)
	h.WithEvaluator(ev, minConfig())
	h.InvalidateCache()
}

func TestRecommendationHandler_Pagination(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h.WithEvaluator(ev, minConfig())

	r := httptest.NewRequest(http.MethodGet, "/api/recommendations?page=1&pageSize=5", nil)
	r = r.WithContext(adminCtx())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRecommendationHandler_EvalStatus_NoPanic(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)
	if h.EvalStatus() != nil {
		t.Error("expected nil EvalStatus before WithEvaluator")
	}

	ev := insights.NewEvaluator(nil, nil, nil)
	h.WithEvaluator(ev, minConfig())
	if h.EvalStatus() == nil {
		t.Error("expected non-nil EvalStatus after WithEvaluator")
	}
}

// ─── ForecastHandler degradation ─────────────────────────────────────────────

func TestForecastHandler_NilForecaster_ReturnsFallback(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)

	w := getWithAdmin(h, "/api/forecasts")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestForecastHandler_WithForecaster_NilDrift_NoError(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)

	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h.WithOperationalForecaster(op, minConfig())

	w := getWithAdmin(h, "/api/forecasts")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestForecastHandler_InvalidateCache_NoPanic(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)
	op := insights.NewOperationalForecaster(nil, nil, nil)
	h.WithOperationalForecaster(op, minConfig())
	h.InvalidateCache()
}

func TestForecastHandler_BetaFlagInResponse(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h.WithOperationalForecaster(op, minConfig())

	w := getWithAdmin(h, "/api/forecasts")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"beta":true`) {
		t.Errorf("expected beta:true in response, got: %s", w.Body.String())
	}
}

func TestForecastHandler_CategoryFilter_NoError(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h.WithOperationalForecaster(op, minConfig())

	w := getWithAdmin(h, "/api/forecasts?category=rotation")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestForecastHandler_EvalStatus_NoPanic(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)
	if h.EvalStatus() != nil {
		t.Error("expected nil before WithOperationalForecaster")
	}
	op := insights.NewOperationalForecaster(nil, nil, nil)
	h.WithOperationalForecaster(op, minConfig())
	if h.EvalStatus() == nil {
		t.Error("expected non-nil EvalStatus")
	}
}

// ─── Recovery: cache survives invalidation cycles ─────────────────────────────

func TestRecommendationHandler_CacheSurvivesMultipleInvalidations(t *testing.T) {
	engine := recommendation.NewEngine(nil, nil)
	h := api.NewRecommendationHandler(engine)
	ev := insights.NewEvaluator(nil, &nilDriftStore{}, policy.NewInMemoryStore())
	h.WithEvaluator(ev, minConfig())

	getWithAdmin(h, "/api/recommendations")
	for i := 0; i < 5; i++ {
		h.InvalidateCache()
		w := getWithAdmin(h, "/api/recommendations")
		if w.Code != http.StatusOK {
			t.Errorf("iteration %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestForecastHandler_CacheSurvivesMultipleInvalidations(t *testing.T) {
	engine := forecast.NewEngine(nil, nil)
	h := api.NewForecastHandler(engine)
	op := insights.NewOperationalForecaster(nil, &nilDriftStore{}, nil)
	h.WithOperationalForecaster(op, minConfig())

	getWithAdmin(h, "/api/forecasts")
	for i := 0; i < 5; i++ {
		h.InvalidateCache()
		w := getWithAdmin(h, "/api/forecasts")
		if w.Code != http.StatusOK {
			t.Errorf("iteration %d: expected 200, got %d", i, w.Code)
		}
	}
}

// ─── compliance nil-safety ────────────────────────────────────────────────────

func TestCompliance_SecretInput_NilSafe(t *testing.T) {
	engine := compliance.NewEngine(nil, nil, nil)
	result := engine.EvaluateAll(context.Background(), nil)
	if result == nil {
		t.Error("expected non-nil compliance result")
	}
}

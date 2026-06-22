package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/autonomy"
	"github.com/docker-secret-operator/dso/internal/correlation"
	"github.com/docker-secret-operator/dso/internal/drift"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/graph"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/recommendation"
	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// Verifies the Phase 6 intelligence/governance handlers are actually wired:
// each must be reachable through RESTServer.ServeHTTP (i.e. NOT 404) and must
// not 500/panic when serving their list endpoint with an in-memory store.
func TestIntelligenceHandlersWired(t *testing.T) {
	logger := zap.NewNop()

	srv := &RESTServer{
		PermissionMatrix:      auth.NewPermissionMatrix(),
		RecommendationHandler: api.NewRecommendationHandler(recommendation.NewEngine(logger, recommendation.NewInMemoryStore())),
		ForecastHandler:       api.NewForecastHandler(forecast.NewEngine(logger, forecast.NewInMemoryStore())),
		AutonomyHandler:       api.NewAutonomyHandler(autonomy.NewEngine(logger, autonomy.NewInMemoryStore())),
		CorrelationHandler:    api.NewCorrelationHandler(correlation.NewEngine(logger, correlation.NewInMemoryStore())),
		DriftHandler:          api.NewDriftHandler(drift.NewEngine(drift.NewInMemoryStore(), logger)),
		PolicyHandler:         api.NewPolicyHandler(policy.NewEngine(policy.NewInMemoryStore(), logger)),
		GraphHandler:          api.NewGraphHandler(graph.NewGraph(logger)),
	}

	admin := &storage.User{ID: "u-admin", Username: "admin", Role: "admin"}

	cases := []struct{ name, path string }{
		{"recommendations", "/api/recommendations"},
		{"forecasts", "/api/forecasts"},
		{"autonomy", "/api/autonomy/actions"},
		{"incidents", "/api/incidents"},
		{"drift", "/api/drift"},
		{"policies", "/api/policies"},
		{"graph", "/api/graph"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(auth.WithAuthenticatedUser(req.Context(), admin))
			rec := httptest.NewRecorder()

			srv.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("%s: route not wired (404)", tc.path)
			}
			if rec.Code >= 500 {
				t.Fatalf("%s: server error %d: %s", tc.path, rec.Code, rec.Body.String())
			}
			if rec.Code != http.StatusOK {
				t.Errorf("%s: expected 200, got %d: %s", tc.path, rec.Code, rec.Body.String())
			}
		})
	}
}

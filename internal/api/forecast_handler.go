package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/forecast"
)

// ForecastHandler handles forecast API endpoints
type ForecastHandler struct {
	engine *forecast.Engine
}

// NewForecastHandler creates a new forecast handler
func NewForecastHandler(engine *forecast.Engine) *ForecastHandler {
	return &ForecastHandler{
		engine: engine,
	}
}

// ServeHTTP routes forecast API requests
func (h *ForecastHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/forecasts" && r.Method == "GET":
		h.ListForecasts(w, r)
	case path == "/api/forecasts/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case path == "/api/forecasts/run" && r.Method == "POST":
		h.RunForecasts(w, r)
	case strings.HasPrefix(path, "/api/forecasts/") && r.Method == "GET":
		h.GetForecast(w, r)
	case strings.HasPrefix(path, "/api/forecasts/") && r.Method == "DELETE":
		h.DeleteForecast(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractForecastIDFromPath extracts forecast ID from URL path
func extractForecastIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	fcID := parts[3]
	if idx := strings.Index(fcID, "/"); idx != -1 {
		fcID = fcID[:idx]
	}
	return fcID
}

// ForecastResponse represents a forecast in API response
type ForecastResponse struct {
	ID             string  `json:"id"`
	ResourceType   string  `json:"resource_type"`
	ResourceID     string  `json:"resource_id"`
	Metric         string  `json:"metric"`
	CurrentValue   float64 `json:"current_value"`
	PredictedValue float64 `json:"predicted_value"`
	GrowthRate     float64 `json:"growth_rate"`
	Confidence     float64 `json:"confidence"`
	Horizon        string  `json:"horizon"`
	Severity       string  `json:"severity"`
	Trend          string  `json:"trend"`
	CreatedAt      int64   `json:"created_at"`
}

// ListForecasts handles GET /api/forecasts
func (h *ForecastHandler) ListForecasts(w http.ResponseWriter, r *http.Request) {
	forecasts, err := h.engine.ListForecasts(100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]ForecastResponse, len(forecasts))
	for i, f := range forecasts {
		responses[i] = h.toForecastResponse(f)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"forecasts": responses,
		"count":     len(responses),
	})
}

// GetForecast handles GET /api/forecasts/:id
func (h *ForecastHandler) GetForecast(w http.ResponseWriter, r *http.Request) {
	fcID := extractForecastIDFromPath(r.URL.Path)
	f, err := h.engine.GetForecast(fcID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if f == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(h.toForecastResponse(f))
}

// DeleteForecast handles DELETE /api/forecasts/:id
func (h *ForecastHandler) DeleteForecast(w http.ResponseWriter, r *http.Request) {
	fcID := extractForecastIDFromPath(r.URL.Path)

	if err := h.engine.DeleteForecast(fcID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// RunForecasts handles POST /api/forecasts/run
func (h *ForecastHandler) RunForecasts(w http.ResponseWriter, r *http.Request) {
	if err := h.engine.GenerateForecasts(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Forecasts generated successfully",
	})
}

// GetMetrics handles GET /api/forecasts/metrics
func (h *ForecastHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_forecasts":     metrics.TotalForecasts,
		"critical_forecasts":  metrics.CriticalForecasts,
		"average_confidence":  metrics.AverageConfidence,
		"prediction_accuracy": metrics.PredictionAccuracy,
		"forecast_runs":       metrics.ForecastRuns,
		"last_updated":        metrics.LastUpdate,
	})
}

// toForecastResponse converts a forecast to API response
func (h *ForecastHandler) toForecastResponse(f *forecast.Forecast) ForecastResponse {
	return ForecastResponse{
		ID:             f.ID,
		ResourceType:   string(f.ResourceType),
		ResourceID:     f.ResourceID,
		Metric:         f.Metric,
		CurrentValue:   f.CurrentValue,
		PredictedValue: f.PredictedValue,
		GrowthRate:     f.GrowthRate,
		Confidence:     f.Confidence,
		Horizon:        string(f.Horizon),
		Severity:       string(f.Severity),
		Trend:          string(f.Trend),
		CreatedAt:      f.CreatedAt.Unix(),
	}
}

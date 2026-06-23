package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/cache"
	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/forecast"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// ForecastHandler handles forecast API endpoints.
// It serves both the legacy generic engine forecasts and the P9 operational
// forecasts derived from rotation/drift/compliance evidence.
type ForecastHandler struct {
	engine      *forecast.Engine
	operational *insights.OperationalForecaster
	config      *config.Config
	fcCache     *cache.Entry[[]forecast.OperationalForecast]
	status      *cache.EvalStatus
}

// NewForecastHandler creates a new forecast handler.
func NewForecastHandler(engine *forecast.Engine) *ForecastHandler {
	return &ForecastHandler{engine: engine}
}

// WithOperationalForecaster attaches a P9 forecaster, config, cache, and status.
func (h *ForecastHandler) WithOperationalForecaster(op *insights.OperationalForecaster, cfg *config.Config) {
	h.operational = op
	h.config = cfg
	h.fcCache = cache.NewEntry[[]forecast.OperationalForecast](cache.DefaultTTL)
	h.status = &cache.EvalStatus{}
}

// InvalidateCache marks the forecast cache stale.
func (h *ForecastHandler) InvalidateCache() {
	if h.fcCache != nil {
		h.fcCache.Invalidate()
	}
}

// EvalStatus returns the evaluation status tracker (may be nil).
func (h *ForecastHandler) EvalStatus() *cache.EvalStatus { return h.status }

// ServeHTTP routes forecast API requests.
func (h *ForecastHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
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

// OperationalForecastResponse is the P9 wire format. Kept separate from the legacy
// ForecastResponse so consumers can distinguish measurements from predictions.
type OperationalForecastResponse struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Reason      string   `json:"reason"`
	Resource    string   `json:"resource,omitempty"`
	Confidence  float64  `json:"confidence"`
	PredictedAt int64    `json:"predicted_at"`
	Evidence    []string `json:"evidence"`
	// Beta marks every P9 forecast so the UI can display a Beta badge.
	Beta bool `json:"beta"`
}

// ForecastResponse is the legacy wire format for engine-based forecasts.
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
// Query params: category, severity, page, pageSize
func (h *ForecastHandler) ListForecasts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	categoryFilter := strings.ToLower(q.Get("category"))
	severityFilter := strings.ToLower(q.Get("severity"))
	page := forecastParseInt(q.Get("page"), 1)
	pageSize := forecastParseInt(q.Get("pageSize"), 50)
	if pageSize > 200 {
		pageSize = 200
	}

	// P9 operational forecasts take the primary slot when the forecaster is wired.
	if h.operational != nil {
		inputs := h.secretInputs()
		var all []forecast.OperationalForecast
		if h.fcCache != nil {
			all = h.fcCache.GetOrCompute(r.Context(), func(ctx context.Context) []forecast.OperationalForecast {
				return h.operational.ForecastAll(ctx, inputs)
			})
			if h.status != nil {
				h.status.RecordForecast(h.fcCache.LastEvalDuration(), len(all))
			}
		} else {
			all = h.operational.ForecastAll(r.Context(), inputs)
		}

		// Filter — allocate fresh slice so the cache entry is never mutated.
		filtered := make([]forecast.OperationalForecast, 0, len(all))
		for _, fc := range all {
			if categoryFilter != "" && string(fc.Category) != categoryFilter {
				continue
			}
			if severityFilter != "" && string(fc.Severity) != severityFilter {
				continue
			}
			filtered = append(filtered, fc)
		}

		// Paginate
		total := len(filtered)
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}

		resp := make([]OperationalForecastResponse, len(filtered[start:end]))
		for i, fc := range filtered[start:end] {
			resp[i] = toOperationalResponse(fc)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"forecasts": resp,
			"count":     len(resp),
			"total":     total,
			"page":      page,
			"pageSize":  pageSize,
			// Beta flag at the envelope level — every forecast in this list is a prediction.
			"beta": true,
		})
		return
	}

	// Fallback: legacy engine forecasts.
	forecasts, err := h.engine.ListForecasts(100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	responses := make([]ForecastResponse, len(forecasts))
	for i, f := range forecasts {
		responses[i] = toLegacyResponse(f)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"forecasts": responses,
		"count":     len(responses),
		"beta":      false,
	})
}

// GetForecast handles GET /api/forecasts/:id
func (h *ForecastHandler) GetForecast(w http.ResponseWriter, r *http.Request) {
	fcID := extractForecastIDFromPath(r.URL.Path)

	// Search P9 forecasts first.
	if h.operational != nil {
		all := h.operational.ForecastAll(r.Context(), h.secretInputs())
		for _, fc := range all {
			if fc.ID == fcID {
				_ = json.NewEncoder(w).Encode(toOperationalResponse(fc))
				return
			}
		}
	}

	f, err := h.engine.GetForecast(fcID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if f == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(toLegacyResponse(f))
}

// DeleteForecast handles DELETE /api/forecasts/:id (legacy only).
func (h *ForecastHandler) DeleteForecast(w http.ResponseWriter, r *http.Request) {
	fcID := extractForecastIDFromPath(r.URL.Path)
	if err := h.engine.DeleteForecast(fcID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// RunForecasts handles POST /api/forecasts/run (legacy engine trigger).
func (h *ForecastHandler) RunForecasts(w http.ResponseWriter, r *http.Request) {
	if err := h.engine.GenerateForecasts(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Forecasts generated",
	})
}

// GetMetrics handles GET /api/forecasts/metrics.
func (h *ForecastHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total_forecasts":     metrics.TotalForecasts,
		"critical_forecasts":  metrics.CriticalForecasts,
		"average_confidence":  metrics.AverageConfidence,
		"prediction_accuracy": metrics.PredictionAccuracy,
		"forecast_runs":       metrics.ForecastRuns,
		"last_updated":        metrics.LastUpdate,
	})
}

func toOperationalResponse(fc forecast.OperationalForecast) OperationalForecastResponse {
	evidence := fc.Evidence
	if evidence == nil {
		evidence = []string{}
	}
	return OperationalForecastResponse{
		ID:          fc.ID,
		Category:    string(fc.Category),
		Severity:    string(fc.Severity),
		Title:       fc.Title,
		Description: fc.Description,
		Reason:      fc.Reason,
		Resource:    fc.Resource,
		Confidence:  fc.Confidence,
		PredictedAt: fc.PredictedAt.Unix(),
		Evidence:    evidence,
		Beta:        true,
	}
}

func toLegacyResponse(f *forecast.Forecast) ForecastResponse {
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

func (h *ForecastHandler) secretInputs() []compliance.SecretInput {
	if h.config == nil {
		return nil
	}
	out := make([]compliance.SecretInput, 0, len(h.config.Secrets))
	for _, s := range h.config.Secrets {
		out = append(out, compliance.SecretInput{Name: s.Name, Provider: s.Provider})
	}
	return out
}

func forecastParseInt(s string, def int) int {
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err == nil && v > 0 {
		return v
	}
	return def
}

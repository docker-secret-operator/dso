package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/cache"
	"github.com/docker-secret-operator/dso/internal/compliance"
	"github.com/docker-secret-operator/dso/internal/insights"
	"github.com/docker-secret-operator/dso/internal/recommendation"
	"github.com/docker-secret-operator/dso/pkg/config"
)

// RecommendationHandler handles recommendation API endpoints.
// It serves both legacy store-based recommendations and the new P8
// live, evidence-derived recommendations from the Evaluator.
type RecommendationHandler struct {
	engine    *recommendation.Engine
	evaluator *insights.Evaluator
	config    *config.Config
	recCache  *cache.Entry[[]*recommendation.Recommendation]
	status    *cache.EvalStatus
}

// NewRecommendationHandler creates a new recommendation handler.
func NewRecommendationHandler(engine *recommendation.Engine) *RecommendationHandler {
	return &RecommendationHandler{engine: engine}
}

// WithEvaluator attaches a live evaluator, config, cache, and status tracker.
func (h *RecommendationHandler) WithEvaluator(ev *insights.Evaluator, cfg *config.Config) {
	h.evaluator = ev
	h.config = cfg
	h.recCache = cache.NewEntry[[]*recommendation.Recommendation](cache.DefaultTTL)
	h.status = &cache.EvalStatus{}
}

// InvalidateCache marks the recommendation cache stale. Call on rotation, drift
// update, or policy change so the next request triggers a fresh evaluation.
func (h *RecommendationHandler) InvalidateCache() {
	if h.recCache != nil {
		h.recCache.Invalidate()
	}
}

// EvalStatus returns the evaluation status tracker (may be nil).
func (h *RecommendationHandler) EvalStatus() *cache.EvalStatus { return h.status }

// ServeHTTP routes recommendation API requests.
func (h *RecommendationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/recommendations" && r.Method == "GET":
		h.ListRecommendations(w, r)
	case path == "/api/recommendations/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case strings.HasPrefix(path, "/api/recommendations/") && strings.HasSuffix(path, "/acknowledge") && r.Method == "POST":
		h.AcknowledgeRecommendation(w, r)
	case strings.HasPrefix(path, "/api/recommendations/") && strings.HasSuffix(path, "/implement") && r.Method == "POST":
		h.ImplementRecommendation(w, r)
	case strings.HasPrefix(path, "/api/recommendations/") && strings.HasSuffix(path, "/dismiss") && r.Method == "POST":
		h.DismissRecommendation(w, r)
	case strings.HasPrefix(path, "/api/recommendations/") && r.Method == "GET":
		h.GetRecommendation(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractRecommendationIDFromPath extracts recommendation ID from URL path.
func extractRecommendationIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	recID := parts[3]
	if idx := strings.Index(recID, "/"); idx != -1 {
		recID = recID[:idx]
	}
	return recID
}

// RecommendationResponse is the wire representation of a recommendation.
type RecommendationResponse struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Reason          string  `json:"reason,omitempty"`
	Resource        string  `json:"resource,omitempty"`
	Priority        string  `json:"priority"`
	Category        string  `json:"category"`
	Status          string  `json:"status"`
	ResourceID      string  `json:"resource_id,omitempty"`
	IncidentID      string  `json:"incident_id,omitempty"`
	SuggestedAction string  `json:"suggested_action"`
	Confidence      float64 `json:"confidence"`
	// Cross-links (P8)
	DriftID  string `json:"driftId,omitempty"`
	PolicyID string `json:"policyId,omitempty"`
	AuditID  string `json:"auditId,omitempty"`
	CreatedAt int64 `json:"created_at"`
}

// ListRecommendations handles GET /api/recommendations
// Query params: severity (priority alias), category, page, pageSize
func (h *RecommendationHandler) ListRecommendations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	severityFilter := strings.ToLower(q.Get("severity"))
	categoryFilter := strings.ToLower(q.Get("category"))
	page := recParseInt(q.Get("page"), 1)
	pageSize := recParseInt(q.Get("pageSize"), 50)
	if pageSize > 200 {
		pageSize = 200
	}

	var all []*recommendation.Recommendation

	// P8 live evaluator takes precedence when available.
	if h.evaluator != nil {
		inputs := h.secretInputs()
		if h.recCache != nil {
			all = h.recCache.GetOrCompute(r.Context(), func(ctx context.Context) []*recommendation.Recommendation {
				return h.evaluator.EvaluateAll(ctx, inputs)
			})
			if h.status != nil {
				h.status.RecordRecommendation(h.recCache.LastEvalDuration(), len(all))
			}
		} else {
			all = h.evaluator.EvaluateAll(r.Context(), inputs)
		}
	} else {
		// Fall back to legacy store.
		statusParam := q.Get("status")
		if statusParam == "" {
			statusParam = "open"
		}
		recs, err := h.engine.ListRecommendations(recommendation.Status(statusParam), 1000)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all = recs
	}

	// Filter — always a fresh slice so the cache entry is never mutated.
	filtered := make([]*recommendation.Recommendation, 0, len(all))
	for _, rec := range all {
		if severityFilter != "" && string(rec.Priority) != severityFilter {
			continue
		}
		if categoryFilter != "" && string(rec.Category) != categoryFilter {
			continue
		}
		filtered = append(filtered, rec)
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
	page_recs := filtered[start:end]

	responses := make([]RecommendationResponse, len(page_recs))
	for i, rec := range page_recs {
		responses[i] = toRecommendationResponse(rec)
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"recommendations": responses,
		"count":           len(responses),
		"total":           total,
		"page":            page,
		"pageSize":        pageSize,
	})
}

// GetRecommendation handles GET /api/recommendations/:id
func (h *RecommendationHandler) GetRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)

	// Check live evaluator first.
	if h.evaluator != nil {
		all := h.evaluator.EvaluateAll(r.Context(), h.secretInputs())
		for _, rec := range all {
			if rec.ID == recID {
				_ = json.NewEncoder(w).Encode(toRecommendationResponse(rec))
				return
			}
		}
	}

	rec, err := h.engine.GetRecommendation(recID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(toRecommendationResponse(rec))
}

// AcknowledgeRecommendation handles POST /api/recommendations/:id/acknowledge
func (h *RecommendationHandler) AcknowledgeRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)
	if err := h.engine.Acknowledge(recID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rec, _ := h.engine.GetRecommendation(recID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"recommendation": toRecommendationResponse(rec),
	})
}

// ImplementRecommendation handles POST /api/recommendations/:id/implement
func (h *RecommendationHandler) ImplementRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)
	if err := h.engine.Implement(recID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rec, _ := h.engine.GetRecommendation(recID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"recommendation": toRecommendationResponse(rec),
	})
}

// DismissRecommendation handles POST /api/recommendations/:id/dismiss
func (h *RecommendationHandler) DismissRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)
	if err := h.engine.Dismiss(recID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rec, _ := h.engine.GetRecommendation(recID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"recommendation": toRecommendationResponse(rec),
	})
}

// GetMetrics handles GET /api/recommendations/metrics
func (h *RecommendationHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total_recommendations":        metrics.TotalRecommendations,
		"open_recommendations":         metrics.OpenRecommendations,
		"acknowledged_recommendations": metrics.AcknowledgedRecommendations,
		"implemented_recommendations":  metrics.ImplementedRecommendations,
		"dismissed_recommendations":    metrics.DismissedRecommendations,
		"average_confidence":           metrics.AverageConfidence,
		"last_updated":                 metrics.LastUpdate,
	})
}

func toRecommendationResponse(rec *recommendation.Recommendation) RecommendationResponse {
	if rec == nil {
		return RecommendationResponse{}
	}
	return RecommendationResponse{
		ID:              rec.ID,
		Title:           rec.Title,
		Description:     rec.Description,
		Reason:          rec.Reason,
		Resource:        rec.Resource,
		Priority:        string(rec.Priority),
		Category:        string(rec.Category),
		Status:          string(rec.Status),
		ResourceID:      rec.ResourceID,
		IncidentID:      rec.IncidentID,
		SuggestedAction: rec.SuggestedAction,
		Confidence:      rec.Confidence,
		DriftID:         rec.DriftID,
		PolicyID:        rec.PolicyID,
		AuditID:         rec.AuditID,
		CreatedAt:       rec.CreatedAt.Unix(),
	}
}

// secretInputs builds compliance.SecretInput list from config.
func (h *RecommendationHandler) secretInputs() []compliance.SecretInput {
	if h.config == nil {
		return nil
	}
	out := make([]compliance.SecretInput, 0, len(h.config.Secrets))
	for _, s := range h.config.Secrets {
		out = append(out, compliance.SecretInput{Name: s.Name, Provider: s.Provider})
	}
	return out
}

func recParseInt(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil && v > 0 {
		return v
	}
	return def
}

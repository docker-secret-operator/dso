package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/recommendation"
)

// RecommendationHandler handles recommendation API endpoints
type RecommendationHandler struct {
	engine *recommendation.Engine
}

// NewRecommendationHandler creates a new recommendation handler
func NewRecommendationHandler(engine *recommendation.Engine) *RecommendationHandler {
	return &RecommendationHandler{
		engine: engine,
	}
}

// ServeHTTP routes recommendation API requests
func (h *RecommendationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
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

// extractRecommendationIDFromPath extracts recommendation ID from URL path
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

// RecommendationResponse represents a recommendation in API response
type RecommendationResponse struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Priority        string  `json:"priority"`
	Category        string  `json:"category"`
	Status          string  `json:"status"`
	ResourceID      string  `json:"resource_id,omitempty"`
	IncidentID      string  `json:"incident_id,omitempty"`
	SuggestedAction string  `json:"suggested_action"`
	Confidence      float64 `json:"confidence"`
	CreatedAt       int64   `json:"created_at"`
}

// ListRecommendations handles GET /api/recommendations
func (h *RecommendationHandler) ListRecommendations(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	if statusParam == "" {
		statusParam = "open"
	}

	recs, err := h.engine.ListRecommendations(recommendation.Status(statusParam), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]RecommendationResponse, len(recs))
	for i, rec := range recs {
		responses[i] = h.toRecommendationResponse(rec)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"recommendations": responses,
		"count":           len(responses),
	})
}

// GetRecommendation handles GET /api/recommendations/:id
func (h *RecommendationHandler) GetRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)
	rec, err := h.engine.GetRecommendation(recID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if rec == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(h.toRecommendationResponse(rec))
}

// AcknowledgeRecommendation handles POST /api/recommendations/:id/acknowledge
func (h *RecommendationHandler) AcknowledgeRecommendation(w http.ResponseWriter, r *http.Request) {
	recID := extractRecommendationIDFromPath(r.URL.Path)

	if err := h.engine.Acknowledge(recID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rec, _ := h.engine.GetRecommendation(recID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":         true,
		"recommendation": h.toRecommendationResponse(rec),
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"recommendation": h.toRecommendationResponse(rec),
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"recommendation": h.toRecommendationResponse(rec),
	})
}

// GetMetrics handles GET /api/recommendations/metrics
func (h *RecommendationHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_recommendations":       metrics.TotalRecommendations,
		"open_recommendations":        metrics.OpenRecommendations,
		"acknowledged_recommendations": metrics.AcknowledgedRecommendations,
		"implemented_recommendations": metrics.ImplementedRecommendations,
		"dismissed_recommendations":   metrics.DismissedRecommendations,
		"average_confidence":          metrics.AverageConfidence,
		"last_updated":                metrics.LastUpdate,
	})
}

// toRecommendationResponse converts a recommendation to API response
func (h *RecommendationHandler) toRecommendationResponse(rec *recommendation.Recommendation) RecommendationResponse {
	return RecommendationResponse{
		ID:              rec.ID,
		Title:           rec.Title,
		Description:     rec.Description,
		Priority:        string(rec.Priority),
		Category:        string(rec.Category),
		Status:          string(rec.Status),
		ResourceID:      rec.ResourceID,
		IncidentID:      rec.IncidentID,
		SuggestedAction: rec.SuggestedAction,
		Confidence:      rec.Confidence,
		CreatedAt:       rec.CreatedAt.Unix(),
	}
}

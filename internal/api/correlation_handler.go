package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/correlation"
)

// CorrelationHandler handles correlation API endpoints
type CorrelationHandler struct {
	engine *correlation.Engine
}

// NewCorrelationHandler creates a new correlation handler
func NewCorrelationHandler(engine *correlation.Engine) *CorrelationHandler {
	return &CorrelationHandler{
		engine: engine,
	}
}

// ServeHTTP routes correlation API requests
func (h *CorrelationHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/incidents" && r.Method == "GET":
		h.ListIncidents(w, r)
	case path == "/api/incidents/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case strings.HasPrefix(path, "/api/incidents/") && strings.HasSuffix(path, "/acknowledge") && r.Method == "POST":
		h.AcknowledgeIncident(w, r)
	case strings.HasPrefix(path, "/api/incidents/") && strings.HasSuffix(path, "/resolve") && r.Method == "POST":
		h.ResolveIncident(w, r)
	case strings.HasPrefix(path, "/api/incidents/") && r.Method == "GET":
		h.GetIncident(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractIncidentIDFromPath extracts incident ID from URL path
func extractIncidentIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	incidentID := parts[3]
	if idx := strings.Index(incidentID, "/"); idx != -1 {
		incidentID = incidentID[:idx]
	}
	return incidentID
}

// IncidentResponse represents an incident in API response
type IncidentResponse struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Severity         string    `json:"severity"`
	Status           string    `json:"status"`
	RootCause        string    `json:"root_cause"`
	AffectedNodes    []string  `json:"affected_nodes"`
	EventCount       int       `json:"event_count"`
	CorrelationScore float64   `json:"correlation_score"`
	FirstSeen        int64     `json:"first_seen"`
	LastSeen         int64     `json:"last_seen"`
	AcknowledgedAt   *int64    `json:"acknowledged_at,omitempty"`
	ResolvedAt       *int64    `json:"resolved_at,omitempty"`
}

// ListIncidents handles GET /api/incidents
func (h *CorrelationHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	statusParam := r.URL.Query().Get("status")
	if statusParam == "" {
		statusParam = "open"
	}

	incidents, err := h.engine.ListIncidents(correlation.IncidentStatus(statusParam), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]IncidentResponse, len(incidents))
	for i, inc := range incidents {
		responses[i] = h.toIncidentResponse(inc)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"incidents": responses,
		"count":     len(responses),
	})
}

// GetIncident handles GET /api/incidents/:id
func (h *CorrelationHandler) GetIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := extractIncidentIDFromPath(r.URL.Path)
	incident, err := h.engine.GetIncident(incidentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if incident == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(h.toIncidentResponse(incident))
}

// AcknowledgeIncident handles POST /api/incidents/:id/acknowledge
func (h *CorrelationHandler) AcknowledgeIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := extractIncidentIDFromPath(r.URL.Path)

	if err := h.engine.AcknowledgeIncident(incidentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	incident, _ := h.engine.GetIncident(incidentID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"incident": h.toIncidentResponse(incident),
	})
}

// ResolveIncident handles POST /api/incidents/:id/resolve
func (h *CorrelationHandler) ResolveIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := extractIncidentIDFromPath(r.URL.Path)

	if err := h.engine.ResolveIncident(incidentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	incident, _ := h.engine.GetIncident(incidentID)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"incident": h.toIncidentResponse(incident),
	})
}

// GetMetrics handles GET /api/incidents/metrics
func (h *CorrelationHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_incidents":         metrics.TotalIncidents,
		"open_incidents":          metrics.OpenIncidents,
		"resolved_incidents":      metrics.ResolvedIncidents,
		"acknowledged_incidents":  metrics.AcknowledgedIncidents,
		"average_correlation_score": metrics.AverageScore,
		"events_processed":        metrics.EventsProcessed,
		"merges_performed":        metrics.MergesPerformed,
		"last_updated":            metrics.LastUpdate,
	})
}

// toIncidentResponse converts an incident to API response
func (h *CorrelationHandler) toIncidentResponse(inc *correlation.Incident) IncidentResponse {
	resp := IncidentResponse{
		ID:               inc.ID,
		Title:            inc.Title,
		Severity:         string(inc.Severity),
		Status:           string(inc.Status),
		RootCause:        inc.RootCause,
		AffectedNodes:    inc.AffectedNodes,
		EventCount:       inc.EventCount,
		CorrelationScore: inc.CorrelationScore,
		FirstSeen:        inc.FirstSeen.Unix(),
		LastSeen:         inc.LastSeen.Unix(),
	}

	if inc.AcknowledgedAt != nil {
		t := inc.AcknowledgedAt.Unix()
		resp.AcknowledgedAt = &t
	}

	if inc.ResolvedAt != nil {
		t := inc.ResolvedAt.Unix()
		resp.ResolvedAt = &t
	}

	return resp
}

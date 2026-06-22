package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/drift"
)

// DriftHandler handles drift detection API endpoints
type DriftHandler struct {
	engine *drift.Engine
}

// NewDriftHandler creates a new drift handler
func NewDriftHandler(engine *drift.Engine) *DriftHandler {
	return &DriftHandler{
		engine: engine,
	}
}

// ServeHTTP routes drift API requests
func (h *DriftHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/drift" && r.Method == "GET":
		h.ListFindings(w, r)
	case path == "/api/drift/scan" && r.Method == "POST":
		h.RunScan(w, r)
	case path == "/api/drift/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case path == "/api/drift/history" && r.Method == "GET":
		h.GetHistory(w, r)
	case strings.HasPrefix(path, "/api/drift/") && strings.HasSuffix(path, "/acknowledge") && r.Method == "POST":
		h.AcknowledgeFinding(w, r)
	case strings.HasPrefix(path, "/api/drift/") && strings.HasSuffix(path, "/resolve") && r.Method == "POST":
		h.ResolveFinding(w, r)
	case strings.HasPrefix(path, "/api/drift/") && r.Method == "GET":
		h.GetFinding(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractFindingIDFromPath extracts finding ID from URL path
func extractFindingIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	findingID := parts[3]
	// Remove any trailing path components
	if idx := strings.Index(findingID, "/"); idx != -1 {
		findingID = findingID[:idx]
	}
	return findingID
}

// FindingResponse represents a drift finding in API response
type FindingResponse struct {
	ID             string      `json:"id"`
	Type           string      `json:"type"`
	Severity       string      `json:"severity"`
	Status         string      `json:"status"`
	Resource       string      `json:"resource"`
	Description    string      `json:"description"`
	Metadata       interface{} `json:"metadata,omitempty"`
	DetectedAt     int64       `json:"detected_at"`
	AcknowledgedAt *int64      `json:"acknowledged_at,omitempty"`
	ResolvedAt     *int64      `json:"resolved_at,omitempty"`
}

// ListFindings handles GET /api/drift
func (h *DriftHandler) ListFindings(w http.ResponseWriter, r *http.Request) {
	findings := h.engine.ListFindings()
	responses := make([]FindingResponse, len(findings))

	for i, finding := range findings {
		var acknowledgedAt, resolvedAt *int64
		if finding.AcknowledgedAt != nil {
			t := finding.AcknowledgedAt.Unix() * 1000
			acknowledgedAt = &t
		}
		if finding.ResolvedAt != nil {
			t := finding.ResolvedAt.Unix() * 1000
			resolvedAt = &t
		}

		responses[i] = FindingResponse{
			ID:             finding.ID,
			Type:           string(finding.Type),
			Severity:       string(finding.Severity),
			Status:         string(finding.Status),
			Resource:       finding.Resource,
			Description:    finding.Description,
			Metadata:       finding.Metadata,
			DetectedAt:     finding.DetectedAt.Unix() * 1000,
			AcknowledgedAt: acknowledgedAt,
			ResolvedAt:     resolvedAt,
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"findings": responses,
		"total":    len(responses),
	})
}

// GetFinding handles GET /api/drift/:id
func (h *DriftHandler) GetFinding(w http.ResponseWriter, r *http.Request) {
	findingID := extractFindingIDFromPath(r.URL.Path)
	finding := h.engine.GetFinding(findingID)
	if finding == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	var acknowledgedAt, resolvedAt *int64
	if finding.AcknowledgedAt != nil {
		t := finding.AcknowledgedAt.Unix() * 1000
		acknowledgedAt = &t
	}
	if finding.ResolvedAt != nil {
		t := finding.ResolvedAt.Unix() * 1000
		resolvedAt = &t
	}

	response := FindingResponse{
		ID:             finding.ID,
		Type:           string(finding.Type),
		Severity:       string(finding.Severity),
		Status:         string(finding.Status),
		Resource:       finding.Resource,
		Description:    finding.Description,
		Metadata:       finding.Metadata,
		DetectedAt:     finding.DetectedAt.Unix() * 1000,
		AcknowledgedAt: acknowledgedAt,
		ResolvedAt:     resolvedAt,
	}

	json.NewEncoder(w).Encode(response)
}

// RunScan handles POST /api/drift/scan
func (h *DriftHandler) RunScan(w http.ResponseWriter, r *http.Request) {
	if err := h.engine.RunScan(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	findings := h.engine.ListFindings()
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "scan completed",
		"findings": len(findings),
	})
}

// AcknowledgeFinding handles POST /api/drift/:id/acknowledge
func (h *DriftHandler) AcknowledgeFinding(w http.ResponseWriter, r *http.Request) {
	findingID := extractFindingIDFromPath(r.URL.Path)
	if err := h.engine.AcknowledgeFinding(findingID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "acknowledged",
		"finding_id": findingID,
	})
}

// ResolveFinding handles POST /api/drift/:id/resolve
func (h *DriftHandler) ResolveFinding(w http.ResponseWriter, r *http.Request) {
	findingID := extractFindingIDFromPath(r.URL.Path)
	if err := h.engine.ResolveFinding(findingID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "resolved",
		"finding_id": findingID,
	})
}

// GetMetrics handles GET /api/drift/metrics
func (h *DriftHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()
	json.NewEncoder(w).Encode(metrics)
}

// GetHistory handles GET /api/drift/history
func (h *DriftHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	// Stub - would fetch from scan store
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scans": []interface{}{},
		"total": 0,
	})
}

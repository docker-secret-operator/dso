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
	store  drift.Store
}

// NewDriftHandler creates a new drift handler
func NewDriftHandler(engine *drift.Engine) *DriftHandler {
	return &DriftHandler{engine: engine}
}

// NewDriftHandlerWithStore creates a drift handler that also has direct store access for history queries.
func NewDriftHandlerWithStore(engine *drift.Engine, store drift.Store) *DriftHandler {
	return &DriftHandler{engine: engine, store: store}
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
	case path == "/api/drift/bulk-ack" && r.Method == "POST":
		h.BulkAcknowledge(w, r)
	case path == "/api/drift/bulk-resolve" && r.Method == "POST":
		h.BulkResolve(w, r)
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
	ID              string      `json:"id"`
	Type            string      `json:"type"`
	Severity        string      `json:"severity"`
	Status          string      `json:"status"`
	Resource        string      `json:"resource"`
	Description     string      `json:"description"`
	Metadata        interface{} `json:"metadata,omitempty"`
	SecretName      string      `json:"secret_name,omitempty"`
	Container       string      `json:"container,omitempty"`
	Provider        string      `json:"provider,omitempty"`
	ExpectedVersion string      `json:"expected_version,omitempty"`
	ActualVersion   string      `json:"actual_version,omitempty"`
	DetectedAt      int64       `json:"detected_at"`
	AcknowledgedAt  *int64      `json:"acknowledged_at,omitempty"`
	ResolvedAt      *int64      `json:"resolved_at,omitempty"`
}

// metaStr extracts a string value from a finding's metadata map.
func metaStr(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	v, _ := meta[key].(string)
	return v
}

// findingToResponse converts a DriftFinding to a FindingResponse.
func findingToResponse(f *drift.DriftFinding) FindingResponse {
	var acknowledgedAt, resolvedAt *int64
	if f.AcknowledgedAt != nil {
		t := f.AcknowledgedAt.Unix() * 1000
		acknowledgedAt = &t
	}
	if f.ResolvedAt != nil {
		t := f.ResolvedAt.Unix() * 1000
		resolvedAt = &t
	}
	meta := f.Metadata
	return FindingResponse{
		ID:              f.ID,
		Type:            string(f.Type),
		Severity:        string(f.Severity),
		Status:          string(f.Status),
		Resource:        f.Resource,
		Description:     f.Description,
		Metadata:        f.Metadata,
		SecretName:      metaStr(meta, "secret_name"),
		Container:       metaStr(meta, "container"),
		Provider:        metaStr(meta, "provider"),
		ExpectedVersion: metaStr(meta, "expected_version"),
		ActualVersion:   metaStr(meta, "actual_version"),
		DetectedAt:      f.DetectedAt.Unix() * 1000,
		AcknowledgedAt:  acknowledgedAt,
		ResolvedAt:      resolvedAt,
	}
}

// ListFindings handles GET /api/drift
func (h *DriftHandler) ListFindings(w http.ResponseWriter, r *http.Request) {
	findings := h.engine.ListFindings()
	responses := make([]FindingResponse, len(findings))
	for i, f := range findings {
		responses[i] = findingToResponse(f)
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
	json.NewEncoder(w).Encode(findingToResponse(finding))
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

// BulkAcknowledge handles POST /api/drift/bulk-ack
// Body: {"ids":["finding-id-1","finding-id-2"]}
func (h *DriftHandler) BulkAcknowledge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
		return
	}

	type idFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []idFailure
	)
	for _, id := range req.IDs {
		if err := h.engine.AcknowledgeFinding(id); err != nil {
			failures = append(failures, idFailure{ID: id, Error: err.Error()})
		} else {
			succeeded++
		}
	}
	if failures == nil {
		failures = []idFailure{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}

// BulkResolve handles POST /api/drift/bulk-resolve
// Body: {"ids":["finding-id-1","finding-id-2"]}
func (h *DriftHandler) BulkResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ids required"})
		return
	}

	type idFailure struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	var (
		succeeded int
		failures  []idFailure
	)
	for _, id := range req.IDs {
		if err := h.engine.ResolveFinding(id); err != nil {
			failures = append(failures, idFailure{ID: id, Error: err.Error()})
		} else {
			succeeded++
		}
	}
	if failures == nil {
		failures = []idFailure{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  succeeded,
		"failed":   len(failures),
		"failures": failures,
	})
}

// GetMetrics handles GET /api/drift/metrics
func (h *DriftHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()
	json.NewEncoder(w).Encode(metrics)
}

// GetHistory handles GET /api/drift/history
func (h *DriftHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"scans": []interface{}{}, "total": 0})
		return
	}
	scans, err := h.store.GetScans(r.Context(), 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if scans == nil {
		scans = []*drift.DriftScan{}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scans": scans,
		"total": len(scans),
	})
}

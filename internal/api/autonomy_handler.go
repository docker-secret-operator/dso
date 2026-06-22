package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/autonomy"
)

// AutonomyHandler handles autonomy API endpoints
type AutonomyHandler struct {
	engine *autonomy.Engine
}

// NewAutonomyHandler creates a new autonomy handler
func NewAutonomyHandler(engine *autonomy.Engine) *AutonomyHandler {
	return &AutonomyHandler{engine: engine}
}

// ServeHTTP routes autonomy API requests
func (h *AutonomyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || (r.Method != http.MethodGet && user.Role != "admin") {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/autonomy/actions" && r.Method == "GET":
		h.ListActions(w, r)
	case path == "/api/autonomy/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	case strings.HasPrefix(path, "/api/autonomy/actions/") && strings.HasSuffix(path, "/execute") && r.Method == "POST":
		h.ExecuteAction(w, r)
	case strings.HasPrefix(path, "/api/autonomy/actions/") && strings.HasSuffix(path, "/cancel") && r.Method == "POST":
		h.CancelAction(w, r)
	case strings.HasPrefix(path, "/api/autonomy/actions/") && strings.HasSuffix(path, "/rollback") && r.Method == "POST":
		h.RollbackAction(w, r)
	case strings.HasPrefix(path, "/api/autonomy/actions/") && r.Method == "GET":
		h.GetAction(w, r)
	default:
		http.NotFound(w, r)
	}
}

func extractActionIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	id := parts[3]
	if idx := strings.Index(id, "/"); idx != -1 {
		id = id[:idx]
	}
	return id
}

type ActionResponse struct {
	ID                string   `json:"id"`
	Type              string   `json:"action_type"`
	Status            string   `json:"status"`
	SafetyLevel       string   `json:"safety_level"`
	ResourceID        string   `json:"resource_id"`
	Trigger           string   `json:"trigger"`
	Reason            string   `json:"reason"`
	RollbackSupported bool     `json:"rollback_supported"`
	StartedAt         *int64   `json:"started_at,omitempty"`
	CompletedAt       *int64   `json:"completed_at,omitempty"`
	CreatedAt         int64    `json:"created_at"`
	Result            string   `json:"result,omitempty"`
	Error             string   `json:"error,omitempty"`
}

// ListActions handles GET /api/autonomy/actions
func (h *AutonomyHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	actions, err := h.engine.ListActions(100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]ActionResponse, len(actions))
	for i, a := range actions {
		responses[i] = h.toActionResponse(a)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"actions": responses,
		"count":   len(responses),
	})
}

// GetAction handles GET /api/autonomy/actions/:id
func (h *AutonomyHandler) GetAction(w http.ResponseWriter, r *http.Request) {
	id := extractActionIDFromPath(r.URL.Path)
	action, err := h.engine.GetAction(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if action == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(h.toActionResponse(action))
}

// ExecuteAction handles POST /api/autonomy/actions/:id/execute
func (h *AutonomyHandler) ExecuteAction(w http.ResponseWriter, r *http.Request) {
	id := extractActionIDFromPath(r.URL.Path)
	action, err := h.engine.GetAction(id)
	if err != nil || action == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := h.engine.ExecuteAction(action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"action":  h.toActionResponse(action),
	})
}

// CancelAction handles POST /api/autonomy/actions/:id/cancel
func (h *AutonomyHandler) CancelAction(w http.ResponseWriter, r *http.Request) {
	id := extractActionIDFromPath(r.URL.Path)

	if err := h.engine.CancelAction(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action, _ := h.engine.GetAction(id)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"action":  h.toActionResponse(action),
	})
}

// RollbackAction handles POST /api/autonomy/actions/:id/rollback
func (h *AutonomyHandler) RollbackAction(w http.ResponseWriter, r *http.Request) {
	id := extractActionIDFromPath(r.URL.Path)

	if err := h.engine.RollbackAction(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	action, _ := h.engine.GetAction(id)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"action":  h.toActionResponse(action),
	})
}

// GetMetrics handles GET /api/autonomy/metrics
func (h *AutonomyHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_actions":       metrics.TotalActions,
		"successful_actions":  metrics.SuccessfulActions,
		"failed_actions":      metrics.FailedActions,
		"rollback_count":      metrics.RollbackCount,
		"automatic_actions":   metrics.AutomaticActions,
		"manual_actions":      metrics.ManualActions,
		"success_rate":        metrics.SuccessRate,
		"last_updated":        metrics.LastUpdate,
	})
}

func (h *AutonomyHandler) toActionResponse(a *autonomy.AutonomousAction) ActionResponse {
	resp := ActionResponse{
		ID:                a.ID,
		Type:              string(a.Type),
		Status:            string(a.Status),
		SafetyLevel:       string(a.SafetyLevel),
		ResourceID:        a.ResourceID,
		Trigger:           a.Trigger,
		Reason:            a.Reason,
		RollbackSupported: a.RollbackSupported,
		CreatedAt:         a.CreatedAt.Unix(),
		Result:            a.Result,
		Error:             a.Error,
	}

	if a.StartedAt != nil {
		t := a.StartedAt.Unix()
		resp.StartedAt = &t
	}

	if a.CompletedAt != nil {
		t := a.CompletedAt.Unix()
		resp.CompletedAt = &t
	}

	return resp
}

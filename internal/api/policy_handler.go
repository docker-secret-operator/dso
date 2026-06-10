package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/policy"
)

// PolicyHandler handles policy/rule API endpoints
type PolicyHandler struct {
	engine *policy.Engine
}

// NewPolicyHandler creates a new policy handler
func NewPolicyHandler(engine *policy.Engine) *PolicyHandler {
	return &PolicyHandler{
		engine: engine,
	}
}

// ServeHTTP routes policy API requests
func (h *PolicyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	user := auth.CurrentUser(r.Context())
	if user == nil || user.Role != "admin" {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return
	}

	path := r.URL.Path

	switch {
	case path == "/api/policies" && r.Method == "GET":
		h.ListPolicies(w, r)
	case path == "/api/policies" && r.Method == "POST":
		h.CreatePolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && strings.HasSuffix(path, "/run") && r.Method == "POST":
		h.RunPolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && strings.HasSuffix(path, "/enable") && r.Method == "POST":
		h.EnablePolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && strings.HasSuffix(path, "/disable") && r.Method == "POST":
		h.DisablePolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && strings.HasSuffix(path, "/history") && r.Method == "GET":
		h.GetHistory(w, r)
	case strings.HasPrefix(path, "/api/policies/") && r.Method == "GET":
		h.GetPolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && r.Method == "PUT":
		h.UpdatePolicy(w, r)
	case strings.HasPrefix(path, "/api/policies/") && r.Method == "DELETE":
		h.DeletePolicy(w, r)
	case path == "/api/policies/metrics" && r.Method == "GET":
		h.GetMetrics(w, r)
	default:
		http.NotFound(w, r)
	}
}

// extractPolicyIDFromPath extracts policy ID from URL path
func extractPolicyIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		return ""
	}
	policyID := parts[3]
	// Remove any trailing path components
	if idx := strings.Index(policyID, "/"); idx != -1 {
		policyID = policyID[:idx]
	}
	return policyID
}

// ListPolicies handles GET /api/policies
func (h *PolicyHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	rules := h.engine.ListRules()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rules": rules,
	})
}

// GetPolicy handles GET /api/policies/:id
func (h *PolicyHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	rule := h.engine.GetRule(policyID)
	if rule == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// CreatePolicy handles POST /api/policies
func (h *PolicyHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	var rule policy.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.engine.RegisterRule(&rule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rule)
}

// UpdatePolicy handles PUT /api/policies/:id
func (h *PolicyHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	rule := h.engine.GetRule(policyID)
	if rule == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	var updates policy.Rule
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update fields
	rule.Name = updates.Name
	rule.Description = updates.Description
	rule.Severity = updates.Severity
	rule.Condition = updates.Condition
	rule.Actions = updates.Actions

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// DeletePolicy handles DELETE /api/policies/:id
func (h *PolicyHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	if err := h.engine.DeleteRule(policyID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RunPolicy handles POST /api/policies/:id/run
func (h *PolicyHandler) RunPolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	if err := h.engine.EvaluateRule(policyID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "policy triggered",
		"id":     policyID,
	})
}

// EnablePolicy handles POST /api/policies/:id/enable
func (h *PolicyHandler) EnablePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	if err := h.engine.EnableRule(policyID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "policy enabled",
		"id":     policyID,
	})
}

// DisablePolicy handles POST /api/policies/:id/disable
func (h *PolicyHandler) DisablePolicy(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)
	if err := h.engine.DisableRule(policyID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "policy disabled",
		"id":     policyID,
	})
}

// GetHistory handles GET /api/policies/:id/history
func (h *PolicyHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	policyID := extractPolicyIDFromPath(r.URL.Path)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"executions": []interface{}{},
		"total":      0,
		"policy_id":  policyID,
	})
}

// GetMetrics handles GET /api/policies/metrics
func (h *PolicyHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.engine.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

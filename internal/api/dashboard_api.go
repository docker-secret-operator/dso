package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/services"
)

// DashboardHandler handles dashboard endpoints
type DashboardHandler struct {
	dashboardService *services.DashboardService
	auditService     *services.AuditService
}

// NewDashboardHandler creates a new dashboard handler
func NewDashboardHandler(
	dashboardService *services.DashboardService,
	auditService *services.AuditService,
) *DashboardHandler {
	return &DashboardHandler{
		dashboardService: dashboardService,
		auditService:     auditService,
	}
}

// ServeHTTP handles dashboard API routes
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route dashboard requests
	if strings.HasPrefix(path, "/api/dashboard/") {
		subpath := path[len("/api/dashboard/"):]

		switch {
		case subpath == "overview":
			h.GetOverview(w, r)
		case subpath == "metrics":
			h.GetMetrics(w, r)
		case strings.HasPrefix(subpath, "workflow/"):
			draftID := subpath[len("workflow/"):]
			h.GetWorkflowChain(w, r, draftID)
		case subpath == "audit":
			h.GetAuditSummary(w, r)
		default:
			h.writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "Dashboard endpoint not found",
			})
		}
		return
	}

	// Root dashboard endpoints
	if path == "/api/dashboard" {
		h.GetOverview(w, r)
		return
	}

	h.writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "Not found",
	})
}

// GetOverview returns workflow overview
// GET /api/dashboard/overview
func (h *DashboardHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	overview, err := h.dashboardService.GetWorkflowOverview(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get overview: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "dashboard.overview_viewed", "dashboard", "overview", "success")
	h.writeJSON(w, http.StatusOK, overview)
}

// GetMetrics returns workflow metrics
// GET /api/dashboard/metrics
func (h *DashboardHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	metrics, err := h.dashboardService.GetWorkflowMetrics(ctx)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get metrics: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "dashboard.metrics_viewed", "dashboard", "metrics", "success")
	h.writeJSON(w, http.StatusOK, metrics)
}

// GetWorkflowChain returns complete workflow for a draft
// GET /api/dashboard/workflow/{draftId}
func (h *DashboardHandler) GetWorkflowChain(w http.ResponseWriter, r *http.Request, draftID string) {
	ctx := r.Context()

	chain, err := h.dashboardService.GetWorkflowChain(ctx, draftID)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Workflow not found",
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "dashboard.workflow_viewed", "draft", draftID, "success")
	h.writeJSON(w, http.StatusOK, chain)
}

// GetAuditSummary returns audit event summary
// GET /api/dashboard/audit
func (h *DashboardHandler) GetAuditSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50 // Default limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		var l int
		fmt.Sscanf(limitStr, "%d", &l)
		if l > 0 && l <= 1000 {
			limit = l
		}
	}

	summary, err := h.dashboardService.GetAuditSummary(ctx, limit)
	if err != nil {
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("Failed to get audit summary: %v", err),
		})
		return
	}

	h.auditService.LogEvent(ctx, "system", "System", "dashboard.audit_viewed", "dashboard", "audit", "success")
	h.writeJSON(w, http.StatusOK, summary)
}

// Helper functions

func (h *DashboardHandler) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

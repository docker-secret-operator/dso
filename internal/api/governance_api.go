package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// GovernanceHandler handles governance validation and dashboard endpoints
type GovernanceHandler struct {
	draftService    *services.DraftService
	reviewService   *services.ReviewService
	approvalService *services.ApprovalService
	auditService    *services.AuditService
	validator       *policy.WorkflowValidator
	expValidator    *policy.ApprovalExpirationValidator
	assignValidator *policy.ApprovalAssignmentValidator
}

// NewGovernanceHandler creates a new governance handler
func NewGovernanceHandler(
	draftService *services.DraftService,
	reviewService *services.ReviewService,
	approvalService *services.ApprovalService,
	auditService *services.AuditService,
) *GovernanceHandler {
	policies := []policy.Policy{
		&policy.SingleApprovalPolicy{},
		&policy.MajorityApprovalPolicy{},
		&policy.UnanimousApprovalPolicy{},
	}

	return &GovernanceHandler{
		draftService:    draftService,
		reviewService:   reviewService,
		approvalService: approvalService,
		auditService:    auditService,
		validator:       policy.NewWorkflowValidator(policies),
		expValidator:    policy.NewApprovalExpirationValidator(0),
		assignValidator: policy.NewApprovalAssignmentValidator(),
	}
}

// ServeHTTP handles governance requests
func (h *GovernanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/")
	ctx := r.Context()

	if strings.HasPrefix(path, "workflow/validate/") {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed"})
			return
		}
		h.validateWorkflow(w, r, ctx)
		return
	}

	if strings.HasPrefix(path, "approvals/") && strings.HasSuffix(path, "/expiration") {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.checkApprovalExpiration(w, r, ctx)
		return
	}

	if path == "governance/dashboard" || path == "governance" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.getGovernanceDashboard(w, r, ctx)
		return
	}

	if path == "governance/violations" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.listPolicyViolations(w, r, ctx)
		return
	}

	if path == "governance/correlations" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h.getCorrelationReport(w, r, ctx)
		return
	}

	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Not found"})
}

// validateWorkflow validates a complete workflow
func (h *GovernanceHandler) validateWorkflow(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	draftID := strings.TrimPrefix(r.URL.Path, "/api/workflow/validate/")

	// Get draft
	draft, err := h.draftService.GetDraft(ctx, draftID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Draft not found"})
		return
	}

	// Get reviews
	reviews, _ := h.reviewService.ListReviews(ctx)
	var draftReviews []*storage.Review
	for _, review := range reviews {
		if review.DraftID == draftID {
			draftReviews = append(draftReviews, review)
		}
	}

	// Get approvals
	var approvals []*storage.Approval
	for _, review := range draftReviews {
		reviewApprovals, _ := h.approvalService.GetApprovalsForReview(ctx, review.ID)
		approvals = append(approvals, reviewApprovals...)
	}

	// Get audit events
	events, _ := h.auditService.QueryEvents(ctx, map[string]interface{}{
		"resource_id": draftID,
	})

	// Validate workflow
	result, _ := h.validator.ValidateWorkflowChain(ctx, draft, draftReviews, approvals, events)

	// Generate governance audit event
	message := fmt.Sprintf("Validation result: passed=%v, score=%d", result.Passed, result.Score)
	h.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "workflow.validation",
		ResourceID:    draftID,
		ResourceType:  "draft",
		ActorID:       "system",
		ActorName:     "system",
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: r.Header.Get("X-Correlation-ID"),
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"draft_id":   draftID,
		"passed":     result.Passed,
		"score":      result.Score,
		"violations": result.Violations,
	})
}

// checkApprovalExpiration checks if approval is expired
func (h *GovernanceHandler) checkApprovalExpiration(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	approvalID := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/approvals/"), "/")[0]

	approval, err := h.approvalService.GetApproval(ctx, approvalID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Approval not found"})
		return
	}

	expStatus := h.expValidator.GetExpirationStatus(approval)

	// Generate audit event
	message := fmt.Sprintf("Expired: %v, TimeRemaining: %v", expStatus.IsExpired, expStatus.TimeRemaining)
	h.auditService.LogEventWithDetails(ctx, &storage.AuditEvent{
		Action:        "approval.expiration_check",
		ResourceID:    approvalID,
		ResourceType:  "approval",
		ActorID:       "system",
		ActorName:     "system",
		Status:        "success",
		ResultMessage: &message,
		CorrelationID: r.Header.Get("X-Correlation-ID"),
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"approval_id":     approvalID,
		"is_expired":      expStatus.IsExpired,
		"expires_at":      expStatus.ExpiresAt,
		"time_remaining":  expStatus.TimeRemaining.String(),
		"percentage_used": int(expStatus.PercentageUsed),
	})
}

// GovernanceDashboard represents the governance dashboard
type GovernanceDashboard struct {
	TotalWorkflows      int                      `json:"total_workflows"`
	TotalViolations     int                      `json:"total_violations"`
	CriticalViolations  int                      `json:"critical_violations"`
	WarningViolations   int                      `json:"warning_violations"`
	InfoViolations      int                      `json:"info_violations"`
	HealthScore         int                      `json:"health_score"`          // 0-100
	RiskLevel           string                   `json:"risk_level"`            // low, medium, high, critical
	PolicyComplianceMap map[string]int           `json:"policy_compliance_map"` // policy -> percentage
	ViolationsByType    map[string]int           `json:"violations_by_type"`
	RecentViolations    []policy.PolicyViolation `json:"recent_violations"`
}

// getGovernanceDashboard returns governance overview
func (h *GovernanceHandler) getGovernanceDashboard(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	dashboard := &GovernanceDashboard{
		PolicyComplianceMap: make(map[string]int),
		ViolationsByType:    make(map[string]int),
		RecentViolations:    make([]policy.PolicyViolation, 0),
	}

	// Get all drafts
	drafts, _ := h.draftService.ListDrafts(ctx, "")
	dashboard.TotalWorkflows = len(drafts)

	// Validate each draft and collect violations
	allViolations := make([]policy.PolicyViolation, 0)
	violationCount := make(map[string]int)

	for _, draft := range drafts {
		reviews, _ := h.reviewService.ListReviews(ctx)
		var draftReviews []*storage.Review
		for _, review := range reviews {
			if review.DraftID == draft.ID {
				draftReviews = append(draftReviews, review)
			}
		}

		var approvals []*storage.Approval
		for _, review := range draftReviews {
			reviewApprovals, _ := h.approvalService.GetApprovalsForReview(ctx, review.ID)
			approvals = append(approvals, reviewApprovals...)
		}

		events, _ := h.auditService.QueryEvents(ctx, map[string]interface{}{
			"resource_id": draft.ID,
		})

		result, _ := h.validator.ValidateWorkflowChain(ctx, draft, draftReviews, approvals, events)

		for _, violation := range result.Violations {
			allViolations = append(allViolations, violation)
			violationCount[violation.Severity]++
			dashboard.ViolationsByType[violation.PolicyName]++
		}
	}

	dashboard.TotalViolations = len(allViolations)
	dashboard.CriticalViolations = violationCount["error"]
	dashboard.WarningViolations = violationCount["warning"]
	dashboard.InfoViolations = violationCount["info"]

	// Calculate health score
	if dashboard.TotalWorkflows > 0 {
		healthyWorkflows := dashboard.TotalWorkflows - dashboard.CriticalViolations
		dashboard.HealthScore = (healthyWorkflows * 100) / dashboard.TotalWorkflows
	} else {
		dashboard.HealthScore = 100
	}

	// Set risk level
	if dashboard.HealthScore >= 90 {
		dashboard.RiskLevel = "low"
	} else if dashboard.HealthScore >= 70 {
		dashboard.RiskLevel = "medium"
	} else if dashboard.HealthScore >= 50 {
		dashboard.RiskLevel = "high"
	} else {
		dashboard.RiskLevel = "critical"
	}

	// Get recent violations (limit to 10)
	limit := 10
	if len(allViolations) < limit {
		limit = len(allViolations)
	}
	if limit > 0 {
		dashboard.RecentViolations = allViolations[:limit]
	}

	// Policy compliance mapping (example)
	dashboard.PolicyComplianceMap["single_approval"] = 85
	dashboard.PolicyComplianceMap["majority_approval"] = 90
	dashboard.PolicyComplianceMap["unanimous_approval"] = 75

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(dashboard)
}

// listPolicyViolations returns all policy violations
func (h *GovernanceHandler) listPolicyViolations(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	policyName := r.URL.Query().Get("policy")
	severity := r.URL.Query().Get("severity")

	drafts, _ := h.draftService.ListDrafts(ctx, "")
	violations := make([]policy.PolicyViolation, 0)

	for _, draft := range drafts {
		reviews, _ := h.reviewService.ListReviews(ctx)
		var draftReviews []*storage.Review
		for _, review := range reviews {
			if review.DraftID == draft.ID {
				draftReviews = append(draftReviews, review)
			}
		}

		var approvals []*storage.Approval
		for _, review := range draftReviews {
			reviewApprovals, _ := h.approvalService.GetApprovalsForReview(ctx, review.ID)
			approvals = append(approvals, reviewApprovals...)
		}

		events, _ := h.auditService.QueryEvents(ctx, map[string]interface{}{
			"resource_id": draft.ID,
		})

		result, _ := h.validator.ValidateWorkflowChain(ctx, draft, draftReviews, approvals, events)

		for _, violation := range result.Violations {
			// Filter by policy if requested
			if policyName != "" && violation.PolicyName != policyName {
				continue
			}
			// Filter by severity if requested
			if severity != "" && violation.Severity != severity {
				continue
			}
			violations = append(violations, violation)
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_violations": len(violations),
		"violations":       violations,
	})
}

// CorrelationReport represents request tracing report
type CorrelationReport struct {
	CorrelationID string
	ResourceID    string
	EventsCount   int
	Timeline      []CorrelationEvent
}

type CorrelationEvent struct {
	EventID      string
	Action       string
	Timestamp    string
	ResourceType string
	Status       string
}

// getCorrelationReport returns correlation ID tracing
func (h *GovernanceHandler) getCorrelationReport(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	correlationID := r.URL.Query().Get("id")

	if correlationID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "correlation_id parameter required"})
		return
	}

	events, _ := h.auditService.QueryEvents(ctx, map[string]interface{}{
		"correlation_id": correlationID,
	})

	report := &CorrelationReport{
		CorrelationID: correlationID,
		EventsCount:   len(events),
		Timeline:      make([]CorrelationEvent, 0),
	}

	for _, event := range events {
		report.Timeline = append(report.Timeline, CorrelationEvent{
			EventID:      event.ID,
			Action:       event.Action,
			Timestamp:    event.Timestamp.String(),
			ResourceType: event.ResourceType,
			Status:       event.Status,
		})

		if report.ResourceID == "" {
			report.ResourceID = event.ResourceID
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(report)
}

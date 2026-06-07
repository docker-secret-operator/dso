package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// WorkflowValidator validates complete workflows against policies
type WorkflowValidator struct {
	policies []Policy
	validator *ApprovalAssignmentValidator
}

// NewWorkflowValidator creates a new workflow validator
func NewWorkflowValidator(policies []Policy) *WorkflowValidator {
	return &WorkflowValidator{
		policies:  policies,
		validator: NewApprovalAssignmentValidator(),
	}
}

// ValidateDraft validates a complete draft workflow
func (w *WorkflowValidator) ValidateDraft(
	ctx context.Context,
	draft *storage.Draft,
	reviews []*storage.Review,
	approvals []*storage.Approval,
	auditEvents []*storage.AuditEvent,
) (PolicyResult, error) {
	result := PolicyResult{
		Passed:     true,
		Violations: []PolicyViolation{},
		Score:      100,
	}

	// Validate draft status
	if draft.Status == "rejected" || draft.Status == "archived" {
		return result, nil // Already finalized
	}

	// Check reviews exist if draft is under review
	if draft.Status == "under_review" && len(reviews) == 0 {
		result.Passed = false
		result.Score = 50
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: "draft_review_requirement",
			Severity:   "error",
			Message:    "Draft under review must have at least one review",
			ResourceID: draft.ID,
			Timestamp:  time.Now(),
			Remediation: "Create a review for this draft",
		})
	}

	// Evaluate all policies
	policyContext := PolicyContext{
		Draft:       draft,
		Review:      nil,
		Approvals:   approvals,
		AuditEvents: auditEvents,
	}

	for _, review := range reviews {
		policyContext.Review = review
		for _, policy := range w.policies {
			policyResult := policy.Evaluate(ctx, policyContext)
			if !policyResult.Passed {
				result.Passed = false
				result.Violations = append(result.Violations, policyResult.Violations...)
				result.Score = minScore(result.Score, policyResult.Score)
			}
		}
	}

	return result, nil
}

// ValidateWorkflowChain validates entire Draft→Review→Approval chain
func (w *WorkflowValidator) ValidateWorkflowChain(
	ctx context.Context,
	draft *storage.Draft,
	reviews []*storage.Review,
	approvals []*storage.Approval,
	auditEvents []*storage.AuditEvent,
) (PolicyResult, error) {
	result := PolicyResult{
		Passed:     true,
		Violations: []PolicyViolation{},
		Score:      100,
	}

	// Validate draft exists and is valid
	if draft == nil {
		return result, fmt.Errorf("draft is required")
	}

	// Validate review-draft relationship
	for _, review := range reviews {
		if review.DraftID != draft.ID {
			result.Passed = false
			result.Violations = append(result.Violations, PolicyViolation{
				PolicyName: "review_draft_relationship",
				Severity:   "error",
				Message:    fmt.Sprintf("Review %s is not associated with draft %s", review.ID, draft.ID),
				ResourceID: review.ID,
				Timestamp:  time.Now(),
			})
		}
	}

	// Validate approval-review relationship
	for _, approval := range approvals {
		found := false
		for _, review := range reviews {
			if approval.ReviewID == review.ID {
				found = true
				break
			}
		}
		if !found {
			result.Passed = false
			result.Violations = append(result.Violations, PolicyViolation{
				PolicyName: "approval_review_relationship",
				Severity:   "error",
				Message:    fmt.Sprintf("Approval %s is not associated with any review", approval.ID),
				ResourceID: approval.ID,
				Timestamp:  time.Now(),
			})
		}
	}

	// Validate workflow state transitions
	// Draft can't be approved without review
	if draft.Status == "approved" && len(reviews) == 0 {
		result.Passed = false
		result.Score = 0
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: "workflow_state_validity",
			Severity:   "error",
			Message:    "Draft cannot be approved without a completed review",
			ResourceID: draft.ID,
			Timestamp:  time.Now(),
		})
	}

	// Run draft validation
	draftResult, _ := w.ValidateDraft(ctx, draft, reviews, approvals, auditEvents)
	if !draftResult.Passed {
		result.Passed = false
		result.Score = minScore(result.Score, draftResult.Score)
		result.Violations = append(result.Violations, draftResult.Violations...)
	}

	return result, nil
}

// CorrelationValidator validates request tracing across workflow
type CorrelationValidator struct{}

// ValidateCorrelation checks that all events have consistent correlation ID
func (cv *CorrelationValidator) ValidateCorrelation(
	ctx context.Context,
	draft *storage.Draft,
	reviews []*storage.Review,
	approvals []*storage.Approval,
	auditEvents []*storage.AuditEvent,
) error {
	// Collect all correlation IDs
	correlationIDs := make(map[string]bool)

	for _, event := range auditEvents {
		if event.CorrelationID != "" {
			correlationIDs[event.CorrelationID] = true
		}
	}

	// All events should have same correlation ID for same request
	if len(correlationIDs) > 1 {
		return fmt.Errorf("workflow has multiple correlation IDs: expected single ID for end-to-end tracing")
	}

	return nil
}

// CorrelationValidator checks that workflow events are properly correlated
func (cv *CorrelationValidator) GetWorkflowCorrelationID(
	auditEvents []*storage.AuditEvent,
) string {
	if len(auditEvents) > 0 {
		return auditEvents[0].CorrelationID
	}
	return ""
}

// Helper function to get minimum score
func minScore(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ApprovalExpirationValidator checks approval expiration state
type ApprovalExpirationValidator struct {
	defaultTTL time.Duration
}

// NewApprovalExpirationValidator creates validator with default TTL
func NewApprovalExpirationValidator(ttl time.Duration) *ApprovalExpirationValidator {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour // Default 7 days
	}
	return &ApprovalExpirationValidator{
		defaultTTL: ttl,
	}
}

// ValidateApprovalExpiration checks if approval should be marked expired
func (aev *ApprovalExpirationValidator) ValidateApprovalExpiration(
	approval *storage.Approval,
) (bool, time.Duration) {
	if approval.Decision != "pending" {
		return false, 0 // Only pending approvals can expire
	}

	expiresAt := approval.CreatedAt.Add(aev.defaultTTL)
	now := time.Now()

	if now.After(expiresAt) {
		return true, 0 // Already expired
	}

	timeRemaining := expiresAt.Sub(now)
	return false, timeRemaining
}

// GetExpirationStatus returns detailed expiration info
func (aev *ApprovalExpirationValidator) GetExpirationStatus(
	approval *storage.Approval,
) ExpirationStatus {
	expiresAt := approval.CreatedAt.Add(aev.defaultTTL)
	timeRemaining := time.Until(expiresAt)

	status := ExpirationStatus{
		ExpiresAt:       expiresAt,
		TimeRemaining:   timeRemaining,
		IsExpired:       timeRemaining <= 0,
		PercentageUsed:  calculatePercentageUsed(approval.CreatedAt, expiresAt),
	}

	return status
}

// ExpirationStatus tracks approval expiration info
type ExpirationStatus struct {
	ExpiresAt      time.Time
	TimeRemaining  time.Duration
	IsExpired      bool
	PercentageUsed float64 // 0-100
}

// Helper to calculate TTL usage percentage
func calculatePercentageUsed(createdAt time.Time, expiresAt time.Time) float64 {
	totalTTL := expiresAt.Sub(createdAt)
	elapsed := time.Since(createdAt)

	if totalTTL <= 0 {
		return 0
	}

	percentage := float64(elapsed) / float64(totalTTL) * 100
	if percentage > 100 {
		return 100
	}
	return percentage
}

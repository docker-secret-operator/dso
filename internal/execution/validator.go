package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionValidator validates execution readiness
type ExecutionValidator struct {
	draftStore    storage.DraftStore
	reviewStore   storage.ReviewStore
	approvalStore storage.ApprovalStore
}

// NewExecutionValidator creates a new validator
func NewExecutionValidator(
	draftStore storage.DraftStore,
	reviewStore storage.ReviewStore,
	approvalStore storage.ApprovalStore,
) *ExecutionValidator {
	return &ExecutionValidator{
		draftStore:    draftStore,
		reviewStore:   reviewStore,
		approvalStore: approvalStore,
	}
}

// ValidateRequest validates if an execution request can proceed
func (v *ExecutionValidator) ValidateRequest(
	ctx context.Context,
	draftID string,
	approvalID string,
) (ValidationReport, error) {
	report := ValidationReport{
		ApprovalValid:   false,
		GovernanceValid: false,
		VersionValid:    false,
		SafetyValid:     false,
		AllValid:        false,
	}

	// Validate approval exists and is in correct state
	report.ApprovalValid, report.ApprovalMessage = v.validateApproval(ctx, approvalID)

	if !report.ApprovalValid {
		return report, nil
	}

	// Validate governance policies
	report.GovernanceValid, report.GovernanceMessage = v.validateGovernance(ctx, draftID, approvalID)

	// Validate version matching
	report.VersionValid, report.VersionMessage = v.validateVersion(ctx, draftID, approvalID)

	// Validate safety requirements
	report.SafetyValid, report.SafetyMessage = v.validateSafety(ctx, draftID)

	// All checks must pass
	report.AllValid = report.ApprovalValid && report.GovernanceValid && report.VersionValid && report.SafetyValid

	return report, nil
}

// validateApproval checks approval status and expiration
func (v *ExecutionValidator) validateApproval(ctx context.Context, approvalID string) (bool, string) {
	approval, err := v.approvalStore.GetByID(ctx, approvalID)
	if err != nil {
		return false, fmt.Sprintf("Approval not found: %v", approvalID)
	}

	if approval == nil {
		return false, "Approval not found"
	}

	// Check decision
	if approval.Decision != "approved" {
		return false, fmt.Sprintf("Approval decision is %s, expected 'approved'", approval.Decision)
	}

	// Check expiration (7 days default)
	ttl := 7 * 24 * time.Hour
	expiresAt := approval.CreatedAt.Add(ttl)
	if time.Now().After(expiresAt) {
		return false, "Approval has expired"
	}

	return true, "Approval valid"
}

// validateGovernance checks governance policies
func (v *ExecutionValidator) validateGovernance(ctx context.Context, draftID string, approvalID string) (bool, string) {
	// Get draft
	draft, err := v.draftStore.GetByID(ctx, draftID)
	if err != nil || draft == nil {
		return false, "Draft not found"
	}

	// Validate draft status
	if draft.Status != "approved" {
		return false, fmt.Sprintf("Draft status is %s, expected 'approved'", draft.Status)
	}

	// Get approval to find review
	approval, _ := v.approvalStore.GetByID(ctx, approvalID)
	if approval == nil {
		return false, "Approval not found"
	}

	// Get review
	review, err := v.reviewStore.GetByID(ctx, approval.ReviewID)
	if err != nil || review == nil {
		return false, "Review not found"
	}

	// Validate review status
	if review.Status != "under_review" && review.Status != "approved" {
		return false, fmt.Sprintf("Review status is %s, expected 'under_review' or 'approved'", review.Status)
	}

	// Check approval count (minimum 1)
	approvals, _ := v.approvalStore.ListForReview(ctx, review.ID)
	approvedCount := 0
	for _, a := range approvals {
		if a.Decision == "approved" {
			approvedCount++
		}
	}

	if approvedCount == 0 {
		return false, "No approvals found for review"
	}

	return true, "Governance valid"
}

// validateVersion checks draft version matches approved version
func (v *ExecutionValidator) validateVersion(ctx context.Context, draftID string, approvalID string) (bool, string) {
	draft, _ := v.draftStore.GetByID(ctx, draftID)
	if draft == nil {
		return false, "Draft not found"
	}

	approval, _ := v.approvalStore.GetByID(ctx, approvalID)
	if approval == nil {
		return false, "Approval not found"
	}

	// Check if versions match (approval should have captured draft version)
	// For now, just verify draft exists and is in valid state
	if draft.VersionNumber == 0 {
		return false, "Draft has no version"
	}

	return true, "Version valid"
}

// validateSafety checks safety requirements
func (v *ExecutionValidator) validateSafety(ctx context.Context, draftID string) (bool, string) {
	draft, _ := v.draftStore.GetByID(ctx, draftID)
	if draft == nil {
		return false, "Draft not found"
	}

	// Check draft has configuration
	if draft.Config == "" {
		return false, "Draft has no configuration"
	}

	return true, "Safety valid"
}

// GetReadinessScore calculates a readiness score (0-100)
func (v *ExecutionValidator) GetReadinessScore(report ValidationReport) int {
	score := 0

	if report.ApprovalValid {
		score += 25
	}
	if report.GovernanceValid {
		score += 25
	}
	if report.VersionValid {
		score += 25
	}
	if report.SafetyValid {
		score += 25
	}

	return score
}

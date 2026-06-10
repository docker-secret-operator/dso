package services

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ApprovalService handles approval operations
type ApprovalService struct {
	approvalStore storage.ApprovalStore
	auditStore    storage.AuditStore
}

// NewApprovalService creates a new approval service
func NewApprovalService(
	approvalStore storage.ApprovalStore,
	auditStore storage.AuditStore,
) *ApprovalService {
	return &ApprovalService{
		approvalStore: approvalStore,
		auditStore:    auditStore,
	}
}

// CreateApproval creates a new approval
func (as *ApprovalService) CreateApproval(ctx context.Context, approval *storage.Approval) (*storage.Approval, error) {
	if approval.ReviewID == "" || approval.ReviewerID == "" {
		return nil, fmt.Errorf("ReviewID and ReviewerID are required")
	}

	// Set defaults
	if approval.ID == "" {
		approval.ID = generateID()
	}
	if approval.Decision == "" {
		approval.Decision = "pending"
	}
	now := time.Now()
	approval.CreatedAt = now

	if err := as.approvalStore.Create(ctx, approval); err != nil {
		return nil, err
	}

	return approval, nil
}

// GetApproval retrieves an approval by ID
func (as *ApprovalService) GetApproval(ctx context.Context, id string) (*storage.Approval, error) {
	return as.approvalStore.GetByID(ctx, id)
}

// ListApprovalsForAllReviews lists approvals across all reviews
// Note: Returns empty for now - use GetApprovalsForReview for per-review query
func (as *ApprovalService) ListApprovalsForAllReviews(ctx context.Context) ([]*storage.Approval, error) {
	return make([]*storage.Approval, 0), nil
}

// UpdateApproval updates an approval
func (as *ApprovalService) UpdateApproval(ctx context.Context, approval *storage.Approval) (*storage.Approval, error) {
	if approval.ID == "" {
		return nil, fmt.Errorf("approval ID is required")
	}

	if err := as.approvalStore.Update(ctx, approval); err != nil {
		return nil, err
	}

	return approval, nil
}

// DeleteApproval deletes an approval (soft delete)
func (as *ApprovalService) DeleteApproval(ctx context.Context, id string) error {
	approval, err := as.approvalStore.GetByID(ctx, id)
	if err != nil {
		return err
	}

	approval.Decision = "closed"
	return as.approvalStore.Update(ctx, approval)
}

// GetApprovalsForReview gets all approvals for a review
func (as *ApprovalService) GetApprovalsForReview(ctx context.Context, reviewID string) ([]*storage.Approval, error) {
	return as.approvalStore.ListForReview(ctx, reviewID)
}

// GetPendingApprovalsForReviewer gets pending approvals for a reviewer
func (as *ApprovalService) GetPendingApprovalsForReviewer(ctx context.Context, reviewerID string) ([]*storage.Approval, error) {
	return as.approvalStore.ListPendingForReviewer(ctx, reviewerID)
}

// ApproveApproval approves an approval
func (as *ApprovalService) ApproveApproval(ctx context.Context, approvalID string, reason string) (*storage.Approval, error) {
	approval, err := as.GetApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}

	if !CanApprove(approval.Decision) {
		return nil, fmt.Errorf("cannot approve: current status is %s", approval.Decision)
	}

	approval.Decision = "approved"
	now := time.Now()
	approval.DecidedAt = &now
	approval.Comments = &reason

	return as.UpdateApproval(ctx, approval)
}

// RejectApproval rejects an approval
func (as *ApprovalService) RejectApproval(ctx context.Context, approvalID string, reason string) (*storage.Approval, error) {
	approval, err := as.GetApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}

	if !CanReject(approval.Decision) {
		return nil, fmt.Errorf("cannot reject: current status is %s", approval.Decision)
	}

	approval.Decision = "rejected"
	now := time.Now()
	approval.DecidedAt = &now
	approval.Comments = &reason

	return as.UpdateApproval(ctx, approval)
}

// ExpireApproval marks an approval as expired
func (as *ApprovalService) ExpireApproval(ctx context.Context, approvalID string) (*storage.Approval, error) {
	approval, err := as.GetApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}

	if !CanExpire(approval.Decision) {
		return nil, fmt.Errorf("cannot expire: current status is %s", approval.Decision)
	}

	approval.Decision = "expired"
	now := time.Now()
	approval.DecidedAt = &now

	return as.UpdateApproval(ctx, approval)
}

// CloseApproval closes an approval
func (as *ApprovalService) CloseApproval(ctx context.Context, approvalID string) (*storage.Approval, error) {
	approval, err := as.GetApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}

	if !CanCloseApproval(approval.Decision) {
		return nil, fmt.Errorf("cannot close: current status is %s", approval.Decision)
	}

	approval.Decision = "closed"
	return as.UpdateApproval(ctx, approval)
}

// CheckApprovalRules checks approval rules for a review
type ApprovalRuleResult struct {
	RuleName    string
	Required    int
	Received    int
	IsMet       bool
	Description string
}

// CheckUnanimousApproval checks if all reviewers approved
func (as *ApprovalService) CheckUnanimousApproval(ctx context.Context, reviewID string) (bool, error) {
	approvals, err := as.GetApprovalsForReview(ctx, reviewID)
	if err != nil {
		return false, err
	}

	if len(approvals) == 0 {
		return false, nil // No approvals = not unanimous
	}

	for _, approval := range approvals {
		if approval.Decision != "approved" {
			return false, nil
		}
	}

	return true, nil
}

// CheckMajorityApproval checks if majority of reviewers approved
func (as *ApprovalService) CheckMajorityApproval(ctx context.Context, reviewID string) (bool, error) {
	approvals, err := as.GetApprovalsForReview(ctx, reviewID)
	if err != nil {
		return false, err
	}

	if len(approvals) == 0 {
		return false, nil
	}

	approvedCount := 0
	for _, approval := range approvals {
		if approval.Decision == "approved" {
			approvedCount++
		}
	}

	return approvedCount > len(approvals)/2, nil
}

// CheckSingleApproval checks if at least one reviewer approved
func (as *ApprovalService) CheckSingleApproval(ctx context.Context, reviewID string) (bool, error) {
	approvals, err := as.GetApprovalsForReview(ctx, reviewID)
	if err != nil {
		return false, err
	}

	for _, approval := range approvals {
		if approval.Decision == "approved" {
			return true, nil
		}
	}

	return false, nil
}

// GetApprovalStats gets statistics for a review's approvals
func (as *ApprovalService) GetApprovalStats(ctx context.Context, reviewID string) map[string]int {
	approvals, _ := as.GetApprovalsForReview(ctx, reviewID)

	stats := map[string]int{
		"total":    len(approvals),
		"pending":  0,
		"approved": 0,
		"rejected": 0,
		"expired":  0,
		"closed":   0,
	}

	for _, approval := range approvals {
		stats[approval.Decision]++
	}

	return stats
}

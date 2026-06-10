package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ApprovalAssignmentValidator validates approval assignments
type ApprovalAssignmentValidator struct{}

// NewApprovalAssignmentValidator creates a new validator
func NewApprovalAssignmentValidator() *ApprovalAssignmentValidator {
	return &ApprovalAssignmentValidator{}
}

// ValidateAssignment validates if an approval can be assigned
func (v *ApprovalAssignmentValidator) ValidateAssignment(
	ctx context.Context,
	newApproval *storage.Approval,
	review *storage.Review,
	existingApprovals []*storage.Approval,
) error {
	// Check if review is in valid state for assignment
	if review.Status == "closed" {
		return fmt.Errorf("cannot assign approval: review is closed")
	}

	if review.Status == "rejected" {
		return fmt.Errorf("cannot assign approval: review is rejected")
	}

	// Check for duplicate reviewer
	for _, existing := range existingApprovals {
		if existing.ReviewID == review.ID && existing.ReviewerID == newApproval.ReviewerID {
			return fmt.Errorf("reviewer %s already assigned to this review", newApproval.ReviewerID)
		}
	}

	// Check if reviewer ID is empty
	if newApproval.ReviewerID == "" {
		return fmt.Errorf("reviewer_id cannot be empty")
	}

	// Check if approval is already closed
	if newApproval.Decision == "closed" {
		return fmt.Errorf("cannot assign approval: approval is already closed")
	}

	return nil
}

// ValidateExistingApprovals validates the set of approvals for a review
func (v *ApprovalAssignmentValidator) ValidateExistingApprovals(
	ctx context.Context,
	review *storage.Review,
	approvals []*storage.Approval,
	minReviewers int,
	maxReviewers int,
) error {
	// Count reviewers for this review
	reviewerCount := 0
	reviewers := make(map[string]bool)

	for _, approval := range approvals {
		if approval.ReviewID == review.ID {
			reviewerCount++
			if reviewers[approval.ReviewerID] {
				return fmt.Errorf("duplicate reviewer assignment: %s", approval.ReviewerID)
			}
			reviewers[approval.ReviewerID] = true

			// Check if approval is expired
			if IsApprovalExpired(approval) {
				return fmt.Errorf("approval for reviewer %s is expired", approval.ReviewerID)
			}

			// Check if approval is closed
			if approval.Decision == "closed" {
				return fmt.Errorf("approval for reviewer %s is already closed", approval.ReviewerID)
			}
		}
	}

	// Check min/max reviewers
	if minReviewers > 0 && reviewerCount < minReviewers {
		return fmt.Errorf("minimum reviewers required: %d, found: %d", minReviewers, reviewerCount)
	}

	if maxReviewers > 0 && reviewerCount > maxReviewers {
		return fmt.Errorf("maximum reviewers exceeded: %d, found: %d", maxReviewers, reviewerCount)
	}

	return nil
}

// ApprovalExpiration tracks expiration state
type ApprovalExpiration struct {
	ExpiresAt time.Time
	TTL       time.Duration
}

// IsApprovalExpired checks if approval has expired
func IsApprovalExpired(approval *storage.Approval) bool {
	// If no expiration set, never expires
	if approval.CreatedAt.IsZero() {
		return false
	}

	// Default TTL: 7 days from creation
	defaultTTL := 7 * 24 * time.Hour
	expiresAt := approval.CreatedAt.Add(defaultTTL)

	return time.Now().After(expiresAt)
}

// GetExpirationTime returns when approval expires
func GetExpirationTime(approval *storage.Approval, ttl time.Duration) time.Time {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour // Default 7 days
	}
	return approval.CreatedAt.Add(ttl)
}

// GetTimeUntilExpiration returns time until approval expires
func GetTimeUntilExpiration(approval *storage.Approval, ttl time.Duration) time.Duration {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	expiresAt := approval.CreatedAt.Add(ttl)
	return time.Until(expiresAt)
}

// ReviewGovernanceConfig defines review governance rules
type ReviewGovernanceConfig struct {
	MinReviewers      int           // Minimum reviewers required
	MaxReviewers      int           // Maximum reviewers allowed
	RequiredApprovals int           // Minimum approvals needed
	ApprovalQuorum    float64       // Percentage of reviewers who must approve (0-100)
	ApprovalTTL       time.Duration // Time before approval expires
}

// ValidateReviewGovernance validates review against governance rules
func (v *ApprovalAssignmentValidator) ValidateReviewGovernance(
	ctx context.Context,
	review *storage.Review,
	approvals []*storage.Approval,
	config ReviewGovernanceConfig,
) (PolicyResult, error) {
	result := PolicyResult{
		Passed:     true,
		Violations: []PolicyViolation{},
		Score:      100,
	}

	// Count approvals for this review
	reviewApprovals := make([]*storage.Approval, 0)
	for _, approval := range approvals {
		if approval.ReviewID == review.ID {
			reviewApprovals = append(reviewApprovals, approval)
		}
	}

	// Validate minimum reviewers
	if config.MinReviewers > 0 && len(reviewApprovals) < config.MinReviewers {
		result.Passed = false
		result.Score = (len(reviewApprovals) * 100) / config.MinReviewers
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: "min_reviewers",
			Severity:   "error",
			Message:    fmt.Sprintf("Minimum reviewers required: %d, found: %d", config.MinReviewers, len(reviewApprovals)),
			ResourceID: review.ID,
			Timestamp:  time.Now(),
		})
	}

	// Validate maximum reviewers
	if config.MaxReviewers > 0 && len(reviewApprovals) > config.MaxReviewers {
		result.Passed = false
		result.Score = 0
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: "max_reviewers",
			Severity:   "error",
			Message:    fmt.Sprintf("Maximum reviewers exceeded: %d, limit: %d", len(reviewApprovals), config.MaxReviewers),
			ResourceID: review.ID,
			Timestamp:  time.Now(),
		})
	}

	// Count approved and expired
	approvedCount := 0
	expiredCount := 0
	for _, approval := range reviewApprovals {
		if approval.Decision == "approved" {
			approvedCount++
		}
		if IsApprovalExpired(approval) {
			expiredCount++
		}
	}

	// Validate required approvals
	if config.RequiredApprovals > 0 && approvedCount < config.RequiredApprovals {
		result.Passed = false
		result.Score = (approvedCount * 100) / config.RequiredApprovals
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: "required_approvals",
			Severity:   "error",
			Message:    fmt.Sprintf("Required approvals: %d, found: %d", config.RequiredApprovals, approvedCount),
			ResourceID: review.ID,
			Timestamp:  time.Now(),
		})
	}

	// Validate quorum
	if config.ApprovalQuorum > 0 && len(reviewApprovals) > 0 {
		requiredQuorum := float64(approvedCount) / float64(len(reviewApprovals)) * 100
		if requiredQuorum < config.ApprovalQuorum {
			result.Passed = false
			result.Score = int(requiredQuorum)
			result.Violations = append(result.Violations, PolicyViolation{
				PolicyName: "approval_quorum",
				Severity:   "error",
				Message:    fmt.Sprintf("Approval quorum required: %.0f%%, achieved: %.0f%%", config.ApprovalQuorum, requiredQuorum),
				ResourceID: review.ID,
				Timestamp:  time.Now(),
			})
		}
	}

	// Warn about expired approvals
	if expiredCount > 0 {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName:  "expired_approvals",
			Severity:    "warning",
			Message:     fmt.Sprintf("Found %d expired approvals", expiredCount),
			ResourceID:  review.ID,
			Timestamp:   time.Now(),
			Remediation: "Expired approvals should be reassigned",
		})
	}

	return result, nil
}

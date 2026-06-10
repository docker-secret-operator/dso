package policy

import (
	"context"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// PolicyViolation represents a single policy violation
type PolicyViolation struct {
	PolicyName  string    `json:"policy_name"`
	Severity    string    `json:"severity"` // error, warning, info
	Message     string    `json:"message"`
	ResourceID  string    `json:"resource_id"`
	Timestamp   time.Time `json:"timestamp"`
	Remediation string    `json:"remediation,omitempty"`
}

// PolicyResult represents the result of policy evaluation
type PolicyResult struct {
	Passed     bool
	Violations []PolicyViolation
	Warnings   []string
	Score      int // 0-100
}

// PolicyContext contains data needed for policy evaluation
type PolicyContext struct {
	Draft       *storage.Draft
	Review      *storage.Review
	Approvals   []*storage.Approval
	AuditEvents []*storage.AuditEvent
}

// Policy is the interface for workflow policies
type Policy interface {
	Name() string
	Evaluate(ctx context.Context, context PolicyContext) PolicyResult
}

// ApprovalPolicy validates approval decisions
type ApprovalPolicy interface {
	Policy
	ValidateAssignment(ctx context.Context, approval *storage.Approval, review *storage.Review, existingApprovals []*storage.Approval) error
}

// ReviewPolicy validates review configuration
type ReviewPolicy interface {
	Policy
	GetMinReviewers() int
	GetMaxReviewers() int
	GetRequiredApprovals() int
	GetApprovalQuorum() float64
}

// WorkflowPolicy validates overall workflow state
type WorkflowPolicy interface {
	Policy
	Evaluate(ctx context.Context, context PolicyContext) PolicyResult
}

// SingleApprovalPolicy requires at least one approval
type SingleApprovalPolicy struct{}

func (p *SingleApprovalPolicy) Name() string {
	return "single_approval"
}

func (p *SingleApprovalPolicy) Evaluate(ctx context.Context, context PolicyContext) PolicyResult {
	result := PolicyResult{
		Passed:     false,
		Violations: []PolicyViolation{},
		Score:      0,
	}

	if context.Review == nil {
		return result
	}

	approvedCount := 0
	for _, approval := range context.Approvals {
		if approval.ReviewID == context.Review.ID && approval.Decision == "approved" {
			approvedCount++
		}
	}

	if approvedCount >= 1 {
		result.Passed = true
		result.Score = 100
	} else {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: p.Name(),
			Severity:   "error",
			Message:    "At least one approval required",
			ResourceID: context.Review.ID,
			Timestamp:  time.Now(),
		})
		result.Score = 0
	}

	return result
}

// MajorityApprovalPolicy requires majority of reviewers to approve
type MajorityApprovalPolicy struct{}

func (p *MajorityApprovalPolicy) Name() string {
	return "majority_approval"
}

func (p *MajorityApprovalPolicy) Evaluate(ctx context.Context, context PolicyContext) PolicyResult {
	result := PolicyResult{
		Passed:     false,
		Violations: []PolicyViolation{},
		Score:      0,
	}

	if context.Review == nil {
		return result
	}

	reviewApprovals := make([]*storage.Approval, 0)
	for _, approval := range context.Approvals {
		if approval.ReviewID == context.Review.ID {
			reviewApprovals = append(reviewApprovals, approval)
		}
	}

	if len(reviewApprovals) == 0 {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: p.Name(),
			Severity:   "error",
			Message:    "No approvals found",
			ResourceID: context.Review.ID,
			Timestamp:  time.Now(),
		})
		return result
	}

	approvedCount := 0
	for _, approval := range reviewApprovals {
		if approval.Decision == "approved" {
			approvedCount++
		}
	}

	requiredCount := (len(reviewApprovals) / 2) + 1
	if approvedCount >= requiredCount {
		result.Passed = true
		result.Score = 100
	} else {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName:  p.Name(),
			Severity:    "error",
			Message:     "Majority approval not achieved",
			ResourceID:  context.Review.ID,
			Timestamp:   time.Now(),
			Remediation: "Need more approvals to reach majority",
		})
		result.Score = (approvedCount * 100) / requiredCount
	}

	return result
}

// UnanimousApprovalPolicy requires all reviewers to approve
type UnanimousApprovalPolicy struct{}

func (p *UnanimousApprovalPolicy) Name() string {
	return "unanimous_approval"
}

func (p *UnanimousApprovalPolicy) Evaluate(ctx context.Context, context PolicyContext) PolicyResult {
	result := PolicyResult{
		Passed:     false,
		Violations: []PolicyViolation{},
		Score:      0,
	}

	if context.Review == nil {
		return result
	}

	reviewApprovals := make([]*storage.Approval, 0)
	for _, approval := range context.Approvals {
		if approval.ReviewID == context.Review.ID {
			reviewApprovals = append(reviewApprovals, approval)
		}
	}

	if len(reviewApprovals) == 0 {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: p.Name(),
			Severity:   "error",
			Message:    "No approvals found",
			ResourceID: context.Review.ID,
			Timestamp:  time.Now(),
		})
		return result
	}

	approvedCount := 0
	rejectedCount := 0
	pendingCount := 0

	for _, approval := range reviewApprovals {
		switch approval.Decision {
		case "approved":
			approvedCount++
		case "rejected":
			rejectedCount++
		case "pending":
			pendingCount++
		}
	}

	if rejectedCount > 0 {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: p.Name(),
			Severity:   "error",
			Message:    "Rejections found - unanimous approval not possible",
			ResourceID: context.Review.ID,
			Timestamp:  time.Now(),
		})
		result.Score = 0
		return result
	}

	if approvedCount == len(reviewApprovals) {
		result.Passed = true
		result.Score = 100
	} else if pendingCount > 0 {
		result.Violations = append(result.Violations, PolicyViolation{
			PolicyName: p.Name(),
			Severity:   "warning",
			Message:    "Pending approvals - unanimous approval not yet achieved",
			ResourceID: context.Review.ID,
			Timestamp:  time.Now(),
		})
		result.Score = (approvedCount * 100) / len(reviewApprovals)
	}

	return result
}

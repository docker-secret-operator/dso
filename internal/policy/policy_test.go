package policy

import (
	"context"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

func TestSingleApprovalPolicy(t *testing.T) {
	ctx := context.Background()
	policy := &SingleApprovalPolicy{}

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// Test: No approvals
	result := policy.Evaluate(ctx, PolicyContext{
		Review:    review,
		Approvals: []*storage.Approval{},
	})

	if result.Passed {
		t.Error("Expected policy to fail with no approvals")
	}
	if result.Score != 0 {
		t.Errorf("Expected score 0, got %d", result.Score)
	}

	// Test: One approval
	result = policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{
				ID:       "approval-1",
				ReviewID: "review-1",
				Decision: "approved",
			},
		},
	})

	if !result.Passed {
		t.Error("Expected policy to pass with one approval")
	}
	if result.Score != 100 {
		t.Errorf("Expected score 100, got %d", result.Score)
	}
}

func TestMajorityApprovalPolicy(t *testing.T) {
	ctx := context.Background()
	policy := &MajorityApprovalPolicy{}

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// Test: 2 approvals, 1 reject (50% approved, need 50%+1)
	result := policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{ID: "a1", ReviewID: "review-1", Decision: "approved"},
			{ID: "a2", ReviewID: "review-1", Decision: "rejected"},
		},
	})

	if result.Passed {
		t.Error("Expected policy to fail with 50% approval rate")
	}

	// Test: 3 approvals, 2 approved (66% approved, need 50%+1)
	result = policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{ID: "a1", ReviewID: "review-1", Decision: "approved"},
			{ID: "a2", ReviewID: "review-1", Decision: "approved"},
			{ID: "a3", ReviewID: "review-1", Decision: "rejected"},
		},
	})

	if !result.Passed {
		t.Error("Expected policy to pass with 66% approval rate")
	}
}

func TestUnanimousApprovalPolicy(t *testing.T) {
	ctx := context.Background()
	policy := &UnanimousApprovalPolicy{}

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// Test: All approved
	result := policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{ID: "a1", ReviewID: "review-1", Decision: "approved"},
			{ID: "a2", ReviewID: "review-1", Decision: "approved"},
		},
	})

	if !result.Passed {
		t.Error("Expected policy to pass with all approvals")
	}

	// Test: One rejection fails unanimous
	result = policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{ID: "a1", ReviewID: "review-1", Decision: "approved"},
			{ID: "a2", ReviewID: "review-1", Decision: "rejected"},
		},
	})

	if result.Passed {
		t.Error("Expected policy to fail with any rejection")
	}

	// Test: Pending approval warning but not failure
	result = policy.Evaluate(ctx, PolicyContext{
		Review: review,
		Approvals: []*storage.Approval{
			{ID: "a1", ReviewID: "review-1", Decision: "approved"},
			{ID: "a2", ReviewID: "review-1", Decision: "pending"},
		},
	})

	if result.Passed {
		t.Error("Expected policy to fail with pending approvals")
	}
	if len(result.Violations) == 0 {
		t.Error("Expected warnings for pending approvals")
	}
}

func TestApprovalAssignmentValidator(t *testing.T) {
	ctx := context.Background()
	validator := NewApprovalAssignmentValidator()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	newApproval := &storage.Approval{
		ReviewID:   "review-1",
		ReviewerID: "reviewer-1",
	}

	// Test: Valid assignment
	err := validator.ValidateAssignment(ctx, newApproval, review, []*storage.Approval{})
	if err != nil {
		t.Errorf("Expected valid assignment, got error: %v", err)
	}

	// Test: Duplicate reviewer
	existing := []*storage.Approval{
		{
			ReviewID:   "review-1",
			ReviewerID: "reviewer-1",
		},
	}

	err = validator.ValidateAssignment(ctx, newApproval, review, existing)
	if err == nil {
		t.Error("Expected error for duplicate reviewer")
	}

	// Test: Closed review
	closedReview := &storage.Review{
		ID:     "review-1",
		Status: "closed",
	}

	err = validator.ValidateAssignment(ctx, newApproval, closedReview, []*storage.Approval{})
	if err == nil {
		t.Error("Expected error for closed review")
	}

	// Test: Empty reviewer ID
	invalidApproval := &storage.Approval{
		ReviewID:   "review-1",
		ReviewerID: "",
	}

	err = validator.ValidateAssignment(ctx, invalidApproval, review, []*storage.Approval{})
	if err == nil {
		t.Error("Expected error for empty reviewer ID")
	}
}

func TestReviewGovernanceValidation(t *testing.T) {
	ctx := context.Background()
	validator := NewApprovalAssignmentValidator()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	config := ReviewGovernanceConfig{
		MinReviewers:      2,
		MaxReviewers:      5,
		RequiredApprovals: 1,
		ApprovalQuorum:    50.0,
	}

	// Test: Too few reviewers
	approvals := []*storage.Approval{
		{ReviewID: "review-1", ReviewerID: "reviewer-1", Decision: "approved"},
	}

	result, _ := validator.ValidateReviewGovernance(ctx, review, approvals, config)
	if result.Passed {
		t.Error("Expected policy to fail with too few reviewers")
	}

	// Test: Sufficient reviewers and approvals
	approvals = []*storage.Approval{
		{ReviewID: "review-1", ReviewerID: "reviewer-1", Decision: "approved"},
		{ReviewID: "review-1", ReviewerID: "reviewer-2", Decision: "approved"},
	}

	result, _ = validator.ValidateReviewGovernance(ctx, review, approvals, config)
	if !result.Passed {
		t.Error("Expected policy to pass with sufficient approvals")
	}

	// Test: Too many reviewers
	config.MaxReviewers = 1
	result, _ = validator.ValidateReviewGovernance(ctx, review, approvals, config)
	if result.Passed {
		t.Error("Expected policy to fail with too many reviewers")
	}
}

func TestApprovalExpiration(t *testing.T) {
	// Test: Non-expired approval
	approval := &storage.Approval{
		CreatedAt: time.Now().Add(-2 * 24 * time.Hour), // 2 days old
	}

	isExpired := IsApprovalExpired(approval)
	if isExpired {
		t.Error("Expected approval to not be expired")
	}

	// Test: Expired approval
	approval = &storage.Approval{
		CreatedAt: time.Now().Add(-10 * 24 * time.Hour), // 10 days old (> 7 day default)
	}

	isExpired = IsApprovalExpired(approval)
	if !isExpired {
		t.Error("Expected approval to be expired")
	}

	// Test: Custom TTL
	ttl := 1 * time.Hour
	timeUntil := GetTimeUntilExpiration(approval, ttl)

	if timeUntil > 0 {
		t.Error("Expected approval to be expired with 1-hour TTL")
	}
}

func TestWorkflowValidator(t *testing.T) {
	ctx := context.Background()
	policies := []Policy{
		&SingleApprovalPolicy{},
	}
	validator := NewWorkflowValidator(policies)

	draft := &storage.Draft{
		ID:     "draft-1",
		Status: "under_review",
	}

	review := &storage.Review{
		ID:      "review-1",
		DraftID: "draft-1",
		Status:  "under_review",
	}

	// Test: Validation with missing reviews
	result, _ := validator.ValidateDraft(ctx, draft, []*storage.Review{}, []*storage.Approval{}, []*storage.AuditEvent{})

	if result.Passed {
		t.Error("Expected validation to fail with missing reviews")
	}

	// Test: Validation with valid workflow
	approvals := []*storage.Approval{
		{
			ID:       "approval-1",
			ReviewID: "review-1",
			Decision: "approved",
		},
	}

	result, _ = validator.ValidateDraft(ctx, draft, []*storage.Review{review}, approvals, []*storage.AuditEvent{})

	if !result.Passed {
		t.Logf("Violations: %+v", result.Violations)
		t.Error("Expected validation to pass with single approval")
	}
}

func TestWorkflowChainValidation(t *testing.T) {
	ctx := context.Background()
	policies := []Policy{}
	validator := NewWorkflowValidator(policies)

	draft := &storage.Draft{
		ID:     "draft-1",
		Status: "under_review",
	}

	review := &storage.Review{
		ID:      "review-1",
		DraftID: "draft-1",
		Status:  "under_review",
	}

	// Test: Review not associated with draft
	invalidReview := &storage.Review{
		ID:      "review-2",
		DraftID: "draft-2", // Wrong draft
		Status:  "under_review",
	}

	result, _ := validator.ValidateWorkflowChain(ctx, draft, []*storage.Review{invalidReview}, []*storage.Approval{}, []*storage.AuditEvent{})

	if result.Passed {
		t.Error("Expected validation to fail with unassociated review")
	}

	// Test: Valid chain
	result, _ = validator.ValidateWorkflowChain(ctx, draft, []*storage.Review{review}, []*storage.Approval{}, []*storage.AuditEvent{})

	if !result.Passed {
		t.Logf("Chain validation failed with violations: %+v", result.Violations)
	}
}

func TestCorrelationValidation(t *testing.T) {
	ctx := context.Background()
	validator := &CorrelationValidator{}

	// Test: Same correlation ID
	events := []*storage.AuditEvent{
		{CorrelationID: "corr-1", Action: "draft.created"},
		{CorrelationID: "corr-1", Action: "review.created"},
		{CorrelationID: "corr-1", Action: "approval.assigned"},
	}

	err := validator.ValidateCorrelation(ctx, nil, nil, nil, events)
	if err != nil {
		t.Errorf("Expected valid correlation, got error: %v", err)
	}

	// Test: Different correlation IDs
	events = []*storage.AuditEvent{
		{CorrelationID: "corr-1", Action: "draft.created"},
		{CorrelationID: "corr-2", Action: "review.created"},
	}

	err = validator.ValidateCorrelation(ctx, nil, nil, nil, events)
	if err == nil {
		t.Error("Expected error for inconsistent correlation IDs")
	}
}

// Benchmark tests

func BenchmarkPolicyEvaluation(b *testing.B) {
	ctx := context.Background()
	policy := &SingleApprovalPolicy{}

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "pending"},
		{ID: "a3", ReviewID: "review-1", Decision: "rejected"},
	}

	context := PolicyContext{
		Review:    review,
		Approvals: approvals,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		policy.Evaluate(ctx, context)
	}
}

func BenchmarkApprovalAssignmentValidation(b *testing.B) {
	ctx := context.Background()
	validator := NewApprovalAssignmentValidator()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	approval := &storage.Approval{
		ReviewID:   "review-1",
		ReviewerID: "reviewer-1",
	}

	existing := []*storage.Approval{}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		validator.ValidateAssignment(ctx, approval, review, existing)
	}
}

func BenchmarkWorkflowValidation(b *testing.B) {
	ctx := context.Background()
	validator := NewWorkflowValidator([]Policy{
		&SingleApprovalPolicy{},
		&MajorityApprovalPolicy{},
	})

	draft := &storage.Draft{
		ID:     "draft-1",
		Status: "under_review",
	}

	review := &storage.Review{
		ID:      "review-1",
		DraftID: "draft-1",
		Status:  "under_review",
	}

	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		validator.ValidateDraft(ctx, draft, []*storage.Review{review}, approvals, []*storage.AuditEvent{})
	}
}

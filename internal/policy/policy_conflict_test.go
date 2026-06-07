package policy

import (
	"context"
	"testing"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// TestPolicyConflicts validates behavior when multiple policies conflict
func TestPolicyConflictSingleVsMajority(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// Scenario: Single approval says pass (1 approved), but Majority says fail (need 2+)
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "pending"},
	}

	singlePolicy := &SingleApprovalPolicy{}
	majorityPolicy := &MajorityApprovalPolicy{}

	singleResult := singlePolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})
	majorityResult := majorityPolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})

	// Single should pass (≥1 approval)
	if !singleResult.Passed {
		t.Error("Single approval policy should pass with 1 approval")
	}

	// Majority should fail (need >50%, have 50%)
	if majorityResult.Passed {
		t.Error("Majority policy should fail with only 50% approval")
	}

	if singleResult.Score != 100 {
		t.Errorf("Single score should be 100, got %d", singleResult.Score)
	}

	if majorityResult.Score != 50 {
		t.Errorf("Majority score should be 50, got %d", majorityResult.Score)
	}
}

// TestPolicyConflictMajorityVsUnanimous validates Majority vs Unanimous conflict
func TestPolicyConflictMajorityVsUnanimous(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// 3 reviewers: 2 approved, 1 pending
	// Majority: PASS (66% approved)
	// Unanimous: FAIL (not 100%)
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "approved"},
		{ID: "a3", ReviewID: "review-1", Decision: "pending"},
	}

	majorityPolicy := &MajorityApprovalPolicy{}
	unanimousPolicy := &UnanimousApprovalPolicy{}

	majorityResult := majorityPolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})
	unanimousResult := unanimousPolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})

	if !majorityResult.Passed {
		t.Error("Majority policy should pass with 66% approval")
	}

	if unanimousResult.Passed {
		t.Error("Unanimous policy should fail with pending approvals")
	}

	if majorityResult.Score != 100 {
		t.Errorf("Majority score should be 100, got %d", majorityResult.Score)
	}

	if unanimousResult.Score != 66 {
		t.Errorf("Unanimous score should be 66, got %d", unanimousResult.Score)
	}
}

// TestPolicyConflictWithRejection validates behavior when any rejection exists
func TestPolicyConflictWithRejection(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// 2 approved, 1 rejected
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "approved"},
		{ID: "a3", ReviewID: "review-1", Decision: "rejected"},
	}

	singlePolicy := &SingleApprovalPolicy{}
	majorityPolicy := &MajorityApprovalPolicy{}
	unanimousPolicy := &UnanimousApprovalPolicy{}

	singleResult := singlePolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})
	majorityResult := majorityPolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})
	unanimousResult := unanimousPolicy.Evaluate(ctx, PolicyContext{Review: review, Approvals: approvals})

	// Single: PASS (has 2 approvals)
	if !singleResult.Passed {
		t.Error("Single approval should pass with any approval")
	}

	// Majority: PASS (66% approved)
	if !majorityResult.Passed {
		t.Error("Majority approval should pass with 66%")
	}

	// Unanimous: FAIL (has rejection)
	if unanimousResult.Passed {
		t.Error("Unanimous approval should fail with any rejection")
	}

	if unanimousResult.Score != 0 {
		t.Errorf("Unanimous score should be 0 with rejection, got %d", unanimousResult.Score)
	}
}

// TestPolicyConflictGovernanceConfig validates config-based conflict resolution
func TestPolicyConflictGovernanceConfig(t *testing.T) {
	ctx := context.Background()
	validator := NewApprovalAssignmentValidator()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// Scenario: Config requires unanimous, but have majority
	config := ReviewGovernanceConfig{
		MinReviewers:      3,
		MaxReviewers:      5,
		RequiredApprovals: 3, // Need all 3
		ApprovalQuorum:    100.0, // 100% required
	}

	// Reality: 2/3 approved
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "approved"},
		{ID: "a3", ReviewID: "review-1", Decision: "pending"},
	}

	result, _ := validator.ValidateReviewGovernance(ctx, review, approvals, config)

	if result.Passed {
		t.Error("Governance should fail when config requires unanimity but only 66% approved")
	}

	if result.Score != 66 {
		t.Errorf("Score should reflect 66%% progress, got %d", result.Score)
	}

	// Should have violations for both required_approvals and approval_quorum
	if len(result.Violations) < 2 {
		t.Errorf("Expected 2+ violations, got %d", len(result.Violations))
	}
}

// TestPolicyScoreConsistency validates scores are consistent across evaluations
func TestPolicyScoreConsistency(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	policies := []Policy{
		&SingleApprovalPolicy{},
		&MajorityApprovalPolicy{},
		&UnanimousApprovalPolicy{},
	}

	// Same approvals, evaluate multiple times
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "pending"},
	}

	context1 := PolicyContext{Review: review, Approvals: approvals}

	scores := make(map[string][]int)
	for i := 0; i < 5; i++ {
		for _, policy := range policies {
			result := policy.Evaluate(ctx, context1)
			scores[policy.Name()] = append(scores[policy.Name()], result.Score)
		}
	}

	// Verify all scores are consistent across evaluations
	for policyName, scoreList := range scores {
		for i := 1; i < len(scoreList); i++ {
			if scoreList[i] != scoreList[0] {
				t.Errorf("Policy %s had inconsistent scores: %v", policyName, scoreList)
			}
		}
	}
}

// TestPolicyEdgeCaseEmptyReview validates behavior with nil/empty review
func TestPolicyEdgeCaseEmptyReview(t *testing.T) {
	ctx := context.Background()

	policies := []Policy{
		&SingleApprovalPolicy{},
		&MajorityApprovalPolicy{},
		&UnanimousApprovalPolicy{},
	}

	// Evaluate with nil review
	context := PolicyContext{Review: nil, Approvals: []*storage.Approval{}}

	for _, policy := range policies {
		result := policy.Evaluate(ctx, context)
		if result.Passed {
			t.Errorf("Policy %s should fail with nil review", policy.Name())
		}
		if result.Score != 0 {
			t.Errorf("Policy %s should have 0 score with nil review, got %d", policy.Name(), result.Score)
		}
	}
}

// TestPolicyEdgeCaseZeroApprovals validates behavior with no approvals
func TestPolicyEdgeCaseZeroApprovals(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	policies := []Policy{
		&SingleApprovalPolicy{},
		&MajorityApprovalPolicy{},
		&UnanimousApprovalPolicy{},
	}

	context := PolicyContext{Review: review, Approvals: []*storage.Approval{}}

	for _, policy := range policies {
		result := policy.Evaluate(ctx, context)
		if result.Passed {
			t.Errorf("Policy %s should fail with zero approvals", policy.Name())
		}
		if result.Score != 0 {
			t.Errorf("Policy %s should have 0 score with no approvals", policy.Name())
		}
	}
}

// TestPolicyComplexScenario validates complex multi-policy scenario
func TestPolicyComplexScenario(t *testing.T) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	// 5 reviewers: 3 approved, 1 rejected, 1 pending
	approvals := []*storage.Approval{
		{ID: "a1", ReviewID: "review-1", Decision: "approved"},
		{ID: "a2", ReviewID: "review-1", Decision: "approved"},
		{ID: "a3", ReviewID: "review-1", Decision: "approved"},
		{ID: "a4", ReviewID: "review-1", Decision: "rejected"},
		{ID: "a5", ReviewID: "review-1", Decision: "pending"},
	}

	policyCtx := PolicyContext{Review: review, Approvals: approvals}

	// Single: PASS (has 3 approvals)
	single := (&SingleApprovalPolicy{}).Evaluate(ctx, policyCtx)
	if !single.Passed || single.Score != 100 {
		t.Errorf("Single approval failed: passed=%v, score=%d", single.Passed, single.Score)
	}

	// Majority: PASS (60% approved, need >50%)
	majority := (&MajorityApprovalPolicy{}).Evaluate(ctx, policyCtx)
	if !majority.Passed || majority.Score != 100 {
		t.Errorf("Majority approval failed: passed=%v, score=%d", majority.Passed, majority.Score)
	}

	// Unanimous: FAIL (has rejection)
	unanimous := (&UnanimousApprovalPolicy{}).Evaluate(ctx, policyCtx)
	if unanimous.Passed || unanimous.Score != 0 {
		t.Errorf("Unanimous approval should fail: passed=%v, score=%d", unanimous.Passed, unanimous.Score)
	}
}

// BenchmarkPolicyConflictResolution benchmarks policy conflict evaluation
func BenchmarkPolicyConflictResolution(b *testing.B) {
	ctx := context.Background()

	review := &storage.Review{
		ID:     "review-1",
		Status: "under_review",
	}

	approvals := make([]*storage.Approval, 100)
	for i := 0; i < 100; i++ {
		decision := "pending"
		if i%3 == 0 {
			decision = "approved"
		} else if i%5 == 0 {
			decision = "rejected"
		}
		approvals[i] = &storage.Approval{
			ID:       string(rune(i)),
			ReviewID: "review-1",
			Decision: decision,
		}
	}

	policies := []Policy{
		&SingleApprovalPolicy{},
		&MajorityApprovalPolicy{},
		&UnanimousApprovalPolicy{},
	}

	policyCtx := PolicyContext{Review: review, Approvals: approvals}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, policy := range policies {
			policy.Evaluate(ctx, policyCtx)
		}
	}
}

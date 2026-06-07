package execution

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionPlanner generates execution plans from approved workflows
type ExecutionPlanner struct {
	draftStore storage.DraftStore
}

// NewExecutionPlanner creates a new execution planner
func NewExecutionPlanner(draftStore storage.DraftStore) *ExecutionPlanner {
	return &ExecutionPlanner{
		draftStore: draftStore,
	}
}

// GeneratePlan creates an execution plan from a draft
func (p *ExecutionPlanner) GeneratePlan(
	ctx context.Context,
	executionID string,
	draft *storage.Draft,
	approval *storage.Approval,
	correlationID string,
) (*ExecutionPlan, error) {
	// Generate plan ID
	planID := generatePlanID(executionID)

	// Parse draft configuration to extract steps
	steps := p.extractSteps(draft)

	// Calculate metrics
	totalDuration := p.estimateDuration(steps)
	riskScore := p.calculateRisk(steps)
	affectedResources := p.extractResources(steps)

	plan := &ExecutionPlan{
		ID:                planID,
		ExecutionID:       executionID,
		ApprovalID:        approval.ID,
		DraftID:           draft.ID,
		CorrelationID:     correlationID,
		Status:            "draft",
		Steps:             steps,
		TotalSteps:        len(steps),
		EstimatedDuration: totalDuration,
		RiskScore:         riskScore,
		AffectedResources: affectedResources,
		RollbackAvailable: p.checkRollbackCapability(steps),
		CreatedAt:         time.Now(),
	}

	return plan, nil
}

// extractSteps parses draft configuration to extract execution steps
func (p *ExecutionPlanner) extractSteps(draft *storage.Draft) []*ExecutionStep {
	steps := make([]*ExecutionStep, 0)

	// Parse draft configuration (simplified - would be more complex in production)
	// For now, create example steps based on draft content

	// Step 1: Deployment preparation
	steps = append(steps, &ExecutionStep{
		ID:                fmt.Sprintf("step-1-%s", draft.ID),
		Sequence:          1,
		Name:              "Prepare Deployment",
		Description:       "Prepare deployment environment and validate prerequisites",
		Action:            "prepare",
		Payload:           make(map[string]string),
		RollbackAvailable: true,
		EstimatedTime:     30 * time.Second,
		RiskLevel:         "low",
	})

	// Step 2: Configuration deployment
	steps = append(steps, &ExecutionStep{
		ID:                fmt.Sprintf("step-2-%s", draft.ID),
		Sequence:          2,
		Name:              "Deploy Configuration",
		Description:       "Deploy configuration to target systems",
		Action:            "deploy",
		Payload:           make(map[string]string),
		RollbackAvailable: true,
		EstimatedTime:     1 * time.Minute,
		RiskLevel:         "medium",
	})

	// Step 3: Validation
	steps = append(steps, &ExecutionStep{
		ID:                fmt.Sprintf("step-3-%s", draft.ID),
		Sequence:          3,
		Name:              "Validate Deployment",
		Description:       "Validate configuration deployment succeeded",
		Action:            "validate",
		Payload:           make(map[string]string),
		RollbackAvailable: false,
		EstimatedTime:     15 * time.Second,
		RiskLevel:         "low",
	})

	return steps
}

// estimateDuration calculates total estimated execution duration
func (p *ExecutionPlanner) estimateDuration(steps []*ExecutionStep) time.Duration {
	total := time.Duration(0)
	for _, step := range steps {
		total += step.EstimatedTime
	}
	// Add buffer for overhead
	total += 10 * time.Second
	return total
}

// calculateRisk calculates overall risk score (0-100)
func (p *ExecutionPlanner) calculateRisk(steps []*ExecutionStep) int {
	if len(steps) == 0 {
		return 0
	}

	totalRisk := 0
	for _, step := range steps {
		switch step.RiskLevel {
		case "low":
			totalRisk += 10
		case "medium":
			totalRisk += 30
		case "high":
			totalRisk += 50
		}
	}

	// Average risk across steps, cap at 100
	avgRisk := totalRisk / len(steps)
	if avgRisk > 100 {
		avgRisk = 100
	}

	return avgRisk
}

// extractResources identifies affected resources
func (p *ExecutionPlanner) extractResources(steps []*ExecutionStep) []string {
	resources := make([]string, 0)

	// Extract from steps (simplified)
	for _, step := range steps {
		if step.Action == "deploy" {
			resources = append(resources, "configuration")
		}
	}

	// Remove duplicates
	seen := make(map[string]bool)
	unique := make([]string, 0)
	for _, r := range resources {
		if !seen[r] {
			seen[r] = true
			unique = append(unique, r)
		}
	}

	return unique
}

// checkRollbackCapability checks if all steps have rollback capability
func (p *ExecutionPlanner) checkRollbackCapability(steps []*ExecutionStep) bool {
	for _, step := range steps {
		if !step.RollbackAvailable {
			return false
		}
	}
	return true
}

// ValidatePlan validates an execution plan
func (p *ExecutionPlanner) ValidatePlan(plan *ExecutionPlan) error {
	if plan.ID == "" {
		return fmt.Errorf("plan ID cannot be empty")
	}

	if plan.ExecutionID == "" {
		return fmt.Errorf("execution ID cannot be empty")
	}

	if plan.TotalSteps == 0 {
		return fmt.Errorf("plan must have at least one step")
	}

	if len(plan.Steps) != plan.TotalSteps {
		return fmt.Errorf("step count mismatch: expected %d, got %d", plan.TotalSteps, len(plan.Steps))
	}

	// Validate step sequence
	for i, step := range plan.Steps {
		if step.Sequence != i+1 {
			return fmt.Errorf("invalid step sequence at index %d: expected %d, got %d", i, i+1, step.Sequence)
		}
	}

	return nil
}

// generatePlanID creates a unique plan ID
func generatePlanID(executionID string) string {
	hash := md5.Sum([]byte(executionID + time.Now().String()))
	return fmt.Sprintf("plan-%s", hex.EncodeToString(hash[:]))
}

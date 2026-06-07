package execution

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// SimulatedExecutionEngine simulates step execution without real actions
type SimulatedExecutionEngine struct {
	stateMachine *ExecutionStateMachine
	stepMachine  *StepStateMachine
}

// NewSimulatedExecutionEngine creates a new simulated execution engine
func NewSimulatedExecutionEngine() *SimulatedExecutionEngine {
	return &SimulatedExecutionEngine{
		stateMachine: NewExecutionStateMachine(),
		stepMachine:  NewStepStateMachine(),
	}
}

// ExecuteStep simulates execution of a single step
func (see *SimulatedExecutionEngine) ExecuteStep(ctx context.Context, step *storage.ExecutionStep) (*storage.StepResult, error) {
	// Simulate execution delay based on estimated time
	estimatedDuration := time.Duration(step.EstimatedTime) * time.Second

	// Add some randomness (±20%)
	variation := time.Duration(rand.Int63n(int64(estimatedDuration / 5)))
	if rand.Float64() > 0.5 {
		estimatedDuration = estimatedDuration + variation
	} else {
		estimatedDuration = estimatedDuration - variation
	}

	// Sleep for simulated execution
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(estimatedDuration):
	}

	// Determine outcome based on step configuration
	outcome := see.determineOutcome(step)

	startedAt := time.Now().Add(-estimatedDuration)
	completedAt := time.Now()

	result := &storage.StepResult{
		ID:             fmt.Sprintf("result-%d", time.Now().Unix()),
		StepID:         step.ID,
		CorrelationID:  "", // Will be set by caller
		Status:         outcome.Status,
		Duration:       int(estimatedDuration.Seconds()),
		Output:         outcome.Output,
		Error:          outcome.Error,
		StartedAt:      startedAt,
		CompletedAt:    &completedAt,
		Version:        1,
	}

	return result, nil
}

// ExecutionOutcome represents the simulated result of an execution
type ExecutionOutcome struct {
	Status string
	Output *string
	Error  *string
}

// determineOutcome simulates step execution outcome
func (see *SimulatedExecutionEngine) determineOutcome(step *storage.ExecutionStep) ExecutionOutcome {
	// Risk level influences failure probability
	failureProbability := 0.05 // 5% base failure rate

	switch step.RiskLevel {
	case "low":
		failureProbability = 0.02 // 2%
	case "medium":
		failureProbability = 0.05 // 5%
	case "high":
		failureProbability = 0.10 // 10%
	}

	if rand.Float64() < failureProbability {
		errorMsg := fmt.Sprintf("Simulated error in step %s: %s", step.ID, step.Name)
		return ExecutionOutcome{
			Status: "failed",
			Error:  &errorMsg,
		}
	}

	// Success outcome with simulated output
	output := fmt.Sprintf("{\"step\": \"%s\", \"action\": \"%s\", \"result\": \"simulated success\"}", step.Name, step.Action)
	return ExecutionOutcome{
		Status: "completed",
		Output: &output,
	}
}

// ExecutionContext represents the full state during execution
type ExecutionContext struct {
	ExecutionID    string
	CorrelationID  string
	CurrentState   ExecutionState
	Steps          []*storage.ExecutionStep
	CompletedSteps int
	FailedSteps    int
	StartedAt      time.Time
	Duration       time.Duration
}

// ExecutionRunner manages execution of a plan
type ExecutionRunner struct {
	engine        *SimulatedExecutionEngine
	stateMachine  *ExecutionStateMachine
	stepMachine   *StepStateMachine
}

// NewExecutionRunner creates a new execution runner
func NewExecutionRunner() *ExecutionRunner {
	return &ExecutionRunner{
		engine:       NewSimulatedExecutionEngine(),
		stateMachine: NewExecutionStateMachine(),
		stepMachine:  NewStepStateMachine(),
	}
}

// RunExecution simulates execution of an entire plan
func (er *ExecutionRunner) RunExecution(ctx context.Context, executionID string, correlationID string, steps []*storage.ExecutionStep) ([]*storage.StepResult, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("no steps to execute")
	}

	results := make([]*storage.StepResult, 0, len(steps))

	for _, step := range steps {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Execute step
		result, err := er.engine.ExecuteStep(ctx, step)
		if err != nil {
			return results, err
		}

		result.ExecutionID = executionID
		result.CorrelationID = correlationID

		results = append(results, result)

		// Check for cancellation between steps
		if result.Status == "failed" {
			// Continue executing remaining steps even if one fails (dry-run mode)
		}
	}

	return results, nil
}

// CalculateExecutionDuration sums all step results
func CalculateExecutionDuration(results []*storage.StepResult) time.Duration {
	totalSeconds := 0
	for _, result := range results {
		totalSeconds += result.Duration
	}

	return time.Duration(totalSeconds) * time.Second
}

// CalculateExecutionStatus determines overall status from step results
func CalculateExecutionStatus(results []*storage.StepResult) string {
	if len(results) == 0 {
		return "completed"
	}

	hasFailures := false
	for _, result := range results {
		if result.Status == "failed" {
			hasFailures = true
			break
		}
	}

	if hasFailures {
		return "failed"
	}

	return "completed"
}

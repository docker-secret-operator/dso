package execution

import (
	"fmt"
)

// ExecutionState represents the lifecycle state of an execution
type ExecutionState string

const (
	// Pending - initial state, awaiting validation
	ExecutionStatePending ExecutionState = "pending"
	// Validated - passed validation, awaiting plan
	ExecutionStateValidated ExecutionState = "validated"
	// Planned - plan generated, awaiting queue
	ExecutionStatePlanned ExecutionState = "planned"
	// Queued - in execution queue, awaiting worker
	ExecutionStateQueued ExecutionState = "queued"
	// Running - currently executing
	ExecutionStateRunning ExecutionState = "running"
	// Completed - execution finished successfully
	ExecutionStateCompleted ExecutionState = "completed"
	// Failed - execution failed
	ExecutionStateFailed ExecutionState = "failed"
	// Cancelled - execution was cancelled
	ExecutionStateCancelled ExecutionState = "cancelled"
	// Rejected - rejected during validation
	ExecutionStateRejected ExecutionState = "rejected"
	// Expired - TTL expired
	ExecutionStateExpired ExecutionState = "expired"
)

// StepState represents the lifecycle state of a step
type StepState string

const (
	StepStatePending   StepState = "pending"
	StepStateStarted   StepState = "started"
	StepStateCompleted StepState = "completed"
	StepStateFailed    StepState = "failed"
	StepStateCancelled StepState = "cancelled"
)

// ExecutionStateMachine validates state transitions
type ExecutionStateMachine struct {
	allowedTransitions map[ExecutionState]map[ExecutionState]bool
}

// NewExecutionStateMachine creates a new state machine
func NewExecutionStateMachine() *ExecutionStateMachine {
	esm := &ExecutionStateMachine{
		allowedTransitions: make(map[ExecutionState]map[ExecutionState]bool),
	}

	// Initialize allowed transitions
	esm.allowedTransitions[ExecutionStatePending] = map[ExecutionState]bool{
		ExecutionStateValidated: true,
		ExecutionStateRejected:  true,
		ExecutionStateExpired:   true,
	}

	esm.allowedTransitions[ExecutionStateValidated] = map[ExecutionState]bool{
		ExecutionStatePlanned:   true,
		ExecutionStateRejected:  true,
		ExecutionStateExpired:   true,
		ExecutionStateCancelled: true,
	}

	esm.allowedTransitions[ExecutionStatePlanned] = map[ExecutionState]bool{
		ExecutionStateQueued:    true,
		ExecutionStateRejected:  true,
		ExecutionStateExpired:   true,
		ExecutionStateCancelled: true,
	}

	esm.allowedTransitions[ExecutionStateQueued] = map[ExecutionState]bool{
		ExecutionStateRunning:   true,
		ExecutionStateExpired:   true,
		ExecutionStateCancelled: true,
	}

	esm.allowedTransitions[ExecutionStateRunning] = map[ExecutionState]bool{
		ExecutionStateCompleted: true,
		ExecutionStateFailed:    true,
		ExecutionStateCancelled: true,
	}

	// Terminal states - no transitions out
	esm.allowedTransitions[ExecutionStateCompleted] = map[ExecutionState]bool{}
	esm.allowedTransitions[ExecutionStateFailed] = map[ExecutionState]bool{}
	esm.allowedTransitions[ExecutionStateCancelled] = map[ExecutionState]bool{}
	esm.allowedTransitions[ExecutionStateRejected] = map[ExecutionState]bool{}
	esm.allowedTransitions[ExecutionStateExpired] = map[ExecutionState]bool{}

	return esm
}

// CanTransition checks if a transition is allowed
func (esm *ExecutionStateMachine) CanTransition(from ExecutionState, to ExecutionState) bool {
	fromTransitions, exists := esm.allowedTransitions[from]
	if !exists {
		return false
	}

	allowed, exists := fromTransitions[to]
	return allowed
}

// ValidateTransition returns error if transition is not allowed
func (esm *ExecutionStateMachine) ValidateTransition(from ExecutionState, to ExecutionState) error {
	if !esm.CanTransition(from, to) {
		return fmt.Errorf("illegal state transition: %s → %s", from, to)
	}

	return nil
}

// StepStateMachine validates step state transitions
type StepStateMachine struct {
	allowedTransitions map[StepState]map[StepState]bool
}

// NewStepStateMachine creates a new step state machine
func NewStepStateMachine() *StepStateMachine {
	ssm := &StepStateMachine{
		allowedTransitions: make(map[StepState]map[StepState]bool),
	}

	// Initialize allowed transitions
	ssm.allowedTransitions[StepStatePending] = map[StepState]bool{
		StepStateStarted:   true,
		StepStateCancelled: true,
	}

	ssm.allowedTransitions[StepStateStarted] = map[StepState]bool{
		StepStateCompleted: true,
		StepStateFailed:    true,
		StepStateCancelled: true,
	}

	// Terminal states - no transitions out
	ssm.allowedTransitions[StepStateCompleted] = map[StepState]bool{}
	ssm.allowedTransitions[StepStateFailed] = map[StepState]bool{}
	ssm.allowedTransitions[StepStateCancelled] = map[StepState]bool{}

	return ssm
}

// CanTransition checks if a step transition is allowed
func (ssm *StepStateMachine) CanTransition(from StepState, to StepState) bool {
	fromTransitions, exists := ssm.allowedTransitions[from]
	if !exists {
		return false
	}

	allowed, exists := fromTransitions[to]
	return allowed
}

// ValidateTransition returns error if transition is not allowed
func (ssm *StepStateMachine) ValidateTransition(from StepState, to StepState) error {
	if !ssm.CanTransition(from, to) {
		return fmt.Errorf("illegal step state transition: %s → %s", from, to)
	}

	return nil
}

// IsTerminalState checks if a state is terminal (no further transitions)
func IsTerminalExecutionState(state ExecutionState) bool {
	switch state {
	case ExecutionStateCompleted, ExecutionStateFailed, ExecutionStateCancelled, ExecutionStateRejected, ExecutionStateExpired:
		return true
	default:
		return false
	}
}

// IsTerminalStepState checks if a step state is terminal
func IsTerminalStepState(state StepState) bool {
	switch state {
	case StepStateCompleted, StepStateFailed, StepStateCancelled:
		return true
	default:
		return false
	}
}

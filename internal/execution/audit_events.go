package execution

import (
	"fmt"
	"time"
)

// OrchestrationAuditEvent represents an orchestration-specific audit event
type OrchestrationAuditEvent struct {
	ID            string
	CorrelationID string
	ExecutionID   string
	Action        string // execution.queued, execution.started, etc.
	Status        string // success, failure
	ActorID       string
	ActorName     string
	Details       string
	ResourceID    string
	ResourceType  string
	Timestamp     time.Time
}

// EventPersister defines the interface for persisting audit events
type EventPersister interface {
	LogExecutionEvent(event OrchestrationAuditEvent) error
}

// ExecutionAuditEvents provides audit event generation for orchestration
type ExecutionAuditEvents struct {
	events    []OrchestrationAuditEvent
	persister EventPersister
}

// NewExecutionAuditEvents creates a new audit event helper
func NewExecutionAuditEvents(persister EventPersister) *ExecutionAuditEvents {
	return &ExecutionAuditEvents{
		events:    make([]OrchestrationAuditEvent, 0),
		persister: persister,
	}
}

// LogExecutionQueued logs when an execution is queued
func (eae *ExecutionAuditEvents) LogExecutionQueued(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.queued",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionCancelled logs when an execution is cancelled
func (eae *ExecutionAuditEvents) LogExecutionCancelled(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.cancelled",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionPaused logs when an execution is paused
func (eae *ExecutionAuditEvents) LogExecutionPaused(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.paused",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionResumed logs when an execution is resumed
func (eae *ExecutionAuditEvents) LogExecutionResumed(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.resumed",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionTimedOut logs when an execution times out
func (eae *ExecutionAuditEvents) LogExecutionTimedOut(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.timed_out",
		Status:        "failure",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionRecovered logs when an execution is recovered
func (eae *ExecutionAuditEvents) LogExecutionRecovered(executionID string, correlationID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.recovered",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionStarted logs when an execution starts
func (eae *ExecutionAuditEvents) LogExecutionStarted(executionID string, correlationID string, workerID string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.started",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Details:       fmt.Sprintf("started by worker %s", workerID),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogStepStarted logs when a step starts
func (eae *ExecutionAuditEvents) LogStepStarted(executionID string, stepID string, correlationID string, workerID string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.step_started",
		Status:        "success",
		ResourceID:    stepID,
		ResourceType:  "step",
		Details:       fmt.Sprintf("started by worker %s", workerID),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogStepCompleted logs when a step completes
func (eae *ExecutionAuditEvents) LogStepCompleted(executionID string, stepID string, correlationID string, workerID string, duration time.Duration) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.step_completed",
		Status:        "success",
		ResourceID:    stepID,
		ResourceType:  "step",
		Details:       fmt.Sprintf("completed by worker %s in %v", workerID, duration),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionCompleted logs when an execution completes
func (eae *ExecutionAuditEvents) LogExecutionCompleted(executionID string, correlationID string, workerID string, duration time.Duration, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.completed",
		Status:        "success",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Details:       fmt.Sprintf("completed by worker %s in %v", workerID, duration),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogExecutionFailed logs when an execution fails
func (eae *ExecutionAuditEvents) LogExecutionFailed(executionID string, correlationID string, workerID string, errorMsg string, actorID string, actorName string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		CorrelationID: correlationID,
		ExecutionID:   executionID,
		Action:        "execution.failed",
		Status:        "failure",
		ActorID:       actorID,
		ActorName:     actorName,
		ResourceID:    executionID,
		ResourceType:  "execution",
		Details:       fmt.Sprintf("failed by worker %s: %s", workerID, errorMsg),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogWorkerRegistered logs when a worker registers
func (eae *ExecutionAuditEvents) LogWorkerRegistered(workerID string, capabilities []string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Action:        "worker.registered",
		Status:        "success",
		ResourceID:    workerID,
		ResourceType:  "worker",
		Details:       fmt.Sprintf("registered with %d capabilities", len(capabilities)),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogWorkerUnhealthy logs when a worker becomes unhealthy
func (eae *ExecutionAuditEvents) LogWorkerUnhealthy(workerID string, reason string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Action:        "worker.unhealthy",
		Status:        "failure",
		ResourceID:    workerID,
		ResourceType:  "worker",
		Details:       fmt.Sprintf("became unhealthy: %s", reason),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// LogWorkerStopped logs when a worker stops
func (eae *ExecutionAuditEvents) LogWorkerStopped(workerID string, reason string) {
	event := OrchestrationAuditEvent{
		ID:            fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Action:        "worker.stopped",
		Status:        "success",
		ResourceID:    workerID,
		ResourceType:  "worker",
		Details:       fmt.Sprintf("stopped: %s", reason),
		Timestamp:     time.Now(),
	}
	eae.events = append(eae.events, event)
	if eae.persister != nil {
		eae.persister.LogExecutionEvent(event)
	}
}

// ListEvents returns all logged events
func (eae *ExecutionAuditEvents) ListEvents() []OrchestrationAuditEvent {
	return eae.events
}

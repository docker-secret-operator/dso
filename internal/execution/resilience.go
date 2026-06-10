package execution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
)

// ExecutionCancellation handles execution cancellation
type ExecutionCancellation struct {
	ExecutionID   string
	CorrelationID string
	RequestedAt   time.Time
	RequestedBy   string
	Reason        string
}

// ExecutionPause handles execution pause/resume
type ExecutionPause struct {
	ExecutionID   string
	CorrelationID string
	PausedAt      time.Time
	ResumedAt     *time.Time
	Reason        string
	IsPaused      bool
}

// ExecutionTimeout represents an execution timeout
type ExecutionTimeout struct {
	ExecutionID   string
	CorrelationID string
	TimeoutType   string // step_timeout, execution_timeout, worker_timeout
	DurationSecs  int
	OccurredAt    time.Time
}

// DeadLetterQueueItem represents a failed execution in DLQ
type DeadLetterQueueItem struct {
	ID            string
	ExecutionID   string
	CorrelationID string
	Reason        string
	ErrorMessage  string
	RetryCount    int
	MaxRetries    int
	EnqueuedAt    time.Time
}

// ResilienceManager handles execution resilience and recovery
type ResilienceManager struct {
	cancellations   map[string]*ExecutionCancellation
	pauses          map[string]*ExecutionPause
	timeouts        map[string]*ExecutionTimeout
	deadLetterQueue map[string]*DeadLetterQueueItem
	recoveryState   map[string]interface{}
	dlqCounter      int64
	mutex           sync.RWMutex
	auditEvents     *ExecutionAuditEvents
	workerManager   *WorkerManager
	executionQueue  *ExecutionQueue
}

// NewResilienceManager creates a new resilience manager
func NewResilienceManager(
	auditEvents *ExecutionAuditEvents,
	workerManager *WorkerManager,
	executionQueue *ExecutionQueue,
) *ResilienceManager {
	return &ResilienceManager{
		cancellations:   make(map[string]*ExecutionCancellation),
		pauses:          make(map[string]*ExecutionPause),
		timeouts:        make(map[string]*ExecutionTimeout),
		deadLetterQueue: make(map[string]*DeadLetterQueueItem),
		recoveryState:   make(map[string]interface{}),
		auditEvents:     auditEvents,
		workerManager:   workerManager,
		executionQueue:  executionQueue,
	}
}

// CancelExecution cancels a queued or running execution
func (rm *ResilienceManager) CancelExecution(ctx context.Context, executionID string, correlationID string, reason string) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	cancellation := &ExecutionCancellation{
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		RequestedAt:   time.Now(),
		Reason:        reason,
	}

	rm.cancellations[executionID] = cancellation

	// Log audit event
	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		rm.auditEvents.LogExecutionCancelled(executionID, correlationID, actorID, actorName)
	}

	// Try to remove from queue
	if rm.executionQueue != nil {
		rm.executionQueue.Remove(ctx, executionID)
	}

	return nil
}

// IsCancelled checks if execution was cancelled
func (rm *ResilienceManager) IsCancelled(executionID string) bool {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	_, exists := rm.cancellations[executionID]
	return exists
}

// PauseExecution pauses a running execution
func (rm *ResilienceManager) PauseExecution(ctx context.Context, executionID string, correlationID string, reason string) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	pause := &ExecutionPause{
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		PausedAt:      time.Now(),
		Reason:        reason,
		IsPaused:      true,
	}

	rm.pauses[executionID] = pause

	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		rm.auditEvents.LogExecutionPaused(executionID, correlationID, actorID, actorName)
	}

	return nil
}

// ResumeExecution resumes a paused execution
func (rm *ResilienceManager) ResumeExecution(ctx context.Context, executionID string) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	pause, exists := rm.pauses[executionID]
	if !exists {
		return fmt.Errorf("execution not paused: %s", executionID)
	}

	now := time.Now()
	pause.ResumedAt = &now
	pause.IsPaused = false

	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		rm.auditEvents.LogExecutionResumed(executionID, pause.CorrelationID, actorID, actorName)
	}

	return nil
}

// IsPaused checks if execution is paused
func (rm *ResilienceManager) IsPaused(executionID string) bool {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	pause, exists := rm.pauses[executionID]
	return exists && pause.IsPaused
}

// RecordTimeout records an execution timeout
func (rm *ResilienceManager) RecordTimeout(ctx context.Context, executionID string, correlationID string, timeoutType string, durationSecs int) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	timeout := &ExecutionTimeout{
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		TimeoutType:   timeoutType,
		DurationSecs:  durationSecs,
		OccurredAt:    time.Now(),
	}

	rm.timeouts[executionID] = timeout

	// Add to dead letter queue
	rm.dlqCounter++
	dlqItem := &DeadLetterQueueItem{
		ID:            fmt.Sprintf("dlq-%d-%d", time.Now().Unix(), rm.dlqCounter),
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		Reason:        fmt.Sprintf("%s exceeded %d seconds", timeoutType, durationSecs),
		RetryCount:    0,
		MaxRetries:    3,
		EnqueuedAt:    time.Now(),
	}

	rm.deadLetterQueue[dlqItem.ID] = dlqItem

	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		rm.auditEvents.LogExecutionTimedOut(executionID, correlationID, actorID, actorName)
	}

	return nil
}

// AddToDeadLetterQueue adds a failed execution to DLQ
func (rm *ResilienceManager) AddToDeadLetterQueue(ctx context.Context, executionID string, correlationID string, reason string, errorMsg string) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	rm.dlqCounter++
	dlqItem := &DeadLetterQueueItem{
		ID:            fmt.Sprintf("dlq-%d-%d", time.Now().Unix(), rm.dlqCounter),
		ExecutionID:   executionID,
		CorrelationID: correlationID,
		Reason:        reason,
		ErrorMessage:  errorMsg,
		RetryCount:    0,
		MaxRetries:    3,
		EnqueuedAt:    time.Now(),
	}

	rm.deadLetterQueue[dlqItem.ID] = dlqItem

	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		rm.auditEvents.LogExecutionFailed(executionID, correlationID, actorID, fmt.Sprintf("Dead letter: %s", reason), actorName, actorName)
	}

	return nil
}

// GetDeadLetterQueue returns all DLQ items
func (rm *ResilienceManager) GetDeadLetterQueue() []*DeadLetterQueueItem {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	items := make([]*DeadLetterQueueItem, 0, len(rm.deadLetterQueue))
	for _, item := range rm.deadLetterQueue {
		items = append(items, item)
	}

	return items
}

// GetDLQCount returns count of items in dead letter queue
func (rm *ResilienceManager) GetDLQCount() int {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	return len(rm.deadLetterQueue)
}

// RetryDLQItem retries a specific DLQ item
func (rm *ResilienceManager) RetryDLQItem(ctx context.Context, itemID string) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	item, exists := rm.deadLetterQueue[itemID]
	if !exists {
		return fmt.Errorf("DLQ item not found: %s", itemID)
	}

	if item.RetryCount >= item.MaxRetries {
		return fmt.Errorf("DLQ item exceeded max retries: %s", itemID)
	}

	item.RetryCount++

	if rm.executionQueue != nil {
		queueItem := &ExecutionQueueItem{
			ExecutionID:   item.ExecutionID,
			CorrelationID: item.CorrelationID,
			Priority:      50,
			RetryCount:    item.RetryCount,
			MaxRetries:    item.MaxRetries,
			ExpiresAt:     time.Now().Add(5 * time.Minute),
			CreatedAt:     time.Now(),
			EnqueuedAt:    time.Now(),
		}
		rm.executionQueue.Requeue(ctx, queueItem)
	}

	// Remove from DLQ if requeued successfully
	delete(rm.deadLetterQueue, itemID)

	if rm.auditEvents != nil {
		actorID := "system"
		actorName := "system"
		if user := auth.CurrentUser(ctx); user != nil {
			actorID = user.ID
			actorName = user.Username
		}
		// Assuming we reuse LogExecutionResumed or a new LogExecutionRecovered
		rm.auditEvents.LogExecutionRecovered(item.ExecutionID, item.CorrelationID, actorID, actorName)
	}

	return nil
}

// SaveRecoveryState saves state for recovery on restart
func (rm *ResilienceManager) SaveRecoveryState(key string, value interface{}) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	rm.recoveryState[key] = value
}

// GetRecoveryState retrieves saved recovery state
func (rm *ResilienceManager) GetRecoveryState(key string) (interface{}, bool) {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	value, exists := rm.recoveryState[key]
	return value, exists
}

// ClearRecoveryState clears recovery state after successful recovery
func (rm *ResilienceManager) ClearRecoveryState(key string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	delete(rm.recoveryState, key)
}

// RecoverFromWorkerFailure handles worker failure recovery
func (rm *ResilienceManager) RecoverFromWorkerFailure(ctx context.Context, workerID string, activeExecutions []*DispatchedExecution) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Mark worker as unhealthy
	if rm.workerManager != nil {
		rm.workerManager.SetWorkerState(ctx, workerID, WorkerStateUnhealthy)

		if rm.auditEvents != nil {
			rm.auditEvents.LogWorkerUnhealthy(workerID, "Worker failure detected")
		}
	}

	actorID := "system"
	actorName := "system"
	if user := auth.CurrentUser(ctx); user != nil {
		actorID = user.ID
		actorName = user.Username
	}

	// Requeue active executions
	if rm.executionQueue != nil {
		for _, exec := range activeExecutions {
			item := &ExecutionQueueItem{
				ExecutionID:   exec.ExecutionID,
				CorrelationID: exec.CorrelationID,
				Priority:      100, // High priority for recovery
				RetryCount:    0,
				MaxRetries:    3,
				ExpiresAt:     time.Now().Add(5 * time.Minute),
				CreatedAt:     exec.StartedAt,
				EnqueuedAt:    time.Now(),
			}

			rm.executionQueue.Requeue(ctx, item)

			if rm.auditEvents != nil {
				rm.auditEvents.LogExecutionQueued(exec.ExecutionID, exec.CorrelationID, actorID, actorName)
				rm.auditEvents.LogExecutionRecovered(exec.ExecutionID, exec.CorrelationID, actorID, actorName)
			}
		}
	}

	return nil
}

// RecoverQueueState recovers queue state on restart
func (rm *ResilienceManager) RecoverQueueState(ctx context.Context) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	// Restore queued executions from recovery state
	if queuedExecs, exists := rm.recoveryState["queued_executions"]; exists {
		if execs, ok := queuedExecs.([]*ExecutionQueueItem); ok {
			for _, exec := range execs {
				if rm.executionQueue != nil {
					rm.executionQueue.Enqueue(ctx, exec.ExecutionID, exec.CorrelationID, exec.Priority, 5*time.Minute)
				}
			}
		}
	}

	// Restore running executions from recovery state
	if runningExecs, exists := rm.recoveryState["running_executions"]; exists {
		if execs, ok := runningExecs.([]*DispatchedExecution); ok {
			for _, exec := range execs {
				// Requeue with high priority
				item := &ExecutionQueueItem{
					ExecutionID:   exec.ExecutionID,
					CorrelationID: exec.CorrelationID,
					Priority:      100,
					RetryCount:    0,
					MaxRetries:    3,
					ExpiresAt:     time.Now().Add(5 * time.Minute),
					CreatedAt:     exec.StartedAt,
					EnqueuedAt:    time.Now(),
				}

				rm.executionQueue.Requeue(ctx, item)

				if rm.auditEvents != nil {
					actorID := "system"
					actorName := "system"
					if user := auth.CurrentUser(ctx); user != nil {
						actorID = user.ID
						actorName = user.Username
					}
					rm.auditEvents.LogExecutionQueued(exec.ExecutionID, exec.CorrelationID, actorID, actorName)
				}
			}
		}
	}

	return nil
}

// ResilienceMetrics represents resilience statistics
type ResilienceMetrics struct {
	CancelledCount  int
	PausedCount     int
	TimeoutCount    int
	DeadLetterCount int
	RecoveredCount  int
	WorkerFailures  int
}

// GetMetrics returns current resilience metrics
func (rm *ResilienceManager) GetMetrics() ResilienceMetrics {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	pausedCount := 0
	for _, pause := range rm.pauses {
		if pause.IsPaused {
			pausedCount++
		}
	}

	return ResilienceMetrics{
		CancelledCount:  len(rm.cancellations),
		PausedCount:     pausedCount,
		TimeoutCount:    len(rm.timeouts),
		DeadLetterCount: len(rm.deadLetterQueue),
	}
}

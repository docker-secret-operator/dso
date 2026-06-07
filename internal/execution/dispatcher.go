package execution

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Dispatcher manages worker assignment and execution flow
type Dispatcher struct {
	workerManager       *WorkerManager
	executionQueue      *ExecutionQueue
	executionRunner     *ExecutionRunner
	stateMachine        *ExecutionStateMachine
	activeExecutions    map[string]*DispatchedExecution
	mutex               sync.RWMutex
	stopChan            chan struct{}
	dispatchInterval    time.Duration
	maxConcurrentWorkers int
}

// DispatchedExecution tracks an execution being processed
type DispatchedExecution struct {
	ExecutionID    string
	CorrelationID  string
	WorkerID       string
	Status         ExecutionState
	StartedAt      time.Time
	CompletedAt    *time.Time
	Duration       time.Duration
	StepResults    []*StepResult
	Error          *string
}

// StepResult represents a step execution result (internal)
type StepResult struct {
	StepID      string
	Status      string
	Duration    time.Duration
	Output      string
	Error       *string
}

// NewDispatcher creates a new execution dispatcher
func NewDispatcher(
	workerManager *WorkerManager,
	executionQueue *ExecutionQueue,
	maxConcurrentWorkers int,
) *Dispatcher {
	return &Dispatcher{
		workerManager:        workerManager,
		executionQueue:       executionQueue,
		executionRunner:      NewExecutionRunner(),
		stateMachine:         NewExecutionStateMachine(),
		activeExecutions:     make(map[string]*DispatchedExecution),
		stopChan:             make(chan struct{}),
		dispatchInterval:     100 * time.Millisecond,
		maxConcurrentWorkers: maxConcurrentWorkers,
	}
}

// Start begins the dispatcher loop
func (d *Dispatcher) Start(ctx context.Context) error {
	go func() {
		ticker := time.NewTicker(d.dispatchInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.stopChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.dispatchNext(ctx)
			case <-d.executionQueue.NotifyChannel():
				d.dispatchNext(ctx)
			}
		}
	}()

	return nil
}

// Stop stops the dispatcher
func (d *Dispatcher) Stop() error {
	close(d.stopChan)
	return nil
}

// dispatchNext dequeues and dispatches the next execution
func (d *Dispatcher) dispatchNext(ctx context.Context) {
	d.mutex.RLock()
	activeCount := len(d.activeExecutions)
	d.mutex.RUnlock()

	if activeCount >= d.maxConcurrentWorkers {
		return
	}

	// Get next item from queue
	item, err := d.executionQueue.Dequeue(ctx)
	if err != nil {
		return // Queue empty
	}

	// Select healthy worker
	workers, err := d.workerManager.GetHealthyWorkers(ctx)
	if err != nil || len(workers) == 0 {
		// No healthy workers, requeue
		d.executionQueue.Requeue(ctx, item)
		return
	}

	// Find worker with capacity
	var selectedWorker *Worker
	for _, worker := range workers {
		if worker.GetCapacity() > 0 {
			selectedWorker = worker
			break
		}
	}

	if selectedWorker == nil {
		// All workers at capacity, requeue
		d.executionQueue.Requeue(ctx, item)
		return
	}

	// Dispatch to worker
	d.dispatchToWorker(ctx, item, selectedWorker)
}

// dispatchToWorker assigns execution to a specific worker
func (d *Dispatcher) dispatchToWorker(ctx context.Context, item *ExecutionQueueItem, worker *Worker) {
	worker.IncrementRunning()

	// Track dispatched execution
	dispatched := &DispatchedExecution{
		ExecutionID:   item.ExecutionID,
		CorrelationID: item.CorrelationID,
		WorkerID:      worker.ID,
		Status:        ExecutionStateRunning,
		StartedAt:     time.Now(),
	}

	d.mutex.Lock()
	d.activeExecutions[item.ExecutionID] = dispatched
	d.mutex.Unlock()

	// Execute asynchronously
	go func() {
		defer func() {
			worker.DecrementRunning()
			d.mutex.Lock()
			delete(d.activeExecutions, item.ExecutionID)
			d.mutex.Unlock()
		}()

		// Simulate execution
		// In full Phase 4.5, this would call ExecutionRunner with actual steps
		duration := time.Duration(100+item.RetryCount*50) * time.Millisecond

		select {
		case <-ctx.Done():
			return
		case <-time.After(duration):
		}

		now := time.Now()
		dispatched.CompletedAt = &now
		dispatched.Duration = time.Since(dispatched.StartedAt)
		dispatched.Status = ExecutionStateCompleted

		worker.IncrementCompleted()
	}()
}

// GetActiveExecution returns an active execution
func (d *Dispatcher) GetActiveExecution(ctx context.Context, executionID string) (*DispatchedExecution, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	exec, exists := d.activeExecutions[executionID]
	if !exists {
		return nil, fmt.Errorf("execution not active: %s", executionID)
	}

	return exec, nil
}

// ListActiveExecutions returns all active executions
func (d *Dispatcher) ListActiveExecutions(ctx context.Context) ([]*DispatchedExecution, error) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	executions := make([]*DispatchedExecution, 0, len(d.activeExecutions))
	for _, exec := range d.activeExecutions {
		executions = append(executions, exec)
	}

	return executions, nil
}

// GetQueueStats returns queue statistics
func (d *Dispatcher) GetQueueStats(ctx context.Context) map[string]interface{} {
	queueLen := d.executionQueue.Length(ctx)

	d.mutex.RLock()
	activeCount := len(d.activeExecutions)
	d.mutex.RUnlock()

	stats := d.executionQueue.Stats(ctx)
	stats["queued"] = queueLen
	stats["active"] = activeCount

	return stats
}

// DispatcherMetrics represents dispatcher performance metrics
type DispatcherMetrics struct {
	QueuedCount        int
	ActiveCount        int
	CompletedCount     int
	FailedCount        int
	AverageDuration    time.Duration
	ThroughputPerSec   float64
	QueueWaitTime      time.Duration
	DispatchWaitTime   time.Duration
}

// GetMetrics returns current dispatcher metrics
func (d *Dispatcher) GetMetrics(ctx context.Context) DispatcherMetrics {
	stats := d.GetQueueStats(ctx)

	d.mutex.RLock()
	activeCount := len(d.activeExecutions)
	d.mutex.RUnlock()

	queued := stats["queued_count"].(int)

	return DispatcherMetrics{
		QueuedCount:    queued,
		ActiveCount:    activeCount,
		CompletedCount: 0, // Would need to track from persistence
		FailedCount:    0,
	}
}

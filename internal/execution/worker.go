package execution

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerState represents worker lifecycle state
type WorkerState string

const (
	WorkerStateRegistering WorkerState = "registering"
	WorkerStateHealthy     WorkerState = "healthy"
	WorkerStateUnhealthy   WorkerState = "unhealthy"
	WorkerStateStopping    WorkerState = "stopping"
	WorkerStateStopped     WorkerState = "stopped"
)

// Worker represents an execution worker
type Worker struct {
	ID                string
	State             WorkerState
	Capabilities      []string
	MaxConcurrent     int
	CurrentlyRunning  int
	CompletedCount    int
	FailedCount       int
	LastHeartbeat     time.Time
	RegisteredAt      time.Time
	Version           int
	mutex             sync.RWMutex
}

// WorkerCapabilities defines what a worker can do
type WorkerCapabilities struct {
	CanRotateSecrets      bool
	CanVerifyRotation     bool
	CanUpdateConfig       bool
	CanNotifyTeam         bool
	CanExecuteArbitrary   bool
	SupportedProviders    []string
	MaxConcurrentSteps    int
	StepTimeoutSeconds    int
}

// Heartbeat represents worker health status
type Heartbeat struct {
	WorkerID       string
	Timestamp      time.Time
	State          WorkerState
	RunningSteps   int
	TotalCompleted int
	TotalFailed    int
	LastError      string
	SystemLoad     float64
	MemoryUsage    uint64
}

// WorkerRegistry manages worker registration and discovery
type WorkerRegistry struct {
	workers map[string]*Worker
	mutex   sync.RWMutex
}

// NewWorkerRegistry creates a new worker registry
func NewWorkerRegistry() *WorkerRegistry {
	return &WorkerRegistry{
		workers: make(map[string]*Worker),
	}
}

// Register registers a new worker
func (wr *WorkerRegistry) Register(ctx context.Context, workerID string, capabilities []string, maxConcurrent int) (*Worker, error) {
	wr.mutex.Lock()
	defer wr.mutex.Unlock()

	if _, exists := wr.workers[workerID]; exists {
		return nil, fmt.Errorf("worker already registered: %s", workerID)
	}

	worker := &Worker{
		ID:               workerID,
		State:            WorkerStateRegistering,
		Capabilities:     capabilities,
		MaxConcurrent:    maxConcurrent,
		CurrentlyRunning: 0,
		CompletedCount:   0,
		FailedCount:      0,
		LastHeartbeat:    time.Now(),
		RegisteredAt:     time.Now(),
		Version:          1,
	}

	wr.workers[workerID] = worker
	return worker, nil
}

// Unregister unregisters a worker
func (wr *WorkerRegistry) Unregister(ctx context.Context, workerID string) error {
	wr.mutex.Lock()
	defer wr.mutex.Unlock()

	if _, exists := wr.workers[workerID]; !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	delete(wr.workers, workerID)
	return nil
}

// GetWorker retrieves a worker
func (wr *WorkerRegistry) GetWorker(ctx context.Context, workerID string) (*Worker, error) {
	wr.mutex.RLock()
	defer wr.mutex.RUnlock()

	worker, exists := wr.workers[workerID]
	if !exists {
		return nil, fmt.Errorf("worker not found: %s", workerID)
	}

	return worker, nil
}

// ListWorkers returns all workers
func (wr *WorkerRegistry) ListWorkers(ctx context.Context) ([]*Worker, error) {
	wr.mutex.RLock()
	defer wr.mutex.RUnlock()

	workers := make([]*Worker, 0, len(wr.workers))
	for _, w := range wr.workers {
		workers = append(workers, w)
	}

	return workers, nil
}

// ListWorkersByState returns workers in a specific state
func (wr *WorkerRegistry) ListWorkersByState(ctx context.Context, state WorkerState) ([]*Worker, error) {
	wr.mutex.RLock()
	defer wr.mutex.RUnlock()

	workers := make([]*Worker, 0)
	for _, w := range wr.workers {
		if w.State == state {
			workers = append(workers, w)
		}
	}

	return workers, nil
}

// UpdateHeartbeat updates worker heartbeat
func (wr *WorkerRegistry) UpdateHeartbeat(ctx context.Context, workerID string, heartbeat *Heartbeat) error {
	wr.mutex.Lock()
	defer wr.mutex.Unlock()

	worker, exists := wr.workers[workerID]
	if !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	worker.LastHeartbeat = heartbeat.Timestamp
	worker.State = heartbeat.State
	worker.CurrentlyRunning = heartbeat.RunningSteps
	worker.CompletedCount = heartbeat.TotalCompleted
	worker.FailedCount = heartbeat.TotalFailed

	return nil
}

// SetWorkerState updates worker state
func (wr *WorkerRegistry) SetWorkerState(ctx context.Context, workerID string, state WorkerState) error {
	wr.mutex.Lock()
	defer wr.mutex.Unlock()

	worker, exists := wr.workers[workerID]
	if !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	worker.State = state
	return nil
}

// CheckWorkerHealth validates if worker is healthy
func (wr *WorkerRegistry) CheckWorkerHealth(ctx context.Context, workerID string) (bool, error) {
	wr.mutex.RLock()
	defer wr.mutex.RUnlock()

	worker, exists := wr.workers[workerID]
	if !exists {
		return false, fmt.Errorf("worker not found: %s", workerID)
	}

	// Worker is healthy if:
	// 1. State is healthy
	// 2. Heartbeat within last 30 seconds
	if worker.State != WorkerStateHealthy {
		return false, nil
	}

	timeSinceHeartbeat := time.Since(worker.LastHeartbeat)
	if timeSinceHeartbeat > 30*time.Second {
		return false, nil
	}

	return true, nil
}

// GetHealthyWorkers returns all healthy workers
func (wr *WorkerRegistry) GetHealthyWorkers(ctx context.Context) ([]*Worker, error) {
	wr.mutex.RLock()
	defer wr.mutex.RUnlock()

	healthyWorkers := make([]*Worker, 0)
	now := time.Now()

	for _, w := range wr.workers {
		if w.State == WorkerStateHealthy && now.Sub(w.LastHeartbeat) <= 30*time.Second {
			healthyWorkers = append(healthyWorkers, w)
		}
	}

	return healthyWorkers, nil
}

// GetWorkerCapacity returns available capacity for a worker
func (w *Worker) GetCapacity() int {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	return w.MaxConcurrent - w.CurrentlyRunning
}

// CanExecuteStep checks if worker can execute a step
func (w *Worker) CanExecuteStep(action string) bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	if w.State != WorkerStateHealthy {
		return false
	}

	// Check if action is in capabilities
	for _, cap := range w.Capabilities {
		if cap == action {
			return true
		}
	}

	return false
}

// IncrementRunning increments running count
func (w *Worker) IncrementRunning() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.CurrentlyRunning < w.MaxConcurrent {
		w.CurrentlyRunning++
	}
}

// DecrementRunning decrements running count
func (w *Worker) DecrementRunning() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.CurrentlyRunning > 0 {
		w.CurrentlyRunning--
	}
}

// IncrementCompleted increments completed count
func (w *Worker) IncrementCompleted() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.CompletedCount++
}

// IncrementFailed increments failed count
func (w *Worker) IncrementFailed() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.FailedCount++
}

// WorkerManager manages worker lifecycle
type WorkerManager struct {
	registry *WorkerRegistry
	mutex    sync.RWMutex
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager() *WorkerManager {
	return &WorkerManager{
		registry: NewWorkerRegistry(),
	}
}

// Start starts a worker
func (wm *WorkerManager) Start(ctx context.Context, workerID string, capabilities []string, maxConcurrent int) (*Worker, error) {
	return wm.registry.Register(ctx, workerID, capabilities, maxConcurrent)
}

// Stop stops a worker
func (wm *WorkerManager) Stop(ctx context.Context, workerID string) error {
	if err := wm.registry.SetWorkerState(ctx, workerID, WorkerStateStopping); err != nil {
		return err
	}

	// Allow graceful shutdown window
	time.Sleep(100 * time.Millisecond)

	if err := wm.registry.SetWorkerState(ctx, workerID, WorkerStateStopped); err != nil {
		return err
	}

	return wm.registry.Unregister(ctx, workerID)
}

// RegisterWorker registers a worker (marks as healthy)
func (wm *WorkerManager) RegisterWorker(ctx context.Context, workerID string) error {
	return wm.registry.SetWorkerState(ctx, workerID, WorkerStateHealthy)
}

// SendHeartbeat sends heartbeat from worker
func (wm *WorkerManager) SendHeartbeat(ctx context.Context, heartbeat *Heartbeat) error {
	return wm.registry.UpdateHeartbeat(ctx, heartbeat.WorkerID, heartbeat)
}

// GetWorkerHealth gets health status
func (wm *WorkerManager) GetWorkerHealth(ctx context.Context, workerID string) (bool, error) {
	return wm.registry.CheckWorkerHealth(ctx, workerID)
}

// GetHealthyWorkers returns all healthy workers
func (wm *WorkerManager) GetHealthyWorkers(ctx context.Context) ([]*Worker, error) {
	return wm.registry.GetHealthyWorkers(ctx)
}

// GetWorker retrieves a worker
func (wm *WorkerManager) GetWorker(ctx context.Context, workerID string) (*Worker, error) {
	return wm.registry.GetWorker(ctx, workerID)
}

// ListWorkers returns all workers
func (wm *WorkerManager) ListWorkers(ctx context.Context) ([]*Worker, error) {
	return wm.registry.ListWorkers(ctx)
}

// ListWorkersByState returns workers by state
func (wm *WorkerManager) ListWorkersByState(ctx context.Context, state WorkerState) ([]*Worker, error) {
	return wm.registry.ListWorkersByState(ctx, state)
}

// SetWorkerState sets worker state (for resilience)
func (wm *WorkerManager) SetWorkerState(ctx context.Context, workerID string, state WorkerState) error {
	return wm.registry.SetWorkerState(ctx, workerID, state)
}

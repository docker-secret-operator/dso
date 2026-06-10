package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Scheduler manages job execution
type Scheduler struct {
	jobs        map[string]*Job
	handlers    map[string]Handler
	store       SchedulerStore
	execStore   ExecutionStore
	workerPool  *WorkerPool
	logger      *zap.Logger
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	metrics     map[string]*JobMetrics
	eventBus    interface{} // Can be EventBus for publishing events
}

// NewScheduler creates a new scheduler
func NewScheduler(
	store SchedulerStore,
	execStore ExecutionStore,
	logger *zap.Logger,
) *Scheduler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Scheduler{
		jobs:      make(map[string]*Job),
		handlers:  make(map[string]Handler),
		store:     store,
		execStore: execStore,
		workerPool: NewWorkerPool(5, logger), // 5 concurrent workers by default
		logger:    logger,
		metrics:   make(map[string]*JobMetrics),
		done:      make(chan struct{}),
	}
}

// Initialize starts the scheduler
func (s *Scheduler) Initialize(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Start worker pool
	s.workerPool.Start(s.ctx)

	// Load jobs from storage
	jobs, err := s.store.ListJobs(s.ctx)
	if err != nil {
		s.logger.Error("failed to load jobs", zap.Error(err))
	} else {
		for _, job := range jobs {
			s.jobs[job.ID] = job
			s.initializeMetrics(job.ID)
		}
	}

	// Start background loop
	go s.runLoop()

	s.logger.Info("Scheduler initialized", zap.Int("jobs", len(s.jobs)))
	return nil
}

// Shutdown gracefully stops the scheduler
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}

	if s.workerPool != nil {
		s.workerPool.Stop()
	}

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		s.logger.Warn("scheduler shutdown timeout")
	}

	return nil
}

// Register registers a job with its handler
func (s *Scheduler) Register(job *Job, handler Handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("job already registered: %s", job.ID)
	}

	if job.NextRun.IsZero() {
		job.NextRun = time.Now()
	}

	s.jobs[job.ID] = job
	s.handlers[job.ID] = handler
	s.initializeMetrics(job.ID)

	// Persist to storage
	if err := s.store.CreateJob(s.ctx, job); err != nil {
		s.logger.Error("failed to persist job", zap.String("job_id", job.ID), zap.Error(err))
	}

	s.logger.Info("Job registered", zap.String("job_id", job.ID), zap.String("name", job.Name))
	return nil
}

// RunNow executes a job immediately
func (s *Scheduler) RunNow(jobID string) error {
	s.mu.Lock()
	job, exists := s.jobs[jobID]
	handler, hasHandler := s.handlers[jobID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	if !hasHandler {
		return fmt.Errorf("no handler for job: %s", jobID)
	}

	// Execute asynchronously
	go s.executeJob(job, handler)
	return nil
}

// Pause pauses a job
func (s *Scheduler) Pause(jobID string) error {
	s.mu.Lock()
	job, exists := s.jobs[jobID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = StatusPaused
	return s.store.UpdateJob(s.ctx, job)
}

// Resume resumes a paused job
func (s *Scheduler) Resume(jobID string) error {
	s.mu.Lock()
	job, exists := s.jobs[jobID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("job not found: %s", jobID)
	}

	job.Status = StatusPending
	job.NextRun = time.Now()
	return s.store.UpdateJob(s.ctx, job)
}

// Delete removes a job
func (s *Scheduler) Delete(jobID string) error {
	s.mu.Lock()
	delete(s.jobs, jobID)
	delete(s.handlers, jobID)
	delete(s.metrics, jobID)
	s.mu.Unlock()

	return s.store.DeleteJob(s.ctx, jobID)
}

// List returns all registered jobs
func (s *Scheduler) List() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

// Get returns a specific job
func (s *Scheduler) Get(jobID string) *Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.jobs[jobID]
}

// GetMetrics returns metrics for a job
func (s *Scheduler) GetMetrics(jobID string) *JobMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics[jobID]
}

// GetAllMetrics returns metrics for all jobs
func (s *Scheduler) GetAllMetrics() []*JobMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := make([]*JobMetrics, 0, len(s.metrics))
	for _, m := range s.metrics {
		metrics = append(metrics, m)
	}
	return metrics
}

// runLoop is the main scheduling loop
func (s *Scheduler) runLoop() {
	defer close(s.done)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.checkAndDispatch()
		}
	}
}

// checkAndDispatch checks for runnable jobs and dispatches them
func (s *Scheduler) checkAndDispatch() {
	now := time.Now()

	s.mu.Lock()
	jobsToRun := make([]*Job, 0)
	handlersToRun := make([]Handler, 0)

	for _, job := range s.jobs {
		// Skip disabled/paused jobs
		if job.Status == StatusDisabled || job.Status == StatusPaused {
			continue
		}

		// Check if it's time to run
		if now.After(job.NextRun) || now.Equal(job.NextRun) {
			handler, exists := s.handlers[job.ID]
			if exists {
				jobsToRun = append(jobsToRun, job)
				handlersToRun = append(handlersToRun, handler)
			}
		}
	}
	s.mu.Unlock()

	// Execute jobs
	for i, job := range jobsToRun {
		// Dispatch to worker pool (non-blocking)
		go s.executeJob(job, handlersToRun[i])
	}
}

// executeJob executes a single job
func (s *Scheduler) executeJob(job *Job, handler Handler) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("job panic",
				zap.String("job_id", job.ID),
				zap.Any("panic", r))
			s.recordExecution(job.ID, false, 0, fmt.Sprintf("panic: %v", r))
		}
	}()

	s.mu.Lock()
	job.Status = StatusRunning
	s.mu.Unlock()

	startTime := time.Now()
	// Use context timeout for job execution
	ctx, cancel := context.WithTimeout(s.ctx, job.Timeout)
	defer cancel()

	// Execute job with context awareness
	jobDone := make(chan error, 1)
	go func() {
		jobDone <- handler()
	}()

	var err error
	select {
	case <-ctx.Done():
		err = fmt.Errorf("job timeout")
	case result := <-jobDone:
		err = result
	}

	duration := time.Since(startTime)

	s.mu.Lock()
	if err != nil {
		job.Status = StatusFailed
		job.RetryCount++
		s.recordExecution(job.ID, false, duration.Milliseconds(), err.Error())
	} else {
		job.Status = StatusSuccess
		job.RetryCount = 0
		job.LastRun = &startTime
		s.recordExecution(job.ID, true, duration.Milliseconds(), "")
	}

	// Calculate next run time
	s.scheduleNextRun(job)

	// Persist changes
	if err := s.store.UpdateJob(s.ctx, job); err != nil {
		s.logger.Error("failed to update job", zap.String("job_id", job.ID), zap.Error(err))
	}
	s.mu.Unlock()
}

// scheduleNextRun calculates the next run time for a job
func (s *Scheduler) scheduleNextRun(job *Job) {
	now := time.Now()

	switch job.Type {
	case OneTimeJob:
		job.Status = StatusSuccess
	case DelayedJob:
		job.Status = StatusSuccess
	case IntervalJob:
		job.NextRun = now.Add(job.Interval)
	case RecurringJob:
		// For now, just add interval; cron parsing would be added for true cron support
		job.NextRun = now.Add(1 * time.Hour)
	}
}

// recordExecution records job execution in metrics and storage
func (s *Scheduler) recordExecution(jobID string, success bool, durationMs int64, errorMsg string) {
	// Update metrics
	if metrics, exists := s.metrics[jobID]; exists {
		metrics.TotalRuns++
		if success {
			metrics.SuccessfulRuns++
		} else {
			metrics.FailedRuns++
		}
		now := time.Now()
		metrics.LastExecutionTime = &now
		if success {
			metrics.LastSuccessTime = &now
		} else {
			metrics.LastFailureTime = &now
		}
	}

	// Record to storage
	exec := &JobExecution{
		ID:        fmt.Sprintf("%s-%d", jobID, time.Now().UnixNano()),
		JobID:     jobID,
		Success:   success,
		DurationMs: durationMs,
		CreatedAt: time.Now(),
	}

	if errorMsg != "" {
		exec.Error = &errorMsg
	}

	if err := s.execStore.LogExecution(s.ctx, exec); err != nil {
		s.logger.Error("failed to log execution", zap.String("job_id", jobID), zap.Error(err))
	}
}

// initializeMetrics initializes metrics for a job
func (s *Scheduler) initializeMetrics(jobID string) {
	s.metrics[jobID] = &JobMetrics{
		JobID: jobID,
	}
}

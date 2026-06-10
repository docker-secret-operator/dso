package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WorkerPool manages concurrent job execution
type WorkerPool struct {
	workers      int
	queue        chan func()
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	activeCount  int
	mu           sync.Mutex
	totalJobs    int
	completedJobs int
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers int, logger *zap.Logger) *WorkerPool {
	if logger == nil {
		logger = zap.NewNop()
	}

	if workers <= 0 {
		workers = 5
	}

	return &WorkerPool{
		workers: workers,
		queue:   make(chan func(), workers*2), // Buffer size
		logger:  logger,
		done:    make(chan struct{}),
	}
}

// Start begins the worker pool
func (wp *WorkerPool) Start(ctx context.Context) {
	wp.ctx, wp.cancel = context.WithCancel(ctx)

	// Start workers
	for i := 0; i < wp.workers; i++ {
		go wp.work()
	}
}

// Stop gracefully stops the worker pool
func (wp *WorkerPool) Stop() {
	if wp.cancel != nil {
		wp.cancel()
	}

	select {
	case <-wp.done:
	case <-time.After(5 * time.Second):
		wp.logger.Warn("worker pool shutdown timeout")
	}
}

// Submit submits a job to the worker pool
func (wp *WorkerPool) Submit(job func()) {
	select {
	case wp.queue <- job:
		wp.mu.Lock()
		wp.totalJobs++
		wp.mu.Unlock()
	case <-wp.ctx.Done():
		wp.logger.Warn("worker pool shutdown, job discarded")
	}
}

// work is the worker loop
func (wp *WorkerPool) work() {
	defer func() {
		wp.mu.Lock()
		wp.activeCount--
		wp.mu.Unlock()
		select {
		case <-wp.done:
		default:
			close(wp.done)
		}
	}()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case job, ok := <-wp.queue:
			if !ok {
				return
			}

			wp.mu.Lock()
			wp.activeCount++
			wp.mu.Unlock()

			// Execute with panic recovery
			func() {
				defer func() {
					if r := recover(); r != nil {
						wp.logger.Error("worker panic", zap.Any("panic", r))
					}

					wp.mu.Lock()
					wp.activeCount--
					wp.completedJobs++
					wp.mu.Unlock()
				}()

				job()
			}()
		}
	}
}

// GetStats returns worker pool statistics
func (wp *WorkerPool) GetStats() map[string]interface{} {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	return map[string]interface{}{
		"total_workers":    wp.workers,
		"active_workers":   wp.activeCount,
		"total_jobs":       wp.totalJobs,
		"completed_jobs":   wp.completedJobs,
		"queued_jobs":      len(wp.queue),
	}
}

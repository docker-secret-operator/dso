package watcher

import "sync"

// WorkerPool runs submitted jobs on a fixed number of goroutines instead of
// spawning one goroutine per job. This bounds concurrency for rotation
// execution (rolling swap / restart) so a burst of secret-change events
// affecting many containers can't accumulate unbounded goroutines.
type WorkerPool struct {
	jobs       chan func()
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
	numWorkers int
}

// NewWorkerPool creates a pool with numWorkers persistent goroutines and a
// job queue buffered up to queueSize. If numWorkers is less than 1 it is
// treated as 1.
func NewWorkerPool(numWorkers, queueSize int) *WorkerPool {
	if numWorkers < 1 {
		numWorkers = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}

	return &WorkerPool{
		jobs:       make(chan func(), queueSize),
		stopCh:     make(chan struct{}),
		numWorkers: numWorkers,
	}
}

// Start launches the worker goroutines. Safe to call once per pool.
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case <-wp.stopCh:
			return
		case job, ok := <-wp.jobs:
			if !ok {
				return
			}
			job()
		}
	}
}

// Submit enqueues a job for execution by the pool. It returns false if the
// pool has been stopped and the job was not accepted.
func (wp *WorkerPool) Submit(job func()) bool {
	select {
	case <-wp.stopCh:
		return false
	default:
	}

	select {
	case <-wp.stopCh:
		return false
	case wp.jobs <- job:
		return true
	}
}

// Stop signals all workers to exit after finishing their current job and
// blocks until they have. Safe to call multiple times.
func (wp *WorkerPool) Stop() {
	wp.stopOnce.Do(func() {
		close(wp.stopCh)
	})
	wp.wg.Wait()
}

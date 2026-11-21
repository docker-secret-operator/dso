package watcher

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_ExecutesSubmittedJob(t *testing.T) {
	wp := NewWorkerPool(2, 4)
	wp.Start()
	defer wp.Stop()

	done := make(chan struct{})
	ok := wp.Submit(func() { close(done) })
	if !ok {
		t.Fatal("Submit returned false for a running pool with room in the queue")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("submitted job was never executed")
	}
}

func TestWorkerPool_BoundsConcurrency(t *testing.T) {
	const numWorkers = 3
	const numJobs = 20

	wp := NewWorkerPool(numWorkers, numJobs)
	wp.Start()
	defer wp.Stop()

	var (
		current int32
		peak    int32
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	wg.Add(numJobs)
	for i := 0; i < numJobs; i++ {
		wp.Submit(func() {
			defer wg.Done()
			c := atomic.AddInt32(&current, 1)
			mu.Lock()
			if c > peak {
				peak = c
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&current, -1)
		})
	}

	wg.Wait()

	if peak > numWorkers {
		t.Fatalf("observed peak concurrency %d exceeds worker pool size %d", peak, numWorkers)
	}
}

func TestWorkerPool_SubmitAfterStopReturnsFalse(t *testing.T) {
	wp := NewWorkerPool(1, 1)
	wp.Start()
	wp.Stop()

	if wp.Submit(func() {}) {
		t.Fatal("Submit returned true after the pool was stopped")
	}
}

func TestWorkerPool_StopWaitsForInFlightJobs(t *testing.T) {
	wp := NewWorkerPool(1, 1)
	wp.Start()

	var finished int32
	wp.Submit(func() {
		time.Sleep(50 * time.Millisecond)
		atomic.StoreInt32(&finished, 1)
	})

	// Give the worker a moment to pick up the job before we stop.
	time.Sleep(10 * time.Millisecond)
	wp.Stop()

	if atomic.LoadInt32(&finished) != 1 {
		t.Fatal("Stop returned before the in-flight job finished")
	}
}

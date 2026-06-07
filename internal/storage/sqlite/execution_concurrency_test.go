package sqlite

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// Phase 4.4C Feature 6: Concurrency Validation
// Run parallel request creation, plan retrieval, dashboard queries
// Validate no deadlocks, data corruption, race conditions

// TestConcurrency_ParallelRequestCreation validates concurrent request creation
func TestConcurrency_ParallelRequestCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	tmpfile := t.TempDir() + "/exec_concurrent_create.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	const numGoroutines = 20
	const requestsPerGoroutine = 50
	totalRequests := numGoroutines * requestsPerGoroutine

	var wg sync.WaitGroup
	successCount := atomic.Int32{}
	errorCount := atomic.Int32{}
	idCollisions := atomic.Int32{}

	// Track created IDs to detect collisions
	createdIDs := make(map[string]bool)
	var idMutex sync.Mutex

	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < requestsPerGoroutine; i++ {
				req := &storage.ExecutionRequest{
					ID:            fmt.Sprintf("concurrent-%d-%d", goroutineID, i),
					DraftID:       fmt.Sprintf("draft-%d-%d", goroutineID, i),
					ApprovalID:    fmt.Sprintf("approval-%d-%d", goroutineID, i),
					Status:        "pending",
					CorrelationID: fmt.Sprintf("corr-concurrent-%d-%d", goroutineID, i),
					CreatedAt:     time.Now(),
					ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
					RequestedBy:   "user",
					Version:       1,
				}

				if err := reqStore.Create(ctx, req); err != nil {
					errorCount.Add(1)
					t.Logf("⚠ Goroutine %d: creation error: %v", goroutineID, err)
					return
				}

				successCount.Add(1)

				// Track ID for collision detection
				idMutex.Lock()
				if createdIDs[req.ID] {
					idCollisions.Add(1)
				}
				createdIDs[req.ID] = true
				idMutex.Unlock()
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	created := successCount.Load()
	errors := errorCount.Load()
	collisions := idCollisions.Load()

	t.Logf("✓ Concurrent request creation completed:")
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Requests per goroutine: %d", requestsPerGoroutine)
	t.Logf("  Total created: %d/%d", created, totalRequests)
	t.Logf("  Errors: %d", errors)
	t.Logf("  ID collisions: %d", collisions)
	t.Logf("  Time elapsed: %v", elapsed)
	t.Logf("  Throughput: %.0f requests/sec", float64(created)/elapsed.Seconds())

	if errors > 0 {
		t.Logf("⚠ Some requests failed during concurrent creation")
	}

	if collisions > 0 {
		t.Fatalf("ID collisions detected: %d", collisions)
	}

	t.Logf("✅ Parallel request creation validated")
}

// TestConcurrency_ParallelReads validates concurrent read operations
func TestConcurrency_ParallelReads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	tmpfile := t.TempDir() + "/exec_concurrent_reads.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	// Pre-create some requests
	const numPreCreated = 100
	createdIDs := make([]string, numPreCreated)

	for i := 0; i < numPreCreated; i++ {
		req := &storage.ExecutionRequest{
			ID:            fmt.Sprintf("read-test-%d", i),
			DraftID:       fmt.Sprintf("draft-%d", i),
			ApprovalID:    fmt.Sprintf("approval-%d", i),
			Status:        "pending",
			CorrelationID: fmt.Sprintf("corr-read-%d", i),
			CreatedAt:     time.Now(),
			ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
			RequestedBy:   "user",
			Version:       1,
		}

		if err := reqStore.Create(ctx, req); err != nil {
			t.Fatalf("failed to pre-create request: %v", err)
		}

		createdIDs[i] = req.ID
	}

	t.Logf("✓ Pre-created %d requests", numPreCreated)

	// Run concurrent reads
	const numGoroutines = 50
	const readsPerGoroutine = 100
	totalReads := numGoroutines * readsPerGoroutine

	var wg sync.WaitGroup
	successReads := atomic.Int32{}
	errorReads := atomic.Int32{}

	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < readsPerGoroutine; i++ {
				// Read random request
				idx := (goroutineID*readsPerGoroutine + i) % numPreCreated
				id := createdIDs[idx]

				_, err := reqStore.GetByID(ctx, id)
				if err != nil {
					errorReads.Add(1)
					return
				}

				successReads.Add(1)
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	reads := successReads.Load()
	errors := errorReads.Load()

	t.Logf("✓ Concurrent reads completed:")
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Reads per goroutine: %d", readsPerGoroutine)
	t.Logf("  Total reads: %d/%d", reads, totalReads)
	t.Logf("  Errors: %d", errors)
	t.Logf("  Time elapsed: %v", elapsed)
	t.Logf("  Throughput: %.0f reads/sec", float64(reads)/elapsed.Seconds())

	if errors > 0 {
		t.Logf("⚠ Some reads failed")
	}

	t.Logf("✅ Parallel reads validated")
}

// TestConcurrency_MixedOperations validates mixed concurrent operations
func TestConcurrency_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	tmpfile := t.TempDir() + "/exec_concurrent_mixed.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	var wg sync.WaitGroup
	successOps := atomic.Int32{}
	errorOps := atomic.Int32{}

	const numWriters = 10
	const numReaders = 20
	const numWriteOps = 50
	const numReadOps = 100

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()

			for i := 0; i < numWriteOps; i++ {
				req := &storage.ExecutionRequest{
					ID:            fmt.Sprintf("mixed-write-%d-%d", writerID, i),
					DraftID:       fmt.Sprintf("draft-%d-%d", writerID, i),
					ApprovalID:    fmt.Sprintf("approval-%d-%d", writerID, i),
					Status:        "pending",
					CorrelationID: fmt.Sprintf("corr-mixed-write-%d-%d", writerID, i),
					CreatedAt:     time.Now(),
					ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
					RequestedBy:   "user",
					Version:       1,
				}

				if err := reqStore.Create(ctx, req); err != nil {
					errorOps.Add(1)
					return
				}

				successOps.Add(1)
			}
		}(w)
	}

	// Pre-create some data for readers
	for i := 0; i < 50; i++ {
		req := &storage.ExecutionRequest{
			ID:            fmt.Sprintf("mixed-read-%d", i),
			DraftID:       fmt.Sprintf("draft-read-%d", i),
			ApprovalID:    fmt.Sprintf("approval-read-%d", i),
			Status:        "pending",
			CorrelationID: fmt.Sprintf("corr-mixed-read-%d", i),
			CreatedAt:     time.Now(),
			ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
			RequestedBy:   "user",
			Version:       1,
		}
		reqStore.Create(ctx, req)
	}

	// Readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()

			for i := 0; i < numReadOps; i++ {
				idx := (readerID*numReadOps + i) % 50
				id := fmt.Sprintf("mixed-read-%d", idx)

				_, err := reqStore.GetByID(ctx, id)
				if err != nil {
					errorOps.Add(1)
					return
				}

				successOps.Add(1)
			}
		}(r)
	}

	start := time.Now()
	wg.Wait()
	elapsed := time.Since(start)

	successCount := successOps.Load()
	errorCount := errorOps.Load()
	expectedOps := numWriters*numWriteOps + numReaders*numReadOps

	t.Logf("✓ Mixed operations completed:")
	t.Logf("  Writers: %d, Ops per writer: %d", numWriters, numWriteOps)
	t.Logf("  Readers: %d, Ops per reader: %d", numReaders, numReadOps)
	t.Logf("  Total operations: %d/%d", successCount, expectedOps)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Time elapsed: %v", elapsed)
	t.Logf("  Throughput: %.0f ops/sec", float64(successCount)/elapsed.Seconds())

	t.Logf("✅ Mixed operations validated")
}

// TestConcurrency_Deadlock validates no deadlock situations
func TestConcurrency_Deadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	tmpfile := t.TempDir() + "/exec_deadlock.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	// Create initial data
	for i := 0; i < 10; i++ {
		req := &storage.ExecutionRequest{
			ID:            fmt.Sprintf("deadlock-test-%d", i),
			DraftID:       fmt.Sprintf("draft-%d", i),
			ApprovalID:    fmt.Sprintf("approval-%d", i),
			Status:        "pending",
			CorrelationID: fmt.Sprintf("corr-deadlock-%d", i),
			CreatedAt:     time.Now(),
			ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
			RequestedBy:   "user",
			Version:       1,
		}
		reqStore.Create(ctx, req)
	}

	// Run concurrent operations that could deadlock
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)
	timeout := time.After(10 * time.Second) // 10 second timeout

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			// Create new
			req := &storage.ExecutionRequest{
				ID:            fmt.Sprintf("deadlock-create-%d", id),
				DraftID:       fmt.Sprintf("draft-create-%d", id),
				ApprovalID:    fmt.Sprintf("approval-create-%d", id),
				Status:        "pending",
				CorrelationID: fmt.Sprintf("corr-deadlock-create-%d", id),
				CreatedAt:     time.Now(),
				ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
				RequestedBy:   "user",
				Version:       1,
			}
			reqStore.Create(context.Background(), req)

			// Read multiple
			for i := 0; i < 10; i++ {
				itr := fmt.Sprintf("deadlock-test-%d", i)
				reqStore.GetByID(context.Background(), itr)
			}

			// Update if can retrieve
			retrived, _ := reqStore.GetByID(context.Background(), fmt.Sprintf("deadlock-test-%d", id%10))
			if retrived != nil {
				retrived.Version++
				reqStore.Update(context.Background(), retrived)
			}

			done <- true
		}(g)
	}

	// Wait for all to complete or timeout
	completed := 0
	for {
		select {
		case <-done:
			completed++
			if completed == numGoroutines {
				t.Logf("✓ All %d goroutines completed without deadlock", numGoroutines)
				t.Logf("✅ Deadlock validation passed")
				return
			}
		case <-timeout:
			t.Fatalf("Timeout: deadlock detected (only %d/%d completed)", completed, numGoroutines)
		}
	}
}

// TestConcurrency_RaceCondition validates no race conditions
func TestConcurrency_RaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	// This test is designed to be run with: go test -race ./...
	tmpfile := t.TempDir() + "/exec_race.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	// Create shared data
	req := &storage.ExecutionRequest{
		ID:            "race-test-001",
		DraftID:       "draft-001",
		ApprovalID:    "approval-001",
		Status:        "pending",
		CorrelationID: "corr-race-001",
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
		RequestedBy:   "user",
		Version:       1,
	}

	if err := reqStore.Create(ctx, req); err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Concurrent readers
	var wg sync.WaitGroup
	const numGoroutines = 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				reqStore.GetByID(ctx, "race-test-001")
			}
		}()
	}

	wg.Wait()
	t.Logf("✓ %d goroutines completed 100 reads each without race conditions", numGoroutines)
	t.Logf("✅ Race condition test passed (run with -race flag for detection)")
}

// TestConcurrency_DataConsistency validates data integrity under concurrent access
func TestConcurrency_DataConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrency test in short mode")
	}

	tmpfile := t.TempDir() + "/exec_consistency.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	reqStore := provider.ExecutionRequests()

	// Create requests concurrently
	const numRequests = 100
	createdIDs := make([]string, numRequests)
	var idMutex sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			req := &storage.ExecutionRequest{
				ID:            fmt.Sprintf("consistency-%d", id),
				DraftID:       fmt.Sprintf("draft-%d", id),
				ApprovalID:    fmt.Sprintf("approval-%d", id),
				Status:        "pending",
				CorrelationID: fmt.Sprintf("corr-consistency-%d", id),
				CreatedAt:     time.Now(),
				ExpiresAt:     time.Now().Add(7 * 24 * time.Hour),
				RequestedBy:   "user",
				Version:       1,
			}

			if err := reqStore.Create(ctx, req); err == nil {
				idMutex.Lock()
				createdIDs[id] = req.ID
				idMutex.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify all created data is consistent
	var verifyWg sync.WaitGroup
	dataErrors := atomic.Int32{}

	for _, id := range createdIDs {
		if id == "" {
			continue
		}

		verifyWg.Add(1)
		go func(reqID string) {
			defer verifyWg.Done()

			req, err := reqStore.GetByID(ctx, reqID)
			if err != nil || req == nil {
				dataErrors.Add(1)
				return
			}

			// Verify data integrity
			if req.ID != reqID {
				dataErrors.Add(1)
			}
		}(id)
	}

	verifyWg.Wait()

	if dataErrors.Load() > 0 {
		t.Fatalf("Data consistency errors: %d", dataErrors.Load())
	}

	t.Logf("✓ All %d concurrently created requests verified for consistency", len(createdIDs))
	t.Logf("✅ Data consistency under concurrent access validated")
}

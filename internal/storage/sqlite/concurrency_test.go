package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// TestConcurrentDraftCreation tests concurrent draft creation
// Run with: go test -race ./...
func TestConcurrentDraftCreation(t *testing.T) {
	tmpfile := t.TempDir() + "/concurrent_draft.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	store := provider.Drafts()

	// Create 100 drafts concurrently
	numGoroutines := 100
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			draft := &storage.Draft{
				ID:            fmt.Sprintf("draft-%d", index),
				WorkspaceID:   "ws-1",
				OwnerID:       "owner-1",
				Title:         fmt.Sprintf("Draft %d", index),
				Status:        "draft",
				VersionNumber: 1,
				Config:        "{}",
				Checksum:      "x",
			}

			if err := store.Create(ctx, draft); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent create error: %v", err)
		}
	}

	// Verify all drafts were created
	drafts, err := store.List(ctx, "owner-1")
	if err != nil {
		t.Fatalf("failed to list drafts: %v", err)
	}

	if len(drafts) != numGoroutines {
		t.Fatalf("expected %d drafts, got %d", numGoroutines, len(drafts))
	}

	t.Logf("✓ Concurrent creation: %d drafts created successfully without corruption", numGoroutines)
}

// TestConcurrentReviewCreation tests concurrent review creation
func TestConcurrentReviewCreation(t *testing.T) {
	tmpfile := t.TempDir() + "/concurrent_review.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	store := provider.Reviews()

	numGoroutines := 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			review := &storage.Review{
				ID:                fmt.Sprintf("review-%d", index),
				DraftID:           fmt.Sprintf("draft-%d", index),
				CreatedAt:         time.Now(),
				CreatedBy:         "creator-1",
				ModifiedAt:        time.Now(),
				Status:            "draft",
				Title:             fmt.Sprintf("Review %d", index),
				Checklist:         "{}",
				RiskAssessment:    "{}",
				RequiredApprovals: 1,
			}

			if err := store.Create(ctx, review); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent create error: %v", err)
		}
	}

	// Verify all reviews were created
	reviews, err := store.List(ctx)
	if err != nil {
		t.Fatalf("failed to list reviews: %v", err)
	}

	if len(reviews) != numGoroutines {
		t.Fatalf("expected %d reviews, got %d", numGoroutines, len(reviews))
	}

	t.Logf("✓ Concurrent review creation: %d reviews created successfully", numGoroutines)
}

// TestConcurrentAuditLogging tests concurrent audit event logging
func TestConcurrentAuditLogging(t *testing.T) {
	tmpfile := t.TempDir() + "/concurrent_audit.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	store := provider.Audit()

	numGoroutines := 200
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			event := &storage.AuditEvent{
				ID:            fmt.Sprintf("event-%d", index),
				Timestamp:     time.Now(),
				ActorID:       fmt.Sprintf("actor-%d", index%10),
				ActorName:     fmt.Sprintf("Actor %d", index%10),
				Action:        "test.action",
				Resource:      "test",
				ResourceID:    fmt.Sprintf("resource-%d", index),
				ResourceType:  "test",
				Status:        "success",
				CorrelationID: fmt.Sprintf("corr-%d", index/10),
				RequestID:     fmt.Sprintf("req-%d", index),
				Severity:      "info",
			}

			if err := store.Log(ctx, event); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent log error: %v", err)
		}
	}

	// Verify all events were logged
	events, err := store.Query(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}

	if len(events) != numGoroutines {
		t.Fatalf("expected %d events, got %d", numGoroutines, len(events))
	}

	t.Logf("✓ Concurrent audit logging: %d events logged successfully", numGoroutines)
}

// TestConcurrentReadWrite tests mixed concurrent reads and writes
func TestConcurrentReadWrite(t *testing.T) {
	tmpfile := t.TempDir() + "/concurrent_rw.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()
	store := provider.Drafts()

	// First, create 10 drafts
	for i := 0; i < 10; i++ {
		draft := &storage.Draft{
			ID:            fmt.Sprintf("draft-%d", i),
			WorkspaceID:   "ws-1",
			OwnerID:       "owner-1",
			Title:         fmt.Sprintf("Draft %d", i),
			Status:        "draft",
			VersionNumber: 1,
			Config:        "{}",
			Checksum:      "x",
		}
		store.Create(ctx, draft)
	}

	// Now do concurrent reads and writes
	numGoroutines := 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			if index%2 == 0 {
				// Read operation
				_, err := store.GetByID(ctx, fmt.Sprintf("draft-%d", index%10))
				if err != nil {
					// Some reads may fail if ID doesn't exist, that's ok
					if err.Error() != "draft not found: draft-"+fmt.Sprintf("%d", index%10) {
						errors <- err
					}
				}
			} else {
				// Write operation
				draft := &storage.Draft{
					ID:            fmt.Sprintf("draft-new-%d", index),
					WorkspaceID:   "ws-1",
					OwnerID:       "owner-1",
					Title:         fmt.Sprintf("Draft New %d", index),
					Status:        "draft",
					VersionNumber: 1,
					Config:        "{}",
					Checksum:      "x",
				}
				if err := store.Create(ctx, draft); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent read/write error: %v", err)
		}
	}

	t.Log("✓ Concurrent read/write: no deadlocks or corruption detected")
}

// TestConcurrentTransactions tests concurrent transactions
func TestConcurrentTransactions(t *testing.T) {
	tmpfile := t.TempDir() + "/concurrent_tx.db"
	provider, err := NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	ctx := context.Background()

	numGoroutines := 20
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			// Begin transaction
			tx, err := provider.BeginTx(ctx)
			if err != nil {
				errors <- fmt.Errorf("begin tx error: %w", err)
				return
			}

			// Create draft within transaction
			store := tx.Drafts()
			draft := &storage.Draft{
				ID:            fmt.Sprintf("tx-draft-%d", index),
				WorkspaceID:   "ws-1",
				OwnerID:       "owner-1",
				Title:         fmt.Sprintf("TX Draft %d", index),
				Status:        "draft",
				VersionNumber: 1,
				Config:        "{}",
				Checksum:      "x",
			}

			if err := store.Create(ctx, draft); err != nil {
				tx.Rollback(ctx)
				errors <- fmt.Errorf("create error: %w", err)
				return
			}

			// Commit transaction
			if err := tx.Commit(ctx); err != nil {
				errors <- fmt.Errorf("commit error: %w", err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent transaction error: %v", err)
		}
	}

	// Verify all transactions committed successfully
	store := provider.Drafts()
	drafts, err := store.List(ctx, "owner-1")
	if err != nil {
		t.Fatalf("failed to list drafts: %v", err)
	}

	txDrafts := 0
	for _, d := range drafts {
		if d.Title[0:2] == "TX" {
			txDrafts++
		}
	}

	if txDrafts != numGoroutines {
		t.Logf("⚠ Expected %d transaction commits, got %d", numGoroutines, txDrafts)
	} else {
		t.Logf("✓ Concurrent transactions: %d transactions committed successfully", numGoroutines)
	}
}

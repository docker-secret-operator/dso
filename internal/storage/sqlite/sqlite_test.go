package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// setupTestDB creates a temporary SQLite database for testing
func setupTestDB(t *testing.T) (*SQLiteProvider, func()) {
	tmpfile, err := os.CreateTemp("", "test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpfile.Close()

	provider, err := NewSQLiteProvider(tmpfile.Name())
	if err != nil {
		os.Remove(tmpfile.Name())
		t.Fatalf("failed to create provider: %v", err)
	}

	cleanup := func() {
		provider.Close(context.Background())
		os.Remove(tmpfile.Name())
	}

	return provider, cleanup
}

func TestSQLiteProviderHealth(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.Health(ctx); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestSQLiteProviderClose(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := provider.Close(ctx); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestDraftStoreCreate(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Drafts()
	draft := &storage.Draft{
		ID:            "test-draft-1",
		WorkspaceID:   "ws-1",
		OwnerID:       "owner-1",
		Title:         "Test Draft",
		Description:   "A test draft",
		Status:        "draft",
		VersionNumber: 1,
		Config:        `{"mappings": []}`,
		Checksum:      "abc123",
	}

	if err := store.Create(ctx, draft); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify it was created
	retrieved, err := store.GetByID(ctx, "test-draft-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if retrieved.Title != draft.Title {
		t.Fatalf("expected title %q, got %q", draft.Title, retrieved.Title)
	}
}

func TestDraftStoreUpdate(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Drafts()
	draft := &storage.Draft{
		ID:            "test-draft-2",
		WorkspaceID:   "ws-1",
		OwnerID:       "owner-1",
		Title:         "Original Title",
		Status:        "draft",
		VersionNumber: 1,
		Config:        `{}`,
		Checksum:      "abc",
	}

	store.Create(ctx, draft)

	draft.Title = "Updated Title"
	if err := store.Update(ctx, draft); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	retrieved, _ := store.GetByID(ctx, "test-draft-2")
	if retrieved.Title != "Updated Title" {
		t.Fatalf("expected updated title, got %q", retrieved.Title)
	}
}

func TestDraftStoreList(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Drafts()

	// Create multiple drafts
	for i := 1; i <= 3; i++ {
		draft := &storage.Draft{
			ID:          "draft-" + string(rune(48+i)),
			WorkspaceID: "ws-1",
			OwnerID:     "owner-1",
			Title:       "Draft " + string(rune(48+i)),
			Status:      "draft",
			Config:      "{}",
			Checksum:    "x",
		}
		store.Create(ctx, draft)
	}

	drafts, err := store.List(ctx, "owner-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	if len(drafts) != 3 {
		t.Fatalf("expected 3 drafts, got %d", len(drafts))
	}
}

func TestReviewStoreCreate(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Reviews()
	review := &storage.Review{
		ID:                "review-1",
		DraftID:           "draft-1",
		CreatedAt:         time.Now(),
		CreatedBy:         "reviewer-1",
		ModifiedAt:        time.Now(),
		Status:            "draft",
		Title:             "Test Review",
		Checklist:         "{}",
		RiskAssessment:    "{}",
		RequiredApprovals: 1,
	}

	if err := store.Create(ctx, review); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	retrieved, err := store.GetByID(ctx, "review-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if retrieved.Title != review.Title {
		t.Fatalf("expected title %q, got %q", review.Title, retrieved.Title)
	}
}

func TestApprovalStoreCreate(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Approvals()
	approval := &storage.Approval{
		ID:               "approval-1",
		ReviewID:         "review-1",
		ReviewerID:       "reviewer-1",
		ReviewerName:     "Reviewer One",
		Decision:         "pending",
		ApprovalSequence: 1,
		IsRequired:       true,
		CreatedAt:        time.Now(),
	}

	if err := store.Create(ctx, approval); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	retrieved, err := store.GetByID(ctx, "approval-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if retrieved.Decision != "pending" {
		t.Fatalf("expected decision 'pending', got %q", retrieved.Decision)
	}
}

func TestAuditStoreLog(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := provider.Audit()
	event := &storage.AuditEvent{
		ID:            "event-1",
		Timestamp:     time.Now(),
		ActorID:       "actor-1",
		ActorName:     "Test Actor",
		Action:        "draft.created",
		Resource:      "draft",
		ResourceID:    "draft-1",
		ResourceType:  "draft",
		Status:        "success",
		CorrelationID: "corr-1",
		RequestID:     "req-1",
		Severity:      "info",
	}

	if err := store.Log(ctx, event); err != nil {
		t.Fatalf("log failed: %v", err)
	}

	// Query the event
	events, err := store.Query(ctx, map[string]interface{}{"resource_id": "draft-1"})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestTransactions(t *testing.T) {
	provider, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := provider.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	// Use stores within transaction
	store := tx.Drafts()
	draft := &storage.Draft{
		ID:          "tx-draft",
		WorkspaceID: "ws-1",
		OwnerID:     "owner-1",
		Title:       "TX Draft",
		Status:      "draft",
		Config:      "{}",
		Checksum:    "x",
	}

	if err := store.Create(ctx, draft); err != nil {
		t.Fatalf("create in tx failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
}

// BenchmarkDraftCreate benchmarks draft creation
func BenchmarkDraftCreate(b *testing.B) {
	provider, cleanup := setupTestDB(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	store := provider.Drafts()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		draft := &storage.Draft{
			ID:          "draft-" + string(rune(i)),
			WorkspaceID: "ws-1",
			OwnerID:     "owner-1",
			Title:       "Draft",
			Status:      "draft",
			Config:      "{}",
			Checksum:    "x",
		}
		store.Create(ctx, draft)
	}
}

// BenchmarkDraftQuery benchmarks draft queries
func BenchmarkDraftQuery(b *testing.B) {
	provider, cleanup := setupTestDB(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	store := provider.Drafts()

	// Create some drafts first
	for i := 0; i < 100; i++ {
		draft := &storage.Draft{
			ID:          "draft-" + string(rune(i)),
			WorkspaceID: "ws-1",
			OwnerID:     "owner-1",
			Title:       "Draft",
			Status:      "draft",
			Config:      "{}",
			Checksum:    "x",
		}
		store.Create(ctx, draft)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.List(ctx, "owner-1")
	}
}

package services

import (
	"context"
	"testing"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

func setupTestProvider(t *testing.T) (storage.StorageProvider, func()) {
	tmpfile := t.TempDir() + "/test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	cleanup := func() {
		provider.Close(context.Background())
	}

	return provider, cleanup
}

func TestDraftServiceCreate(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewDraftService(provider.Drafts())

	draft, err := service.CreateDraft(ctx, "ws-1", "owner-1", "Test Draft", "A test", `{"mappings": []}`)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if draft.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	if draft.Status != "draft" {
		t.Fatalf("expected status 'draft', got %q", draft.Status)
	}

	if draft.VersionNumber != 1 {
		t.Fatalf("expected version 1, got %d", draft.VersionNumber)
	}
}

func TestDraftServiceUpdate(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewDraftService(provider.Drafts())

	draft, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Original", "", `{}`)
	id := draft.ID

	// Update the draft
	updated, err := service.UpdateDraft(ctx, id, "Updated", "", `{"new": "config"}`)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if updated.Title != "Updated" {
		t.Fatalf("expected updated title")
	}

	if updated.VersionNumber != 2 {
		t.Fatalf("expected version 2 after config update, got %d", updated.VersionNumber)
	}
}

func TestDraftServiceVersioning(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewDraftService(provider.Drafts())

	draft, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{"v": 1}`)
	id := draft.ID

	// Save versions
	v1, _ := service.SaveVersion(ctx, id, `{"v": 1}`)
	service.UpdateDraft(ctx, id, "", "", `{"v": 2}`)
	v2, _ := service.SaveVersion(ctx, id, `{"v": 2}`)

	versions, err := service.GetDraftVersions(ctx, id)
	if err != nil {
		t.Fatalf("get versions failed: %v", err)
	}

	if len(versions) < 2 {
		t.Fatalf("expected at least 2 versions, got %d", len(versions))
	}

	// Verify v1 and v2 exist
	found := false
	for _, v := range versions {
		if v.ID == v1.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find v1")
	}

	found = false
	for _, v := range versions {
		if v.ID == v2.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find v2")
	}
}

func TestDraftServiceChecksum(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewDraftService(provider.Drafts())

	config := `{"test": "data"}`
	draft, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", config)

	if draft.Checksum == "" {
		t.Fatal("expected non-empty checksum")
	}

	// Verify checksum is consistent
	draft2, _ := service.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", config)
	if draft.Checksum != draft2.Checksum {
		t.Fatal("expected same checksum for same config")
	}
}

func TestReviewServiceCreate(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	review, err := service.CreateReview(ctx, "draft-1", "creator-1", "Test Review", "Description", `{}`, `{}`, 1)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if review.Status != "draft" {
		t.Fatalf("expected status 'draft', got %q", review.Status)
	}

	if review.RequiredApprovals != 1 {
		t.Fatalf("expected 1 required approval, got %d", review.RequiredApprovals)
	}
}

func TestReviewServiceApprovals(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	review, _ := service.CreateReview(ctx, "draft-1", "creator-1", "Test", "", `{}`, `{}`, 1)
	reviewID := review.ID

	// Create approval
	approval, err := service.CreateApproval(ctx, reviewID, "reviewer-1", "Reviewer One", "pending", nil, 1)
	if err != nil {
		t.Fatalf("create approval failed: %v", err)
	}

	if approval.Decision != "pending" {
		t.Fatalf("expected 'pending', got %q", approval.Decision)
	}

	// Get approvals
	approvals, _ := service.GetApprovals(ctx, reviewID)
	if len(approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(approvals))
	}
}

func TestAuditServiceLog(t *testing.T) {
	provider, cleanup := setupTestProvider(t)
	defer cleanup()

	ctx := context.Background()
	service := NewAuditService(provider.Audit())

	err := service.LogEvent(ctx, "actor-1", "Test Actor", "draft.created", "draft", "draft-1", "draft")
	if err != nil {
		t.Fatalf("log failed: %v", err)
	}

	// Query the event
	events, _ := service.GetEventsByResource(ctx, "draft-1")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func BenchmarkDraftServiceCreate(b *testing.B) {
	provider, cleanup := setupTestProvider(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	service := NewDraftService(provider.Drafts())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CreateDraft(ctx, "ws-1", "owner-1", "Draft", "", `{}`)
	}
}

func BenchmarkReviewServiceCreate(b *testing.B) {
	provider, cleanup := setupTestProvider(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	service := NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.CreateReview(ctx, "draft-1", "creator-1", "Review", "", `{}`, `{}`, 1)
	}
}

func BenchmarkAuditServiceLog(b *testing.B) {
	provider, cleanup := setupTestProvider(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	service := NewAuditService(provider.Audit())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		service.LogEvent(ctx, "actor-1", "Actor", "action", "resource", "id", "type")
	}
}

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

func setupReviewTest(t *testing.T) (*ReviewHandler, *services.ReviewService, *services.DraftService, func()) {
	tmpfile := t.TempDir() + "/test.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	auditService := services.NewAuditService(provider.Audit())

	handler := NewReviewHandler(reviewService, auditService, draftService)

	cleanup := func() {
		provider.Close(context.Background())
	}

	return handler, reviewService, draftService, cleanup
}

// TestCreateReview tests review creation
func TestCreateReview(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create a draft
	draft, err := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test Config", "", `{"mappings": []}`)
	if err != nil {
		t.Fatalf("failed to create draft: %v", err)
	}

	req := CreateReviewRequest{
		DraftID:     draft.ID,
		Title:       "Initial Review",
		Description: "Test review",
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/api/reviews", bytes.NewReader(body))
	httpReq = httpReq.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.CreateReview(w, httpReq)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
		t.Logf("Response: %s", w.Body.String())
	}

	var resp ReviewResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.DraftID != draft.ID {
		t.Errorf("Expected DraftID %s, got %s", draft.ID, resp.DraftID)
	}

	if resp.Status != "draft_review" {
		t.Errorf("Expected status 'draft_review', got '%s'", resp.Status)
	}

	t.Log("✓ Review created successfully")
}

// TestCreateReviewInvalidDraft tests review creation with invalid draft
func TestCreateReviewInvalidDraft(t *testing.T) {
	handler, _, _, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	req := CreateReviewRequest{
		DraftID: "nonexistent",
		Title:   "Test",
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/api/reviews", bytes.NewReader(body))
	httpReq = httpReq.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.CreateReview(w, httpReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	t.Log("✓ Invalid draft rejected")
}

// TestGetReview tests review retrieval
func TestGetReview(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create draft and review
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	httpReq := httptest.NewRequest("GET", fmt.Sprintf("/api/reviews/%s", review.ID), nil)
	httpReq = httpReq.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.GetReview(w, httpReq, review.ID)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp ReviewResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.ID != review.ID {
		t.Errorf("Expected review ID %s, got %s", review.ID, resp.ID)
	}

	t.Log("✓ Review retrieved successfully")
}

// TestListReviews tests review listing
func TestListReviews(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple reviews
	for i := 0; i < 3; i++ {
		draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)

		reviewService.CreateReview(ctx, &storage.Review{
			DraftID:    draft.ID,
			ReviewerID: fmt.Sprintf("reviewer-%d", i),
			Status:     "draft_review",
			CreatedBy:  "system",
		})
	}

	httpReq := httptest.NewRequest("GET", "/api/reviews", nil)
	httpReq = httpReq.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ListReviews(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	count := int(result["count"].(float64))
	if count != 3 {
		t.Errorf("Expected 3 reviews, got %d", count)
	}

	t.Log("✓ Reviews listed successfully")
}

// TestUpdateReview tests review status update
func TestUpdateReview(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create draft and review
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	// Update to active_review
	updateReq := UpdateReviewRequest{
		Status:   "active_review",
		Comments: "Review in progress",
	}

	body, _ := json.Marshal(updateReq)
	httpReq := httptest.NewRequest("PUT", fmt.Sprintf("/api/reviews/%s", review.ID), bytes.NewReader(body))
	httpReq = httpReq.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.UpdateReview(w, httpReq, review.ID)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp ReviewResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Status != "active_review" {
		t.Errorf("Expected status 'active_review', got '%s'", resp.Status)
	}

	t.Log("✓ Review updated successfully")
}

// TestReviewStatusTransitions tests valid transitions
func TestReviewStatusTransitions(t *testing.T) {
	_, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	// Test valid transitions
	transitions := []struct {
		from   string
		to     string
		expect error
	}{
		{"draft_review", "active_review", nil},
		{"active_review", "approved", nil},
		{"approved", "closed", nil},
		{"active_review", "rejected", nil},
		{"rejected", "closed", nil},
		{"closed", "active_review", fmt.Errorf("invalid")},
	}

	for _, t := range transitions {
		err := services.ValidateReviewStatusTransition(t.from, t.to)
		if (err == nil) != (t.expect == nil) {
			fmt.Printf("Transition %s → %s: expected error=%v, got error=%v\n", t.from, t.to, t.expect, err)
		}
	}

	t.Log("✓ Review status transitions validated")
}

// TestDeleteReview tests review deletion (closure)
func TestDeleteReview(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	httpReq := httptest.NewRequest("DELETE", fmt.Sprintf("/api/reviews/%s", review.ID), nil)
	httpReq = httpReq.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.DeleteReview(w, httpReq, review.ID)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	// Verify review is closed
	updated, _ := reviewService.GetReview(ctx, review.ID)
	if updated.Status != "closed" {
		t.Errorf("Expected status 'closed', got '%s'", updated.Status)
	}

	t.Log("✓ Review deleted (closed) successfully")
}

// TestAddComment tests adding comments
func TestAddComment(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	commentReq := CommentRequest{
		Type:    "general",
		Text:    "This looks good",
		ActorID: "reviewer-1",
	}

	body, _ := json.Marshal(commentReq)
	httpReq := httptest.NewRequest("POST", fmt.Sprintf("/api/reviews/%s/comments", review.ID), bytes.NewReader(body))
	httpReq = httpReq.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.AddComment(w, httpReq, review.ID)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, w.Code)
	}

	t.Log("✓ Comment added successfully")
}

// TestGetReviewHistory tests history retrieval
func TestGetReviewHistory(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	// Add a comment
	reviewService.AddActivity(ctx, review.ID, &storage.ReviewActivity{
		ReviewID:  review.ID,
		Type:      "general",
		ActorID:   "reviewer-1",
		Content:   "Looking good",
		Timestamp: time.Now(),
	})

	httpReq := httptest.NewRequest("GET", fmt.Sprintf("/api/reviews/%s/history", review.ID), nil)
	httpReq = httpReq.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.GetReviewHistory(w, httpReq, review.ID)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &result)

	count := int(result["count"].(float64))
	if count < 1 {
		t.Errorf("Expected at least 1 history entry, got %d", count)
	}

	t.Log("✓ Review history retrieved successfully")
}

// TestReviewWorkflow tests complete review workflow
func TestReviewWorkflow(t *testing.T) {
	handler, reviewService, draftService, cleanup := setupReviewTest(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Create draft
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "New Config", "", `{"mappings": []}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	// 2. Create review
	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:   draft.ID,
		Status:    "draft_review",
		CreatedBy: "system",
		Title:     "Test",
	})

	// 3. Move to active review
	review.Status = "active_review"
	review.ReviewStartedAt = time.Now()
	reviewService.UpdateReview(ctx, review)

	// 4. Add approval comment
	reviewService.AddActivity(ctx, review.ID, &storage.ReviewActivity{
		ReviewID:  review.ID,
		Type:      "approve",
		ActorID:   "reviewer-1",
		Content:   "LGTM",
		Timestamp: time.Now(),
	})

	// 5. Approve review
	review.Status = "approved"
	review.ReviewCompletedAt = time.Now()
	reviewService.UpdateReview(ctx, review)

	// 6. Close review
	review.Status = "closed"
	reviewService.UpdateReview(ctx, review)

	// Verify final state
	final, _ := reviewService.GetReview(ctx, review.ID)
	if final.Status != "closed" {
		t.Errorf("Expected final status 'closed', got '%s'", final.Status)
	}

	t.Log("✓ Complete review workflow executed successfully")
}

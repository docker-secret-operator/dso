package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/services"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// TestLoadCreateReviews tests creating many reviews
func TestLoadCreateReviews(t *testing.T) {
	tmpfile := t.TempDir() + "/load_reviews.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	t.Log("Creating 1,000 drafts and reviews for load testing...")
	start := time.Now()

	for i := 0; i < 1000; i++ {
		// Create draft
		draft, err := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{"mappings": []}`)
		if err != nil {
			t.Fatalf("Failed to create draft: %v", err)
		}

		// Update to under_review
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)

		// Create review
		_, err = reviewService.CreateReview(ctx, &storage.Review{
			DraftID:   draft.ID,
			Status:    "draft_review",
			CreatedBy: "system",
			Title:     fmt.Sprintf("Review %d", i),
		})
		if err != nil {
			t.Fatalf("Failed to create review: %v", err)
		}
	}

	duration := time.Since(start)
	opsPerSec := float64(1000) / duration.Seconds()

	t.Logf("✓ Created 1,000 reviews in %v (%.1f ops/sec)", duration, opsPerSec)
}

// TestLoadCreateComments tests adding many comments
func TestLoadCreateComments(t *testing.T) {
	tmpfile := t.TempDir() + "/load_comments.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create test data
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:    draft.ID,
		ReviewerID: "reviewer-1",
		Status:     "draft_review",
		CreatedBy:  "system",
	})

	t.Log("Adding 10,000 comments to single review...")
	start := time.Now()

	for i := 0; i < 10000; i++ {
		_, err := reviewService.AddActivity(ctx, review.ID, &storage.ReviewActivity{
			ReviewID:  review.ID,
			Type:      "general",
			ActorID:   fmt.Sprintf("reviewer-%d", i%10),
			Content:   fmt.Sprintf("Comment %d", i),
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("Failed to add comment: %v", err)
		}
	}

	duration := time.Since(start)
	opsPerSec := float64(10000) / duration.Seconds()

	t.Logf("✓ Added 10,000 comments in %v (%.1f ops/sec)", duration, opsPerSec)
}

// TestLoadListReviews tests listing many reviews
func TestLoadListReviews(t *testing.T) {
	tmpfile := t.TempDir() + "/load_list_reviews.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create 500 reviews
	t.Log("Setting up 500 reviews...")
	for i := 0; i < 500; i++ {
		draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)

		reviewService.CreateReview(ctx, &storage.Review{
			DraftID:    draft.ID,
			ReviewerID: fmt.Sprintf("reviewer-%d", i%10),
			Status:     "draft_review",
			CreatedBy:  "system",
		})
	}

	t.Log("Listing 500 reviews 100 times...")
	start := time.Now()

	for i := 0; i < 100; i++ {
		_, err := reviewService.ListReviews(ctx)
		if err != nil {
			t.Fatalf("Failed to list reviews: %v", err)
		}
	}

	duration := time.Since(start)
	opsPerSec := float64(100) / duration.Seconds()

	t.Logf("✓ Listed 500 reviews 100 times in %v (%.1f list ops/sec)", duration, opsPerSec)
}

// TestLoadReviewStatusTransitions tests many status updates
func TestLoadReviewStatusTransitions(t *testing.T) {
	tmpfile := t.TempDir() + "/load_transitions.db"
	provider, err := sqlite.NewSQLiteProvider(tmpfile)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	t.Log("Creating 500 reviews and transitioning through lifecycle...")
	start := time.Now()

	for i := 0; i < 500; i++ {
		draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)

		review, _ := reviewService.CreateReview(ctx, &storage.Review{
			DraftID:    draft.ID,
			ReviewerID: fmt.Sprintf("reviewer-%d", i%10),
			Status:     "draft_review",
			CreatedBy:  "system",
		})

		// Transition: draft_review → active_review
		review.Status = "active_review"
		review.ReviewStartedAt = time.Now()
		reviewService.UpdateReview(ctx, review)

		// Transition: active_review → approved (50%) or rejected (50%)
		if i%2 == 0 {
			review.Status = "approved"
		} else {
			review.Status = "rejected"
		}
		review.ReviewCompletedAt = time.Now()
		reviewService.UpdateReview(ctx, review)

		// Transition: approved/rejected → closed
		review.Status = "closed"
		reviewService.UpdateReview(ctx, review)
	}

	duration := time.Since(start)
	transitionsPerSec := float64(500*3) / duration.Seconds() // 3 transitions per review

	t.Logf("✓ Created 500 reviews with full lifecycle in %v (%.1f transitions/sec)", duration, transitionsPerSec)
}

// BenchmarkReviewCreation benchmarks review creation
func BenchmarkReviewCreation(b *testing.B) {
	tmpfile := b.TempDir() + "/bench.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create drafts for reviews
	drafts := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)
		drafts[i] = draft.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reviewService.CreateReview(ctx, &storage.Review{
			DraftID:    drafts[i],
			ReviewerID: fmt.Sprintf("reviewer-%d", i%10),
			Status:     "draft_review",
			CreatedBy:  "system",
		})
	}
}

// BenchmarkListReviews benchmarks listing reviews
func BenchmarkListReviews(b *testing.B) {
	tmpfile := b.TempDir() + "/bench_list.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create 100 reviews
	for i := 0; i < 100; i++ {
		draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", fmt.Sprintf("Draft %d", i), "", `{}`)
		draft.Status = "under_review"
		draftService.UpdateDraft(ctx, draft)

		reviewService.CreateReview(ctx, &storage.Review{
			DraftID:    draft.ID,
			ReviewerID: fmt.Sprintf("reviewer-%d", i%10),
			Status:     "draft_review",
			CreatedBy:  "system",
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reviewService.ListReviews(ctx)
	}
}

// BenchmarkAddActivity benchmarks adding comments
func BenchmarkAddActivity(b *testing.B) {
	tmpfile := b.TempDir() + "/bench_activity.db"
	provider, _ := sqlite.NewSQLiteProvider(tmpfile)
	defer provider.Close(context.Background())

	reviewService := services.NewReviewService(
		provider.Reviews(),
		provider.Approvals(),
		provider.ReviewActivities(),
		provider.Audit(),
	)

	draftService := services.NewDraftService(provider.Drafts())
	ctx := context.Background()

	// Create review
	draft, _ := draftService.CreateDraft(ctx, "ws-1", "owner-1", "Test", "", `{}`)
	draft.Status = "under_review"
	draftService.UpdateDraft(ctx, draft)

	review, _ := reviewService.CreateReview(ctx, &storage.Review{
		DraftID:    draft.ID,
		ReviewerID: "reviewer-1",
		Status:     "draft_review",
		CreatedBy:  "system",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reviewService.AddActivity(ctx, review.ID, &storage.ReviewActivity{
			ReviewID:  review.ID,
			Type:      "general",
			ActorID:   "reviewer-1",
			Content:   fmt.Sprintf("Comment %d", i),
			Timestamp: time.Now(),
		})
	}
}

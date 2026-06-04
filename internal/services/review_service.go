package services

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ReviewService handles review operations
type ReviewService struct {
	reviewStore   storage.ReviewStore
	approvalStore storage.ApprovalStore
	activityStore storage.ReviewActivityStore
	auditStore    storage.AuditStore
}

// NewReviewService creates a new review service
func NewReviewService(
	reviewStore storage.ReviewStore,
	approvalStore storage.ApprovalStore,
	activityStore storage.ReviewActivityStore,
	auditStore storage.AuditStore,
) *ReviewService {
	return &ReviewService{
		reviewStore:   reviewStore,
		approvalStore: approvalStore,
		activityStore: activityStore,
		auditStore:    auditStore,
	}
}

// CreateReview creates a new review from a Review object
func (rs *ReviewService) CreateReview(ctx context.Context, review *storage.Review) (*storage.Review, error) {
	if review.DraftID == "" {
		return nil, fmt.Errorf("DraftID is required")
	}

	// Set defaults
	if review.ID == "" {
		review.ID = generateID()
	}
	if review.Status == "" {
		review.Status = "draft_review"
	}
	now := time.Now()
	review.CreatedAt = now
	review.ModifiedAt = now

	if err := rs.reviewStore.Create(ctx, review); err != nil {
		return nil, err
	}

	return review, nil
}

// GetReview retrieves a review
func (rs *ReviewService) GetReview(ctx context.Context, id string) (*storage.Review, error) {
	return rs.reviewStore.GetByID(ctx, id)
}

// GetReviewByDraft retrieves review for a draft
func (rs *ReviewService) GetReviewByDraft(ctx context.Context, draftID string) (*storage.Review, error) {
	return rs.reviewStore.GetByDraftID(ctx, draftID)
}

// ListReviews retrieves all reviews
func (rs *ReviewService) ListReviews(ctx context.Context) ([]*storage.Review, error) {
	return rs.reviewStore.List(ctx)
}

// UpdateReviewStatus updates review status
func (rs *ReviewService) UpdateReviewStatus(ctx context.Context, reviewID, status string, actorID string) error {
	if reviewID == "" || status == "" {
		return fmt.Errorf("required fields cannot be empty")
	}

	review, err := rs.reviewStore.GetByID(ctx, reviewID)
	if err != nil {
		return err
	}

	review.Status = status
	review.ModifiedAt = time.Now()

	if err := rs.reviewStore.Update(ctx, review); err != nil {
		return err
	}

	// Log activity
	activity := &storage.ReviewActivity{
		ID:          generateID(),
		ReviewID:    reviewID,
		Type:        "review_status_changed",
		ActorID:     actorID,
		Description: fmt.Sprintf("Review status changed to %s", status),
		Timestamp:   time.Now(),
	}
	_ = rs.activityStore.Log(ctx, activity)

	return nil
}

// CreateApproval creates an approval decision
func (rs *ReviewService) CreateApproval(ctx context.Context, reviewID, reviewerID, reviewerName, decision string, comments *string, approvalSequence int) (*storage.Approval, error) {
	if reviewID == "" || reviewerID == "" || decision == "" {
		return nil, fmt.Errorf("required fields cannot be empty")
	}

	approval := &storage.Approval{
		ID:               generateID(),
		ReviewID:         reviewID,
		ReviewerID:       reviewerID,
		ReviewerName:     reviewerName,
		Decision:         decision,
		Comments:         comments,
		ApprovalSequence: approvalSequence,
		IsRequired:       true,
		CreatedAt:        time.Now(),
	}

	if decision != "pending" {
		now := time.Now()
		approval.DecidedAt = &now
	}

	if err := rs.approvalStore.Create(ctx, approval); err != nil {
		return nil, err
	}

	// Log activity
	activity := &storage.ReviewActivity{
		ID:          generateID(),
		ReviewID:    reviewID,
		Type:        "approval_given",
		ActorID:     reviewerID,
		Description: fmt.Sprintf("Approval decision: %s", decision),
		Timestamp:   time.Now(),
	}
	_ = rs.activityStore.Log(ctx, activity)

	return approval, nil
}

// GetApprovals retrieves approvals for a review
func (rs *ReviewService) GetApprovals(ctx context.Context, reviewID string) ([]*storage.Approval, error) {
	return rs.approvalStore.ListForReview(ctx, reviewID)
}

// GetPendingApprovals retrieves pending approvals for a reviewer
func (rs *ReviewService) GetPendingApprovals(ctx context.Context, reviewerID string) ([]*storage.Approval, error) {
	return rs.approvalStore.ListPendingForReviewer(ctx, reviewerID)
}

// GetReviewActivities retrieves review timeline
func (rs *ReviewService) GetReviewActivities(ctx context.Context, reviewID string) ([]*storage.ReviewActivity, error) {
	return rs.activityStore.ListForReview(ctx, reviewID)
}

// UpdateReview updates a review
func (rs *ReviewService) UpdateReview(ctx context.Context, review *storage.Review) (*storage.Review, error) {
	if review.ID == "" {
		return nil, fmt.Errorf("review ID is required")
	}

	review.ModifiedAt = time.Now()

	if err := rs.reviewStore.Update(ctx, review); err != nil {
		return nil, err
	}

	return review, nil
}

// AddActivity adds an activity/comment to a review
func (rs *ReviewService) AddActivity(ctx context.Context, reviewID string, activity *storage.ReviewActivity) (*storage.ReviewActivity, error) {
	if reviewID == "" {
		return nil, fmt.Errorf("review ID is required")
	}

	if activity.ID == "" {
		activity.ID = generateID()
	}
	if activity.ReviewID == "" {
		activity.ReviewID = reviewID
	}
	if activity.Timestamp.IsZero() {
		activity.Timestamp = time.Now()
	}

	if err := rs.activityStore.Log(ctx, activity); err != nil {
		return nil, err
	}

	return activity, nil
}

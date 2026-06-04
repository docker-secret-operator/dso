package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ReviewStore implements storage.ReviewStore for SQLite
type ReviewStore struct {
	db DBProvider
}

// Create creates a new review
func (rs *ReviewStore) Create(ctx context.Context, review *storage.Review) error {
	if review.ID == "" {
		return fmt.Errorf("review ID cannot be empty")
	}
	if review.DraftID == "" {
		return fmt.Errorf("draft ID cannot be empty")
	}
	if review.CreatedBy == "" {
		return fmt.Errorf("created_by cannot be empty")
	}

	now := time.Now()
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	}
	if review.ModifiedAt.IsZero() {
		review.ModifiedAt = now
	}
	if review.Status == "" {
		review.Status = "draft"
	}

	query := `
		INSERT INTO reviews (id, draft_id, created_at, created_by, modified_at, status, checklist, risk_assessment, required_approvals, approval_timeout_hours, title, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := rs.db.ExecContext(ctx, query,
		review.ID,
		review.DraftID,
		review.CreatedAt,
		review.CreatedBy,
		review.ModifiedAt,
		review.Status,
		review.Checklist,
		review.RiskAssessment,
		review.RequiredApprovals,
		review.ApprovalTimeoutHrs,
		review.Title,
		review.Description,
	)

	if err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	return nil
}

// Update updates an existing review
func (rs *ReviewStore) Update(ctx context.Context, review *storage.Review) error {
	if review.ID == "" {
		return fmt.Errorf("review ID cannot be empty")
	}

	review.ModifiedAt = time.Now()

	query := `
		UPDATE reviews
		SET draft_id = ?, created_by = ?, modified_at = ?, status = ?,
		    checklist = ?, risk_assessment = ?, required_approvals = ?, approval_timeout_hours = ?, title = ?, description = ?
		WHERE id = ?
	`

	result, err := rs.db.ExecContext(ctx, query,
		review.DraftID,
		review.CreatedBy,
		review.ModifiedAt,
		review.Status,
		review.Checklist,
		review.RiskAssessment,
		review.RequiredApprovals,
		review.ApprovalTimeoutHrs,
		review.Title,
		review.Description,
		review.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update review: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("review not found: %s", review.ID)
	}

	return nil
}

// GetByID retrieves a review by ID
func (rs *ReviewStore) GetByID(ctx context.Context, id string) (*storage.Review, error) {
	query := `
		SELECT id, draft_id, created_at, created_by, modified_at, status, checklist, risk_assessment, required_approvals, approval_timeout_hours, title, description
		FROM reviews
		WHERE id = ?
	`

	review := &storage.Review{}
	err := rs.db.QueryRowContext(ctx, query, id).Scan(
		&review.ID,
		&review.DraftID,
		&review.CreatedAt,
		&review.CreatedBy,
		&review.ModifiedAt,
		&review.Status,
		&review.Checklist,
		&review.RiskAssessment,
		&review.RequiredApprovals,
		&review.ApprovalTimeoutHrs,
		&review.Title,
		&review.Description,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("review not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get review: %w", err)
	}

	return review, nil
}

// GetByDraftID retrieves a review by draft ID
func (rs *ReviewStore) GetByDraftID(ctx context.Context, draftID string) (*storage.Review, error) {
	query := `
		SELECT id, draft_id, created_at, created_by, modified_at, status, checklist, risk_assessment, required_approvals, approval_timeout_hours, title, description
		FROM reviews
		WHERE draft_id = ?
	`

	review := &storage.Review{}
	err := rs.db.QueryRowContext(ctx, query, draftID).Scan(
		&review.ID,
		&review.DraftID,
		&review.CreatedAt,
		&review.CreatedBy,
		&review.ModifiedAt,
		&review.Status,
		&review.Checklist,
		&review.RiskAssessment,
		&review.RequiredApprovals,
		&review.ApprovalTimeoutHrs,
		&review.Title,
		&review.Description,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("review not found for draft: %s", draftID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get review: %w", err)
	}

	return review, nil
}

// List retrieves all reviews
func (rs *ReviewStore) List(ctx context.Context) ([]*storage.Review, error) {
	query := `
		SELECT id, draft_id, created_at, created_by, modified_at, status, checklist, risk_assessment, required_approvals, approval_timeout_hours, title, description
		FROM reviews
		ORDER BY created_at DESC
	`

	rows, err := rs.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query reviews: %w", err)
	}
	defer rows.Close()

	var reviews []*storage.Review
	for rows.Next() {
		review := &storage.Review{}
		if err := rows.Scan(
			&review.ID,
			&review.DraftID,
			&review.CreatedAt,
			&review.CreatedBy,
			&review.ModifiedAt,
			&review.Status,
			&review.Checklist,
			&review.RiskAssessment,
			&review.RequiredApprovals,
			&review.ApprovalTimeoutHrs,
			&review.Title,
			&review.Description,
		); err != nil {
			return nil, fmt.Errorf("failed to scan review: %w", err)
		}
		reviews = append(reviews, review)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating reviews: %w", err)
	}

	return reviews, nil
}

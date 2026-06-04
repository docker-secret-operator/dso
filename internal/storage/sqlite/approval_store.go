package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ApprovalStore implements storage.ApprovalStore for SQLite
type ApprovalStore struct {
	db DBProvider
}

// Create creates a new approval
func (as *ApprovalStore) Create(ctx context.Context, approval *storage.Approval) error {
	if approval.ID == "" {
		return fmt.Errorf("approval ID cannot be empty")
	}
	if approval.ReviewID == "" {
		return fmt.Errorf("review ID cannot be empty")
	}
	if approval.ReviewerID == "" {
		return fmt.Errorf("reviewer ID cannot be empty")
	}

	if approval.CreatedAt.IsZero() {
		approval.CreatedAt = time.Now()
	}
	if approval.Decision == "" {
		approval.Decision = "pending"
	}

	query := `
		INSERT INTO approvals (id, review_id, reviewer_id, reviewer_name, decision, comments, rejection_reason, approval_sequence, is_required, created_at, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := as.db.ExecContext(ctx, query,
		approval.ID,
		approval.ReviewID,
		approval.ReviewerID,
		approval.ReviewerName,
		approval.Decision,
		approval.Comments,
		approval.RejectionReason,
		approval.ApprovalSequence,
		approval.IsRequired,
		approval.CreatedAt,
		approval.DecidedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create approval: %w", err)
	}

	return nil
}

// Update updates an existing approval
func (as *ApprovalStore) Update(ctx context.Context, approval *storage.Approval) error {
	if approval.ID == "" {
		return fmt.Errorf("approval ID cannot be empty")
	}

	query := `
		UPDATE approvals
		SET review_id = ?, reviewer_id = ?, reviewer_name = ?, decision = ?, comments = ?, rejection_reason = ?, approval_sequence = ?, is_required = ?, decided_at = ?
		WHERE id = ?
	`

	result, err := as.db.ExecContext(ctx, query,
		approval.ReviewID,
		approval.ReviewerID,
		approval.ReviewerName,
		approval.Decision,
		approval.Comments,
		approval.RejectionReason,
		approval.ApprovalSequence,
		approval.IsRequired,
		approval.DecidedAt,
		approval.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update approval: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("approval not found: %s", approval.ID)
	}

	return nil
}

// GetByID retrieves an approval by ID
func (as *ApprovalStore) GetByID(ctx context.Context, id string) (*storage.Approval, error) {
	query := `
		SELECT id, review_id, reviewer_id, reviewer_name, decision, comments, rejection_reason, approval_sequence, is_required, created_at, decided_at
		FROM approvals
		WHERE id = ?
	`

	approval := &storage.Approval{}
	err := as.db.QueryRowContext(ctx, query, id).Scan(
		&approval.ID,
		&approval.ReviewID,
		&approval.ReviewerID,
		&approval.ReviewerName,
		&approval.Decision,
		&approval.Comments,
		&approval.RejectionReason,
		&approval.ApprovalSequence,
		&approval.IsRequired,
		&approval.CreatedAt,
		&approval.DecidedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("approval not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get approval: %w", err)
	}

	return approval, nil
}

// ListForReview retrieves all approvals for a review
func (as *ApprovalStore) ListForReview(ctx context.Context, reviewID string) ([]*storage.Approval, error) {
	query := `
		SELECT id, review_id, reviewer_id, reviewer_name, decision, comments, rejection_reason, approval_sequence, is_required, created_at, decided_at
		FROM approvals
		WHERE review_id = ?
		ORDER BY approval_sequence ASC
	`

	rows, err := as.db.QueryContext(ctx, query, reviewID)
	if err != nil {
		return nil, fmt.Errorf("failed to query approvals: %w", err)
	}
	defer rows.Close()

	var approvals []*storage.Approval
	for rows.Next() {
		approval := &storage.Approval{}
		if err := rows.Scan(
			&approval.ID,
			&approval.ReviewID,
			&approval.ReviewerID,
			&approval.ReviewerName,
			&approval.Decision,
			&approval.Comments,
			&approval.RejectionReason,
			&approval.ApprovalSequence,
			&approval.IsRequired,
			&approval.CreatedAt,
			&approval.DecidedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan approval: %w", err)
		}
		approvals = append(approvals, approval)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating approvals: %w", err)
	}

	return approvals, nil
}

// ListPendingForReviewer retrieves all pending approvals for a reviewer
func (as *ApprovalStore) ListPendingForReviewer(ctx context.Context, reviewerID string) ([]*storage.Approval, error) {
	query := `
		SELECT id, review_id, reviewer_id, reviewer_name, decision, comments, rejection_reason, approval_sequence, is_required, created_at, decided_at
		FROM approvals
		WHERE reviewer_id = ? AND decision = 'pending'
		ORDER BY created_at DESC
	`

	rows, err := as.db.QueryContext(ctx, query, reviewerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending approvals: %w", err)
	}
	defer rows.Close()

	var approvals []*storage.Approval
	for rows.Next() {
		approval := &storage.Approval{}
		if err := rows.Scan(
			&approval.ID,
			&approval.ReviewID,
			&approval.ReviewerID,
			&approval.ReviewerName,
			&approval.Decision,
			&approval.Comments,
			&approval.RejectionReason,
			&approval.ApprovalSequence,
			&approval.IsRequired,
			&approval.CreatedAt,
			&approval.DecidedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan approval: %w", err)
		}
		approvals = append(approvals, approval)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pending approvals: %w", err)
	}

	return approvals, nil
}

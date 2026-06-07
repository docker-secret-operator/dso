package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionRequestStore implements storage.ExecutionRequestStore for SQLite
type ExecutionRequestStore struct {
	db *SQLiteDB
}

// NewExecutionRequestStore creates a new execution request store
func NewExecutionRequestStore(db *SQLiteDB) *ExecutionRequestStore {
	return &ExecutionRequestStore{db: db}
}

// Create inserts a new execution request
func (s *ExecutionRequestStore) Create(ctx context.Context, req *storage.ExecutionRequest) error {
	query := `
		INSERT INTO execution_requests (
			id, correlation_id, draft_id, review_id, approval_id, plan_id,
			status, created_at, validated_at, expires_at, requested_by, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query,
		req.ID,
		req.CorrelationID,
		req.DraftID,
		req.ReviewID,
		req.ApprovalID,
		req.PlanID,
		req.Status,
		req.CreatedAt,
		req.ValidatedAt,
		req.ExpiresAt,
		req.RequestedBy,
		req.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution request: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("failed to create execution request: no rows affected")
	}

	return nil
}

// Update updates an execution request with optimistic locking
func (s *ExecutionRequestStore) Update(ctx context.Context, req *storage.ExecutionRequest) error {
	query := `
		UPDATE execution_requests
		SET status = ?, validated_at = ?, plan_id = ?, version = version + 1
		WHERE id = ? AND version = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		req.Status,
		req.ValidatedAt,
		req.PlanID,
		req.ID,
		req.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to update execution request: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("execution request not found or version mismatch")
	}

	return nil
}

// GetByID retrieves an execution request by ID
func (s *ExecutionRequestStore) GetByID(ctx context.Context, id string) (*storage.ExecutionRequest, error) {
	query := `
		SELECT id, correlation_id, draft_id, review_id, approval_id, plan_id,
		       status, created_at, validated_at, expires_at, requested_by, version
		FROM execution_requests
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)

	var req storage.ExecutionRequest
	err := row.Scan(
		&req.ID,
		&req.CorrelationID,
		&req.DraftID,
		&req.ReviewID,
		&req.ApprovalID,
		&req.PlanID,
		&req.Status,
		&req.CreatedAt,
		&req.ValidatedAt,
		&req.ExpiresAt,
		&req.RequestedBy,
		&req.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution request: %w", err)
	}

	return &req, nil
}

// GetByCorrelationID retrieves an execution request by correlation ID
func (s *ExecutionRequestStore) GetByCorrelationID(ctx context.Context, correlationID string) (*storage.ExecutionRequest, error) {
	query := `
		SELECT id, correlation_id, draft_id, review_id, approval_id, plan_id,
		       status, created_at, validated_at, expires_at, requested_by, version
		FROM execution_requests
		WHERE correlation_id = ?
	`

	row := s.db.QueryRowContext(ctx, query, correlationID)

	var req storage.ExecutionRequest
	err := row.Scan(
		&req.ID,
		&req.CorrelationID,
		&req.DraftID,
		&req.ReviewID,
		&req.ApprovalID,
		&req.PlanID,
		&req.Status,
		&req.CreatedAt,
		&req.ValidatedAt,
		&req.ExpiresAt,
		&req.RequestedBy,
		&req.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution request by correlation ID: %w", err)
	}

	return &req, nil
}

// ListByStatus retrieves execution requests by status
func (s *ExecutionRequestStore) ListByStatus(ctx context.Context, status string) ([]*storage.ExecutionRequest, error) {
	query := `
		SELECT id, correlation_id, draft_id, review_id, approval_id, plan_id,
		       status, created_at, validated_at, expires_at, requested_by, version
		FROM execution_requests
		WHERE status = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution requests: %w", err)
	}
	defer rows.Close()

	var requests []*storage.ExecutionRequest
	for rows.Next() {
		var req storage.ExecutionRequest
		err := rows.Scan(
			&req.ID,
			&req.CorrelationID,
			&req.DraftID,
			&req.ReviewID,
			&req.ApprovalID,
			&req.PlanID,
			&req.Status,
			&req.CreatedAt,
			&req.ValidatedAt,
			&req.ExpiresAt,
			&req.RequestedBy,
			&req.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution request: %w", err)
		}
		requests = append(requests, &req)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution requests: %w", err)
	}

	return requests, nil
}

// ListByApproval retrieves execution requests for an approval
func (s *ExecutionRequestStore) ListByApproval(ctx context.Context, approvalID string) ([]*storage.ExecutionRequest, error) {
	query := `
		SELECT id, correlation_id, draft_id, review_id, approval_id, plan_id,
		       status, created_at, validated_at, expires_at, requested_by, version
		FROM execution_requests
		WHERE approval_id = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, approvalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution requests: %w", err)
	}
	defer rows.Close()

	var requests []*storage.ExecutionRequest
	for rows.Next() {
		var req storage.ExecutionRequest
		err := rows.Scan(
			&req.ID,
			&req.CorrelationID,
			&req.DraftID,
			&req.ReviewID,
			&req.ApprovalID,
			&req.PlanID,
			&req.Status,
			&req.CreatedAt,
			&req.ValidatedAt,
			&req.ExpiresAt,
			&req.RequestedBy,
			&req.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution request: %w", err)
		}
		requests = append(requests, &req)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution requests: %w", err)
	}

	return requests, nil
}

// List retrieves paginated execution requests
func (s *ExecutionRequestStore) List(ctx context.Context, limit int, offset int) ([]*storage.ExecutionRequest, error) {
	query := `
		SELECT id, correlation_id, draft_id, review_id, approval_id, plan_id,
		       status, created_at, validated_at, expires_at, requested_by, version
		FROM execution_requests
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution requests: %w", err)
	}
	defer rows.Close()

	var requests []*storage.ExecutionRequest
	for rows.Next() {
		var req storage.ExecutionRequest
		err := rows.Scan(
			&req.ID,
			&req.CorrelationID,
			&req.DraftID,
			&req.ReviewID,
			&req.ApprovalID,
			&req.PlanID,
			&req.Status,
			&req.CreatedAt,
			&req.ValidatedAt,
			&req.ExpiresAt,
			&req.RequestedBy,
			&req.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution request: %w", err)
		}
		requests = append(requests, &req)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution requests: %w", err)
	}

	return requests, nil
}

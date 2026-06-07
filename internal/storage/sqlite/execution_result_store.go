package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionResultStore implements storage.ExecutionResultStore for SQLite
type ExecutionResultStore struct {
	db *SQLiteDB
}

// Create creates a new execution result
func (ers *ExecutionResultStore) Create(ctx context.Context, result *storage.ExecutionResult) error {
	query := `
		INSERT INTO execution_results (id, execution_id, correlation_id, worker_id, status, error, duration_seconds, completed_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := ers.db.ExecContext(ctx, query,
		result.ID,
		result.ExecutionID,
		result.CorrelationID,
		result.WorkerID,
		result.Status,
		result.Error,
		result.Duration,
		result.CompletedAt,
		result.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution result: %w", err)
	}

	return nil
}

// GetByID retrieves a result by ID
func (ers *ExecutionResultStore) GetByID(ctx context.Context, id string) (*storage.ExecutionResult, error) {
	query := `
		SELECT id, execution_id, correlation_id, worker_id, status, error, duration_seconds, completed_at, version
		FROM execution_results
		WHERE id = ?
	`

	row := ers.db.QueryRowContext(ctx, query, id)

	result := &storage.ExecutionResult{}
	err := row.Scan(
		&result.ID,
		&result.ExecutionID,
		&result.CorrelationID,
		&result.WorkerID,
		&result.Status,
		&result.Error,
		&result.Duration,
		&result.CompletedAt,
		&result.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution result: %w", err)
	}

	return result, nil
}

// GetByExecutionID retrieves result by execution ID
func (ers *ExecutionResultStore) GetByExecutionID(ctx context.Context, executionID string) (*storage.ExecutionResult, error) {
	query := `
		SELECT id, execution_id, correlation_id, worker_id, status, error, duration_seconds, completed_at, version
		FROM execution_results
		WHERE execution_id = ?
	`

	row := ers.db.QueryRowContext(ctx, query, executionID)

	result := &storage.ExecutionResult{}
	err := row.Scan(
		&result.ID,
		&result.ExecutionID,
		&result.CorrelationID,
		&result.WorkerID,
		&result.Status,
		&result.Error,
		&result.Duration,
		&result.CompletedAt,
		&result.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution result: %w", err)
	}

	return result, nil
}

// ListByStatus lists results by status
func (ers *ExecutionResultStore) ListByStatus(ctx context.Context, status string, limit int, offset int) ([]*storage.ExecutionResult, error) {
	query := `
		SELECT id, execution_id, correlation_id, worker_id, status, error, duration_seconds, completed_at, version
		FROM execution_results
		WHERE status = ?
		ORDER BY completed_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := ers.db.QueryContext(ctx, query, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution results: %w", err)
	}
	defer rows.Close()

	results := make([]*storage.ExecutionResult, 0)
	for rows.Next() {
		result := &storage.ExecutionResult{}
		err := rows.Scan(
			&result.ID,
			&result.ExecutionID,
			&result.CorrelationID,
			&result.WorkerID,
			&result.Status,
			&result.Error,
			&result.Duration,
			&result.CompletedAt,
			&result.Version,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan execution result: %w", err)
		}

		results = append(results, result)
	}

	return results, nil
}

// List lists all results with pagination
func (ers *ExecutionResultStore) List(ctx context.Context, limit int, offset int) ([]*storage.ExecutionResult, error) {
	query := `
		SELECT id, execution_id, correlation_id, worker_id, status, error, duration_seconds, completed_at, version
		FROM execution_results
		ORDER BY completed_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := ers.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution results: %w", err)
	}
	defer rows.Close()

	results := make([]*storage.ExecutionResult, 0)
	for rows.Next() {
		result := &storage.ExecutionResult{}
		err := rows.Scan(
			&result.ID,
			&result.ExecutionID,
			&result.CorrelationID,
			&result.WorkerID,
			&result.Status,
			&result.Error,
			&result.Duration,
			&result.CompletedAt,
			&result.Version,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan execution result: %w", err)
		}

		results = append(results, result)
	}

	return results, nil
}

package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// StepResultStore implements storage.StepResultStore for SQLite
type StepResultStore struct {
	db *SQLiteDB
}

// Create creates a new step result
func (srs *StepResultStore) Create(ctx context.Context, result *storage.StepResult) error {
	query := `
		INSERT INTO step_results (id, step_id, execution_id, correlation_id, status, duration_seconds, output, error, started_at, completed_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := srs.db.ExecContext(ctx, query,
		result.ID, result.StepID, result.ExecutionID, result.CorrelationID,
		result.Status, result.Duration, result.Output, result.Error,
		result.StartedAt, result.CompletedAt, result.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create step result: %w", err)
	}

	return nil
}

// GetByID retrieves a step result by ID
func (srs *StepResultStore) GetByID(ctx context.Context, id string) (*storage.StepResult, error) {
	query := `
		SELECT id, step_id, execution_id, correlation_id, status, duration_seconds, output, error, started_at, completed_at, version
		FROM step_results WHERE id = ?
	`

	row := srs.db.QueryRowContext(ctx, query, id)
	result := &storage.StepResult{}

	err := row.Scan(
		&result.ID, &result.StepID, &result.ExecutionID, &result.CorrelationID,
		&result.Status, &result.Duration, &result.Output, &result.Error,
		&result.StartedAt, &result.CompletedAt, &result.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get step result: %w", err)
	}

	return result, nil
}

// ListByExecution lists results for an execution
func (srs *StepResultStore) ListByExecution(ctx context.Context, executionID string) ([]*storage.StepResult, error) {
	query := `
		SELECT id, step_id, execution_id, correlation_id, status, duration_seconds, output, error, started_at, completed_at, version
		FROM step_results
		WHERE execution_id = ?
		ORDER BY started_at ASC
	`

	rows, err := srs.db.QueryContext(ctx, query, executionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list step results: %w", err)
	}
	defer rows.Close()

	results := make([]*storage.StepResult, 0)
	for rows.Next() {
		result := &storage.StepResult{}
		err := rows.Scan(
			&result.ID, &result.StepID, &result.ExecutionID, &result.CorrelationID,
			&result.Status, &result.Duration, &result.Output, &result.Error,
			&result.StartedAt, &result.CompletedAt, &result.Version,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan step result: %w", err)
		}

		results = append(results, result)
	}

	return results, nil
}

// ListByStep lists results for a step
func (srs *StepResultStore) ListByStep(ctx context.Context, stepID string) ([]*storage.StepResult, error) {
	query := `
		SELECT id, step_id, execution_id, correlation_id, status, duration_seconds, output, error, started_at, completed_at, version
		FROM step_results
		WHERE step_id = ?
		ORDER BY started_at ASC
	`

	rows, err := srs.db.QueryContext(ctx, query, stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to list step results: %w", err)
	}
	defer rows.Close()

	results := make([]*storage.StepResult, 0)
	for rows.Next() {
		result := &storage.StepResult{}
		err := rows.Scan(
			&result.ID, &result.StepID, &result.ExecutionID, &result.CorrelationID,
			&result.Status, &result.Duration, &result.Output, &result.Error,
			&result.StartedAt, &result.CompletedAt, &result.Version,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan step result: %w", err)
		}

		results = append(results, result)
	}

	return results, nil
}

// CreateBatch creates multiple step results
func (srs *StepResultStore) CreateBatch(ctx context.Context, results []*storage.StepResult) error {
	for _, result := range results {
		if err := srs.Create(ctx, result); err != nil {
			return err
		}
	}

	return nil
}

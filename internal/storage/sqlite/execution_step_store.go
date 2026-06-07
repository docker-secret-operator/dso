package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionStepStore implements storage.ExecutionStepStore for SQLite
type ExecutionStepStore struct {
	db *SQLiteDB
}

// NewExecutionStepStore creates a new execution step store
func NewExecutionStepStore(db *SQLiteDB) *ExecutionStepStore {
	return &ExecutionStepStore{db: db}
}

// Create inserts a new execution step
func (s *ExecutionStepStore) Create(ctx context.Context, step *storage.ExecutionStep) error {
	query := `
		INSERT INTO execution_steps (
			id, plan_id, sequence, name, description, action,
			estimated_time_seconds, risk_level, rollback_available, payload, created_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query,
		step.ID,
		step.PlanID,
		step.Sequence,
		step.Name,
		step.Description,
		step.Action,
		step.EstimatedTime,
		step.RiskLevel,
		step.RollbackAvailable,
		step.Payload,
		step.CreatedAt,
		step.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution step: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("failed to create execution step: no rows affected")
	}

	return nil
}

// GetByID retrieves an execution step by ID
func (s *ExecutionStepStore) GetByID(ctx context.Context, id string) (*storage.ExecutionStep, error) {
	query := `
		SELECT id, plan_id, sequence, name, description, action,
		       estimated_time_seconds, risk_level, rollback_available, payload, created_at, version
		FROM execution_steps
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)

	var step storage.ExecutionStep
	err := row.Scan(
		&step.ID,
		&step.PlanID,
		&step.Sequence,
		&step.Name,
		&step.Description,
		&step.Action,
		&step.EstimatedTime,
		&step.RiskLevel,
		&step.RollbackAvailable,
		&step.Payload,
		&step.CreatedAt,
		&step.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution step: %w", err)
	}

	return &step, nil
}

// ListByPlan retrieves all steps for a plan, ordered by sequence
func (s *ExecutionStepStore) ListByPlan(ctx context.Context, planID string) ([]*storage.ExecutionStep, error) {
	query := `
		SELECT id, plan_id, sequence, name, description, action,
		       estimated_time_seconds, risk_level, rollback_available, payload, created_at, version
		FROM execution_steps
		WHERE plan_id = ?
		ORDER BY sequence ASC
	`

	rows, err := s.db.QueryContext(ctx, query, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution steps: %w", err)
	}
	defer rows.Close()

	var steps []*storage.ExecutionStep
	for rows.Next() {
		var step storage.ExecutionStep
		err := rows.Scan(
			&step.ID,
			&step.PlanID,
			&step.Sequence,
			&step.Name,
			&step.Description,
			&step.Action,
			&step.EstimatedTime,
			&step.RiskLevel,
			&step.RollbackAvailable,
			&step.Payload,
			&step.CreatedAt,
			&step.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution step: %w", err)
		}
		steps = append(steps, &step)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution steps: %w", err)
	}

	return steps, nil
}

// CreateBatch inserts multiple execution steps in a single transaction
func (s *ExecutionStepStore) CreateBatch(ctx context.Context, steps []*storage.ExecutionStep) error {
	if len(steps) == 0 {
		return nil
	}

	query := `
		INSERT INTO execution_steps (
			id, plan_id, sequence, name, description, action,
			estimated_time_seconds, risk_level, rollback_available, payload, created_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	stmt, err := s.db.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch insert: %w", err)
	}
	defer stmt.Close()

	for _, step := range steps {
		_, err := stmt.ExecContext(ctx,
			step.ID,
			step.PlanID,
			step.Sequence,
			step.Name,
			step.Description,
			step.Action,
			step.EstimatedTime,
			step.RiskLevel,
			step.RollbackAvailable,
			step.Payload,
			step.CreatedAt,
			step.Version,
		)
		if err != nil {
			return fmt.Errorf("failed to insert execution step: %w", err)
		}
	}

	return nil
}

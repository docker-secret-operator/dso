package sqlite

import (
	"context"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ExecutionPlanStore implements storage.ExecutionPlanStore for SQLite
type ExecutionPlanStore struct {
	db *SQLiteDB
}

// NewExecutionPlanStore creates a new execution plan store
func NewExecutionPlanStore(db *SQLiteDB) *ExecutionPlanStore {
	return &ExecutionPlanStore{db: db}
}

// Create inserts a new execution plan
func (s *ExecutionPlanStore) Create(ctx context.Context, plan *storage.ExecutionPlan) error {
	query := `
		INSERT INTO execution_plans (
			id, execution_id, correlation_id, approval_id, draft_id, status,
			total_steps, estimated_duration_seconds, risk_score, affected_resources,
			rollback_available, created_at, validated_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := s.db.ExecContext(ctx, query,
		plan.ID,
		plan.ExecutionID,
		plan.CorrelationID,
		plan.ApprovalID,
		plan.DraftID,
		plan.Status,
		plan.TotalSteps,
		plan.EstimatedDuration,
		plan.RiskScore,
		plan.AffectedResources,
		plan.RollbackAvailable,
		plan.CreatedAt,
		plan.ValidatedAt,
		plan.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution plan: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return fmt.Errorf("failed to create execution plan: no rows affected")
	}

	return nil
}

// Update updates an execution plan with optimistic locking
func (s *ExecutionPlanStore) Update(ctx context.Context, plan *storage.ExecutionPlan) error {
	query := `
		UPDATE execution_plans
		SET status = ?, validated_at = ?, version = version + 1
		WHERE id = ? AND version = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		plan.Status,
		plan.ValidatedAt,
		plan.ID,
		plan.Version,
	)

	if err != nil {
		return fmt.Errorf("failed to update execution plan: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}

	if affected == 0 {
		return fmt.Errorf("execution plan not found or version mismatch")
	}

	return nil
}

// GetByID retrieves an execution plan by ID
func (s *ExecutionPlanStore) GetByID(ctx context.Context, id string) (*storage.ExecutionPlan, error) {
	query := `
		SELECT id, execution_id, correlation_id, approval_id, draft_id, status,
		       total_steps, estimated_duration_seconds, risk_score, affected_resources,
		       rollback_available, created_at, validated_at, version
		FROM execution_plans
		WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)

	var plan storage.ExecutionPlan
	err := row.Scan(
		&plan.ID,
		&plan.ExecutionID,
		&plan.CorrelationID,
		&plan.ApprovalID,
		&plan.DraftID,
		&plan.Status,
		&plan.TotalSteps,
		&plan.EstimatedDuration,
		&plan.RiskScore,
		&plan.AffectedResources,
		&plan.RollbackAvailable,
		&plan.CreatedAt,
		&plan.ValidatedAt,
		&plan.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution plan: %w", err)
	}

	return &plan, nil
}

// GetByExecutionID retrieves a plan by execution ID
func (s *ExecutionPlanStore) GetByExecutionID(ctx context.Context, executionID string) (*storage.ExecutionPlan, error) {
	query := `
		SELECT id, execution_id, correlation_id, approval_id, draft_id, status,
		       total_steps, estimated_duration_seconds, risk_score, affected_resources,
		       rollback_available, created_at, validated_at, version
		FROM execution_plans
		WHERE execution_id = ?
	`

	row := s.db.QueryRowContext(ctx, query, executionID)

	var plan storage.ExecutionPlan
	err := row.Scan(
		&plan.ID,
		&plan.ExecutionID,
		&plan.CorrelationID,
		&plan.ApprovalID,
		&plan.DraftID,
		&plan.Status,
		&plan.TotalSteps,
		&plan.EstimatedDuration,
		&plan.RiskScore,
		&plan.AffectedResources,
		&plan.RollbackAvailable,
		&plan.CreatedAt,
		&plan.ValidatedAt,
		&plan.Version,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get execution plan: %w", err)
	}

	return &plan, nil
}

// ListByStatus retrieves plans by status
func (s *ExecutionPlanStore) ListByStatus(ctx context.Context, status string) ([]*storage.ExecutionPlan, error) {
	query := `
		SELECT id, execution_id, correlation_id, approval_id, draft_id, status,
		       total_steps, estimated_duration_seconds, risk_score, affected_resources,
		       rollback_available, created_at, validated_at, version
		FROM execution_plans
		WHERE status = ?
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution plans: %w", err)
	}
	defer rows.Close()

	var plans []*storage.ExecutionPlan
	for rows.Next() {
		var plan storage.ExecutionPlan
		err := rows.Scan(
			&plan.ID,
			&plan.ExecutionID,
			&plan.CorrelationID,
			&plan.ApprovalID,
			&plan.DraftID,
			&plan.Status,
			&plan.TotalSteps,
			&plan.EstimatedDuration,
			&plan.RiskScore,
			&plan.AffectedResources,
			&plan.RollbackAvailable,
			&plan.CreatedAt,
			&plan.ValidatedAt,
			&plan.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution plan: %w", err)
		}
		plans = append(plans, &plan)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution plans: %w", err)
	}

	return plans, nil
}

// List retrieves paginated execution plans
func (s *ExecutionPlanStore) List(ctx context.Context, limit int, offset int) ([]*storage.ExecutionPlan, error) {
	query := `
		SELECT id, execution_id, correlation_id, approval_id, draft_id, status,
		       total_steps, estimated_duration_seconds, risk_score, affected_resources,
		       rollback_available, created_at, validated_at, version
		FROM execution_plans
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list execution plans: %w", err)
	}
	defer rows.Close()

	var plans []*storage.ExecutionPlan
	for rows.Next() {
		var plan storage.ExecutionPlan
		err := rows.Scan(
			&plan.ID,
			&plan.ExecutionID,
			&plan.CorrelationID,
			&plan.ApprovalID,
			&plan.DraftID,
			&plan.Status,
			&plan.TotalSteps,
			&plan.EstimatedDuration,
			&plan.RiskScore,
			&plan.AffectedResources,
			&plan.RollbackAvailable,
			&plan.CreatedAt,
			&plan.ValidatedAt,
			&plan.Version,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution plan: %w", err)
		}
		plans = append(plans, &plan)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading execution plans: %w", err)
	}

	return plans, nil
}

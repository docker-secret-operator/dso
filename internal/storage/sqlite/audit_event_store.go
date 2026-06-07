package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/execution"
)

// AuditEventStore implements append-only audit event persistence
type AuditEventStore struct {
	db *sql.DB
}

// NewAuditEventStore creates a new audit event store
func NewAuditEventStore(db *sql.DB) *AuditEventStore {
	return &AuditEventStore{db: db}
}

// Create persists an audit event (append-only)
func (s *AuditEventStore) Create(ctx context.Context, event *execution.OrchestrationAuditEvent) error {
	query := `
		INSERT INTO audit_events (
			id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		event.ID,
		event.ExecutionID,
		event.CorrelationID,
		event.Action,
		event.Status,
		event.Details,
		event.ResourceID,
		event.ResourceType,
		event.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	return nil
}

// GetByID retrieves a single audit event
func (s *AuditEventStore) GetByID(ctx context.Context, id string) (*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		WHERE id = ?
	`

	var event execution.OrchestrationAuditEvent
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&event.ID,
		&event.ExecutionID,
		&event.CorrelationID,
		&event.Action,
		&event.Status,
		&event.Details,
		&event.ResourceID,
		&event.ResourceType,
		&event.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit event: %w", err)
	}

	return &event, nil
}

// ListByCorrelationID retrieves all events for a correlation ID
func (s *AuditEventStore) ListByCorrelationID(ctx context.Context, correlationID string) ([]*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		WHERE correlation_id = ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, correlationID)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*execution.OrchestrationAuditEvent
	for rows.Next() {
		var event execution.OrchestrationAuditEvent
		err := rows.Scan(
			&event.ID,
			&event.ExecutionID,
			&event.CorrelationID,
			&event.Action,
			&event.Status,
			&event.Details,
			&event.ResourceID,
			&event.ResourceType,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// ListByExecutionID retrieves all events for an execution ID
func (s *AuditEventStore) ListByExecutionID(ctx context.Context, executionID string) ([]*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		WHERE execution_id = ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, executionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*execution.OrchestrationAuditEvent
	for rows.Next() {
		var event execution.OrchestrationAuditEvent
		err := rows.Scan(
			&event.ID,
			&event.ExecutionID,
			&event.CorrelationID,
			&event.Action,
			&event.Status,
			&event.Details,
			&event.ResourceID,
			&event.ResourceType,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// ListByAction retrieves events by action type
func (s *AuditEventStore) ListByAction(ctx context.Context, action string, limit int) ([]*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		WHERE action = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, action, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*execution.OrchestrationAuditEvent
	for rows.Next() {
		var event execution.OrchestrationAuditEvent
		err := rows.Scan(
			&event.ID,
			&event.ExecutionID,
			&event.CorrelationID,
			&event.Action,
			&event.Status,
			&event.Details,
			&event.ResourceID,
			&event.ResourceType,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// ListAfterTimestamp retrieves events after a specific timestamp
func (s *AuditEventStore) ListAfterTimestamp(ctx context.Context, timestamp time.Time, limit int) ([]*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		WHERE timestamp > ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, timestamp, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*execution.OrchestrationAuditEvent
	for rows.Next() {
		var event execution.OrchestrationAuditEvent
		err := rows.Scan(
			&event.ID,
			&event.ExecutionID,
			&event.CorrelationID,
			&event.Action,
			&event.Status,
			&event.Details,
			&event.ResourceID,
			&event.ResourceType,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// List retrieves all audit events
func (s *AuditEventStore) List(ctx context.Context, limit int, offset int) ([]*execution.OrchestrationAuditEvent, error) {
	query := `
		SELECT id, execution_id, correlation_id, action, status, details, resource_id, resource_type, timestamp
		FROM audit_events
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*execution.OrchestrationAuditEvent
	for rows.Next() {
		var event execution.OrchestrationAuditEvent
		err := rows.Scan(
			&event.ID,
			&event.ExecutionID,
			&event.CorrelationID,
			&event.Action,
			&event.Status,
			&event.Details,
			&event.ResourceID,
			&event.ResourceType,
			&event.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

// Count returns total audit event count
func (s *AuditEventStore) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM audit_events`

	var count int
	err := s.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit events: %w", err)
	}

	return count, nil
}

// DeleteOlderThan deletes events older than specified duration (for cleanup)
func (s *AuditEventStore) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	query := `DELETE FROM audit_events WHERE timestamp < ?`

	result, err := s.db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old audit events: %w", err)
	}

	return result.RowsAffected()
}

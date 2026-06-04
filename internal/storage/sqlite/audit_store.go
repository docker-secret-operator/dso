package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// AuditStore implements storage.AuditStore for SQLite (append-only)
type AuditStore struct {
	db DBProvider
}

// Log appends a new audit event (no updates allowed)
func (as *AuditStore) Log(ctx context.Context, event *storage.AuditEvent) error {
	if event.ID == "" {
		return fmt.Errorf("event ID cannot be empty")
	}
	if event.ActorID == "" {
		return fmt.Errorf("actor ID cannot be empty")
	}
	if event.Action == "" {
		return fmt.Errorf("action cannot be empty")
	}
	if event.ResourceID == "" {
		return fmt.Errorf("resource ID cannot be empty")
	}
	if event.ResourceType == "" {
		return fmt.Errorf("resource type cannot be empty")
	}
	if event.CorrelationID == "" {
		return fmt.Errorf("correlation ID cannot be empty")
	}
	if event.RequestID == "" {
		return fmt.Errorf("request ID cannot be empty")
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Status == "" {
		event.Status = "success"
	}
	if event.Severity == "" {
		event.Severity = "info"
	}

	query := `
		INSERT INTO audit_events (id, timestamp, actor_id, actor_name, actor_email, action, resource, resource_id, resource_type, status, result_code, result_message, old_value, new_value, delta, correlation_id, request_id, ip_address, user_agent, severity, retention_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := as.db.ExecContext(ctx, query,
		event.ID,
		event.Timestamp,
		event.ActorID,
		event.ActorName,
		event.ActorEmail,
		event.Action,
		event.Resource,
		event.ResourceID,
		event.ResourceType,
		event.Status,
		event.ResultCode,
		event.ResultMessage,
		event.OldValue,
		event.NewValue,
		event.Delta,
		event.CorrelationID,
		event.RequestID,
		event.IPAddress,
		event.UserAgent,
		event.Severity,
		event.RetentionUntil,
	)

	if err != nil {
		return fmt.Errorf("failed to log audit event: %w", err)
	}

	return nil
}

// Query retrieves audit events matching filters
func (as *AuditStore) Query(ctx context.Context, filters map[string]interface{}) ([]*storage.AuditEvent, error) {
	query := `
		SELECT id, timestamp, actor_id, actor_name, actor_email, action, resource, resource_id, resource_type, status, result_code, result_message, old_value, new_value, delta, correlation_id, request_id, ip_address, user_agent, severity, retention_until
		FROM audit_events
		WHERE 1=1
	`

	var args []interface{}

	// Build dynamic WHERE clause
	if actorID, ok := filters["actor_id"]; ok {
		query += " AND actor_id = ?"
		args = append(args, actorID)
	}
	if action, ok := filters["action"]; ok {
		query += " AND action = ?"
		args = append(args, action)
	}
	if resourceID, ok := filters["resource_id"]; ok {
		query += " AND resource_id = ?"
		args = append(args, resourceID)
	}
	if resourceType, ok := filters["resource_type"]; ok {
		query += " AND resource_type = ?"
		args = append(args, resourceType)
	}
	if status, ok := filters["status"]; ok {
		query += " AND status = ?"
		args = append(args, status)
	}
	if startTime, ok := filters["start_time"]; ok {
		query += " AND timestamp >= ?"
		args = append(args, startTime)
	}
	if endTime, ok := filters["end_time"]; ok {
		query += " AND timestamp <= ?"
		args = append(args, endTime)
	}

	query += " ORDER BY timestamp DESC LIMIT 10000"

	rows, err := as.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	var events []*storage.AuditEvent
	for rows.Next() {
		event := &storage.AuditEvent{}
		if err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.ActorID,
			&event.ActorName,
			&event.ActorEmail,
			&event.Action,
			&event.Resource,
			&event.ResourceID,
			&event.ResourceType,
			&event.Status,
			&event.ResultCode,
			&event.ResultMessage,
			&event.OldValue,
			&event.NewValue,
			&event.Delta,
			&event.CorrelationID,
			&event.RequestID,
			&event.IPAddress,
			&event.UserAgent,
			&event.Severity,
			&event.RetentionUntil,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit events: %w", err)
	}

	return events, nil
}

// Export exports audit events within a time range
func (as *AuditStore) Export(ctx context.Context, startTime, endTime time.Time) ([]*storage.AuditEvent, error) {
	query := `
		SELECT id, timestamp, actor_id, actor_name, actor_email, action, resource, resource_id, resource_type, status, result_code, result_message, old_value, new_value, delta, correlation_id, request_id, ip_address, user_agent, severity, retention_until
		FROM audit_events
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`

	rows, err := as.db.QueryContext(ctx, query, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to export audit events: %w", err)
	}
	defer rows.Close()

	var events []*storage.AuditEvent
	for rows.Next() {
		event := &storage.AuditEvent{}
		if err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.ActorID,
			&event.ActorName,
			&event.ActorEmail,
			&event.Action,
			&event.Resource,
			&event.ResourceID,
			&event.ResourceType,
			&event.Status,
			&event.ResultCode,
			&event.ResultMessage,
			&event.OldValue,
			&event.NewValue,
			&event.Delta,
			&event.CorrelationID,
			&event.RequestID,
			&event.IPAddress,
			&event.UserAgent,
			&event.Severity,
			&event.RetentionUntil,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating audit events: %w", err)
	}

	return events, nil
}

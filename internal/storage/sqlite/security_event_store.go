package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type SecurityEventStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *SecurityEventStore) Log(ctx context.Context, event *storage.SecurityEvent) error {
	query := `
		INSERT INTO security_events (id, type, severity, username, user_id, ip_address, user_agent, message, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, event.ID, event.Type, event.Severity, event.Username, event.UserID,
		event.IPAddress, event.UserAgent, event.Message, event.Metadata, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to log security event: %w", err)
	}
	return nil
}

func (s *SecurityEventStore) GetByID(ctx context.Context, id string) (*storage.SecurityEvent, error) {
	query := `SELECT id, type, severity, username, user_id, ip_address, user_agent, message, metadata, created_at FROM security_events WHERE id = ?`

	var event storage.SecurityEvent
	var userID sql.NullString
	var userAgent sql.NullString
	var metadata sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&event.ID, &event.Type, &event.Severity, &event.Username, &userID,
		&event.IPAddress, &userAgent, &event.Message, &metadata, &event.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get security event: %w", err)
	}

	if userID.Valid {
		event.UserID = &userID.String
	}
	if userAgent.Valid {
		event.UserAgent = &userAgent.String
	}
	if metadata.Valid {
		event.Metadata = &metadata.String
	}

	return &event, nil
}

func (s *SecurityEventStore) Query(ctx context.Context, filters map[string]interface{}) ([]*storage.SecurityEvent, error) {
	query := `SELECT id, type, severity, username, user_id, ip_address, user_agent, message, metadata, created_at FROM security_events WHERE 1=1`
	var args []interface{}

	if severity, ok := filters["severity"].(string); ok && severity != "" {
		query += ` AND severity = ?`
		args = append(args, severity)
	}

	if eventType, ok := filters["type"].(string); ok && eventType != "" {
		query += ` AND type = ?`
		args = append(args, eventType)
	}

	if actor, ok := filters["actor"].(string); ok && actor != "" {
		query += ` AND username = ?`
		args = append(args, actor)
	}

	if ip, ok := filters["ip_address"].(string); ok && ip != "" {
		query += ` AND ip_address = ?`
		args = append(args, ip)
	}

	if username, ok := filters["username"].(string); ok && username != "" {
		query += ` AND username = ?`
		args = append(args, username)
	}

	if startTime, ok := filters["start_time"].(time.Time); ok && !startTime.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, startTime)
	}

	if endTime, ok := filters["end_time"].(time.Time); ok && !endTime.IsZero() {
		query += ` AND created_at <= ?`
		args = append(args, endTime)
	}

	query += ` ORDER BY created_at DESC`

	if limit, ok := filters["limit"].(int); ok && limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	} else if limit, ok := filters["limit"].(float64); ok && limit > 0 {
		query += ` LIMIT ?`
		args = append(args, int(limit))
	}

	if offset, ok := filters["offset"].(int); ok && offset >= 0 {
		query += ` OFFSET ?`
		args = append(args, offset)
	} else if offset, ok := filters["offset"].(float64); ok && offset >= 0 {
		query += ` OFFSET ?`
		args = append(args, int(offset))
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query security events: %w", err)
	}
	defer rows.Close()

	var events []*storage.SecurityEvent
	for rows.Next() {
		var event storage.SecurityEvent
		var userID sql.NullString
		var userAgent sql.NullString
		var metadata sql.NullString

		if err := rows.Scan(&event.ID, &event.Type, &event.Severity, &event.Username, &userID,
			&event.IPAddress, &userAgent, &event.Message, &metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan security event: %w", err)
		}

		if userID.Valid {
			event.UserID = &userID.String
		}
		if userAgent.Valid {
			event.UserAgent = &userAgent.String
		}
		if metadata.Valid {
			event.Metadata = &metadata.String
		}

		events = append(events, &event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning security events: %w", err)
	}

	return events, nil
}

func (s *SecurityEventStore) List(ctx context.Context, limit int, offset int) ([]*storage.SecurityEvent, error) {
	query := `SELECT id, type, severity, username, user_id, ip_address, user_agent, message, metadata, created_at FROM security_events ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list security events: %w", err)
	}
	defer rows.Close()

	var events []*storage.SecurityEvent
	for rows.Next() {
		var event storage.SecurityEvent
		var userID sql.NullString
		var userAgent sql.NullString
		var metadata sql.NullString

		if err := rows.Scan(&event.ID, &event.Type, &event.Severity, &event.Username, &userID,
			&event.IPAddress, &userAgent, &event.Message, &metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan security event: %w", err)
		}

		if userID.Valid {
			event.UserID = &userID.String
		}
		if userAgent.Valid {
			event.UserAgent = &userAgent.String
		}
		if metadata.Valid {
			event.Metadata = &metadata.String
		}

		events = append(events, &event)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning security events: %w", err)
	}

	return events, nil
}

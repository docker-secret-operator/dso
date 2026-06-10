package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type SecurityAlertStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *SecurityAlertStore) Create(ctx context.Context, alert *storage.SecurityAlert) error {
	query := `
		INSERT INTO security_alerts (id, type, severity, state, title, message, affected_user, ip_address,
			details, acknowledged_by, acknowledged_at, resolved_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, alert.ID, alert.Type, alert.Severity, alert.State,
		alert.Title, alert.Message, alert.AffectedUser, alert.IPAddress, alert.Details,
		alert.AcknowledgedBy, alert.AcknowledgedAt, alert.ResolvedAt, alert.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create security alert: %w", err)
	}
	return nil
}

func (s *SecurityAlertStore) Update(ctx context.Context, alert *storage.SecurityAlert) error {
	query := `
		UPDATE security_alerts
		SET type = ?, severity = ?, state = ?, title = ?, message = ?, affected_user = ?,
			ip_address = ?, details = ?, acknowledged_by = ?, acknowledged_at = ?, resolved_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, alert.Type, alert.Severity, alert.State,
		alert.Title, alert.Message, alert.AffectedUser, alert.IPAddress, alert.Details,
		alert.AcknowledgedBy, alert.AcknowledgedAt, alert.ResolvedAt, alert.ID)
	if err != nil {
		return fmt.Errorf("failed to update security alert: %w", err)
	}
	return nil
}

func (s *SecurityAlertStore) GetByID(ctx context.Context, id string) (*storage.SecurityAlert, error) {
	query := `SELECT id, type, severity, state, title, message, affected_user, ip_address, details,
		acknowledged_by, acknowledged_at, resolved_at, created_at FROM security_alerts WHERE id = ?`

	var alert storage.SecurityAlert
	var affectedUser sql.NullString
	var ipAddress sql.NullString
	var details sql.NullString
	var acknowledgedBy sql.NullString
	var acknowledgedAt sql.NullTime
	var resolvedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(&alert.ID, &alert.Type, &alert.Severity,
		&alert.State, &alert.Title, &alert.Message, &affectedUser, &ipAddress, &details,
		&acknowledgedBy, &acknowledgedAt, &resolvedAt, &alert.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get security alert: %w", err)
	}

	if affectedUser.Valid {
		alert.AffectedUser = &affectedUser.String
	}
	if ipAddress.Valid {
		alert.IPAddress = &ipAddress.String
	}
	if details.Valid {
		alert.Details = &details.String
	}
	if acknowledgedBy.Valid {
		alert.AcknowledgedBy = &acknowledgedBy.String
	}
	if acknowledgedAt.Valid {
		alert.AcknowledgedAt = &acknowledgedAt.Time
	}
	if resolvedAt.Valid {
		alert.ResolvedAt = &resolvedAt.Time
	}

	return &alert, nil
}

func (s *SecurityAlertStore) List(ctx context.Context, limit int, offset int) ([]*storage.SecurityAlert, error) {
	query := `SELECT id, type, severity, state, title, message, affected_user, ip_address, details,
		acknowledged_by, acknowledged_at, resolved_at, created_at FROM security_alerts
		ORDER BY created_at DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list security alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*storage.SecurityAlert
	for rows.Next() {
		var alert storage.SecurityAlert
		var affectedUser sql.NullString
		var ipAddress sql.NullString
		var details sql.NullString
		var acknowledgedBy sql.NullString
		var acknowledgedAt sql.NullTime
		var resolvedAt sql.NullTime

		if err := rows.Scan(&alert.ID, &alert.Type, &alert.Severity, &alert.State, &alert.Title,
			&alert.Message, &affectedUser, &ipAddress, &details, &acknowledgedBy, &acknowledgedAt,
			&resolvedAt, &alert.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan security alert: %w", err)
		}

		if affectedUser.Valid {
			alert.AffectedUser = &affectedUser.String
		}
		if ipAddress.Valid {
			alert.IPAddress = &ipAddress.String
		}
		if details.Valid {
			alert.Details = &details.String
		}
		if acknowledgedBy.Valid {
			alert.AcknowledgedBy = &acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			alert.AcknowledgedAt = &acknowledgedAt.Time
		}
		if resolvedAt.Valid {
			alert.ResolvedAt = &resolvedAt.Time
		}

		alerts = append(alerts, &alert)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning security alerts: %w", err)
	}

	return alerts, nil
}

func (s *SecurityAlertStore) ListByState(ctx context.Context, state string) ([]*storage.SecurityAlert, error) {
	query := `SELECT id, type, severity, state, title, message, affected_user, ip_address, details,
		acknowledged_by, acknowledged_at, resolved_at, created_at FROM security_alerts
		WHERE state = ? ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, state)
	if err != nil {
		return nil, fmt.Errorf("failed to list security alerts by state: %w", err)
	}
	defer rows.Close()

	var alerts []*storage.SecurityAlert
	for rows.Next() {
		var alert storage.SecurityAlert
		var affectedUser sql.NullString
		var ipAddress sql.NullString
		var details sql.NullString
		var acknowledgedBy sql.NullString
		var acknowledgedAt sql.NullTime
		var resolvedAt sql.NullTime

		if err := rows.Scan(&alert.ID, &alert.Type, &alert.Severity, &alert.State, &alert.Title,
			&alert.Message, &affectedUser, &ipAddress, &details, &acknowledgedBy, &acknowledgedAt,
			&resolvedAt, &alert.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan security alert: %w", err)
		}

		if affectedUser.Valid {
			alert.AffectedUser = &affectedUser.String
		}
		if ipAddress.Valid {
			alert.IPAddress = &ipAddress.String
		}
		if details.Valid {
			alert.Details = &details.String
		}
		if acknowledgedBy.Valid {
			alert.AcknowledgedBy = &acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			alert.AcknowledgedAt = &acknowledgedAt.Time
		}
		if resolvedAt.Valid {
			alert.ResolvedAt = &resolvedAt.Time
		}

		alerts = append(alerts, &alert)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning security alerts: %w", err)
	}

	return alerts, nil
}

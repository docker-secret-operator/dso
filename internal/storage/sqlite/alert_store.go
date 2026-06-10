package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type AlertStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *AlertStore) Create(ctx context.Context, alert *storage.Alert) error {
	query := `
		INSERT INTO alerts (id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, alert.ID, alert.RuleID, alert.State, alert.Severity,
		alert.Metric, alert.Message, alert.Value, alert.Threshold, alert.AcknowledgedBy,
		alert.AcknowledgedAt, alert.ResolvedBy, alert.ResolvedAt, alert.SuppressedBy,
		alert.SuppressedUntil, alert.LastFiredAt, alert.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create alert: %w", err)
	}
	return nil
}

func (s *AlertStore) Update(ctx context.Context, alert *storage.Alert) error {
	query := `
		UPDATE alerts
		SET state = ?, severity = ?, message = ?, value = ?, acknowledged_by = ?,
			acknowledged_at = ?, resolved_by = ?, resolved_at = ?, suppressed_by = ?,
			suppressed_until = ?, last_fired_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, alert.State, alert.Severity, alert.Message, alert.Value,
		alert.AcknowledgedBy, alert.AcknowledgedAt, alert.ResolvedBy, alert.ResolvedAt,
		alert.SuppressedBy, alert.SuppressedUntil, alert.LastFiredAt, alert.ID)
	if err != nil {
		return fmt.Errorf("failed to update alert: %w", err)
	}
	return nil
}

func (s *AlertStore) GetByID(ctx context.Context, id string) (*storage.Alert, error) {
	query := `
		SELECT id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at
		FROM alerts WHERE id = ?
	`

	var alert storage.Alert
	var acknowledgedBy sql.NullString
	var acknowledgedAt sql.NullTime
	var resolvedBy sql.NullString
	var resolvedAt sql.NullTime
	var suppressedBy sql.NullString
	var suppressedUntil sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(&alert.ID, &alert.RuleID, &alert.State,
		&alert.Severity, &alert.Metric, &alert.Message, &alert.Value, &alert.Threshold,
		&acknowledgedBy, &acknowledgedAt, &resolvedBy, &resolvedAt, &suppressedBy,
		&suppressedUntil, &alert.LastFiredAt, &alert.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get alert: %w", err)
	}

	if acknowledgedBy.Valid {
		alert.AcknowledgedBy = &acknowledgedBy.String
	}
	if acknowledgedAt.Valid {
		alert.AcknowledgedAt = &acknowledgedAt.Time
	}
	if resolvedBy.Valid {
		alert.ResolvedBy = &resolvedBy.String
	}
	if resolvedAt.Valid {
		alert.ResolvedAt = &resolvedAt.Time
	}
	if suppressedBy.Valid {
		alert.SuppressedBy = &suppressedBy.String
	}
	if suppressedUntil.Valid {
		alert.SuppressedUntil = &suppressedUntil.Time
	}

	return &alert, nil
}

func (s *AlertStore) List(ctx context.Context, limit int, offset int) ([]*storage.Alert, error) {
	query := `
		SELECT id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at
		FROM alerts ORDER BY created_at DESC LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}
	defer rows.Close()

	return s.scanAlerts(rows)
}

func (s *AlertStore) ListByState(ctx context.Context, state string, limit int, offset int) ([]*storage.Alert, error) {
	query := `
		SELECT id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at
		FROM alerts WHERE state = ? ORDER BY created_at DESC LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, state, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts by state: %w", err)
	}
	defer rows.Close()

	return s.scanAlerts(rows)
}

func (s *AlertStore) ListByRuleID(ctx context.Context, ruleID string) ([]*storage.Alert, error) {
	query := `
		SELECT id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at
		FROM alerts WHERE rule_id = ? ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, ruleID)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts by rule: %w", err)
	}
	defer rows.Close()

	return s.scanAlerts(rows)
}

func (s *AlertStore) GetActiveByRuleID(ctx context.Context, ruleID string) (*storage.Alert, error) {
	query := `
		SELECT id, rule_id, state, severity, metric, message, value, threshold,
			acknowledged_by, acknowledged_at, resolved_by, resolved_at, suppressed_by,
			suppressed_until, last_fired_at, created_at
		FROM alerts WHERE rule_id = ? AND state = 'active' LIMIT 1
	`

	var alert storage.Alert
	var acknowledgedBy sql.NullString
	var acknowledgedAt sql.NullTime
	var resolvedBy sql.NullString
	var resolvedAt sql.NullTime
	var suppressedBy sql.NullString
	var suppressedUntil sql.NullTime

	err := s.db.QueryRowContext(ctx, query, ruleID).Scan(&alert.ID, &alert.RuleID, &alert.State,
		&alert.Severity, &alert.Metric, &alert.Message, &alert.Value, &alert.Threshold,
		&acknowledgedBy, &acknowledgedAt, &resolvedBy, &resolvedAt, &suppressedBy,
		&suppressedUntil, &alert.LastFiredAt, &alert.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get active alert: %w", err)
	}

	if acknowledgedBy.Valid {
		alert.AcknowledgedBy = &acknowledgedBy.String
	}
	if acknowledgedAt.Valid {
		alert.AcknowledgedAt = &acknowledgedAt.Time
	}
	if resolvedBy.Valid {
		alert.ResolvedBy = &resolvedBy.String
	}
	if resolvedAt.Valid {
		alert.ResolvedAt = &resolvedAt.Time
	}
	if suppressedBy.Valid {
		alert.SuppressedBy = &suppressedBy.String
	}
	if suppressedUntil.Valid {
		alert.SuppressedUntil = &suppressedUntil.Time
	}

	return &alert, nil
}

func (s *AlertStore) scanAlerts(rows *sql.Rows) ([]*storage.Alert, error) {
	var alerts []*storage.Alert
	for rows.Next() {
		var alert storage.Alert
		var acknowledgedBy sql.NullString
		var acknowledgedAt sql.NullTime
		var resolvedBy sql.NullString
		var resolvedAt sql.NullTime
		var suppressedBy sql.NullString
		var suppressedUntil sql.NullTime

		if err := rows.Scan(&alert.ID, &alert.RuleID, &alert.State, &alert.Severity,
			&alert.Metric, &alert.Message, &alert.Value, &alert.Threshold,
			&acknowledgedBy, &acknowledgedAt, &resolvedBy, &resolvedAt, &suppressedBy,
			&suppressedUntil, &alert.LastFiredAt, &alert.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}

		if acknowledgedBy.Valid {
			alert.AcknowledgedBy = &acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			alert.AcknowledgedAt = &acknowledgedAt.Time
		}
		if resolvedBy.Valid {
			alert.ResolvedBy = &resolvedBy.String
		}
		if resolvedAt.Valid {
			alert.ResolvedAt = &resolvedAt.Time
		}
		if suppressedBy.Valid {
			alert.SuppressedBy = &suppressedBy.String
		}
		if suppressedUntil.Valid {
			alert.SuppressedUntil = &suppressedUntil.Time
		}

		alerts = append(alerts, &alert)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning alerts: %w", err)
	}

	return alerts, nil
}

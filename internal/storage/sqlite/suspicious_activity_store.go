package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type SuspiciousActivityStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *SuspiciousActivityStore) Create(ctx context.Context, activity *storage.SuspiciousActivity) error {
	query := `
		INSERT INTO suspicious_activities (id, type, severity, ip_address, usernames, first_seen, last_seen,
			occurrence_count, message, metadata, acknowledged_by, acknowledged_at, ignored_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, activity.ID, activity.Type, activity.Severity, activity.IPAddress,
		activity.Usernames, activity.FirstSeen, activity.LastSeen, activity.OccurrenceCount, activity.Message,
		activity.Metadata, activity.AcknowledgedBy, activity.AcknowledgedAt, activity.IgnoredAt, activity.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create suspicious activity: %w", err)
	}
	return nil
}

func (s *SuspiciousActivityStore) Update(ctx context.Context, activity *storage.SuspiciousActivity) error {
	query := `
		UPDATE suspicious_activities
		SET type = ?, severity = ?, ip_address = ?, usernames = ?, first_seen = ?, last_seen = ?,
			occurrence_count = ?, message = ?, metadata = ?, acknowledged_by = ?, acknowledged_at = ?,
			ignored_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, activity.Type, activity.Severity, activity.IPAddress,
		activity.Usernames, activity.FirstSeen, activity.LastSeen, activity.OccurrenceCount, activity.Message,
		activity.Metadata, activity.AcknowledgedBy, activity.AcknowledgedAt, activity.IgnoredAt, activity.ID)
	if err != nil {
		return fmt.Errorf("failed to update suspicious activity: %w", err)
	}
	return nil
}

func (s *SuspiciousActivityStore) GetByID(ctx context.Context, id string) (*storage.SuspiciousActivity, error) {
	query := `SELECT id, type, severity, ip_address, usernames, first_seen, last_seen, occurrence_count, message,
		metadata, acknowledged_by, acknowledged_at, ignored_at, created_at FROM suspicious_activities WHERE id = ?`

	var activity storage.SuspiciousActivity
	var ipAddress sql.NullString
	var usernames sql.NullString
	var metadata sql.NullString
	var acknowledgedBy sql.NullString
	var acknowledgedAt sql.NullTime
	var ignoredAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(&activity.ID, &activity.Type, &activity.Severity,
		&ipAddress, &usernames, &activity.FirstSeen, &activity.LastSeen, &activity.OccurrenceCount,
		&activity.Message, &metadata, &acknowledgedBy, &acknowledgedAt, &ignoredAt, &activity.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get suspicious activity: %w", err)
	}

	if ipAddress.Valid {
		activity.IPAddress = &ipAddress.String
	}
	if usernames.Valid {
		activity.Usernames = &usernames.String
	}
	if metadata.Valid {
		activity.Metadata = &metadata.String
	}
	if acknowledgedBy.Valid {
		activity.AcknowledgedBy = &acknowledgedBy.String
	}
	if acknowledgedAt.Valid {
		activity.AcknowledgedAt = &acknowledgedAt.Time
	}
	if ignoredAt.Valid {
		activity.IgnoredAt = &ignoredAt.Time
	}

	return &activity, nil
}

func (s *SuspiciousActivityStore) List(ctx context.Context, limit int, offset int) ([]*storage.SuspiciousActivity, error) {
	query := `SELECT id, type, severity, ip_address, usernames, first_seen, last_seen, occurrence_count, message,
		metadata, acknowledged_by, acknowledged_at, ignored_at, created_at FROM suspicious_activities
		ORDER BY last_seen DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list suspicious activities: %w", err)
	}
	defer rows.Close()

	var activities []*storage.SuspiciousActivity
	for rows.Next() {
		var activity storage.SuspiciousActivity
		var ipAddress sql.NullString
		var usernames sql.NullString
		var metadata sql.NullString
		var acknowledgedBy sql.NullString
		var acknowledgedAt sql.NullTime
		var ignoredAt sql.NullTime

		if err := rows.Scan(&activity.ID, &activity.Type, &activity.Severity, &ipAddress, &usernames,
			&activity.FirstSeen, &activity.LastSeen, &activity.OccurrenceCount, &activity.Message,
			&metadata, &acknowledgedBy, &acknowledgedAt, &ignoredAt, &activity.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan suspicious activity: %w", err)
		}

		if ipAddress.Valid {
			activity.IPAddress = &ipAddress.String
		}
		if usernames.Valid {
			activity.Usernames = &usernames.String
		}
		if metadata.Valid {
			activity.Metadata = &metadata.String
		}
		if acknowledgedBy.Valid {
			activity.AcknowledgedBy = &acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			activity.AcknowledgedAt = &acknowledgedAt.Time
		}
		if ignoredAt.Valid {
			activity.IgnoredAt = &ignoredAt.Time
		}

		activities = append(activities, &activity)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning suspicious activities: %w", err)
	}

	return activities, nil
}

func (s *SuspiciousActivityStore) ListUnacknowledged(ctx context.Context) ([]*storage.SuspiciousActivity, error) {
	query := `SELECT id, type, severity, ip_address, usernames, first_seen, last_seen, occurrence_count, message,
		metadata, acknowledged_by, acknowledged_at, ignored_at, created_at FROM suspicious_activities
		WHERE acknowledged_at IS NULL AND ignored_at IS NULL ORDER BY last_seen DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list unacknowledged suspicious activities: %w", err)
	}
	defer rows.Close()

	var activities []*storage.SuspiciousActivity
	for rows.Next() {
		var activity storage.SuspiciousActivity
		var ipAddress sql.NullString
		var usernames sql.NullString
		var metadata sql.NullString
		var acknowledgedBy sql.NullString
		var acknowledgedAt sql.NullTime
		var ignoredAt sql.NullTime

		if err := rows.Scan(&activity.ID, &activity.Type, &activity.Severity, &ipAddress, &usernames,
			&activity.FirstSeen, &activity.LastSeen, &activity.OccurrenceCount, &activity.Message,
			&metadata, &acknowledgedBy, &acknowledgedAt, &ignoredAt, &activity.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan suspicious activity: %w", err)
		}

		if ipAddress.Valid {
			activity.IPAddress = &ipAddress.String
		}
		if usernames.Valid {
			activity.Usernames = &usernames.String
		}
		if metadata.Valid {
			activity.Metadata = &metadata.String
		}
		if acknowledgedBy.Valid {
			activity.AcknowledgedBy = &acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			activity.AcknowledgedAt = &acknowledgedAt.Time
		}
		if ignoredAt.Valid {
			activity.IgnoredAt = &ignoredAt.Time
		}

		activities = append(activities, &activity)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning suspicious activities: %w", err)
	}

	return activities, nil
}

package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// ReviewActivityStore implements storage.ReviewActivityStore for SQLite
type ReviewActivityStore struct {
	db DBProvider
}

// Log logs a review activity
func (ras *ReviewActivityStore) Log(ctx context.Context, activity *storage.ReviewActivity) error {
	if activity.ID == "" {
		return fmt.Errorf("activity ID cannot be empty")
	}
	if activity.ReviewID == "" {
		return fmt.Errorf("review ID cannot be empty")
	}
	if activity.Type == "" {
		return fmt.Errorf("activity type cannot be empty")
	}
	if activity.ActorID == "" {
		return fmt.Errorf("actor ID cannot be empty")
	}

	if activity.Timestamp.IsZero() {
		activity.Timestamp = time.Now()
	}

	query := `
		INSERT INTO review_activities (id, review_id, type, actor_id, description, metadata, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := ras.db.ExecContext(ctx, query,
		activity.ID,
		activity.ReviewID,
		activity.Type,
		activity.ActorID,
		activity.Description,
		activity.Metadata,
		activity.Timestamp,
	)

	if err != nil {
		return fmt.Errorf("failed to log review activity: %w", err)
	}

	return nil
}

// ListForReview retrieves all activities for a review
func (ras *ReviewActivityStore) ListForReview(ctx context.Context, reviewID string) ([]*storage.ReviewActivity, error) {
	query := `
		SELECT id, review_id, type, actor_id, description, metadata, timestamp
		FROM review_activities
		WHERE review_id = ?
		ORDER BY timestamp DESC
	`

	rows, err := ras.db.QueryContext(ctx, query, reviewID)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities: %w", err)
	}
	defer rows.Close()

	var activities []*storage.ReviewActivity
	for rows.Next() {
		activity := &storage.ReviewActivity{}
		if err := rows.Scan(
			&activity.ID,
			&activity.ReviewID,
			&activity.Type,
			&activity.ActorID,
			&activity.Description,
			&activity.Metadata,
			&activity.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}
		activities = append(activities, activity)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating activities: %w", err)
	}

	return activities, nil
}

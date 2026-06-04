package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// SnapshotStore implements storage.SnapshotStore for SQLite
type SnapshotStore struct {
	db DBProvider
}

// Create creates a new snapshot
func (ss *SnapshotStore) Create(ctx context.Context, snapshot *storage.Snapshot) error {
	if snapshot.ID == "" {
		return fmt.Errorf("snapshot ID cannot be empty")
	}
	if snapshot.DraftID == "" {
		return fmt.Errorf("draft ID cannot be empty")
	}
	if snapshot.Source == "" {
		return fmt.Errorf("source cannot be empty")
	}

	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO snapshots (id, draft_id, config, checksum, source, source_id, description, tags, verified, applied, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := ss.db.ExecContext(ctx, query,
		snapshot.ID,
		snapshot.DraftID,
		snapshot.Config,
		snapshot.Checksum,
		snapshot.Source,
		snapshot.SourceID,
		snapshot.Description,
		snapshot.Tags,
		snapshot.Verified,
		snapshot.Applied,
		snapshot.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	return nil
}

// GetByID retrieves a snapshot by ID
func (ss *SnapshotStore) GetByID(ctx context.Context, id string) (*storage.Snapshot, error) {
	query := `
		SELECT id, draft_id, config, checksum, source, source_id, description, tags, verified, applied, created_at
		FROM snapshots
		WHERE id = ?
	`

	snapshot := &storage.Snapshot{}
	err := ss.db.QueryRowContext(ctx, query, id).Scan(
		&snapshot.ID,
		&snapshot.DraftID,
		&snapshot.Config,
		&snapshot.Checksum,
		&snapshot.Source,
		&snapshot.SourceID,
		&snapshot.Description,
		&snapshot.Tags,
		&snapshot.Verified,
		&snapshot.Applied,
		&snapshot.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	return snapshot, nil
}

// ListForDraft retrieves all snapshots for a draft
func (ss *SnapshotStore) ListForDraft(ctx context.Context, draftID string) ([]*storage.Snapshot, error) {
	query := `
		SELECT id, draft_id, config, checksum, source, source_id, description, tags, verified, applied, created_at
		FROM snapshots
		WHERE draft_id = ?
		ORDER BY created_at DESC
	`

	rows, err := ss.db.QueryContext(ctx, query, draftID)
	if err != nil {
		return nil, fmt.Errorf("failed to query snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []*storage.Snapshot
	for rows.Next() {
		snapshot := &storage.Snapshot{}
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.DraftID,
			&snapshot.Config,
			&snapshot.Checksum,
			&snapshot.Source,
			&snapshot.SourceID,
			&snapshot.Description,
			&snapshot.Tags,
			&snapshot.Verified,
			&snapshot.Applied,
			&snapshot.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan snapshot: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating snapshots: %w", err)
	}

	return snapshots, nil
}

// Delete soft-deletes a snapshot
func (ss *SnapshotStore) Delete(ctx context.Context, id string) error {
	// For snapshots, we use hard delete since they're not critical
	query := `DELETE FROM snapshots WHERE id = ?`

	result, err := ss.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	return nil
}

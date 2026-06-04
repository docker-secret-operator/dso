package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// DraftStore implements storage.DraftStore for SQLite
type DraftStore struct {
	db DBProvider
}

// Create creates a new draft
func (ds *DraftStore) Create(ctx context.Context, draft *storage.Draft) error {
	if draft.ID == "" {
		return fmt.Errorf("draft ID cannot be empty")
	}
	if draft.WorkspaceID == "" {
		return fmt.Errorf("workspace ID cannot be empty")
	}
	if draft.OwnerID == "" {
		return fmt.Errorf("owner ID cannot be empty")
	}

	now := time.Now()
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	if draft.ModifiedAt.IsZero() {
		draft.ModifiedAt = now
	}
	if draft.Status == "" {
		draft.Status = "draft"
	}
	if draft.VersionNumber == 0 {
		draft.VersionNumber = 1
	}

	query := `
		INSERT INTO drafts (id, workspace_id, owner_id, title, description, status, version_number, config, checksum, created_at, modified_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := ds.db.ExecContext(ctx, query,
		draft.ID,
		draft.WorkspaceID,
		draft.OwnerID,
		draft.Title,
		draft.Description,
		draft.Status,
		draft.VersionNumber,
		draft.Config,
		draft.Checksum,
		draft.CreatedAt,
		draft.ModifiedAt,
		draft.ExpiresAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create draft: %w", err)
	}

	return nil
}

// Update updates an existing draft
func (ds *DraftStore) Update(ctx context.Context, draft *storage.Draft) error {
	if draft.ID == "" {
		return fmt.Errorf("draft ID cannot be empty")
	}

	draft.ModifiedAt = time.Now()

	query := `
		UPDATE drafts
		SET workspace_id = ?, owner_id = ?, title = ?, description = ?, status = ?,
		    version_number = ?, config = ?, checksum = ?, modified_at = ?, expires_at = ?
		WHERE id = ?
	`

	result, err := ds.db.ExecContext(ctx, query,
		draft.WorkspaceID,
		draft.OwnerID,
		draft.Title,
		draft.Description,
		draft.Status,
		draft.VersionNumber,
		draft.Config,
		draft.Checksum,
		draft.ModifiedAt,
		draft.ExpiresAt,
		draft.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update draft: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("draft not found: %s", draft.ID)
	}

	return nil
}

// GetByID retrieves a draft by ID
func (ds *DraftStore) GetByID(ctx context.Context, id string) (*storage.Draft, error) {
	query := `
		SELECT id, workspace_id, owner_id, title, description, status, version_number, config, checksum, created_at, modified_at, expires_at
		FROM drafts
		WHERE id = ?
	`

	draft := &storage.Draft{}
	err := ds.db.QueryRowContext(ctx, query, id).Scan(
		&draft.ID,
		&draft.WorkspaceID,
		&draft.OwnerID,
		&draft.Title,
		&draft.Description,
		&draft.Status,
		&draft.VersionNumber,
		&draft.Config,
		&draft.Checksum,
		&draft.CreatedAt,
		&draft.ModifiedAt,
		&draft.ExpiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("draft not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get draft: %w", err)
	}

	return draft, nil
}

// List retrieves all drafts for an owner
func (ds *DraftStore) List(ctx context.Context, ownerID string) ([]*storage.Draft, error) {
	query := `
		SELECT id, workspace_id, owner_id, title, description, status, version_number, config, checksum, created_at, modified_at, expires_at
		FROM drafts
		WHERE owner_id = ?
		ORDER BY created_at DESC
	`

	rows, err := ds.db.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query drafts: %w", err)
	}
	defer rows.Close()

	var drafts []*storage.Draft
	for rows.Next() {
		draft := &storage.Draft{}
		if err := rows.Scan(
			&draft.ID,
			&draft.WorkspaceID,
			&draft.OwnerID,
			&draft.Title,
			&draft.Description,
			&draft.Status,
			&draft.VersionNumber,
			&draft.Config,
			&draft.Checksum,
			&draft.CreatedAt,
			&draft.ModifiedAt,
			&draft.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan draft: %w", err)
		}
		drafts = append(drafts, draft)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating drafts: %w", err)
	}

	return drafts, nil
}

// Delete soft-deletes a draft
func (ds *DraftStore) Delete(ctx context.Context, id string) error {
	query := `
		UPDATE drafts
		SET status = 'archived', modified_at = ?
		WHERE id = ?
	`

	result, err := ds.db.ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to delete draft: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("draft not found: %s", id)
	}

	return nil
}

// SaveVersion saves a draft version
func (ds *DraftStore) SaveVersion(ctx context.Context, version *storage.DraftVersion) error {
	if version.ID == "" {
		return fmt.Errorf("version ID cannot be empty")
	}
	if version.DraftID == "" {
		return fmt.Errorf("draft ID cannot be empty")
	}

	query := `
		INSERT INTO draft_versions (id, draft_id, version_number, config, checksum, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := ds.db.ExecContext(ctx, query,
		version.ID,
		version.DraftID,
		version.VersionNumber,
		version.Config,
		version.Checksum,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf("failed to save draft version: %w", err)
	}

	return nil
}

// GetVersions retrieves all versions for a draft
func (ds *DraftStore) GetVersions(ctx context.Context, draftID string) ([]*storage.DraftVersion, error) {
	query := `
		SELECT id, draft_id, version_number, config, checksum, created_at
		FROM draft_versions
		WHERE draft_id = ?
		ORDER BY version_number DESC
	`

	rows, err := ds.db.QueryContext(ctx, query, draftID)
	if err != nil {
		return nil, fmt.Errorf("failed to query draft versions: %w", err)
	}
	defer rows.Close()

	var versions []*storage.DraftVersion
	for rows.Next() {
		version := &storage.DraftVersion{}
		if err := rows.Scan(
			&version.ID,
			&version.DraftID,
			&version.VersionNumber,
			&version.Config,
			&version.Checksum,
			&version.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan version: %w", err)
		}
		versions = append(versions, version)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating versions: %w", err)
	}

	return versions, nil
}

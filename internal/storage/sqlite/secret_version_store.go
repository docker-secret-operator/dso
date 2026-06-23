package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SecretVersion records metadata about a single rotation event.
// Secret values are never stored — only cryptographic hash + metadata.
type SecretVersion struct {
	ID             string
	SecretName     string
	Version        int
	Provider       string
	Hash           string
	RotatedBy      string
	RotationSource string // manual | scheduler | bulk_rotate | provider_sync
	ExecutionID    string
	CreatedAt      time.Time
}

// SecretVersionStore persists SecretVersion records.
type SecretVersionStore struct {
	db *sql.DB
}

// NewSecretVersionStore creates a new version store backed by db.
func NewSecretVersionStore(db *sql.DB) *SecretVersionStore {
	return &SecretVersionStore{db: db}
}

// Create inserts a new version record. Version numbers auto-increment per secret.
func (s *SecretVersionStore) Create(ctx context.Context, v *SecretVersion) error {
	// Determine the next version number for this secret.
	var next int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM secret_versions WHERE secret_name = ?`,
		v.SecretName,
	).Scan(&next)
	if err != nil {
		return fmt.Errorf("secret_version_store: determine version: %w", err)
	}
	v.Version = next

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO secret_versions
			(id, secret_name, version, provider, hash, rotated_by, rotation_source, execution_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.SecretName, v.Version, v.Provider, v.Hash,
		v.RotatedBy, v.RotationSource, v.ExecutionID, v.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("secret_version_store: insert: %w", err)
	}
	return nil
}

// ListBySecret returns all versions for a secret, newest first.
func (s *SecretVersionStore) ListBySecret(ctx context.Context, secretName string) ([]*SecretVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, secret_name, version, provider, hash, rotated_by, rotation_source, execution_id, created_at
		FROM secret_versions
		WHERE secret_name = ?
		ORDER BY version DESC`,
		secretName,
	)
	if err != nil {
		return nil, fmt.Errorf("secret_version_store: list: %w", err)
	}
	defer rows.Close()

	var out []*SecretVersion
	for rows.Next() {
		v := &SecretVersion{}
		if err := rows.Scan(
			&v.ID, &v.SecretName, &v.Version, &v.Provider, &v.Hash,
			&v.RotatedBy, &v.RotationSource, &v.ExecutionID, &v.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("secret_version_store: scan: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetByVersion retrieves a specific version of a secret.
func (s *SecretVersionStore) GetByVersion(ctx context.Context, secretName string, version int) (*SecretVersion, error) {
	v := &SecretVersion{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, secret_name, version, provider, hash, rotated_by, rotation_source, execution_id, created_at
		FROM secret_versions
		WHERE secret_name = ? AND version = ?`,
		secretName, version,
	).Scan(
		&v.ID, &v.SecretName, &v.Version, &v.Provider, &v.Hash,
		&v.RotatedBy, &v.RotationSource, &v.ExecutionID, &v.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secret_version_store: get by version: %w", err)
	}
	return v, nil
}

// CurrentVersion returns the highest version number for a secret (0 if none).
func (s *SecretVersionStore) CurrentVersion(ctx context.Context, secretName string) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM secret_versions WHERE secret_name = ?`,
		secretName,
	).Scan(&v)
	return v, err
}

package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

type BackupStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

func (s *BackupStore) Create(ctx context.Context, backup *storage.Backup) error {
	query := `
		INSERT INTO backups (id, filename, size_bytes, checksum, backup_type, status,
			duration_ms, error_msg, created_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query, backup.ID, backup.Filename, backup.SizeBytes,
		backup.Checksum, backup.BackupType, backup.Status, backup.DurationMs,
		backup.ErrorMsg, backup.CreatedAt, backup.CompletedAt)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	return nil
}

func (s *BackupStore) Update(ctx context.Context, backup *storage.Backup) error {
	query := `
		UPDATE backups
		SET filename = ?, size_bytes = ?, checksum = ?, backup_type = ?, status = ?,
			duration_ms = ?, error_msg = ?, created_at = ?, completed_at = ?
		WHERE id = ?
	`
	_, err := s.db.ExecContext(ctx, query, backup.Filename, backup.SizeBytes,
		backup.Checksum, backup.BackupType, backup.Status, backup.DurationMs,
		backup.ErrorMsg, backup.CreatedAt, backup.CompletedAt, backup.ID)
	if err != nil {
		return fmt.Errorf("failed to update backup: %w", err)
	}
	return nil
}

func (s *BackupStore) GetByID(ctx context.Context, id string) (*storage.Backup, error) {
	query := `
		SELECT id, filename, size_bytes, checksum, backup_type, status, duration_ms,
			error_msg, created_at, completed_at
		FROM backups WHERE id = ?
	`

	var backup storage.Backup
	var errorMsg sql.NullString
	var completedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, query, id).Scan(&backup.ID, &backup.Filename,
		&backup.SizeBytes, &backup.Checksum, &backup.BackupType, &backup.Status,
		&backup.DurationMs, &errorMsg, &backup.CreatedAt, &completedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get backup: %w", err)
	}

	if errorMsg.Valid {
		backup.ErrorMsg = &errorMsg.String
	}
	if completedAt.Valid {
		backup.CompletedAt = &completedAt.Time
	}

	return &backup, nil
}

func (s *BackupStore) List(ctx context.Context, limit int, offset int) ([]*storage.Backup, error) {
	query := `
		SELECT id, filename, size_bytes, checksum, backup_type, status, duration_ms,
			error_msg, created_at, completed_at
		FROM backups ORDER BY created_at DESC LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	defer rows.Close()

	return s.scanBackups(rows)
}

func (s *BackupStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM backups WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete backup: %w", err)
	}
	return nil
}

func (s *BackupStore) ListCompleted(ctx context.Context) ([]*storage.Backup, error) {
	query := `
		SELECT id, filename, size_bytes, checksum, backup_type, status, duration_ms,
			error_msg, created_at, completed_at
		FROM backups WHERE status = 'completed' ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list completed backups: %w", err)
	}
	defer rows.Close()

	return s.scanBackups(rows)
}

func (s *BackupStore) scanBackups(rows *sql.Rows) ([]*storage.Backup, error) {
	var backups []*storage.Backup
	for rows.Next() {
		var backup storage.Backup
		var errorMsg sql.NullString
		var completedAt sql.NullTime

		if err := rows.Scan(&backup.ID, &backup.Filename, &backup.SizeBytes,
			&backup.Checksum, &backup.BackupType, &backup.Status, &backup.DurationMs,
			&errorMsg, &backup.CreatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("failed to scan backup: %w", err)
		}

		if errorMsg.Valid {
			backup.ErrorMsg = &errorMsg.String
		}
		if completedAt.Valid {
			backup.CompletedAt = &completedAt.Time
		}

		backups = append(backups, &backup)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error scanning backups: %w", err)
	}

	return backups, nil
}

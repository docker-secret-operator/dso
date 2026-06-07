package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// SessionStore implements storage.SessionStore using SQLite
type SessionStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

// Create inserts a new session
func (ss *SessionStore) Create(ctx context.Context, session *storage.Session) error {
	query := `
		INSERT INTO sessions (id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := ss.db.ExecContext(ctx, query, session.ID, session.UserID, session.TokenHash, session.IPAddress, session.UserAgent, session.CreatedAt, session.ExpiresAt, session.LastActivity)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

// GetByID retrieves a session by ID
func (ss *SessionStore) GetByID(ctx context.Context, id string) (*storage.Session, error) {
	query := `SELECT id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, last_activity FROM sessions WHERE id = ?`
	row := ss.db.QueryRowContext(ctx, query, id)

	var session storage.Session
	err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &session.IPAddress, &session.UserAgent, &session.CreatedAt, &session.ExpiresAt, &session.LastActivity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

// GetByTokenHash retrieves a session by token hash
func (ss *SessionStore) GetByTokenHash(ctx context.Context, tokenHash string) (*storage.Session, error) {
	query := `SELECT id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, last_activity FROM sessions WHERE token_hash = ?`
	row := ss.db.QueryRowContext(ctx, query, tokenHash)

	var session storage.Session
	err := row.Scan(&session.ID, &session.UserID, &session.TokenHash, &session.IPAddress, &session.UserAgent, &session.CreatedAt, &session.ExpiresAt, &session.LastActivity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by token: %w", err)
	}

	return &session, nil
}

// UpdateLastActivity updates the last activity timestamp
func (ss *SessionStore) UpdateLastActivity(ctx context.Context, sessionID string) error {
	query := `UPDATE sessions SET last_activity = ? WHERE id = ?`
	_, err := ss.db.ExecContext(ctx, query, time.Now(), sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session activity: %w", err)
	}
	return nil
}

// DeleteExpired removes expired sessions
func (ss *SessionStore) DeleteExpired(ctx context.Context) error {
	query := `DELETE FROM sessions WHERE expires_at < ?`
	_, err := ss.db.ExecContext(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to delete expired sessions: %w", err)
	}
	return nil
}

// Delete removes a specific session
func (ss *SessionStore) Delete(ctx context.Context, sessionID string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := ss.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

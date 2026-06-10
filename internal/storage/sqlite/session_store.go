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

// ListByUserID retrieves all sessions for a user
func (ss *SessionStore) ListByUserID(ctx context.Context, userID string) ([]*storage.Session, error) {
	query := `SELECT id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, last_activity FROM sessions WHERE user_id = ? ORDER BY created_at DESC`
	rows, err := ss.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*storage.Session
	for rows.Next() {
		var s storage.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.IPAddress, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.LastActivity); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// DeleteAllByUserID removes all sessions for a user
func (ss *SessionStore) DeleteAllByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM sessions WHERE user_id = ?`
	_, err := ss.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user sessions: %w", err)
	}
	return nil
}

// ExtendSession updates the expiry of an existing session
func (ss *SessionStore) ExtendSession(ctx context.Context, sessionID string, newExpiry time.Time) error {
	query := `UPDATE sessions SET expires_at = ? WHERE id = ?`
	_, err := ss.db.ExecContext(ctx, query, newExpiry, sessionID)
	if err != nil {
		return fmt.Errorf("failed to extend session: %w", err)
	}
	return nil
}

// ListAll retrieves all active sessions across all users
func (ss *SessionStore) ListAll(ctx context.Context) ([]*storage.Session, error) {
	query := `SELECT id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, last_activity FROM sessions ORDER BY created_at DESC`
	rows, err := ss.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list all sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*storage.Session
	for rows.Next() {
		var s storage.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.TokenHash, &s.IPAddress, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.LastActivity); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
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

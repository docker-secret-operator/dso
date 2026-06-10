package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// UserStore implements storage.UserStore using SQLite
type UserStore struct {
	db interface {
		QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
		ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	}
}

const userSelectCols = `id, username, password_hash, display_name, role, disabled,
	failed_login_count, locked_until, password_changed_at, password_expires_at, must_change_password,
	created_at, updated_at`

func scanUser(scan func(...interface{}) error) (*storage.User, error) {
	var user storage.User
	var lockedUntil sql.NullTime
	var passwordChangedAt sql.NullTime
	var passwordExpiresAt sql.NullTime

	err := scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &user.Disabled,
		&user.FailedLoginCount, &lockedUntil, &passwordChangedAt, &passwordExpiresAt, &user.MustChangePassword,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if lockedUntil.Valid {
		user.LockedUntil = &lockedUntil.Time
	}
	if passwordChangedAt.Valid {
		user.PasswordChangedAt = &passwordChangedAt.Time
	}
	if passwordExpiresAt.Valid {
		user.PasswordExpiresAt = &passwordExpiresAt.Time
	}
	return &user, nil
}

// Create inserts a new user
func (us *UserStore) Create(ctx context.Context, user *storage.User) error {
	query := `
		INSERT INTO users (id, username, password_hash, display_name, role, disabled,
			failed_login_count, locked_until, password_changed_at, password_expires_at, must_change_password,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := us.db.ExecContext(ctx, query,
		user.ID, user.Username, user.PasswordHash, user.DisplayName, user.Role, user.Disabled,
		user.FailedLoginCount, user.LockedUntil, user.PasswordChangedAt, user.PasswordExpiresAt, user.MustChangePassword,
		user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// Update modifies an existing user
func (us *UserStore) Update(ctx context.Context, user *storage.User) error {
	query := `
		UPDATE users
		SET username = ?, password_hash = ?, display_name = ?, role = ?, disabled = ?,
			failed_login_count = ?, locked_until = ?, password_changed_at = ?, password_expires_at = ?,
			must_change_password = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := us.db.ExecContext(ctx, query,
		user.Username, user.PasswordHash, user.DisplayName, user.Role, user.Disabled,
		user.FailedLoginCount, user.LockedUntil, user.PasswordChangedAt, user.PasswordExpiresAt,
		user.MustChangePassword, user.UpdatedAt,
		user.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// GetByID retrieves a user by ID
func (us *UserStore) GetByID(ctx context.Context, id string) (*storage.User, error) {
	query := `SELECT ` + userSelectCols + ` FROM users WHERE id = ?`
	row := us.db.QueryRowContext(ctx, query, id)
	user, err := scanUser(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return user, nil
}

// GetByUsername retrieves a user by username
func (us *UserStore) GetByUsername(ctx context.Context, username string) (*storage.User, error) {
	query := `SELECT ` + userSelectCols + ` FROM users WHERE username = ?`
	row := us.db.QueryRowContext(ctx, query, username)
	user, err := scanUser(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}
	return user, nil
}

// List retrieves all users
func (us *UserStore) List(ctx context.Context) ([]*storage.User, error) {
	query := `SELECT ` + userSelectCols + ` FROM users ORDER BY created_at DESC`
	rows, err := us.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*storage.User
	for rows.Next() {
		user, err := scanUser(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, nil
}

// Delete removes a user
func (us *UserStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`
	_, err := us.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

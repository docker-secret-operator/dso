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

// Create inserts a new user
func (us *UserStore) Create(ctx context.Context, user *storage.User) error {
	query := `
		INSERT INTO users (id, username, password_hash, display_name, role, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := us.db.ExecContext(ctx, query, user.ID, user.Username, user.PasswordHash, user.DisplayName, user.Role, user.Disabled, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// Update modifies an existing user
func (us *UserStore) Update(ctx context.Context, user *storage.User) error {
	query := `
		UPDATE users
		SET username = ?, password_hash = ?, display_name = ?, role = ?, disabled = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := us.db.ExecContext(ctx, query, user.Username, user.PasswordHash, user.DisplayName, user.Role, user.Disabled, user.UpdatedAt, user.ID)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

// GetByID retrieves a user by ID
func (us *UserStore) GetByID(ctx context.Context, id string) (*storage.User, error) {
	query := `SELECT id, username, password_hash, display_name, role, disabled, created_at, updated_at FROM users WHERE id = ?`
	row := us.db.QueryRowContext(ctx, query, id)

	var user storage.User
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByUsername retrieves a user by username
func (us *UserStore) GetByUsername(ctx context.Context, username string) (*storage.User, error) {
	query := `SELECT id, username, password_hash, display_name, role, disabled, created_at, updated_at FROM users WHERE username = ?`
	row := us.db.QueryRowContext(ctx, query, username)

	var user storage.User
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	return &user, nil
}

// List retrieves all users
func (us *UserStore) List(ctx context.Context) ([]*storage.User, error) {
	query := `SELECT id, username, password_hash, display_name, role, disabled, created_at, updated_at FROM users ORDER BY created_at DESC`
	rows, err := us.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var users []*storage.User
	for rows.Next() {
		var user storage.User
		err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.DisplayName, &user.Role, &user.Disabled, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, &user)
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

package auth

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
)

// BootstrapOptions contains options for bootstrapping the authentication system
type BootstrapOptions struct {
	AdminUsername string
	AdminPassword string
	AdminEmail    string
}

// BootstrapAuthSystem initializes the authentication system and creates the initial admin user
func BootstrapAuthSystem(ctx context.Context, userStore storage.UserStore, opts BootstrapOptions) error {
	// Check if users table has any users
	users, err := userStore.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to check existing users: %w", err)
	}

	if len(users) > 0 {
		// Users already exist, no bootstrap needed
		return nil
	}

	// Set defaults if not provided
	if opts.AdminUsername == "" {
		opts.AdminUsername = "admin"
	}
	if opts.AdminPassword == "" {
		opts.AdminPassword = "admin" // Should be changed immediately
	}

	// Hash password
	passwordHash, err := HashPassword(opts.AdminPassword)
	if err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	// Force password change when the default "admin" password is used
	mustChange := opts.AdminPassword == "admin"

	// Create admin user
	now := time.Now()
	admin := &storage.User{
		ID:                 uuid.New().String(),
		Username:           opts.AdminUsername,
		PasswordHash:       passwordHash,
		DisplayName:        "Administrator",
		Role:               "admin",
		Disabled:           false,
		MustChangePassword: mustChange,
		PasswordChangedAt:  &now,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := userStore.Create(ctx, admin); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	log.Printf("[BOOTSTRAP] Created initial admin user: username=%s (CHANGE PASSWORD IMMEDIATELY)", opts.AdminUsername)

	return nil
}

package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
)

// AuthenticationService handles user authentication and session management
type AuthenticationService struct {
	userStore    storage.UserStore
	sessionStore storage.SessionStore
	sessionTTL   time.Duration
}

// NewAuthenticationService creates a new authentication service
func NewAuthenticationService(userStore storage.UserStore, sessionStore storage.SessionStore, sessionTTL time.Duration) *AuthenticationService {
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour // Default 24 hour sessions
	}
	return &AuthenticationService{
		userStore:    userStore,
		sessionStore: sessionStore,
		sessionTTL:   sessionTTL,
	}
}

// AuthenticateResult contains the result of authentication
type AuthenticateResult struct {
	User         *storage.User
	Token        string
	Session      *storage.Session
	ExpiresAt    time.Time
}

// Authenticate verifies username and password, and creates a session
func (as *AuthenticationService) Authenticate(ctx context.Context, username, password, ipAddress, userAgent string) (*AuthenticateResult, error) {
	// Lookup user by username
	user, err := as.userStore.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Verify user is not disabled
	if user.Disabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	// Verify password
	if err := VerifyPassword(password, user.PasswordHash); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	// Create session
	session, token, err := as.CreateSession(ctx, user, ipAddress, userAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &AuthenticateResult{
		User:      user,
		Token:     token,
		Session:   session,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// CreateSession creates a new session for a user
func (as *AuthenticationService) CreateSession(ctx context.Context, user *storage.User, ipAddress, userAgent string) (*storage.Session, string, error) {
	// Generate token
	token, err := GenerateToken()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token: %w", err)
	}

	// Create session record
	now := time.Now()
	session := &storage.Session{
		ID:           uuid.New().String(),
		UserID:       user.ID,
		TokenHash:    HashToken(token),
		IPAddress:    ipAddress,
		UserAgent:    userAgent,
		CreatedAt:    now,
		ExpiresAt:    now.Add(as.sessionTTL),
		LastActivity: now,
	}

	// Store session
	if err := as.sessionStore.Create(ctx, session); err != nil {
		return nil, "", fmt.Errorf("failed to store session: %w", err)
	}

	return session, token, nil
}

// ValidateSessionResult contains the result of session validation
type ValidateSessionResult struct {
	User    *storage.User
	Session *storage.Session
}

// ValidateSession checks if a token is valid and returns the user and session
func (as *AuthenticationService) ValidateSession(ctx context.Context, token string) (*ValidateSessionResult, error) {
	// Hash the token for lookup
	tokenHash := HashToken(token)

	// Lookup session by token hash
	session, err := as.sessionStore.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup session: %w", err)
	}
	if session == nil {
		return nil, fmt.Errorf("session not found")
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Delete expired session
		_ = as.sessionStore.Delete(ctx, session.ID)
		return nil, fmt.Errorf("session expired")
	}

	// Lookup user
	user, err := as.userStore.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	// Check if user is disabled
	if user.Disabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	// Update last activity
	if err := as.sessionStore.UpdateLastActivity(ctx, session.ID); err != nil {
		// Log but don't fail
		fmt.Printf("failed to update session activity: %v\n", err)
	}

	return &ValidateSessionResult{
		User:    user,
		Session: session,
	}, nil
}

// Logout invalidates a session
func (as *AuthenticationService) Logout(ctx context.Context, sessionID string) error {
	return as.sessionStore.Delete(ctx, sessionID)
}

// CleanupExpiredSessions removes all expired sessions from the database
func (as *AuthenticationService) CleanupExpiredSessions(ctx context.Context) error {
	return as.sessionStore.DeleteExpired(ctx)
}

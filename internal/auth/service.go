package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/google/uuid"
)

const (
	maxConcurrentSessions    = 5
	accountLockThreshold     = 5
	accountLockDuration      = 15 * time.Minute
	activityThrottleDuration = 60 * time.Second
)

// Typed authentication errors for error.Is() checking and recovery strategies
var (
	// ErrRateLimited is returned when the rate limiter rejects a login attempt
	ErrRateLimited = errors.New("too many failed attempts")

	// ErrAccountLocked is returned when the user account is locked
	ErrAccountLocked = errors.New("account is locked due to too many failed attempts")

	// ErrMustChangePassword is returned when the user must change their password
	ErrMustChangePassword = errors.New("password change required")

	// ErrInvalidCredentials is returned for invalid username or password
	ErrInvalidCredentials = errors.New("invalid username or password")

	// ErrUserDisabled is returned when the user account is disabled
	ErrUserDisabled = errors.New("user account is disabled")

	// ErrUserNotFound is returned when a user does not exist
	ErrUserNotFound = errors.New("user not found")

	// ErrSessionExpired is returned when a session has expired
	ErrSessionExpired = errors.New("session expired")

	// ErrSessionNotFound is returned when a session does not exist
	ErrSessionNotFound = errors.New("session not found")
)

// AuthenticationService handles user authentication and session management
type AuthenticationService struct {
	userStore    storage.UserStore
	sessionStore storage.SessionStore
	sessionTTL   time.Duration
	rateLimiter  *RateLimiter
	auditLogger  AuditLogger
}

// NewAuthenticationService creates a new authentication service
func NewAuthenticationService(userStore storage.UserStore, sessionStore storage.SessionStore, sessionTTL time.Duration) *AuthenticationService {
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour
	}
	return &AuthenticationService{
		userStore:    userStore,
		sessionStore: sessionStore,
		sessionTTL:   sessionTTL,
		rateLimiter:  NewRateLimiter(DefaultMaxFailures, DefaultWindowDuration),
	}
}

// SetAuditLogger sets an optional audit logger for security events.
func (as *AuthenticationService) SetAuditLogger(l AuditLogger) {
	as.auditLogger = l
}

func (as *AuthenticationService) logAudit(ctx context.Context, actorID, actorName, action, resource, resourceID, resourceType string) {
	if as.auditLogger != nil {
		_ = as.auditLogger.LogEvent(ctx, actorID, actorName, action, resource, resourceID, resourceType)
	}
}

// AuthenticateResult contains the result of authentication
type AuthenticateResult struct {
	User      *storage.User
	Token     string
	Session   *storage.Session
	ExpiresAt time.Time
}

// Authenticate verifies username and password, creates a session, and enforces rate limits.
func (as *AuthenticationService) Authenticate(ctx context.Context, username, password, ipAddress, userAgent string) (*AuthenticateResult, error) {
	ipKey := IPKey(ipAddress)
	userKey := UsernameKey(username)

	// FG1: IP rate limit
	if as.rateLimiter.IsLimited(ipKey) {
		as.logAudit(ctx, "system", "system", "auth.rate_limited", "ip", ipAddress, "authentication")
		return nil, ErrRateLimited
	}

	// FG1: Username rate limit
	if as.rateLimiter.IsLimited(userKey) {
		as.logAudit(ctx, "system", "system", "auth.rate_limited", "user", username, "authentication")
		return nil, ErrRateLimited
	}

	// Lookup user
	user, err := as.userStore.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		// Record failure for IP only (don't reveal whether username exists)
		as.rateLimiter.RecordFailure(ipKey)
		as.logAudit(ctx, "system", "system", "auth.login_failure", "user", username, "authentication")
		// Return generic error to prevent user enumeration, but use typed error for logging
		return nil, ErrInvalidCredentials
	}

	// Check disabled
	if user.Disabled {
		as.logAudit(ctx, user.ID, user.Username, "auth.login_failure", "user", user.ID, "authentication")
		return nil, fmt.Errorf("user account is disabled")
	}

	// FG2: DB-level account lockout
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		as.logAudit(ctx, user.ID, user.Username, "auth.account_locked", "user", user.ID, "authentication")
		return nil, ErrAccountLocked
	}

	// Verify password
	if err := VerifyPassword(password, user.PasswordHash); err != nil {
		as.rateLimiter.RecordFailure(ipKey)
		as.rateLimiter.RecordFailure(userKey)
		as.recordLoginFailure(ctx, user)
		as.logAudit(ctx, user.ID, user.Username, "auth.login_failure", "user", user.ID, "authentication")
		return nil, fmt.Errorf("invalid credentials")
	}

	// Success: reset counters
	as.rateLimiter.Reset(ipKey)
	as.rateLimiter.Reset(userKey)
	as.resetLoginFailures(ctx, user)

	// Create session (enforces concurrent session limit)
	session, token, err := as.CreateSession(ctx, user, ipAddress, userAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	as.logAudit(ctx, user.ID, user.Username, "auth.login_success", "session", session.ID, "authentication")

	return &AuthenticateResult{
		User:      user,
		Token:     token,
		Session:   session,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// recordLoginFailure increments failed_login_count and locks the account if threshold reached.
func (as *AuthenticationService) recordLoginFailure(ctx context.Context, user *storage.User) {
	user.FailedLoginCount++
	if user.FailedLoginCount >= accountLockThreshold {
		until := time.Now().Add(accountLockDuration)
		user.LockedUntil = &until
		as.logAudit(ctx, user.ID, user.Username, "auth.user_locked", "user", user.ID, "authentication")
	}
	user.UpdatedAt = time.Now()
	_ = as.userStore.Update(ctx, user)
}

// resetLoginFailures clears lockout counters after a successful login.
func (as *AuthenticationService) resetLoginFailures(ctx context.Context, user *storage.User) {
	if user.FailedLoginCount == 0 && user.LockedUntil == nil {
		return
	}
	user.FailedLoginCount = 0
	user.LockedUntil = nil
	user.UpdatedAt = time.Now()
	_ = as.userStore.Update(ctx, user)
}

// CreateSession creates a new session for a user, enforcing the concurrent session limit.
func (as *AuthenticationService) CreateSession(ctx context.Context, user *storage.User, ipAddress, userAgent string) (*storage.Session, string, error) {
	// FG9: enforce concurrent session limit
	existing, err := as.sessionStore.ListByUserID(ctx, user.ID)
	if err == nil {
		now := time.Now()
		active := make([]*storage.Session, 0, len(existing))
		for _, s := range existing {
			if s.ExpiresAt.After(now) {
				active = append(active, s)
			}
		}
		// active is sorted DESC by created_at; oldest is last
		for len(active) >= maxConcurrentSessions {
			oldest := active[len(active)-1]
			if delErr := as.sessionStore.Delete(ctx, oldest.ID); delErr == nil {
				as.logAudit(ctx, user.ID, user.Username, "auth.session_revoked_limit", "session", oldest.ID, "authentication")
			}
			active = active[:len(active)-1]
		}
	}

	token, err := GenerateToken()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate token: %w", err)
	}

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

// ValidateSession checks if a token is valid. Last-activity updates are throttled to once per 60 s.
func (as *AuthenticationService) ValidateSession(ctx context.Context, token string) (*ValidateSessionResult, error) {
	tokenHash := HashToken(token)
	session, err := as.sessionStore.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup session: %w", err)
	}
	if session == nil {
		return nil, fmt.Errorf("session not found")
	}

	if time.Now().After(session.ExpiresAt) {
		_ = as.sessionStore.Delete(ctx, session.ID)
		as.logAudit(ctx, session.UserID, "", "auth.session_expired", "session", session.ID, "authentication")
		return nil, fmt.Errorf("session expired")
	}

	user, err := as.userStore.GetByID(ctx, session.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if user.Disabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	// FG6: throttle last_activity updates to once per 60 s
	if time.Since(session.LastActivity) >= activityThrottleDuration {
		if err := as.sessionStore.UpdateLastActivity(ctx, session.ID); err != nil {
			fmt.Printf("failed to update session activity: %v\n", err)
		}
		// reflect the update in-memory so callers see the latest timestamp
		session.LastActivity = time.Now()
	}

	return &ValidateSessionResult{User: user, Session: session}, nil
}

// RefreshSession extends the session TTL by sessionTTL from now.
func (as *AuthenticationService) RefreshSession(ctx context.Context, sessionID string) (time.Time, error) {
	newExpiry := time.Now().Add(as.sessionTTL)
	if err := as.sessionStore.ExtendSession(ctx, sessionID, newExpiry); err != nil {
		return time.Time{}, fmt.Errorf("failed to refresh session: %w", err)
	}
	return newExpiry, nil
}

// Logout invalidates a session
func (as *AuthenticationService) Logout(ctx context.Context, sessionID string) error {
	return as.sessionStore.Delete(ctx, sessionID)
}

// CleanupExpiredSessions removes all expired sessions from the database
func (as *AuthenticationService) CleanupExpiredSessions(ctx context.Context) error {
	return as.sessionStore.DeleteExpired(ctx)
}

// ListUserSessions returns all active sessions for a user
func (as *AuthenticationService) ListUserSessions(ctx context.Context, userID string) ([]*storage.Session, error) {
	return as.sessionStore.ListByUserID(ctx, userID)
}

// RevokeSession deletes a specific session by ID
func (as *AuthenticationService) RevokeSession(ctx context.Context, sessionID string) error {
	return as.sessionStore.Delete(ctx, sessionID)
}

// RevokeAllUserSessions deletes all sessions for a user (logout all devices)
func (as *AuthenticationService) RevokeAllUserSessions(ctx context.Context, userID string) error {
	return as.sessionStore.DeleteAllByUserID(ctx, userID)
}

// ChangePassword verifies the current password, enforces policy, and clears must_change_password.
func (as *AuthenticationService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	user, err := as.userStore.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	if err := VerifyPassword(currentPassword, user.PasswordHash); err != nil {
		return fmt.Errorf("current password is incorrect")
	}

	if err := ValidatePasswordPolicy(newPassword); err != nil {
		return err
	}

	if VerifyPassword(newPassword, user.PasswordHash) == nil {
		return fmt.Errorf("new password must differ from the current password")
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now()
	user.PasswordHash = hash
	user.MustChangePassword = false
	user.PasswordChangedAt = &now
	user.UpdatedAt = now

	as.logAudit(ctx, user.ID, user.Username, "auth.password_changed", "user", user.ID, "authentication")
	return as.userStore.Update(ctx, user)
}

// AdminResetPassword sets a new password for any user and forces a password change on next login.
func (as *AuthenticationService) AdminResetPassword(ctx context.Context, userID, newPassword string) error {
	user, err := as.userStore.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}

	if err := ValidatePasswordPolicy(newPassword); err != nil {
		return err
	}

	if VerifyPassword(newPassword, user.PasswordHash) == nil {
		return fmt.Errorf("new password must differ from the current password")
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now()
	user.PasswordHash = hash
	user.MustChangePassword = true
	user.PasswordChangedAt = &now
	user.UpdatedAt = now

	as.logAudit(ctx, user.ID, user.Username, "auth.password_reset", "user", user.ID, "authentication")
	return as.userStore.Update(ctx, user)
}

// UnlockUser clears the account lockout for a user.
func (as *AuthenticationService) UnlockUser(ctx context.Context, actorID, actorName, userID string) error {
	user, err := as.userStore.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to lookup user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}
	user.FailedLoginCount = 0
	user.LockedUntil = nil
	user.UpdatedAt = time.Now()
	as.rateLimiter.Reset(UsernameKey(user.Username))
	as.logAudit(ctx, actorID, actorName, "auth.user_unlocked", "user", userID, "authentication")
	return as.userStore.Update(ctx, user)
}

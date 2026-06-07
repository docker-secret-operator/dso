package auth

import (
	"context"

	"github.com/docker-secret-operator/dso/internal/storage"
)

// Context keys for storing authenticated user and session
type contextKey string

const (
	contextKeyUser    contextKey = "dso:authenticated_user"
	contextKeySession contextKey = "dso:authenticated_session"
)

// WithAuthenticatedUser returns a context with the authenticated user attached
func WithAuthenticatedUser(ctx context.Context, user *storage.User) context.Context {
	return context.WithValue(ctx, contextKeyUser, user)
}

// CurrentUser retrieves the authenticated user from context
func CurrentUser(ctx context.Context) *storage.User {
	user, ok := ctx.Value(contextKeyUser).(*storage.User)
	if !ok {
		return nil
	}
	return user
}

// WithAuthenticatedSession returns a context with the authenticated session attached
func WithAuthenticatedSession(ctx context.Context, session *storage.Session) context.Context {
	return context.WithValue(ctx, contextKeySession, session)
}

// CurrentSession retrieves the authenticated session from context
func CurrentSession(ctx context.Context) *storage.Session {
	session, ok := ctx.Value(contextKeySession).(*storage.Session)
	if !ok {
		return nil
	}
	return session
}

// IsAuthenticated checks if the context has an authenticated user
func IsAuthenticated(ctx context.Context) bool {
	return CurrentUser(ctx) != nil
}

// CurrentRole retrieves the authenticated user's role from context
func CurrentRole(ctx context.Context) string {
	user := CurrentUser(ctx)
	if user == nil {
		return ""
	}
	return user.Role
}

// CurrentUserID retrieves the authenticated user's ID from context
func CurrentUserID(ctx context.Context) string {
	user := CurrentUser(ctx)
	if user == nil {
		return ""
	}
	return user.ID
}

// CurrentUsername retrieves the authenticated user's username from context
func CurrentUsername(ctx context.Context) string {
	user := CurrentUser(ctx)
	if user == nil {
		return ""
	}
	return user.Username
}

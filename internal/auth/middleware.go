package auth

import (
	"fmt"
	"net/http"
	"strings"
)

// Middleware creates HTTP middleware for authentication
type Middleware struct {
	authService *AuthenticationService
	publicPaths map[string]bool
}

// NewMiddleware creates a new authentication middleware
func NewMiddleware(authService *AuthenticationService, publicPaths map[string]bool) *Middleware {
	return &Middleware{
		authService: authService,
		publicPaths: publicPaths,
	}
}

// Handler wraps an HTTP handler with authentication
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if path is public
		if m.publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Bearer token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			next.ServeHTTP(w, r)
			return
		}

		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		token := strings.TrimPrefix(authHeader, bearerPrefix)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate session
		result, err := m.authService.ValidateSession(r.Context(), token)
		if err != nil {
			// Log but don't reject - let endpoint decide if auth is required
			fmt.Printf("session validation error: %v\n", err)
			next.ServeHTTP(w, r)
			return
		}

		// Inject user and session into context
		ctx := WithAuthenticatedUser(r.Context(), result.User)
		ctx = WithAuthenticatedSession(ctx, result.Session)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

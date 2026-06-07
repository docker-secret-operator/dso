package auth

import (
	"encoding/json"
	"net/http"
)

// AuthorizationMiddleware provides role-based access control middleware
type AuthorizationMiddleware struct{}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// NewAuthorizationMiddleware creates a new authorization middleware
func NewAuthorizationMiddleware() *AuthorizationMiddleware {
	return &AuthorizationMiddleware{}
}

// RequireAuthenticated creates middleware that requires authentication
func (am *AuthorizationMiddleware) RequireAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsAuthenticated(r.Context()) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(&ErrorResponse{Error: "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole creates middleware that requires a specific role
func (am *AuthorizationMiddleware) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsAuthenticated(r.Context()) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(&ErrorResponse{Error: "unauthorized"})
				return
			}

			userRole := CurrentRole(r.Context())
			if !CanAccessEndpoint(userRole, role) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(&ErrorResponse{Error: "forbidden"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyRole creates middleware that requires any of the specified roles
func (am *AuthorizationMiddleware) RequireAnyRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsAuthenticated(r.Context()) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(&ErrorResponse{Error: "unauthorized"})
				return
			}

			userRole := CurrentRole(r.Context())
			if !CanAccessEndpoint(userRole, roles...) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(&ErrorResponse{Error: "forbidden"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

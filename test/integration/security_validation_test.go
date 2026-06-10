package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/storage"
	"github.com/docker-secret-operator/dso/internal/storage/sqlite"
)

// We mock RESTServer checkAuthorization because it is private inside internal/server
// Actually, we can test RBAC matrix directly
func TestRBACEnforcement(t *testing.T) {
	permMatrix := auth.NewPermissionMatrix()

	tests := []struct {
		role   string
		path   string
		expect bool
	}{
		{"viewer", "/api/dashboard", true},
		{"viewer", "/api/operations", true},
		{"viewer", "/api/operations/dlq/retry/123", false},
		{"operator", "/api/operations/dlq/retry/123", true},
		{"operator", "/api/executions", true},
		{"reviewer", "/api/reviews", true},
		{"reviewer", "/api/approvals", false},
		{"approver", "/api/approvals", true},
		{"admin", "/api/operations/dlq/retry/123", true},
	}

	for _, tt := range tests {
		t.Run(tt.role+"_"+tt.path, func(t *testing.T) {

			// We simulate longest prefix match manually
			bestMatch := ""
			requiredRoles := []string{}
			exists := false
			for rulePath, roles := range permMatrix.Rules {
				if len(tt.path) >= len(rulePath) && tt.path[:len(rulePath)] == rulePath && rulePath != "/" {
					if len(rulePath) > len(bestMatch) {
						bestMatch = rulePath
						requiredRoles = roles
						exists = true
					}
				}
			}

			if !exists {
				// not found = allowed (for testing logic)
			}

			if len(requiredRoles) == 0 {
				// allowed
			}

			allowed := auth.CanAccessEndpoint(tt.role, requiredRoles...)
			if allowed != tt.expect {
				t.Errorf("expected %v, got %v for role %s path %s", tt.expect, allowed, tt.role, tt.path)
			}
		})
	}
}

func TestSessionSecurity(t *testing.T) {
	provider, err := sqlite.NewSQLiteProvider(":memory:")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// NewSQLiteProvider runs migrations automatically — no ApplyMigrations call needed.

	authService := auth.NewAuthenticationService(provider.Users(), provider.Sessions(), time.Hour)

	ctx := context.Background()
	provider.Users().Create(ctx, &storage.User{ID: "u-disabled", Username: "disabled", Role: "viewer"})

	// Simulating disable
	user, _ := provider.Users().GetByID(ctx, "u-disabled")
	user.Disabled = true
	provider.Users().Update(ctx, user)

	// CreateSession returns (session, token, error)
	_, token, err := authService.CreateSession(ctx, user, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Disabled user should not validate session
	_, err = authService.ValidateSession(ctx, token)
	if err == nil {
		t.Errorf("Expected disabled user session to fail validation")
	}

	// Malformed bearer token test is an HTTP test
	authMid := auth.NewMiddleware(authService, map[string]bool{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewServer(authMid.Handler(mux))
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/dashboard", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for invalid token, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", ts.URL+"/api/dashboard", nil)
	req.Header.Set("Authorization", "bearer token without bearer correctly formatted")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for malformed token, got %d", resp.StatusCode)
	}
}

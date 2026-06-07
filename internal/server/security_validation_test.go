package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/storage"
)

func TestSecurityValidation(t *testing.T) {
	// Setup in-memory DB and start server handlers
	provider, err := storage.NewSQLiteProvider(":memory:")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	err = provider.ApplyMigrations(context.Background())
	if err != nil {
		t.Fatalf("Failed to apply migrations: %v", err)
	}

	// Bootstrap users
	auth.BootstrapAuthSystem(context.Background(), provider.Users(), auth.BootstrapOptions{
		AdminUsername: "admin",
		AdminPassword: "password",
	})
	
	// Create test users
	users := provider.Users()
	ctx := context.Background()
	users.Create(ctx, &storage.User{ID: "u-viewer", Username: "viewer", Role: "viewer", Status: "active"})
	users.Create(ctx, &storage.User{ID: "u-operator", Username: "operator", Role: "operator", Status: "active"})
	users.Create(ctx, &storage.User{ID: "u-reviewer", Username: "reviewer", Role: "reviewer", Status: "active"})
	users.Create(ctx, &storage.User{ID: "u-approver", Username: "approver", Role: "approver", Status: "active"})
	users.Create(ctx, &storage.User{ID: "u-admin", Username: "admin2", Role: "admin", Status: "active"})
	users.Create(ctx, &storage.User{ID: "u-disabled", Username: "disabled", Role: "viewer", Status: "disabled"})

	// Setup Server
	// We will create the rest server and mux exactly as rest.go does
	// But we bypass the actual StartRESTServer which starts Docker stuff
	
	// Just use the logic from rest.go manually to avoid docker init
	// For testing RBAC prefix bug, we need PermissionMatrix and AuthorizationMiddleware
	authService := auth.NewAuthenticationService(users, provider.Sessions(), time.Hour)
	permMatrix := auth.NewPermissionMatrix()
	authMiddleware := auth.NewAuthorizationMiddleware()
	
	// Manually construct a dummy RESTServer struct to test checkAuthorization
	// Wait, we need the actual RESTServer to test routing
	srv := &server.RESTServer{
		PermissionMatrix: permMatrix,
		AuthorizationMiddleware: authMiddleware,
		AuthenticationService: authService,
	}
	
	// ... we can just test checkAuthorization directly
	// Let's create a test server with middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Mock ServeHTTP
		if !srv.checkAuthorization(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	
	authMid := auth.NewMiddleware(authService, map[string]bool{
		"/api/auth/login": true,
	})
	
	ts := httptest.NewServer(authMid.Handler(mux))
	defer ts.Close()
	
	// Helper to login and get token
	login := func(userID string) string {
		session, _ := authService.CreateSession(ctx, userID, "127.0.0.1", "test")
		return session.Token
	}

	tests := []struct {
		name       string
		token      string
		path       string
		wantStatus int
	}{
		{"Viewer - Dashboard", login("u-viewer"), "/api/dashboard", 200},
		{"Viewer - Operations", login("u-viewer"), "/api/operations", 200},
		{"Viewer - DLQ Retry", login("u-viewer"), "/api/operations/dlq/retry/123", 403},
		{"Operator - DLQ Retry", login("u-operator"), "/api/operations/dlq/retry/123", 200},
		{"Operator - Executions", login("u-operator"), "/api/executions", 200},
		{"Reviewer - Reviews", login("u-reviewer"), "/api/reviews", 200},
		{"Reviewer - Approvals", login("u-reviewer"), "/api/approvals", 403},
		{"Approver - Approvals", login("u-approver"), "/api/approvals", 200},
		{"Admin - DLQ Retry", login("u-admin"), "/api/operations/dlq/retry/123", 200},
		{"Disabled User", login("u-disabled"), "/api/dashboard", 401},
		{"Expired Token", "invalid-token", "/api/dashboard", 401},
		{"Malformed Token", "bearer token", "/api/dashboard", 401},
		{"Anonymous - DLQ Retry", "", "/api/operations/dlq/retry/123", 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", ts.URL+tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Path %s: got %d, want %d", tt.path, resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

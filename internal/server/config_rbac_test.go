package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/api"
	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/storage"
)

// Config editing endpoints must be admin-only. Verify a non-admin is rejected
// with 403 by RBAC before reaching the handler, and an admin is not 403'd.
func TestConfigEndpointsRequireAdmin(t *testing.T) {
	srv := &RESTServer{
		PermissionMatrix: auth.NewPermissionMatrix(),
		ConfigEditor:     api.NewConfigEditor(nil, nil, nil),
	}

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/config/raw"},
		{http.MethodPost, "/api/config/validate"},
		{http.MethodPost, "/api/config/apply"},
		{http.MethodGet, "/api/config/backups"},
		{http.MethodPost, "/api/config/rollback"},
	}

	for _, tc := range cases {
		// Non-admin → 403.
		viewer := &storage.User{ID: "v1", Username: "viewer", Role: "viewer"}
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req = req.WithContext(auth.WithAuthenticatedUser(req.Context(), viewer))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as viewer: status = %d, want 403", tc.method, tc.path, rec.Code)
		}

		// Admin → must pass RBAC (any status except 403).
		admin := &storage.User{ID: "a1", Username: "admin", Role: "admin"}
		req = httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req = req.WithContext(auth.WithAuthenticatedUser(req.Context(), admin))
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s %s as admin: got 403, want it to pass RBAC", tc.method, tc.path)
		}
	}
}

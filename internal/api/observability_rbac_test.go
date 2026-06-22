package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker-secret-operator/dso/internal/auth"
	"github.com/docker-secret-operator/dso/internal/policy"
	"github.com/docker-secret-operator/dso/internal/storage"
	"go.uber.org/zap"
)

// Observability handlers now allow any authenticated role to READ (GET) but
// restrict mutations to admin. PolicyHandler is representative of the pattern
// applied across the observability handlers.
func TestObservabilityHandler_ReadVsMutate(t *testing.T) {
	h := NewPolicyHandler(policy.NewEngine(policy.NewInMemoryStore(), zap.NewNop()))

	withUser := func(req *http.Request, role string) *http.Request {
		u := &storage.User{ID: "u", Username: role, Role: role}
		return req.WithContext(auth.WithAuthenticatedUser(req.Context(), u))
	}
	do := func(method, path, role string) int {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, withUser(req, role))
		return rec.Code
	}

	// Viewer can READ.
	if code := do(http.MethodGet, "/api/policies", "viewer"); code == http.StatusForbidden {
		t.Errorf("viewer GET /api/policies = 403, want read access")
	}
	// Viewer cannot MUTATE.
	if code := do(http.MethodPost, "/api/policies", "viewer"); code != http.StatusForbidden {
		t.Errorf("viewer POST /api/policies = %d, want 403", code)
	}
	// Admin can mutate (not blocked by the role gate).
	if code := do(http.MethodPost, "/api/policies", "admin"); code == http.StatusForbidden {
		t.Errorf("admin POST /api/policies = 403, want it to pass the role gate")
	}
	// Unauthenticated is rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/policies", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("unauthenticated GET = %d, want 403", rec.Code)
	}
}

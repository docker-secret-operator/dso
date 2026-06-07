package auth

import (
	"testing"
)

func TestHasRole(t *testing.T) {
	tests := []struct {
		name         string
		userRole     string
		requiredRole string
		expected     bool
	}{
		// Exact matches
		{"viewer can access viewer", RoleViewer, RoleViewer, true},
		{"operator can access operator", RoleOperator, RoleOperator, true},
		{"reviewer can access reviewer", RoleReviewer, RoleReviewer, true},
		{"approver can access approver", RoleApprover, RoleApprover, true},
		{"admin can access admin", RoleAdmin, RoleAdmin, true},

		// Admin can access everything
		{"admin can access viewer", RoleAdmin, RoleViewer, true},
		{"admin can access operator", RoleAdmin, RoleOperator, true},
		{"admin can access reviewer", RoleAdmin, RoleReviewer, true},
		{"admin can access approver", RoleAdmin, RoleApprover, true},

		// Lower roles cannot access higher roles
		{"viewer cannot access operator", RoleViewer, RoleOperator, false},
		{"viewer cannot access reviewer", RoleViewer, RoleReviewer, false},
		{"viewer cannot access approver", RoleViewer, RoleApprover, false},
		{"viewer cannot access admin", RoleViewer, RoleAdmin, false},

		{"operator cannot access reviewer", RoleOperator, RoleReviewer, false},
		{"operator cannot access approver", RoleOperator, RoleApprover, false},
		{"operator cannot access admin", RoleOperator, RoleAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasRole(tt.userRole, tt.requiredRole)
			if result != tt.expected {
				t.Errorf("HasRole(%s, %s) = %v, want %v", tt.userRole, tt.requiredRole, result, tt.expected)
			}
		})
	}
}

func TestHasAnyRole(t *testing.T) {
	tests := []struct {
		name          string
		userRole      string
		requiredRoles []string
		expected      bool
	}{
		// Exact matches
		{"viewer matches viewer", RoleViewer, []string{RoleViewer}, true},
		{"viewer matches viewer or operator", RoleViewer, []string{RoleViewer, RoleOperator}, true},

		// Admin matches everything
		{"admin matches any", RoleAdmin, []string{RoleViewer}, true},
		{"admin matches multiple", RoleAdmin, []string{RoleViewer, RoleOperator}, true},

		// No match
		{"viewer doesn't match operator", RoleViewer, []string{RoleOperator}, false},
		{"viewer doesn't match operator or approver", RoleViewer, []string{RoleOperator, RoleApprover}, false},

		// Empty roles
		{"empty roles list matches all", RoleViewer, []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasAnyRole(tt.userRole, tt.requiredRoles...)
			if result != tt.expected {
				t.Errorf("HasAnyRole(%s, %v) = %v, want %v", tt.userRole, tt.requiredRoles, result, tt.expected)
			}
		})
	}
}

func TestRoleHierarchy(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		expected []string
	}{
		{"viewer hierarchy", RoleViewer, []string{RoleViewer}},
		{"operator hierarchy", RoleOperator, []string{RoleOperator, RoleViewer}},
		{"reviewer hierarchy", RoleReviewer, []string{RoleReviewer, RoleViewer}},
		{"approver hierarchy", RoleApprover, []string{RoleApprover, RoleViewer}},
		{"admin hierarchy", RoleAdmin, []string{RoleAdmin, RoleApprover, RoleReviewer, RoleOperator, RoleViewer}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RoleHierarchy(tt.role)
			if len(result) != len(tt.expected) {
				t.Errorf("RoleHierarchy(%s) returned %d items, expected %d", tt.role, len(result), len(tt.expected))
				return
			}
			for i, r := range result {
				if r != tt.expected[i] {
					t.Errorf("RoleHierarchy(%s)[%d] = %s, want %s", tt.role, i, r, tt.expected[i])
				}
			}
		})
	}
}

func TestCanAccessEndpoint(t *testing.T) {
	tests := []struct {
		name          string
		userRole      string
		requiredRoles []string
		expected      bool
	}{
		// No requirements
		{"no requirements", RoleViewer, []string{}, true},

		// Viewer access
		{"viewer can access viewer", RoleViewer, []string{RoleViewer}, true},
		{"viewer cannot access operator", RoleViewer, []string{RoleOperator}, false},

		// Operator access
		{"operator can access operator", RoleOperator, []string{RoleOperator}, true},
		{"operator can access viewer", RoleOperator, []string{RoleViewer}, true},
		{"operator cannot access approver", RoleOperator, []string{RoleApprover}, false},

		// Admin access
		{"admin can access anything", RoleAdmin, []string{RoleViewer}, true},
		{"admin can access multiple roles", RoleAdmin, []string{RoleViewer, RoleOperator}, true},

		// Multiple role requirements
		{"viewer matches one of many", RoleViewer, []string{RoleOperator, RoleViewer}, true},
		{"operator matches one of many", RoleOperator, []string{RoleApprover, RoleOperator}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanAccessEndpoint(tt.userRole, tt.requiredRoles...)
			if result != tt.expected {
				t.Errorf("CanAccessEndpoint(%s, %v) = %v, want %v", tt.userRole, tt.requiredRoles, result, tt.expected)
			}
		})
	}
}

func TestPermissionMatrix(t *testing.T) {
	pm := NewPermissionMatrix()

	tests := []struct {
		name         string
		path         string
		userRole     string
		expectedBool bool
	}{
		// Public endpoints
		{"/health is public", "/health", RoleViewer, true},
		{"/api/auth/login is public", "/api/auth/login", RoleViewer, true},

		// Authenticated only
		{"/api/auth/logout requires auth", "/api/auth/logout", RoleViewer, true},

		// Viewer access
		{"/api/dashboard requires viewer", "/api/dashboard", RoleViewer, true},
		{"/api/operations requires viewer", "/api/operations", RoleViewer, true},
		{"/api/audit requires viewer", "/api/audit", RoleViewer, true},

		// Operator access
		{"/api/executions requires operator", "/api/executions", RoleOperator, true},
		{"/api/executions denied for viewer", "/api/executions", RoleViewer, false},

		// Reviewer access
		{"/api/reviews requires reviewer", "/api/reviews", RoleReviewer, true},
		{"/api/reviews denied for viewer", "/api/reviews", RoleViewer, false},

		// Approver access
		{"/api/approvals requires approver", "/api/approvals", RoleApprover, true},
		{"/api/approvals denied for operator", "/api/approvals", RoleOperator, false},

		// Admin access
		{"/api/config requires admin", "/api/config", RoleAdmin, true},
		{"/api/config denied for viewer", "/api/config", RoleViewer, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.CanAccess(tt.path, tt.userRole)
			if result != tt.expectedBool {
				t.Errorf("CanAccess(%s, %s) = %v, want %v", tt.path, tt.userRole, result, tt.expectedBool)
			}
		})
	}
}

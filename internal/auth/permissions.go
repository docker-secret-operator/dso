package auth

// PermissionMatrix defines which roles can access which endpoints
type PermissionMatrix struct {
	Rules map[string][]string
}

// NewPermissionMatrix creates the default permission matrix
func NewPermissionMatrix() *PermissionMatrix {
	return &PermissionMatrix{
		Rules: map[string][]string{
			// Public endpoints (no auth required)
			"/health":         {},
			"/api/auth/login": {},
			"/api/events":     {},

			// Authentication endpoints (requires authentication)
			"/api/auth/logout":  {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/auth/me":      {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/auth/session": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},

			// Dashboard endpoints (requires viewer+)
			"/api/dashboard": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},

			// Operations endpoints (requires viewer+)
			"/api/operations":           {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/operations/dlq/retry": {RoleOperator, RoleAdmin},

			// Audit endpoints (requires viewer+)
			"/api/audit":             {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/audit/correlation": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/audit/actors":      {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/audit/export":      {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},

			// Execution endpoints (requires operator+)
			"/api/executions":         {RoleOperator, RoleAdmin},
			"/api/executions/journey": {RoleOperator, RoleAdmin},

			// Orchestration endpoints (requires operator+)
			"/api/orchestration": {RoleOperator, RoleAdmin},

			// Review endpoints (requires reviewer+)
			"/api/reviews": {RoleReviewer, RoleAdmin},

			// Approval endpoints (requires approver+)
			"/api/approvals": {RoleApprover, RoleAdmin},

			// Governance endpoints (requires reviewer+)
			"/api/governance": {RoleReviewer, RoleAdmin},
			"/api/drafts":     {RoleReviewer, RoleAdmin},

			// Configuration endpoints (requires admin)
			"/api/config":           {RoleAdmin},
			"/api/config/raw":       {RoleAdmin},
			"/api/config/providers": {RoleAdmin},

			// Discovery endpoints (requires admin)
			"/api/discovery":                 {RoleAdmin},
			"/api/discovery/docker":          {RoleAdmin},
			"/api/discovery/docker/mappings": {RoleAdmin},
			"/api/discovery/refresh":         {RoleAdmin},
			"/api/discovery/metrics":         {RoleAdmin},

			// Secrets endpoints (requires operator+)
			"/api/secrets": {RoleOperator, RoleAdmin},

			// Logs endpoints (requires operator+)
			"/api/logs": {RoleOperator, RoleAdmin},

			// User management (admin only)
			"/api/users": {RoleAdmin},

			// Session management (any authenticated user — lists own sessions)
			"/api/sessions": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},

			// Admin endpoints
			"/api/admin/sessions": {RoleAdmin},

			// Session refresh
			"/api/auth/refresh": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},

			// Password management
			"/api/auth/change-password": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/auth/reset-password":  {RoleAdmin},

			// Metrics analytics (requires viewer+)
			"/api/metrics":         {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/metrics/history": {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
			"/api/metrics/export":  {RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin},
		},
	}
}

// GetRequiredRoles returns the roles required to access an endpoint
func (pm *PermissionMatrix) GetRequiredRoles(path string) []string {
	if roles, exists := pm.Rules[path]; exists {
		return roles
	}

	// Check for prefix matches for dynamic routes
	return nil
}

// CanAccess checks if a user role can access a path
func (pm *PermissionMatrix) CanAccess(path, userRole string) bool {
	requiredRoles := pm.GetRequiredRoles(path)

	if len(requiredRoles) == 0 {
		// Public endpoint
		return true
	}

	return CanAccessEndpoint(userRole, requiredRoles...)
}

// IsPublic checks if a path is public
func (pm *PermissionMatrix) IsPublic(path string) bool {
	requiredRoles := pm.GetRequiredRoles(path)
	return len(requiredRoles) == 0
}

// GetAllProtectedPaths returns all protected paths
func (pm *PermissionMatrix) GetAllProtectedPaths() map[string][]string {
	protected := make(map[string][]string)
	for path, roles := range pm.Rules {
		if len(roles) > 0 {
			protected[path] = roles
		}
	}
	return protected
}

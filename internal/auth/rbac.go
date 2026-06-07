package auth

// Role constants
const (
	RoleViewer   = "viewer"
	RoleOperator = "operator"
	RoleReviewer = "reviewer"
	RoleApprover = "approver"
	RoleAdmin    = "admin"
)

// AllRoles is the complete list of defined roles
var AllRoles = []string{RoleViewer, RoleOperator, RoleReviewer, RoleApprover, RoleAdmin}

// IsValidRole checks if a role is recognized
func IsValidRole(role string) bool {
	for _, r := range AllRoles {
		if r == role {
			return true
		}
	}
	return false
}

// HasRole checks if a user has a specific role
func HasRole(userRole, requiredRole string) bool {
	if userRole == RoleAdmin {
		return true // Admin has all roles
	}
	return userRole == requiredRole
}

// HasAnyRole checks if a user has any of the specified roles
func HasAnyRole(userRole string, requiredRoles ...string) bool {
	if userRole == RoleAdmin {
		return true // Admin has all roles
	}
	for _, required := range requiredRoles {
		if userRole == required {
			return true
		}
	}
	return false
}

// RoleHierarchy returns roles that include this role (e.g., operator includes viewer permissions)
// This defines which higher roles implicitly grant lower role permissions
func RoleHierarchy(role string) []string {
	switch role {
	case RoleAdmin:
		return []string{RoleAdmin, RoleApprover, RoleReviewer, RoleOperator, RoleViewer}
	case RoleApprover:
		return []string{RoleApprover, RoleViewer}
	case RoleReviewer:
		return []string{RoleReviewer, RoleViewer}
	case RoleOperator:
		return []string{RoleOperator, RoleViewer}
	case RoleViewer:
		return []string{RoleViewer}
	default:
		return []string{}
	}
}

// HasHierarchicalRole checks if a user's role includes the required role in the hierarchy
func HasHierarchicalRole(userRole, requiredRole string) bool {
	hierarchy := RoleHierarchy(userRole)
	for _, r := range hierarchy {
		if r == requiredRole {
			return true
		}
	}
	return false
}

// CanAccessEndpoint checks if a user role can access an endpoint
// This is the main RBAC check using hierarchical roles
func CanAccessEndpoint(userRole string, requiredRoles ...string) bool {
	if len(requiredRoles) == 0 {
		return true // No role requirement
	}

	userHierarchy := RoleHierarchy(userRole)
	for _, required := range requiredRoles {
		for _, userRole := range userHierarchy {
			if userRole == required {
				return true
			}
		}
	}
	return false
}

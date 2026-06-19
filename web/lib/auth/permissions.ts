/**
 * Role-based access control (RBAC) permissions
 * Defines what each role can do
 */

export type Role = 'viewer' | 'operator' | 'reviewer' | 'approver' | 'admin'

/**
 * Permission levels - hierarchical
 * admin > approver > reviewer > operator > viewer
 */
export const ROLE_HIERARCHY: Record<Role, number> = {
  viewer: 1,
  operator: 2,
  reviewer: 3,
  approver: 4,
  admin: 5,
}

/**
 * Check if user can access based on required roles
 * A user can access if their role is one of the required roles
 */
export function canAccess(userRole: string | null | undefined, requiredRoles: Role[]): boolean {
  if (!userRole) return false
  if (requiredRoles.length === 0) return true

  const normalizedRole = (userRole.toLowerCase() as Role)
  return requiredRoles.includes(normalizedRole)
}

/**
 * Check if user role is equal to or higher than minimum level
 */
export function hasMinimumRole(userRole: string | null | undefined, minimumRole: Role): boolean {
  if (!userRole) return false

  const normalizedRole = (userRole.toLowerCase() as Role)
  const userLevel = ROLE_HIERARCHY[normalizedRole] ?? 0
  const minLevel = ROLE_HIERARCHY[minimumRole]

  return userLevel >= minLevel
}

/**
 * Get all pages accessible by a role
 */
export function getAccessiblePages(role: Role | null): string[] {
  if (!role) return []

  const pages: Record<Role, string[]> = {
    viewer: [
      '/dashboard',
      '/audit',
      '/events',
      '/discovery',
    ],
    operator: [
      '/dashboard',
      '/audit',
      '/events',
      '/discovery',
      '/operations',
      '/configuration',
    ],
    reviewer: [
      '/dashboard',
      '/audit',
      '/events',
      '/discovery',
      '/operations',
      '/configuration',
      '/execution',
      '/policies',
    ],
    approver: [
      '/dashboard',
      '/audit',
      '/events',
      '/discovery',
      '/operations',
      '/configuration',
      '/execution',
      '/policies',
      '/recommendations',
    ],
    admin: [
      '/dashboard',
      '/audit',
      '/events',
      '/discovery',
      '/operations',
      '/configuration',
      '/execution',
      '/policies',
      '/recommendations',
      '/users',
      '/settings',
      '/security',
      '/integrations',
      '/scheduler',
    ],
  }

  return pages[role] || []
}

/**
 * Check if a page requires authentication
 */
export function isProtectedPage(pathname: string): boolean {
  const publicPages = ['/login', '/public', '/health']
  return !publicPages.some(p => pathname.startsWith(p))
}

/**
 * Get user-friendly role name
 */
export function getRoleDisplayName(role: Role | null): string {
  const names: Record<Role, string> = {
    viewer: 'Viewer',
    operator: 'Operator',
    reviewer: 'Reviewer',
    approver: 'Approver',
    admin: 'Administrator',
  }

  if (!role) return 'Unknown'
  const normalizedRole = (role.toLowerCase() as Role)
  return names[normalizedRole] || role
}

/**
 * Get role description
 */
export function getRoleDescription(role: Role | null): string {
  const descriptions: Record<Role, string> = {
    viewer: 'View-only access to dashboards and events',
    operator: 'Can view and manage operations',
    reviewer: 'Can review and approve changes',
    approver: 'Can approve execution and policy changes',
    admin: 'Full system access and administration',
  }

  if (!role) return ''
  const normalizedRole = (role.toLowerCase() as Role)
  return descriptions[normalizedRole] || ''
}

/**
 * Is role valid
 */
export function isValidRole(role: unknown): role is Role {
  const validRoles: Role[] = ['viewer', 'operator', 'reviewer', 'approver', 'admin']
  return typeof role === 'string' && validRoles.includes(role as Role)
}

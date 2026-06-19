/**
 * Hook for role-based access control
 * Provides utilities to check permissions
 */

import { useAuth } from '@/contexts/AuthContext'
import * as permissions from '@/lib/auth/permissions'

export interface UsePermissionsReturn {
  canAccess: (requiredRoles: permissions.Role[]) => boolean
  hasMinimumRole: (minimumRole: permissions.Role) => boolean
  getAccessiblePages: () => string[]
  isAdmin: boolean
  isOperator: boolean
  isReviewer: boolean
  isApprover: boolean
  isViewer: boolean
  roleName: string
  roleDescription: string
}

/**
 * Hook to check user permissions
 */
export function usePermissions(): UsePermissionsReturn {
  const { user, role } = useAuth()

  const userRole = (role.toLowerCase() as permissions.Role) || null

  return {
    canAccess: (requiredRoles: permissions.Role[]) => {
      return permissions.canAccess(role, requiredRoles)
    },

    hasMinimumRole: (minimumRole: permissions.Role) => {
      return permissions.hasMinimumRole(role, minimumRole)
    },

    getAccessiblePages: () => {
      return permissions.getAccessiblePages(userRole)
    },

    isAdmin: userRole === 'admin',
    isOperator: userRole === 'operator' || userRole === 'admin',
    isReviewer: userRole === 'reviewer' || userRole === 'approver' || userRole === 'admin',
    isApprover: userRole === 'approver' || userRole === 'admin',
    isViewer: !!userRole,

    roleName: permissions.getRoleDisplayName(userRole),
    roleDescription: permissions.getRoleDescription(userRole),
  }
}

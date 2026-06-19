'use client'

import { ReactNode } from 'react'
import { usePermissions } from '@/hooks/usePermissions'
import * as permissions from '@/lib/auth/permissions'

interface RequireRoleProps {
  roles: permissions.Role[]
  children: ReactNode
  fallback?: ReactNode
}

/**
 * Component that shows content only to users with specific roles
 * Shows fallback if user doesn't have required role
 */
export function RequireRole({ roles, children, fallback }: RequireRoleProps) {
  const { canAccess } = usePermissions()

  if (!canAccess(roles)) {
    return fallback ?? <AccessDeniedScreen roles={roles} />
  }

  return <>{children}</>
}

/**
 * Default access denied screen
 */
function AccessDeniedScreen({ roles }: { roles: permissions.Role[] }) {
  const roleNames = roles.map(r => permissions.getRoleDisplayName(r)).join(', ')

  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center max-w-md">
        <div className="text-6xl mb-4">🔒</div>
        <h1 className="text-2xl font-bold mb-2">Access Denied</h1>
        <p className="text-gray-600 mb-4">
          You don't have permission to view this page. Required role{roles.length > 1 ? 's' : ''}: {roleNames}
        </p>
        <a href="/dashboard" className="text-primary hover:underline">
          Return to Dashboard
        </a>
      </div>
    </div>
  )
}

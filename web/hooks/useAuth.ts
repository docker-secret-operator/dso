/**
 * Hook for authentication operations
 * Wraps AuthContext with common operations
 */

import { useAuth as useAuthContext } from '@/contexts/AuthContext'

/**
 * Hook to access auth state and operations
 * Re-exports useAuth from context for convenience
 */
export function useAuth() {
  return useAuthContext()
}

/**
 * Hook to check if user is authenticated
 */
export function useIsAuthenticated(): boolean {
  const { isAuthenticated } = useAuthContext()
  return isAuthenticated
}

/**
 * Hook to check if auth is still loading
 */
export function useAuthLoading(): boolean {
  const { isLoading } = useAuthContext()
  return isLoading
}

/**
 * Hook to get current user
 */
export function useCurrentUser() {
  const { user } = useAuthContext()
  return user
}

/**
 * Hook to get current user role
 */
export function useUserRole(): string {
  const { role } = useAuthContext()
  return role
}

'use client'

import { ReactNode } from 'react'
import { useRouter } from 'next/navigation'
import { useAuth } from '@/hooks/useAuth'

interface ProtectedRouteProps {
  children: ReactNode
  fallback?: ReactNode
}

/**
 * Component that protects a route from unauthenticated users
 * Shows fallback while loading, redirects to login if not authenticated
 */
export function ProtectedRoute({ children, fallback }: ProtectedRouteProps) {
  const { isAuthenticated, isLoading } = useAuth()
  const router = useRouter()

  // Show loading state while auth initializes
  if (isLoading) {
    return fallback ?? <LoadingScreen />
  }

  // Redirect to login if not authenticated
  if (!isAuthenticated) {
    router.push('/login')
    return fallback ?? <LoadingScreen />
  }

  return <>{children}</>
}

/**
 * Default loading screen
 */
function LoadingScreen() {
  return (
    <div className="flex items-center justify-center min-h-screen">
      <div className="text-center">
        <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-primary mx-auto mb-4"></div>
        <p className="text-gray-600">Loading...</p>
      </div>
    </div>
  )
}

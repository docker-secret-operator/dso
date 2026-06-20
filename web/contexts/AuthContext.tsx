'use client'

import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react'
import * as storage from '@/lib/auth/storage'
import * as session from '@/lib/auth/session'
import { login as apiLogin, currentUser as apiCurrentUser } from '@/lib/api/auth'
import { UserInfo } from '@/lib/api/types'

export interface AuthContextValue {
  user: UserInfo | null
  role: string
  isAuthenticated: boolean
  isLoading: boolean
  mustChangePassword: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshSession: () => Promise<boolean>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<UserInfo | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isAuthenticated, setIsAuthenticated] = useState(false)

  // Initialize session on mount
  useEffect(() => {
    const initAuth = async () => {
      setIsLoading(true)
      try {
        const initialUser = await session.initializeSession()
        if (initialUser) {
          setUser(initialUser)
          setIsAuthenticated(true)

          // Redirect to password change if needed
          if (initialUser.must_change_password && typeof window !== 'undefined') {
            if (window.location.pathname !== '/settings/password') {
              window.location.href = '/settings/password'
            }
          }
        } else {
          setUser(null)
          setIsAuthenticated(false)

          // Redirect to login if not already there
          if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
            window.location.href = '/login'
          }
        }
      } catch (error) {
        if (process.env.NODE_ENV === 'development') {
          console.error('Failed to initialize auth:', error)
        }
        setUser(null)
        setIsAuthenticated(false)
      } finally {
        setIsLoading(false)
      }
    }

    initAuth()
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    setIsLoading(true)
    try {
      const response = await apiLogin({ username, password })

      // Store auth data
      storage.setAccessToken(response.token)
      storage.setStoredUser(response.user)
      storage.setStoredSession(response.session)

      setUser(response.user)
      setIsAuthenticated(true)
    } catch (error) {
      setUser(null)
      setIsAuthenticated(false)
      throw error
    } finally {
      setIsLoading(false)
    }
  }, [])

  const logout = useCallback(async () => {
    setIsLoading(true)
    try {
      await session.clearSession()
      setUser(null)
      setIsAuthenticated(false)

      if (typeof window !== 'undefined') {
        window.location.href = '/login'
      }
    } catch (error) {
      if (process.env.NODE_ENV === 'development') {
        console.error('Logout error:', error)
      }
      // Always clear auth state and redirect even if logout fails
      setUser(null)
      setIsAuthenticated(false)
      if (typeof window !== 'undefined') {
        window.location.href = '/login'
      }
    } finally {
      setIsLoading(false)
    }
  }, [])

  const refreshSession = useCallback(async (): Promise<boolean> => {
    try {
      const success = await session.refreshAccessToken()

      if (success) {
        // Update user data if needed
        const userData = await apiCurrentUser()
        storage.setStoredUser(userData)
        setUser(userData)
        return true
      } else {
        // Refresh failed, clear auth
        setUser(null)
        setIsAuthenticated(false)
        return false
      }
    } catch (error) {
      if (process.env.NODE_ENV === 'development') {
        console.error('Session refresh error:', error)
      }
      setUser(null)
      setIsAuthenticated(false)
      return false
    }
  }, [])

  const value: AuthContextValue = {
    user,
    role: user?.role ?? '',
    isAuthenticated,
    isLoading,
    mustChangePassword: user?.must_change_password ?? false,
    login,
    logout,
    refreshSession,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

/**
 * Hook to access auth context
 * Must be used within AuthProvider
 */
export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return context
}

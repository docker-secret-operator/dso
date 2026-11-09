'use client'

import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react'
import { login as apiLogin, logout as apiLogout, checkSession } from '@/lib/api/auth'

/**
 * PASS 1 SCOPE / rewrite rationale:
 *
 * feature/web-ui's AuthContext assumed the login response carried a token,
 * a user profile, and a session object, all persisted to localStorage, plus
 * a refreshSession()/currentUser() flow against /api/auth/refresh and
 * /api/auth/me. None of that matches the real backend
 * (internal/server/rest.go): login/logout responses are just
 * `{"status":"ok"}`, the session token only ever exists as an HttpOnly
 * cookie, and there is no /me or /refresh endpoint -- DSO's webui is a
 * single-operator model with no per-user profile to fetch.
 *
 * So this version has no `user` or `role` in state (nothing server-visible
 * to hydrate them from) and derives `isAuthenticated` from
 * GET /api/auth/session (200 {"authenticated":true} vs 401), which is the
 * only signal the browser can get about session validity.
 */

export interface AuthContextValue {
  isAuthenticated: boolean
  isLoading: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refreshSession: () => Promise<boolean>
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isLoading, setIsLoading] = useState(true)
  const [isAuthenticated, setIsAuthenticated] = useState(false)

  const refreshSession = useCallback(async (): Promise<boolean> => {
    const ok = await checkSession()
    setIsAuthenticated(ok)
    return ok
  }, [])

  // Initialize session on mount
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setIsLoading(true)
      const ok = await checkSession()
      if (cancelled) return
      setIsAuthenticated(ok)
      setIsLoading(false)

      if (!ok && typeof window !== 'undefined' && window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const login = useCallback(async (username: string, password: string) => {
    setIsLoading(true)
    try {
      await apiLogin({ username, password })
      setIsAuthenticated(true)
    } catch (error) {
      setIsAuthenticated(false)
      throw error
    } finally {
      setIsLoading(false)
    }
  }, [])

  const logout = useCallback(async () => {
    setIsLoading(true)
    try {
      await apiLogout()
    } catch (error) {
      if (process.env.NODE_ENV === 'development') {
        console.error('Logout error:', error)
      }
      // Always clear auth state and redirect even if the request itself failed.
    } finally {
      setIsAuthenticated(false)
      setIsLoading(false)
      if (typeof window !== 'undefined') {
        window.location.href = '/login'
      }
    }
  }, [])

  const value: AuthContextValue = {
    isAuthenticated,
    isLoading,
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

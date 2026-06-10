'use client'

import { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react'

interface AuthUser {
  id: string
  username: string
  display_name: string
  role: string
  must_change_password: boolean
  password_expires_at?: string
}

interface AuthContextValue {
  user: AuthUser | null
  role: string
  loading: boolean
  mustChangePassword: boolean
  logout: () => void
}

const AuthContext = createContext<AuthContextValue>({
  user: null,
  role: '',
  loading: true,
  mustChangePassword: false,
  logout: () => {},
})

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const token = localStorage.getItem('dso_api_token')
    if (!token) {
      setLoading(false)
      return
    }
    fetch('/api/auth/me', {
      headers: { Authorization: `Bearer ${token}` },
    })
      .then(r => (r.ok ? r.json() : null))
      .then((data: AuthUser | null) => {
        setUser(data)
        // FG4: redirect to password change page if required
        if (data?.must_change_password && window.location.pathname !== '/settings/password') {
          window.location.href = '/settings/password'
        }
      })
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const logout = useCallback(() => {
    const token = localStorage.getItem('dso_api_token')
    if (token) {
      fetch('/api/auth/logout', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: '{}',
      }).catch(() => {})
    }
    localStorage.removeItem('dso_api_token')
    window.location.href = '/login'
  }, [])

  const mustChangePassword = user?.must_change_password ?? false

  return (
    <AuthContext.Provider value={{ user, role: user?.role ?? '', loading, mustChangePassword, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}

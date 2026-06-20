'use client'

import { useEffect, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { AuthProvider } from '@/contexts/AuthContext'
import { SessionTimeoutWarning } from '@/components/session-timeout-warning'
import { WebSocketProvider } from '@/contexts/websocket-context'

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false)
  const router = useRouter()
  const pathname = usePathname()
  const isLoginPage = pathname === '/login'

  useEffect(() => {
    const validateAuth = async () => {
      const token = localStorage.getItem('dso_api_token')

      if (!token && !isLoginPage) {
        router.replace('/login')
        setReady(true)
        return
      }

      if (token && !isLoginPage) {
        // Validate token with backend to ensure it's still valid
        try {
          const response = await fetch('/api/auth/validate', {
            method: 'GET',
            headers: {
              'Authorization': `Bearer ${token}`,
            },
          })

          if (!response.ok) {
            // Token is invalid
            localStorage.removeItem('dso_api_token')
            router.replace('/login')
            setReady(true)
            return
          }
        } catch (error) {
          // On network errors, assume token might be valid and continue
          // The interceptor will handle actual auth failures
        }
      }

      if (token && isLoginPage) {
        router.replace('/dashboard')
        setReady(true)
        return
      }

      setReady(true)
    }

    validateAuth()
  }, [isLoginPage, router])

  if (!ready) {
    // Blank dark screen while checking auth — avoids flash
    return <div className="min-h-screen bg-[#0a0b0f]" />
  }

  if (isLoginPage) {
    // Login page renders its own full-screen layout
    return <>{children}</>
  }

  return (
    <AuthProvider>
      <WebSocketProvider>
        {children}
        <SessionTimeoutWarning />
      </WebSocketProvider>
    </AuthProvider>
  )
}

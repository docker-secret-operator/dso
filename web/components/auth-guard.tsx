'use client'

import { useEffect, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { AuthProvider } from '@/contexts/AuthContext'
import { checkSession } from '@/lib/api/auth'

// TODO(pass 2+ UI port): feature/web-ui's AuthGuard also wraps children in
// WebSocketProvider and renders SessionTimeoutWarning. Both are deferred --
// they belong to the dashboard UI porting pass, not the auth-plumbing pass.
// Re-add them here once contexts/websocket-context.tsx and
// components/session-timeout-warning.tsx are ported.

export function AuthGuard({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false)
  const router = useRouter()
  const pathname = usePathname()
  const isLoginPage = pathname === '/login'

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      // There is no client-visible token to inspect (the session lives in an
      // HttpOnly cookie), so auth state can only be learned by asking the
      // server via GET /api/auth/session.
      const authenticated = await checkSession()
      if (cancelled) return

      if (!authenticated && !isLoginPage) {
        router.replace('/login')
        setReady(true)
        return
      }

      if (authenticated && isLoginPage) {
        router.replace('/dashboard')
        setReady(true)
        return
      }

      setReady(true)
    })()
    return () => {
      cancelled = true
    }
  }, [isLoginPage, router])

  if (!ready) {
    // Blank dark screen while checking auth — avoids flash
    return <div className="min-h-screen bg-[#0B1020]" />
  }

  if (isLoginPage) {
    // Login page renders its own full-screen layout
    return <>{children}</>
  }

  return <AuthProvider>{children}</AuthProvider>
}

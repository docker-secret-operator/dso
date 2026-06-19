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
    const token = localStorage.getItem('dso_api_token')

    if (!token && !isLoginPage) {
      router.replace('/login')
      return
    }

    if (token && isLoginPage) {
      router.replace('/dashboard')
      return
    }

    setReady(true)
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

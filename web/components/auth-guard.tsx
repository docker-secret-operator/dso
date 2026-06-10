'use client'

import { useEffect, useState } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { Sidebar } from '@/components/sidebar'
import { Header } from '@/components/header'
import { AuthProvider } from '@/lib/auth-context'
import { SessionTimeoutWarning } from '@/components/session-timeout-warning'
import { WebSocketProvider } from '@/contexts/websocket-context'

interface AuthGuardProps {
  children: React.ReactNode
}

export function AuthGuard({ children }: AuthGuardProps) {
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

  if (!ready) return null

  if (isLoginPage) {
    return <>{children}</>
  }

  return (
    <AuthProvider>
      <WebSocketProvider>
        <div className="flex h-screen overflow-hidden">
          <Sidebar />
          <div className="flex flex-1 flex-col overflow-hidden">
            <Header />
            <main className="flex-1 overflow-y-auto">
              <div className="h-full bg-background">
                {children}
              </div>
            </main>
          </div>
        </div>
        <SessionTimeoutWarning />
      </WebSocketProvider>
    </AuthProvider>
  )
}

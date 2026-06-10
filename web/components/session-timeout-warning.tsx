'use client'

import { useEffect, useState, useRef, useCallback } from 'react'
import { useAuth } from '@/lib/auth-context'
import { apiClient } from '@/lib/api-client'

const WARN_BEFORE_MS = 5 * 60 * 1000   // 5 minutes
const POLL_INTERVAL_MS = 30 * 1000      // check every 30 s

export function SessionTimeoutWarning() {
  const { logout } = useAuth()
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const [showModal, setShowModal] = useState(false)
  const [extending, setExtending] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Fetch session info and update expiry
  const fetchExpiry = useCallback(async () => {
    try {
      const s = await apiClient.getSessionInfo()
      setExpiresAt(new Date(s.expires_at).getTime())
    } catch {
      // session gone — the 401 interceptor will redirect
    }
  }, [])

  useEffect(() => {
    fetchExpiry()
    timerRef.current = setInterval(fetchExpiry, POLL_INTERVAL_MS)
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [fetchExpiry])

  // Determine whether to show the warning
  useEffect(() => {
    if (expiresAt === null) return
    const remaining = expiresAt - Date.now()
    setShowModal(remaining > 0 && remaining <= WARN_BEFORE_MS)
  }, [expiresAt])

  async function handleExtend() {
    setExtending(true)
    try {
      const result = await apiClient.refreshSession()
      setExpiresAt(new Date(result.expires_at).getTime())
      setShowModal(false)
    } catch {
      // if refresh fails, the 401 interceptor handles it
    } finally {
      setExtending(false)
    }
  }

  if (!showModal) return null

  const minsLeft = expiresAt ? Math.max(0, Math.ceil((expiresAt - Date.now()) / 60_000)) : 0

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background rounded-lg border border-border shadow-xl p-6 w-full max-w-sm space-y-4">
        <h2 className="text-lg font-semibold text-foreground">Session Expiring Soon</h2>
        <p className="text-sm text-muted-foreground">
          Your session expires in approximately {minsLeft} minute{minsLeft !== 1 ? 's' : ''}.
          Would you like to stay signed in?
        </p>
        <div className="flex gap-3 justify-end">
          <button
            onClick={logout}
            className="px-4 py-2 text-sm rounded-md border border-border hover:bg-muted"
          >
            Logout
          </button>
          <button
            onClick={handleExtend}
            disabled={extending}
            className="px-4 py-2 text-sm rounded-md bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {extending ? 'Extending…' : 'Extend Session'}
          </button>
        </div>
      </div>
    </div>
  )
}

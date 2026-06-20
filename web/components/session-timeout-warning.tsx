'use client'

import { useEffect, useState, useRef, useCallback } from 'react'
import { useAuth } from '@/contexts/AuthContext'
import { apiClient } from '@/lib/api-client'

const WARN_BEFORE_MS = 5 * 60 * 1000   // 5 minutes
const POLL_INTERVAL_MS = 30 * 1000      // check every 30 s

export function SessionTimeoutWarning() {
  const { logout } = useAuth()
  const [expiresAt, setExpiresAt] = useState<number | null>(null)
  const [showModal, setShowModal] = useState(false)
  const [extending, setExtending] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const mountedRef = useRef(true)

  // Fetch session info and update expiry
  const fetchExpiry = useCallback(async () => {
    if (!mountedRef.current) return
    try {
      const s = await apiClient.getSessionInfo()
      if (mountedRef.current) {
        setExpiresAt(new Date(s.expires_at).getTime())
      }
    } catch {
      // session gone — the 401 interceptor will redirect
    }
  }, [])

  useEffect(() => {
    mountedRef.current = true
    fetchExpiry()
    timerRef.current = setInterval(fetchExpiry, POLL_INTERVAL_MS)
    return () => {
      mountedRef.current = false
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="bg-[#111318] rounded-xl border border-white/[0.09] shadow-2xl p-6 w-full max-w-sm space-y-4">
        <h2 className="text-base font-semibold text-slate-100">Session Expiring Soon</h2>
        <p className="text-sm text-slate-400">
          Your session expires in approximately {minsLeft} minute{minsLeft !== 1 ? 's' : ''}.
          Would you like to stay signed in?
        </p>
        <div className="flex gap-3 justify-end">
          <button
            onClick={logout}
            className="px-4 py-2 text-sm rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
          >
            Sign out
          </button>
          <button
            onClick={handleExtend}
            disabled={extending}
            className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-500 disabled:opacity-50 transition-colors"
          >
            {extending ? 'Extending…' : 'Extend Session'}
          </button>
        </div>
      </div>
    </div>
  )
}

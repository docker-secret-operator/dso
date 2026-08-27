'use client'

import { useEffect, useState } from 'react'
import { Bell } from 'lucide-react'

import { fetchLogs } from '@/lib/api/logs'
import { cn } from '@/lib/utils'
import type { Event } from '@/hooks/useWebSocket'

const POLL_INTERVAL_MS = 30000
const SEEN_KEY = 'dso_notifications_seen_at'

/**
 * Small notification bell surfacing recent failed/error runtime events from
 * the real EventStore (/api/logs). Deliberately does not port
 * advanced-platform's notification-center.tsx: that component categorizes
 * notifications using `action`/`severity`/`message` fields that don't exist
 * on this branch's real Event shape (event_type/status/secret/container) --
 * porting it verbatim would silently produce meaningless categories. See
 * docs/webui-phase1-notes.md.
 */
export function NotificationBell() {
  const [failures, setFailures] = useState<Event[]>([])
  const [open, setOpen] = useState(false)
  const [lastSeenAt, setLastSeenAt] = useState<number>(0)

  useEffect(() => {
    if (typeof window !== 'undefined') {
      const stored = window.localStorage.getItem(SEEN_KEY)
      setLastSeenAt(stored ? Number(stored) : 0)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    async function poll() {
      try {
        const events = await fetchLogs(50, 'failure')
        if (!cancelled) setFailures(events)
      } catch {
        // Non-fatal: notification bell degrades to empty rather than
        // surfacing a page-level error for a secondary feature.
      }
    }
    poll()
    const id = setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const unseenCount = failures.filter((ev) => new Date(ev.timestamp).getTime() > lastSeenAt).length

  function handleOpen() {
    const next = !open
    setOpen(next)
    if (next && typeof window !== 'undefined') {
      const now = Date.now()
      window.localStorage.setItem(SEEN_KEY, String(now))
      setLastSeenAt(now)
    }
  }

  return (
    <div className="relative">
      <button
        type="button"
        data-testid="notification-bell"
        onClick={handleOpen}
        aria-label={`Notifications${unseenCount > 0 ? ` (${unseenCount} unread)` : ''}`}
        className="relative flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
      >
        <Bell className="h-4 w-4" />
        {unseenCount > 0 && (
          <span className="absolute right-1 top-1 flex h-2 w-2 rounded-full bg-red-500" />
        )}
      </button>

      {open && (
        <div
          data-testid="notification-panel"
          className="absolute right-0 top-full z-50 mt-2 w-80 rounded-lg border border-border bg-card shadow-xl"
        >
          <div className="border-b border-border px-4 py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Recent failures
          </div>
          <div className="max-h-72 overflow-y-auto">
            {failures.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-muted-foreground">No recent failures.</p>
            ) : (
              failures.slice(0, 20).map((ev, idx) => (
                <div
                  key={`${ev.timestamp}-${idx}`}
                  className={cn(
                    'border-b border-border/60 px-4 py-2.5 text-sm last:border-0',
                    new Date(ev.timestamp).getTime() > lastSeenAt && 'bg-red-500/5'
                  )}
                >
                  <p className="truncate text-foreground">
                    {ev.event_type ?? 'event'}
                    {ev.secret ? ` — ${ev.secret}` : ''}
                  </p>
                  {ev.error && <p className="truncate text-xs text-red-400/80">{ev.error}</p>}
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {new Date(ev.timestamp).toLocaleString()}
                  </p>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}

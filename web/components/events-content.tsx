'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { ArrowLeft, Circle } from 'lucide-react'
import { useWebSocket, type Event } from '@/hooks/useWebSocket'
import { fetchEvents } from '@/lib/api/events'

function statusColor(status?: string): string {
  switch (status) {
    case 'success':
      return 'text-emerald-400'
    case 'failure':
      return 'text-red-400'
    case 'pending':
      return 'text-amber-400'
    default:
      return 'text-slate-500'
  }
}

function connectionColor(state: string): string {
  switch (state) {
    case 'connected':
      return 'text-emerald-400'
    case 'reconnecting':
      return 'text-amber-400'
    default:
      return 'text-red-400'
  }
}

/**
 * Real events content: GET /api/events for initial history, then
 * /api/events/ws for realtime updates via useWebSocket. Live events are
 * merged in front of the initial history, de-duplicating by identity so a
 * historical event re-broadcast on connect doesn't double up.
 */
export function EventsContent() {
  const [history, setHistory] = useState<Event[]>([])
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState<string | null>(null)

  const { events: liveEvents, connectionState } = useWebSocket('/api/events/ws')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setHistoryLoading(true)
      setHistoryError(null)
      try {
        const data = await fetchEvents(50)
        if (!cancelled) setHistory(data)
      } catch (err) {
        if (!cancelled) {
          setHistoryError(err instanceof Error ? err.message : 'Failed to load events')
        }
      } finally {
        if (!cancelled) setHistoryLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  // Merge: live events (newest first from the hook) take priority; fall back
  // to the REST-loaded history for anything not already seen live. Since the
  // WS connection also replays recent history on connect, de-dup by a best
  // effort composite key.
  const seen = new Set(liveEvents.map((e) => `${e.timestamp}:${e.secret ?? ''}:${e.event_type ?? ''}`))
  const merged = [
    ...liveEvents,
    ...history.filter((e) => !seen.has(`${e.timestamp}:${e.secret ?? ''}:${e.event_type ?? ''}`)),
  ]

  const loading = historyLoading && merged.length === 0

  return (
    <div className="min-h-screen bg-[#0B1020] px-6 py-8">
      <div className="max-w-4xl mx-auto">
        <header className="flex items-center justify-between mb-8">
          <div className="flex items-center gap-3">
            <Link href="/dashboard" className="text-slate-500 hover:text-slate-300 transition-colors">
              <ArrowLeft className="w-4 h-4" />
            </Link>
            <div>
              <h1 className="text-xl font-semibold text-slate-100">Events</h1>
              <p className="text-sm text-slate-500 mt-1">Live secret rotation and injection activity</p>
            </div>
          </div>
          <div className="flex items-center gap-1.5 text-xs" data-testid="ws-connection-state">
            <Circle className={`w-2 h-2 fill-current ${connectionColor(connectionState)}`} />
            <span className={connectionColor(connectionState)}>{connectionState}</span>
          </div>
        </header>

        {loading && (
          <div data-testid="events-loading" className="text-sm text-slate-500">
            Loading events…
          </div>
        )}

        {!loading && historyError && merged.length === 0 && (
          <div data-testid="events-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {historyError}
          </div>
        )}

        {!loading && merged.length === 0 && !historyError && (
          <div data-testid="events-empty" className="text-sm text-slate-600">
            No events yet.
          </div>
        )}

        {merged.length > 0 && (
          <div data-testid="events-list" className="bg-[#111827] border border-white/[0.09] rounded-2xl divide-y divide-white/[0.05]">
            {merged.map((ev, idx) => (
              <div key={`${ev.timestamp}-${idx}`} className="px-5 py-3 flex items-center justify-between text-sm">
                <div className="flex items-center gap-3 min-w-0">
                  <Circle className={`w-2 h-2 fill-current flex-shrink-0 ${statusColor(ev.status)}`} />
                  <div className="min-w-0">
                    <p className="text-slate-200 truncate">
                      {ev.event_type ?? 'event'}
                      {ev.secret ? ` — ${ev.secret}` : ''}
                    </p>
                    {ev.container && (
                      <p className="text-xs text-slate-600 truncate">container: {ev.container}</p>
                    )}
                    {ev.error && (
                      <p className="text-xs text-red-400/80 truncate">{ev.error}</p>
                    )}
                  </div>
                </div>
                <span className="text-xs text-slate-600 flex-shrink-0 ml-4">
                  {new Date(ev.timestamp).toLocaleString()}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}


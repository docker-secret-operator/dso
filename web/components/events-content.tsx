'use client'

import { useEffect, useState } from 'react'
import { Circle } from 'lucide-react'
import { useWebSocket, type Event } from '@/hooks/useWebSocket'
import { fetchEvents } from '@/lib/api/events'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

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
    <div className="px-8 py-8">
      <div className="mx-auto max-w-4xl">
        <header className="mb-8 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">Events</h1>
            <p className="mt-1 text-sm text-muted-foreground">Live secret rotation and injection activity</p>
          </div>
          <Badge
            variant={connectionState === 'connected' ? 'success' : connectionState === 'reconnecting' ? 'warning' : 'destructive'}
            data-testid="ws-connection-state"
          >
            <Circle className="h-2 w-2 fill-current" />
            {connectionState}
          </Badge>
        </header>

        {loading && (
          <div data-testid="events-loading" className="text-sm text-muted-foreground">
            Loading events…
          </div>
        )}

        {!loading && historyError && merged.length === 0 && (
          <div data-testid="events-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {historyError}
          </div>
        )}

        {!loading && merged.length === 0 && !historyError && (
          <div data-testid="events-empty" className="text-sm text-muted-foreground">
            No events yet.
          </div>
        )}

        {merged.length > 0 && (
          <Card data-testid="events-list" className="divide-y divide-border overflow-hidden">
            {merged.map((ev, idx) => (
              <div key={`${ev.timestamp}-${idx}`} className="flex items-center justify-between px-5 py-3 text-sm">
                <div className="flex min-w-0 items-center gap-3">
                  <Circle className={`h-2 w-2 flex-shrink-0 fill-current ${statusColor(ev.status)}`} />
                  <div className="min-w-0">
                    <p className="truncate text-foreground">
                      {ev.event_type ?? 'event'}
                      {ev.secret ? ` — ${ev.secret}` : ''}
                    </p>
                    {ev.container && (
                      <p className="truncate text-xs text-muted-foreground">container: {ev.container}</p>
                    )}
                    {ev.error && <p className="truncate text-xs text-red-400/80">{ev.error}</p>}
                  </div>
                </div>
                <span className="ml-4 flex-shrink-0 text-xs text-muted-foreground">
                  {new Date(ev.timestamp).toLocaleString()}
                </span>
              </div>
            ))}
          </Card>
        )}
      </div>
    </div>
  )
}


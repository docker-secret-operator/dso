'use client'

import { useEffect, useState } from 'react'
import { Circle } from 'lucide-react'
import type { Event } from '@/hooks/useWebSocket'
import { fetchLogs } from '@/lib/api/logs'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

const SEVERITY_OPTIONS = [
  { value: '', label: 'All severities' },
  { value: 'success', label: 'Success' },
  { value: 'failure', label: 'Failure' },
  { value: 'pending', label: 'Pending' },
]

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

/**
 * Real logs content: GET /api/logs, optionally filtered by ?severity=. The
 * backend endpoint (internal/server/rest.go handleLogs) reads from the same
 * in-memory EventStore as /api/events -- this page is a filterable,
 * point-in-time view rather than a live feed (no WebSocket here).
 */
export function LogsContent() {
  const [logs, setLogs] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [severity, setSeverity] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const data = await fetchLogs(100, severity || undefined)
        if (!cancelled) setLogs(data)
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load logs')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [severity])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-4xl">
        <header className="mb-8 flex items-center justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">Logs</h1>
            <p className="mt-1 text-sm text-muted-foreground">Recent runtime events from the DSO agent</p>
          </div>
          <select
            data-testid="logs-severity-filter"
            value={severity}
            onChange={(e) => setSeverity(e.target.value)}
            className="rounded-lg border border-border bg-card px-3 py-2 text-sm text-foreground"
          >
            {SEVERITY_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </header>

        {loading && (
          <div data-testid="logs-loading" className="text-sm text-muted-foreground">
            Loading logs…
          </div>
        )}

        {!loading && error && (
          <div data-testid="logs-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {!loading && !error && logs.length === 0 && (
          <div data-testid="logs-empty" className="text-sm text-muted-foreground">
            No logs yet.
          </div>
        )}

        {!loading && !error && logs.length > 0 && (
          <Card data-testid="logs-list" className="divide-y divide-border overflow-hidden">
            {logs.map((ev, idx) => (
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
                <div className="ml-4 flex flex-shrink-0 items-center gap-3">
                  {ev.status && (
                    <Badge
                      variant={ev.status === 'success' ? 'success' : ev.status === 'failure' ? 'destructive' : 'warning'}
                    >
                      {ev.status}
                    </Badge>
                  )}
                  <span className="text-xs text-muted-foreground">{new Date(ev.timestamp).toLocaleString()}</span>
                </div>
              </div>
            ))}
          </Card>
        )}
      </div>
    </div>
  )
}

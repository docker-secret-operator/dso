'use client'

import { useEffect, useMemo, useState } from 'react'
import { Circle, ScrollText, Search } from 'lucide-react'
import type { Event } from '@/hooks/useWebSocket'
import { fetchLogs } from '@/lib/api/logs'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'

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
  const [search, setSearch] = useState('')

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

  const query = search.trim().toLowerCase()
  const filteredLogs = useMemo(() => {
    if (!query) return logs
    return logs.filter((ev) => {
      const haystack = [ev.event_type, ev.secret, ev.container, ev.error, ev.status].filter(Boolean).join(' ').toLowerCase()
      return haystack.includes(query)
    })
  }, [logs, query])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-4xl">
        <header className="mb-8 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">Logs</h1>
            <p className="mt-1 text-sm text-muted-foreground">Recent runtime events from the DSO agent</p>
          </div>
          <div className="flex items-center gap-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                data-testid="logs-search"
                placeholder="Search logs..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-48 pl-9"
              />
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
          </div>
        </header>

        {loading && (
          <Card data-testid="logs-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <div className="flex min-w-0 items-center gap-3">
                  <Skeleton className="h-2 w-2 flex-shrink-0 rounded-full" />
                  <Skeleton className="h-4 w-48" />
                </div>
                <Skeleton className="h-4 w-24" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div data-testid="logs-error" className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400">
            {error}
          </div>
        )}

        {!loading && !error && logs.length === 0 && (
          <Card>
            <EmptyState
              data-testid="logs-empty"
              icon={ScrollText}
              title="No logs yet"
              description="Runtime events from the DSO agent will show up here as they happen."
            />
          </Card>
        )}

        {!loading && !error && logs.length > 0 && filteredLogs.length === 0 && (
          <Card>
            <EmptyState
              data-testid="logs-no-match"
              icon={Search}
              title={`No logs match "${search.trim()}"`}
              description="Try a different search term."
            />
          </Card>
        )}

        {!loading && !error && filteredLogs.length > 0 && (
          <Card data-testid="logs-list" className="divide-y divide-border overflow-hidden">
            {filteredLogs.map((ev, idx) => (
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

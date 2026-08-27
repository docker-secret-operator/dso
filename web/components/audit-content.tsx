'use client'

import { useEffect, useMemo, useState } from 'react'
import { Circle, FileClock, Search, Info } from 'lucide-react'
import type { Event } from '@/hooks/useWebSocket'
import { fetchAudit } from '@/lib/api/audit'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'

/**
 * Audit History content: GET /api/audit. This is the SAME bounded,
 * in-memory EventStore that backs /api/events (live feed) and /api/logs
 * (point-in-time list) -- there is no separate persistent audit-events
 * table in DSO today. This page is explicitly NOT "Live Events": it is a
 * chronological, searchable record of what already happened, with columns
 * chosen for an audit read (result/resource first), but it shares the exact
 * same data source and the exact same non-persistence-across-restart
 * caveat as Logs. See docs/webui-phase1-notes.md.
 */
export function AuditContent() {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const data = await fetchAudit(200)
        if (!cancelled) setEvents(data)
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load audit history')
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const query = search.trim().toLowerCase()
  const filtered = useMemo(() => {
    if (!query) return events
    return events.filter((ev) => {
      const haystack = [ev.event_type, ev.secret, ev.container, ev.status, ev.error]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return haystack.includes(query)
    })
  }, [events, query])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-xl font-semibold text-foreground">Audit History</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              Chronological record of rotation and injection outcomes
            </p>
          </div>
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              data-testid="audit-search"
              placeholder="Search audit history..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-64 pl-9"
            />
          </div>
        </header>

        <div className="mb-6 flex items-start gap-2 rounded-lg border border-border bg-muted/20 px-4 py-2.5 text-xs text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
          <p>
            Sourced from DSO&apos;s in-memory runtime event log (the same store behind{' '}
            <span className="font-medium text-foreground">Events</span> and{' '}
            <span className="font-medium text-foreground">Logs</span>). It is bounded and resets on agent
            restart -- this is not a durable, persistent audit trail.
          </p>
        </div>

        {loading && (
          <Card data-testid="audit-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <Skeleton className="h-4 w-64" />
                <Skeleton className="h-4 w-24" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div
            data-testid="audit-error"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            {error}
          </div>
        )}

        {!loading && !error && events.length === 0 && (
          <Card>
            <EmptyState
              data-testid="audit-empty"
              icon={FileClock}
              title="No audit history yet"
              description="Rotation and injection outcomes will appear here as they happen."
            />
          </Card>
        )}

        {!loading && !error && events.length > 0 && filtered.length === 0 && (
          <Card>
            <EmptyState
              data-testid="audit-no-match"
              icon={Search}
              title={`No entries match "${search.trim()}"`}
              description="Try a different search term."
            />
          </Card>
        )}

        {!loading && !error && filtered.length > 0 && (
          <Card data-testid="audit-table" className="overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="pl-5">Timestamp</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Resource</TableHead>
                  <TableHead>Result</TableHead>
                  <TableHead className="pr-5">Detail</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((ev, idx) => (
                  <TableRow key={`${ev.timestamp}-${idx}`}>
                    <TableCell className="pl-5 text-xs text-muted-foreground">
                      {new Date(ev.timestamp).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-foreground">{ev.event_type ?? 'event'}</TableCell>
                    <TableCell className="text-foreground">
                      {ev.secret ?? ev.container ?? '—'}
                    </TableCell>
                    <TableCell>
                      {ev.status ? (
                        <Badge
                          variant={
                            ev.status === 'success' ? 'success' : ev.status === 'failure' ? 'destructive' : 'warning'
                          }
                        >
                          <Circle className="h-1.5 w-1.5 fill-current" />
                          {ev.status}
                        </Badge>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                    <TableCell className="pr-5 max-w-xs truncate text-xs text-muted-foreground">
                      {ev.error ?? '—'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        )}
      </div>
    </div>
  )
}

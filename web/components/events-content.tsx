'use client'

import { useCallback, useMemo, useState, useEffect } from 'react'
import {
  Bell,
  Search,
  ChevronDown,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  X,
} from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { useWebSocket, type Event } from '@/hooks/useWebSocket'
import { fetchEvents } from '@/lib/api/events'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/ui/status-badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { cn } from '@/lib/utils'

type SortKey = 'timestamp' | 'event_type' | 'status' | 'resource'
type SortDirection = 'asc' | 'desc'

/** A short id-key for de-duplicating the same event seen via both the REST
 * history load and the WebSocket's on-connect replay. Not a real backend
 * correlation ID -- the API exposes none (see hooks/useWebSocket.ts) -- this
 * exists purely as a React list key / de-dup heuristic, never surfaced as
 * "the" identity of an event in the UI. */
function eventKey(e: Event): string {
  return `${e.timestamp}:${e.event_type ?? ''}:${e.secret ?? ''}:${e.container ?? ''}`
}

/** The `container` field is either a raw Docker container ID or a
 * compose/service name string, depending on which producer wrote the event
 * (see hooks/useWebSocket.ts's doc comment) -- there is no way to tell which
 * from the string alone. Shortening only kicks in for values that actually
 * look like a long hex container ID, so a real service name is never
 * truncated. */
function displayResource(value: string): string {
  return /^[0-9a-f]{20,}$/i.test(value) ? value.slice(0, 12) : value
}

function RelativeTime({ iso }: { iso: string }) {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return <span className="text-muted-foreground">Not available</span>
  return (
    <span title={date.toLocaleString()} className="text-muted-foreground">
      {formatDistanceToNow(date, { addSuffix: true })}
    </span>
  )
}

function SortButton({
  label,
  active,
  direction,
  onClick,
}: {
  label: string
  active: boolean
  direction: SortDirection
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="inline-flex items-center gap-1 hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring rounded"
    >
      {label}
      {active ? (
        direction === 'asc' ? (
          <ArrowUp className="h-3 w-3" />
        ) : (
          <ArrowDown className="h-3 w-3" />
        )
      ) : (
        <ArrowUpDown className="h-3 w-3 opacity-40" />
      )}
    </button>
  )
}

const selectClass =
  'h-8 rounded-md border border-border bg-card px-2 text-xs text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring'

/**
 * Runtime Events: GET /api/events for initial history, merged with live
 * updates from /api/events/ws. This is DSO's actual runtime event log, not
 * a synthetic activity feed -- see hooks/useWebSocket.ts's Event type for
 * exactly which fields the backend ever populates.
 *
 * SECURITY: only known-safe fields (timestamp/event_type/secret/container/
 * provider/status/error/user) are ever read or rendered below. `error` is
 * redacted at the source (internal/notify/event.go's NewEvent) before it
 * ever reaches this API. There is no code path here that would render a
 * `value`/`plaintext` field even if a future or misbehaving producer added
 * one to the underlying map (see internal/server/eventstore.go -- Event is
 * an open map with no allowlisting DTO at the API layer).
 *
 * NO SYNTHETIC ROTATION TIMELINE: the backend does not emit a
 * "rotation_started"/"shadow_created"/"health_check"/"traffic_switched"
 * sequence -- only terminal outcomes (rotation_succeeded/rotation_failed,
 * recovery_succeeded/recovery_failed, service_degraded/service_recovered,
 * signal_succeeded/signal_failed, secret_fetch). Inventing intermediate
 * stages the backend never reports would be exactly the kind of fabricated
 * data this phase forbids, so events are shown chronologically instead.
 */
export function EventsContent() {
  const [history, setHistory] = useState<Event[]>([])
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [typeFilter, setTypeFilter] = useState<string>('all')
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>({ key: 'timestamp', direction: 'desc' })
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const { events: liveEvents, connectionState } = useWebSocket('/api/events/ws')

  const load = useCallback(async () => {
    setHistoryLoading(true)
    setHistoryError(null)
    try {
      const data = await fetchEvents(50)
      setHistory(data)
    } catch (err) {
      setHistoryError(err instanceof Error ? err.message : 'Failed to load events')
    } finally {
      setHistoryLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  // Live events (newest first from the hook) take priority; fall back to
  // REST history for anything not already seen live, since the WS
  // connection also replays recent history on connect.
  const seen = new Set(liveEvents.map(eventKey))
  const allEvents = [...liveEvents, ...history.filter((e) => !seen.has(eventKey(e)))]

  const loading = historyLoading && allEvents.length === 0

  const statuses = useMemo(
    () => Array.from(new Set(allEvents.map((e) => e.status).filter((s): s is string => Boolean(s)))).sort(),
    [allEvents]
  )
  const eventTypes = useMemo(
    () => Array.from(new Set(allEvents.map((e) => e.event_type).filter((t): t is string => Boolean(t)))).sort(),
    [allEvents]
  )

  const filtersActive = statusFilter !== 'all' || typeFilter !== 'all' || search.trim() !== ''
  const clearFilters = () => {
    setSearch('')
    setStatusFilter('all')
    setTypeFilter('all')
  }

  const filteredEvents = useMemo(() => {
    const query = search.trim().toLowerCase()
    return allEvents.filter((e) => {
      if (statusFilter !== 'all' && e.status !== statusFilter) return false
      if (typeFilter !== 'all' && e.event_type !== typeFilter) return false
      if (!query) return true
      const haystack = [e.event_type, e.secret, e.container, e.error].filter(Boolean).join(' ').toLowerCase()
      return haystack.includes(query)
    })
  }, [allEvents, search, statusFilter, typeFilter])

  const sortedEvents = useMemo(() => {
    const list = [...filteredEvents]
    const { key, direction } = sort
    const dir = direction === 'asc' ? 1 : -1
    list.sort((a, b) => {
      switch (key) {
        case 'timestamp':
          return (a.timestamp < b.timestamp ? -1 : 1) * dir
        case 'event_type':
          return (a.event_type ?? '').localeCompare(b.event_type ?? '') * dir
        case 'status':
          return (a.status ?? '').localeCompare(b.status ?? '') * dir
        case 'resource':
          return (a.secret ?? a.container ?? '').localeCompare(b.secret ?? b.container ?? '') * dir
        default:
          return 0
      }
    })
    return list
  }, [filteredEvents, sort])

  function toggleSort(key: SortKey) {
    setSort((s) => (s.key === key ? { key, direction: s.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: key === 'timestamp' ? 'desc' : 'asc' }))
  }

  function toggleExpanded(key: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <div className="px-6 py-5">
      <div className="mx-auto max-w-6xl space-y-4">
        <header className="flex items-start justify-between gap-3">
          <div>
            <h1 className="text-lg font-semibold text-foreground">Runtime Events</h1>
            <p className="mt-0.5 text-sm text-muted-foreground">Recent activity from the DSO runtime.</p>
          </div>
          <StatusBadge
            status={connectionState}
            tone={connectionState === 'connected' ? 'success' : connectionState === 'reconnecting' ? 'warning' : 'destructive'}
            data-testid="ws-connection-state"
          />
        </header>

        {loading && (
          <div data-testid="events-loading" aria-busy="true">
            <span className="sr-only">Loading events</span>
            <Card>
              <CardContent className="space-y-2 p-4">
                <Skeleton className="h-8 w-full" />
                {Array.from({ length: 6 }).map((_, i) => (
                  <Skeleton key={i} className="h-9 w-full" />
                ))}
              </CardContent>
            </Card>
          </div>
        )}

        {!loading && historyError && allEvents.length === 0 && (
          <div
            data-testid="events-error"
            role="alert"
            aria-live="assertive"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <p className="font-medium">Unable to load events</p>
            <p className="mt-0.5 flex items-center justify-between gap-3 text-red-400/80">
              <span>DSO could not retrieve runtime events.</span>
              <Button variant="link" size="sm" onClick={load} className="h-auto flex-shrink-0 p-0 text-red-300 hover:text-red-200">
                Retry
              </Button>
            </p>
          </div>
        )}

        {!loading && (!historyError || allEvents.length > 0) && (
          <Card data-testid="events-content">
            {allEvents.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
                <div className="relative w-full max-w-xs">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    data-testid="events-search"
                    aria-label="Search events by type, resource, or message"
                    placeholder="Search events..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="h-8 pl-8 text-sm"
                  />
                </div>

                {statuses.length > 1 && (
                  <select
                    aria-label="Filter by status"
                    data-testid="filter-status"
                    value={statusFilter}
                    onChange={(e) => setStatusFilter(e.target.value)}
                    className={selectClass}
                  >
                    <option value="all">All statuses</option>
                    {statuses.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </select>
                )}

                {eventTypes.length > 1 && (
                  <select
                    aria-label="Filter by event type"
                    data-testid="filter-event-type"
                    value={typeFilter}
                    onChange={(e) => setTypeFilter(e.target.value)}
                    className={selectClass}
                  >
                    <option value="all">All event types</option>
                    {eventTypes.map((t) => (
                      <option key={t} value={t}>
                        {t.replace(/_/g, ' ')}
                      </option>
                    ))}
                  </select>
                )}

                {filtersActive && (
                  <button
                    type="button"
                    data-testid="clear-filters"
                    onClick={clearFilters}
                    className="ml-auto flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  >
                    <X className="h-3 w-3" />
                    Clear filters
                  </button>
                )}
              </div>
            )}

            <CardContent className="p-0">
              {allEvents.length === 0 ? (
                <EmptyState
                  data-testid="events-empty"
                  icon={Bell}
                  title="No recent events"
                  description="DSO runtime activity will appear here as containers, secrets, and rotations are processed."
                />
              ) : sortedEvents.length === 0 ? (
                <EmptyState
                  data-testid="events-no-match"
                  icon={Search}
                  title={search.trim() ? `No events match "${search.trim()}"` : 'No events match the selected filters'}
                  description="Try a different search term or clear the filters."
                />
              ) : (
                <Table aria-label="Runtime events">
                  <caption className="sr-only">Runtime events</caption>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-8" />
                      <TableHead>
                        <SortButton
                          label="Time"
                          active={sort.key === 'timestamp'}
                          direction={sort.direction}
                          onClick={() => toggleSort('timestamp')}
                        />
                      </TableHead>
                      <TableHead>
                        <SortButton
                          label="Event"
                          active={sort.key === 'event_type'}
                          direction={sort.direction}
                          onClick={() => toggleSort('event_type')}
                        />
                      </TableHead>
                      <TableHead className="hidden sm:table-cell">
                        <SortButton
                          label="Resource"
                          active={sort.key === 'resource'}
                          direction={sort.direction}
                          onClick={() => toggleSort('resource')}
                        />
                      </TableHead>
                      <TableHead>
                        <SortButton
                          label="Status"
                          active={sort.key === 'status'}
                          direction={sort.direction}
                          onClick={() => toggleSort('status')}
                        />
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sortedEvents.map((ev) => {
                      const key = eventKey(ev)
                      const isOpen = expanded.has(key)
                      return <EventRow key={key} event={ev} rowKey={key} isOpen={isOpen} onToggle={() => toggleExpanded(key)} />
                    })}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        )}
      </div>
    </div>
  )
}

function EventRow({
  event: ev,
  rowKey,
  isOpen,
  onToggle,
}: {
  event: Event
  rowKey: string
  isOpen: boolean
  onToggle: () => void
}) {
  const resource = ev.secret ?? (ev.container ? displayResource(ev.container) : undefined)
  const label = (ev.event_type ?? 'event').replace(/_/g, ' ')

  return (
    <>
      <TableRow className="cursor-pointer hover:bg-accent/40" onClick={onToggle}>
        <TableCell className="w-8 pr-0">
          <button
            type="button"
            aria-expanded={isOpen}
            aria-label={isOpen ? `Collapse details for ${label}` : `Expand details for ${label}`}
            className="flex h-5 w-5 items-center justify-center text-muted-foreground"
            onClick={(e) => {
              e.stopPropagation()
              onToggle()
            }}
          >
            {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        </TableCell>
        <TableCell>
          <RelativeTime iso={ev.timestamp} />
        </TableCell>
        <TableCell className="capitalize text-foreground">{label}</TableCell>
        <TableCell className="hidden sm:table-cell text-muted-foreground">
          {resource ?? <span className="text-muted-foreground/60">Not available</span>}
        </TableCell>
        <TableCell>
          {ev.status ? <StatusBadge status={ev.status} /> : <span className="text-xs text-muted-foreground/60">Not available</span>}
        </TableCell>
      </TableRow>
      {isOpen && (
        <TableRow className="hover:bg-transparent" data-testid={`event-detail-${rowKey}`}>
          <TableCell colSpan={5} className="bg-muted/20 py-3">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 px-2 text-xs sm:grid-cols-4">
              <DetailField label="Timestamp" value={new Date(ev.timestamp).toLocaleString()} />
              <DetailField label="Event type" value={ev.event_type ?? 'Not available'} />
              {ev.secret && <DetailField label="Secret" value={ev.secret} />}
              {ev.container && <DetailField label="Container" value={ev.container} mono />}
              {ev.provider && <DetailField label="Provider" value={ev.provider} />}
              <DetailField label="Status" value={ev.status ?? 'Not available'} />
              {ev.error && (
                <div className="col-span-full">
                  <dt className="text-muted-foreground">Message</dt>
                  <dd className="mt-0.5 text-red-400">{ev.error}</dd>
                </div>
              )}
            </dl>
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function DetailField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn('mt-0.5 text-foreground', mono && 'font-mono text-[11px] break-all')}>{value}</dd>
    </div>
  )
}

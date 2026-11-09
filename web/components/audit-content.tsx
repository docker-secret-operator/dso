'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  FileClock,
  Search,
  Info,
  ChevronDown,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  X,
} from 'lucide-react'
import type { Event } from '@/hooks/useWebSocket'
import { fetchAudit } from '@/lib/api/audit'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/ui/status-badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { cn } from '@/lib/utils'

type SortKey = 'timestamp' | 'user' | 'event_type' | 'status'
type SortDirection = 'asc' | 'desc'

/** Same shortening heuristic as events-content.tsx: only collapse a value
 * that actually looks like a long hex container ID, never a real service
 * name or other identifier. */
function displayResource(value: string): string {
  return /^[0-9a-f]{20,}$/i.test(value) ? value.slice(0, 12) : value
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
 * Audit: GET /api/audit -- the SAME bounded, in-memory EventStore that backs
 * /api/events (live feed) and /api/logs (see internal/server/rest.go's
 * handleAudit doc comment). DSO has no separate persistent audit-events
 * table today; this page is a distinct read of the real runtime event log
 * (actor/action/resource/result first), not a different data source, and
 * it deliberately looks different from Runtime Events -- no live WS badge,
 * a security-history framing, and an explicit note about what "Audit" means
 * here since the same records back both pages.
 *
 * ACTOR: only "secret_fetch" events ever carry a real actor (the Unix-
 * socket peer's resolved OS username -- internal/audit/audit.go's Log,
 * added in Phase 5). Every other event type has no actor identity in the
 * backend at all. This page renders "Not available" for those rows rather
 * than fabricating "System" -- DSO's backend does not define that
 * semantics anywhere.
 *
 * SECURITY: identical guarantee to Events -- only known-safe fields are
 * read (see hooks/useWebSocket.ts's Event type), `error` is redacted at
 * the source, and there is no code path that renders a `value`/`plaintext`
 * field even if the underlying map ever carried one.
 */
export function AuditContent() {
  const [events, setEvents] = useState<Event[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [typeFilter, setTypeFilter] = useState('all')
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>({ key: 'timestamp', direction: 'desc' })
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchAudit(200)
      setEvents(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load audit history')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const statuses = useMemo(
    () => Array.from(new Set(events.map((e) => e.status).filter((s): s is string => Boolean(s)))).sort(),
    [events]
  )
  const eventTypes = useMemo(
    () => Array.from(new Set(events.map((e) => e.event_type).filter((t): t is string => Boolean(t)))).sort(),
    [events]
  )

  const filtersActive = statusFilter !== 'all' || typeFilter !== 'all' || search.trim() !== ''
  const clearFilters = () => {
    setSearch('')
    setStatusFilter('all')
    setTypeFilter('all')
  }

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return events.filter((ev) => {
      if (statusFilter !== 'all' && ev.status !== statusFilter) return false
      if (typeFilter !== 'all' && ev.event_type !== typeFilter) return false
      if (!query) return true
      const haystack = [ev.event_type, ev.secret, ev.container, ev.user, ev.error].filter(Boolean).join(' ').toLowerCase()
      return haystack.includes(query)
    })
  }, [events, search, statusFilter, typeFilter])

  const sorted = useMemo(() => {
    const list = [...filtered]
    const { key, direction } = sort
    const dir = direction === 'asc' ? 1 : -1
    list.sort((a, b) => {
      switch (key) {
        case 'timestamp':
          return (a.timestamp < b.timestamp ? -1 : 1) * dir
        case 'user':
          return (a.user ?? '').localeCompare(b.user ?? '') * dir
        case 'event_type':
          return (a.event_type ?? '').localeCompare(b.event_type ?? '') * dir
        case 'status':
          return (a.status ?? '').localeCompare(b.status ?? '') * dir
        default:
          return 0
      }
    })
    return list
  }, [filtered, sort])

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
        <header>
          <h1 className="text-lg font-semibold text-foreground">Audit</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">Track security-relevant and administrative activity.</p>
          <p className="mt-2 flex items-start gap-1.5 text-xs text-muted-foreground/70">
            <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
            Sourced from DSO&apos;s in-memory runtime event log (the same store behind Events and Logs). Bounded and
            resets on agent restart -- this is not a durable, persistent audit trail.
          </p>
        </header>

        {loading && (
          <div data-testid="audit-loading" aria-busy="true">
            <span className="sr-only">Loading audit history</span>
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

        {!loading && error && (
          <div
            data-testid="audit-error"
            role="alert"
            aria-live="assertive"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <p className="font-medium">Unable to load audit history</p>
            <p className="mt-0.5 flex items-center justify-between gap-3 text-red-400/80">
              <span>DSO could not retrieve audit history.</span>
              <Button variant="link" size="sm" onClick={load} className="h-auto flex-shrink-0 p-0 text-red-300 hover:text-red-200">
                Retry
              </Button>
            </p>
          </div>
        )}

        {!loading && !error && (
          <Card data-testid="audit-content">
            {events.length > 0 && (
              <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
                <div className="relative w-full max-w-xs">
                  <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    data-testid="audit-search"
                    aria-label="Search audit history"
                    placeholder="Search audit history..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    className="h-8 pl-8 text-sm"
                  />
                </div>

                {statuses.length > 1 && (
                  <select
                    aria-label="Filter by result"
                    data-testid="filter-status"
                    value={statusFilter}
                    onChange={(e) => setStatusFilter(e.target.value)}
                    className={selectClass}
                  >
                    <option value="all">All results</option>
                    {statuses.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </select>
                )}

                {eventTypes.length > 1 && (
                  <select
                    aria-label="Filter by action"
                    data-testid="filter-action"
                    value={typeFilter}
                    onChange={(e) => setTypeFilter(e.target.value)}
                    className={selectClass}
                  >
                    <option value="all">All actions</option>
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
              {events.length === 0 ? (
                <EmptyState
                  data-testid="audit-empty"
                  icon={FileClock}
                  title="No audit history yet"
                  description="Rotation, injection, and secret-fetch outcomes will appear here as they happen."
                />
              ) : sorted.length === 0 ? (
                <EmptyState
                  data-testid="audit-no-match"
                  icon={Search}
                  title={search.trim() ? `No entries match "${search.trim()}"` : 'No entries match the selected filters'}
                  description="Try a different search term or clear the filters."
                />
              ) : (
                <Table aria-label="Audit history">
                  <caption className="sr-only">Audit history</caption>
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
                      <TableHead className="hidden sm:table-cell">
                        <SortButton
                          label="Actor"
                          active={sort.key === 'user'}
                          direction={sort.direction}
                          onClick={() => toggleSort('user')}
                        />
                      </TableHead>
                      <TableHead>
                        <SortButton
                          label="Action"
                          active={sort.key === 'event_type'}
                          direction={sort.direction}
                          onClick={() => toggleSort('event_type')}
                        />
                      </TableHead>
                      <TableHead className="hidden md:table-cell">Resource</TableHead>
                      <TableHead>
                        <SortButton
                          label="Result"
                          active={sort.key === 'status'}
                          direction={sort.direction}
                          onClick={() => toggleSort('status')}
                        />
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sorted.map((ev, idx) => {
                      const key = `${ev.timestamp}:${ev.event_type ?? ''}:${idx}`
                      const isOpen = expanded.has(key)
                      return <AuditRow key={key} event={ev} rowKey={key} isOpen={isOpen} onToggle={() => toggleExpanded(key)} />
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

function AuditRow({
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
  const action = (ev.event_type ?? 'event').replace(/_/g, ' ')

  return (
    <>
      <TableRow className="cursor-pointer hover:bg-accent/40" onClick={onToggle}>
        <TableCell className="w-8 pr-0">
          <button
            type="button"
            aria-expanded={isOpen}
            aria-label={isOpen ? `Collapse details for ${action}` : `Expand details for ${action}`}
            className="flex h-5 w-5 items-center justify-center text-muted-foreground"
            onClick={(e) => {
              e.stopPropagation()
              onToggle()
            }}
          >
            {isOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
        </TableCell>
        <TableCell className="text-xs text-muted-foreground" title={new Date(ev.timestamp).toLocaleString()}>
          {new Date(ev.timestamp).toLocaleString()}
        </TableCell>
        <TableCell className="hidden sm:table-cell text-muted-foreground">
          {ev.user ?? <span className="text-muted-foreground/60">Not available</span>}
        </TableCell>
        <TableCell className="capitalize text-foreground">{action}</TableCell>
        <TableCell className="hidden md:table-cell text-muted-foreground">
          {resource ?? <span className="text-muted-foreground/60">Not available</span>}
        </TableCell>
        <TableCell>
          {ev.status ? <StatusBadge status={ev.status} /> : <span className="text-xs text-muted-foreground/60">Not available</span>}
        </TableCell>
      </TableRow>
      {isOpen && (
        <TableRow className="hover:bg-transparent" data-testid={`audit-detail-${rowKey}`}>
          <TableCell colSpan={6} className="bg-muted/20 py-3">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 px-2 text-xs sm:grid-cols-4">
              <DetailField label="Timestamp" value={new Date(ev.timestamp).toLocaleString()} />
              <DetailField label="Actor" value={ev.user ?? 'Not available'} />
              <DetailField label="Action" value={ev.event_type ?? 'Not available'} />
              {ev.secret && <DetailField label="Secret" value={ev.secret} />}
              {ev.container && <DetailField label="Container" value={ev.container} mono />}
              {ev.provider && <DetailField label="Provider" value={ev.provider} />}
              <DetailField label="Result" value={ev.status ?? 'Not available'} />
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

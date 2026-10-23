'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Boxes,
  Search,
  AlertTriangle,
  ChevronDown,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  X,
} from 'lucide-react'
import { fetchContainers, type ContainerSummary } from '@/lib/api/containers'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge, type StatusTone } from '@/components/ui/status-badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { cn } from '@/lib/utils'

interface ContainersState {
  loading: boolean
  error: string | null
  data: ContainerSummary[] | null
}

const initialState: ContainersState = { loading: true, error: null, data: null }

type SortKey = 'name' | 'status' | 'health' | 'strategy' | 'image'
type SortDirection = 'asc' | 'desc'
type StatusFilter = 'all' | 'running' | 'exited' | 'created' | 'paused' | 'restarting' | 'dead'
type HealthFilter = 'all' | 'healthy' | 'unhealthy' | 'starting' | 'none'
type DsoStateFilter = 'all' | 'degraded' | 'operational'

// Docker's own lifecycle state ("is the process running") is a different
// concept from application health ("is it actually working") and from
// DSO's own degraded flag ("did DSO's last rotation/injection fail while
// still serving the previous secret") -- each gets its own explicit tone,
// never a shared/guessed classification.
const STATUS_TONE: Record<string, StatusTone> = {
  running: 'success',
  exited: 'destructive',
  dead: 'destructive',
  paused: 'warning',
  restarting: 'warning',
  created: 'neutral',
}

const HEALTH_TONE: Record<string, StatusTone> = {
  healthy: 'success',
  unhealthy: 'destructive',
  starting: 'warning',
}

function shortId(id: string): string {
  return id.slice(0, 12)
}

function displayName(c: ContainerSummary): string {
  return c.name || shortId(c.id)
}

const selectClass =
  'h-8 rounded-md border border-border bg-card px-2 text-xs text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring'

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

/**
 * Real, read-only containers workspace: GET /api/containers.
 *
 * SECURITY: internal/server/rest.go's handleContainers/ListDockerContainerInfo
 * construct an explicit DTO -- id/name/image/status/health/ports/strategy/
 * secrets(names only)/degraded/degraded_reason. There is no code path here
 * that reads or renders Config.Env, labels, mounts, or any other raw Docker
 * inspect field, because no such field exists on ContainerSummary.
 *
 * No rotate/restart/stop action is exposed here -- this page is
 * intentionally read-only, matching handleContainers' own doc comment.
 *
 * Docker "Status" (is the process running) and Docker "Health" (does its
 * healthcheck pass) and DSO "Degraded" (did the last rotation fail while
 * still serving the previous secret) are three distinct concepts and are
 * never conflated into a single indicator below.
 */
export function ContainersContent() {
  const [state, setState] = useState<ContainersState>(initialState)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [healthFilter, setHealthFilter] = useState<HealthFilter>('all')
  const [dsoStateFilter, setDsoStateFilter] = useState<DsoStateFilter>('all')
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>({ key: 'name', direction: 'asc' })
  const [expanded, setExpanded] = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const data = await fetchContainers()
      setState({ loading: false, error: null, data: data.containers })
    } catch (err) {
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load containers',
      }))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const allContainers = useMemo(() => state.data ?? [], [state.data])

  const statuses = useMemo(
    () => Array.from(new Set(allContainers.map((c) => c.status).filter((s): s is string => Boolean(s)))).sort(),
    [allContainers]
  )
  const healthStates = useMemo(
    () => Array.from(new Set(allContainers.map((c) => c.health).filter((h): h is string => Boolean(h)))).sort(),
    [allContainers]
  )

  const filtersActive =
    statusFilter !== 'all' || healthFilter !== 'all' || dsoStateFilter !== 'all' || search.trim() !== ''

  const clearFilters = () => {
    setSearch('')
    setStatusFilter('all')
    setHealthFilter('all')
    setDsoStateFilter('all')
  }

  const filteredContainers = useMemo(() => {
    const query = search.trim().toLowerCase()
    return allContainers.filter((c) => {
      if (statusFilter !== 'all' && c.status !== statusFilter) return false
      if (healthFilter === 'none' && c.health) return false
      if (healthFilter !== 'all' && healthFilter !== 'none' && c.health !== healthFilter) return false
      if (dsoStateFilter === 'degraded' && !c.degraded) return false
      if (dsoStateFilter === 'operational' && c.degraded) return false
      if (!query) return true
      return (
        (c.name ?? '').toLowerCase().includes(query) ||
        (c.image ?? '').toLowerCase().includes(query) ||
        c.id.toLowerCase().includes(query)
      )
    })
  }, [allContainers, search, statusFilter, healthFilter, dsoStateFilter])

  const sortedContainers = useMemo(() => {
    const list = [...filteredContainers]
    const { key, direction } = sort
    const dir = direction === 'asc' ? 1 : -1
    list.sort((a, b) => {
      switch (key) {
        case 'name':
          return displayName(a).localeCompare(displayName(b)) * dir
        case 'status':
          return (a.status ?? '').localeCompare(b.status ?? '') * dir
        case 'health':
          return (a.health ?? '').localeCompare(b.health ?? '') * dir
        case 'strategy':
          return a.strategy.localeCompare(b.strategy) * dir
        case 'image':
          return (a.image ?? '').localeCompare(b.image ?? '') * dir
        default:
          return 0
      }
    })
    return list
  }, [filteredContainers, sort])

  function toggleSort(key: SortKey) {
    setSort((s) => (s.key === key ? { key, direction: s.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: 'asc' }))
  }

  function toggleExpanded(id: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const summary = useMemo(
    () => ({
      managed: allContainers.length,
      running: allContainers.filter((c) => c.status === 'running').length,
      healthy: allContainers.filter((c) => c.health === 'healthy').length,
      degraded: allContainers.filter((c) => c.degraded).length,
    }),
    [allContainers]
  )

  return (
    <div className="px-6 py-5">
      <div className="mx-auto max-w-6xl space-y-4">
        <header>
          <h1 className="text-lg font-semibold text-foreground">Containers</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">Monitor Docker workloads managed by DSO.</p>
        </header>

        {state.loading && (
          <div data-testid="containers-loading" aria-busy="true" className="space-y-4">
            <span className="sr-only">Loading containers</span>
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

        {!state.loading && state.error && (
          <div
            data-testid="containers-error"
            role="alert"
            aria-live="assertive"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <p className="font-medium">Unable to load containers</p>
            <p className="mt-0.5 flex items-center justify-between gap-3 text-red-400/80">
              <span>DSO could not retrieve container information from the running Docker environment.</span>
              <Button variant="link" size="sm" onClick={load} className="h-auto flex-shrink-0 p-0 text-red-300 hover:text-red-200">
                Retry
              </Button>
            </p>
          </div>
        )}

        {!state.loading && !state.error && state.data && (
          <>
            {allContainers.length > 0 && (
              <div data-testid="containers-summary" className="flex flex-wrap items-center gap-x-5 gap-y-1 text-sm text-muted-foreground">
                <span className="font-medium text-foreground">{summary.managed} managed containers</span>
                <span>{summary.running} running</span>
                <span>{summary.healthy} healthy</span>
                <span className={cn(summary.degraded > 0 && 'font-medium text-amber-400')}>{summary.degraded} degraded</span>
              </div>
            )}

            <Card data-testid="containers-content">
              {allContainers.length > 0 && (
                <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
                  <div className="relative w-full max-w-xs">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      data-testid="containers-search"
                      aria-label="Search containers by name, image, or ID"
                      placeholder="Search by name, image, or ID..."
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
                      onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
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

                  {healthStates.length > 0 && (
                    <select
                      aria-label="Filter by health"
                      data-testid="filter-health"
                      value={healthFilter}
                      onChange={(e) => setHealthFilter(e.target.value as HealthFilter)}
                      className={selectClass}
                    >
                      <option value="all">All health states</option>
                      {healthStates.map((h) => (
                        <option key={h} value={h}>
                          {h}
                        </option>
                      ))}
                      <option value="none">No healthcheck</option>
                    </select>
                  )}

                  <select
                    aria-label="Filter by DSO state"
                    data-testid="filter-dso-state"
                    value={dsoStateFilter}
                    onChange={(e) => setDsoStateFilter(e.target.value as DsoStateFilter)}
                    className={selectClass}
                  >
                    <option value="all">DSO state: all</option>
                    <option value="operational">DSO state: operational</option>
                    <option value="degraded">DSO state: degraded</option>
                  </select>

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
                {allContainers.length === 0 ? (
                  <EmptyState
                    data-testid="containers-empty"
                    icon={Boxes}
                    title="No managed containers"
                    description="DSO has not discovered any Docker containers matching its configured management rules."
                  />
                ) : sortedContainers.length === 0 ? (
                  <EmptyState
                    data-testid="containers-no-match"
                    icon={Search}
                    title={search.trim() ? `No containers match "${search.trim()}"` : 'No containers match the selected filters'}
                    description="Try a different search term or clear the filters."
                  />
                ) : (
                  <Table aria-label="Managed containers">
                    <caption className="sr-only">Managed containers</caption>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-8" />
                        <TableHead>
                          <SortButton
                            label="Container"
                            active={sort.key === 'name'}
                            direction={sort.direction}
                            onClick={() => toggleSort('name')}
                          />
                        </TableHead>
                        <TableHead className="hidden md:table-cell">
                          <SortButton
                            label="Image"
                            active={sort.key === 'image'}
                            direction={sort.direction}
                            onClick={() => toggleSort('image')}
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
                        <TableHead>
                          <SortButton
                            label="Health"
                            active={sort.key === 'health'}
                            direction={sort.direction}
                            onClick={() => toggleSort('health')}
                          />
                        </TableHead>
                        <TableHead className="hidden sm:table-cell">
                          <SortButton
                            label="Strategy"
                            active={sort.key === 'strategy'}
                            direction={sort.direction}
                            onClick={() => toggleSort('strategy')}
                          />
                        </TableHead>
                        <TableHead className="hidden sm:table-cell">Secrets</TableHead>
                        <TableHead className="hidden lg:table-cell">Ports</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {sortedContainers.map((c) => {
                        const isOpen = expanded.has(c.id)
                        return <ContainerRow key={c.id} container={c} isOpen={isOpen} onToggle={() => toggleExpanded(c.id)} />
                      })}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </>
        )}
      </div>
    </div>
  )
}

function ContainerRow({
  container: c,
  isOpen,
  onToggle,
}: {
  container: ContainerSummary
  isOpen: boolean
  onToggle: () => void
}) {
  const name = displayName(c)
  return (
    <>
      <TableRow className="cursor-pointer hover:bg-accent/40" onClick={onToggle}>
        <TableCell className="w-8 pr-0">
          <button
            type="button"
            aria-expanded={isOpen}
            aria-label={isOpen ? `Collapse details for ${name}` : `Expand details for ${name}`}
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
          <span className="block font-medium text-foreground">{name}</span>
          <span className="block font-mono text-xs text-muted-foreground/70">{shortId(c.id)}</span>
        </TableCell>
        <TableCell className="hidden md:table-cell max-w-[16rem] truncate text-muted-foreground" title={c.image}>
          {c.image || <span className="text-muted-foreground/60">Not available</span>}
        </TableCell>
        <TableCell>
          {c.status ? (
            <StatusBadge status={c.status} tone={STATUS_TONE[c.status] ?? 'neutral'} />
          ) : (
            <span className="text-xs text-muted-foreground/60">Not available</span>
          )}
        </TableCell>
        <TableCell>
          {c.health ? (
            <StatusBadge status={c.health} tone={HEALTH_TONE[c.health] ?? 'neutral'} />
          ) : (
            <span className="text-xs text-muted-foreground/60">No healthcheck</span>
          )}
        </TableCell>
        <TableCell className="hidden sm:table-cell capitalize text-muted-foreground">{c.strategy}</TableCell>
        <TableCell className="hidden sm:table-cell text-muted-foreground">
          {c.secrets.length === 0 ? 'None' : `${c.secrets.length} secret${c.secrets.length === 1 ? '' : 's'}`}
        </TableCell>
        <TableCell className="hidden lg:table-cell font-mono text-xs text-muted-foreground">
          {c.ports && c.ports.length > 0 ? c.ports.join(', ') : <span className="text-muted-foreground/60">None published</span>}
        </TableCell>
      </TableRow>
      {c.degraded && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={8} className="border-t-0 bg-amber-500/10 py-1.5">
            <span className="flex items-center gap-1.5 px-2 text-xs text-amber-400">
              <AlertTriangle className="h-3.5 w-3.5 flex-shrink-0" />
              <span className="font-medium">DSO degraded</span>
              {c.degraded_reason && <span className="text-amber-400/80">— {c.degraded_reason}</span>}
            </span>
          </TableCell>
        </TableRow>
      )}
      {isOpen && (
        <TableRow className="hover:bg-transparent" data-testid={`container-detail-${c.id}`}>
          <TableCell colSpan={8} className="bg-muted/20 py-3">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 px-2 text-xs sm:grid-cols-4">
              <DetailField label="Name" value={c.name || 'Not available'} />
              <DetailField label="Container ID" value={c.id} mono />
              <DetailField label="Image" value={c.image || 'Not available'} mono />
              <DetailField label="Docker status" value={c.status || 'Not available'} />
              <DetailField label="Health" value={c.health || 'No healthcheck'} />
              <DetailField label="Strategy" value={c.strategy} />
              <DetailField label="Secrets" value={c.secrets.length > 0 ? c.secrets.join(', ') : 'None'} />
              <DetailField label="Ports" value={c.ports && c.ports.length > 0 ? c.ports.join(', ') : 'None published'} mono />
              <DetailField label="DSO state" value={c.degraded ? 'Degraded' : 'Operational'} />
              {c.degraded_reason && (
                <div className="col-span-full">
                  <dt className="text-muted-foreground">Degraded reason</dt>
                  <dd className="mt-0.5 text-amber-400">{c.degraded_reason}</dd>
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

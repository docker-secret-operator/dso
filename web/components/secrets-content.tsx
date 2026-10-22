'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { formatDistanceToNow } from 'date-fns'
import {
  KeyRound,
  Search,
  RefreshCcw,
  AlertCircle,
  Boxes,
  ShieldCheck,
  ChevronDown,
  ChevronRight,
  ArrowUp,
  ArrowDown,
  ArrowUpDown,
  X,
  type LucideIcon,
} from 'lucide-react'
import { fetchSecrets, rotateSecret, type SecretSummary, type SecretsResponse } from '@/lib/api/dashboard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusBadge } from '@/components/ui/status-badge'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { ProviderIcon } from '@/components/provider-icon'
import { useWebSocket } from '@/hooks/useWebSocket'
import { cn } from '@/lib/utils'

interface SecretsState {
  loading: boolean
  error: string | null
  data: SecretsResponse | null
}

const initialState: SecretsState = { loading: true, error: null, data: null }

type SortKey = 'name' | 'status' | 'version' | 'last_updated_at'
type SortDirection = 'asc' | 'desc'

const STAT_TONE_CLASS: Record<'default' | 'success' | 'destructive' | 'neutral', string> = {
  default: 'text-foreground',
  success: 'text-emerald-400',
  destructive: 'text-red-400',
  neutral: 'text-foreground',
}

function StatTile({
  label,
  value,
  icon: Icon,
  tone = 'default',
}: {
  label: string
  value: number
  icon: LucideIcon
  tone?: 'default' | 'success' | 'destructive' | 'neutral'
}) {
  return (
    <Card>
      <CardHeader className="flex-row items-center gap-2 space-y-0 p-3 pb-1.5">
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
        <CardTitle className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        <p className={cn('text-xl font-semibold', STAT_TONE_CLASS[tone])}>{value}</p>
      </CardContent>
    </Card>
  )
}

/** Human-relative time with a full timestamp available on hover/focus via
 * `title` -- never fabricates a value: callers only reach this when
 * `last_updated_at` is actually present. */
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
 * Real, read-only secrets metadata workspace: GET /api/secrets.
 *
 * SECURITY: internal/server/rest.go's handleListSecrets never returns
 * plaintext secret values -- only name/provider/status/injection
 * type/rotation flag/version/timestamps/last_error are in the response body.
 * This component only ever reads those declared fields off SecretSummary
 * (see lib/api/dashboard.ts); it has no code path that would render a
 * `value`/`plaintext`/`secret` field even if a future or malicious response
 * included one, because no such field is destructured or interpolated
 * anywhere below.
 *
 * There is no rotate/edit/delete/reveal UI here: the backend exposes only a
 * read endpoint (GET /api/secrets), so this page is intentionally read-only.
 *
 * KNOWN BACKEND LIMITATION (not fixed here -- see Phase 3 report): today
 * handleListSecrets hardcodes status="synced" for every cached secret and
 * never populates last_synced_at/last_updated_at/version/last_error, and
 * auto_sync_enabled is declared but never set (always false). This
 * component still reads those fields correctly and will render them the
 * moment the backend starts populating them -- it just means "Version" and
 * "Last Updated" will legitimately show "Not available" today, which is the
 * honest behavior per the zero-vs-unavailable rule, not a bug in this file.
 */
/** Client-side status of a manual-rotation request, keyed by secret name.
 * "requesting" covers the moment between clicking "Rotate Secret" and the
 * POST resolving; "in_progress"/"succeeded"/"failed" reflect the REAL
 * backend rotation_succeeded/rotation_failed events from the existing
 * WebSocket stream, not a fabricated timer -- see RotationStatus doc below. */
type RotationPhase = 'idle' | 'requesting' | 'in_progress' | 'succeeded' | 'failed'

export function SecretsContent() {
  const [state, setState] = useState<SecretsState>(initialState)
  const [search, setSearch] = useState('')
  const [providerFilter, setProviderFilter] = useState<string>('all')
  const [injectionFilter, setInjectionFilter] = useState<string>('all')
  const [rotationFilter, setRotationFilter] = useState<'all' | 'on' | 'off'>('all')
  const [sort, setSort] = useState<{ key: SortKey; direction: SortDirection }>({ key: 'name', direction: 'asc' })
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [confirmTarget, setConfirmTarget] = useState<string | null>(null)
  const [rotationPhase, setRotationPhase] = useState<Record<string, RotationPhase>>({})

  // Real rotation lifecycle events -- the same EventStore/WebSocket stream
  // the Events/Audit pages already consume (internal/agent/trigger.go's
  // emitEvent). No parallel frontend status system is invented here: a
  // secret's badge only ever reflects an actual rotation_succeeded /
  // rotation_failed event for that secret, or the transient "requesting"
  // state between the click and the POST resolving.
  const { events: liveEvents } = useWebSocket('/api/events/ws')

  useEffect(() => {
    if (liveEvents.length === 0) return
    const latest = liveEvents[liveEvents.length - 1]
    if (!latest.secret || !latest.event_type) return
    if (latest.event_type === 'rotation_succeeded') {
      setRotationPhase((prev) => ({ ...prev, [latest.secret as string]: 'succeeded' }))
    } else if (latest.event_type === 'rotation_failed') {
      setRotationPhase((prev) => ({ ...prev, [latest.secret as string]: 'failed' }))
    }
  }, [liveEvents])

  const requestRotate = useCallback(async (name: string) => {
    setConfirmTarget(null)
    setRotationPhase((prev) => ({ ...prev, [name]: 'requesting' }))
    try {
      await rotateSecret(name)
      // The POST returning 202 only means the request was accepted -- it is
      // NOT rotation success. The real outcome arrives via
      // rotation_succeeded/rotation_failed above.
      setRotationPhase((prev) => ({ ...prev, [name]: 'in_progress' }))
    } catch {
      setRotationPhase((prev) => ({ ...prev, [name]: 'failed' }))
    }
  }, [])

  const load = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const data = await fetchSecrets()
      setState({ loading: false, error: null, data })
    } catch (err) {
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load secrets',
      }))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const allSecrets = state.data?.active_secrets ?? []

  const providers = useMemo(() => Array.from(new Set(allSecrets.map((s) => s.provider))).sort(), [allSecrets])
  const injectionTypes = useMemo(
    () => Array.from(new Set(allSecrets.map((s) => s.injection_type))).sort(),
    [allSecrets]
  )

  const filtersActive = providerFilter !== 'all' || injectionFilter !== 'all' || rotationFilter !== 'all' || search.trim() !== ''

  const clearFilters = () => {
    setSearch('')
    setProviderFilter('all')
    setInjectionFilter('all')
    setRotationFilter('all')
  }

  const filteredSecrets = useMemo(() => {
    const query = search.trim().toLowerCase()
    return allSecrets.filter((sec) => {
      if (providerFilter !== 'all' && sec.provider !== providerFilter) return false
      if (injectionFilter !== 'all' && sec.injection_type !== injectionFilter) return false
      if (rotationFilter === 'on' && !sec.rotation_enabled) return false
      if (rotationFilter === 'off' && sec.rotation_enabled) return false
      if (!query) return true
      return sec.name.toLowerCase().includes(query) || sec.provider.toLowerCase().includes(query)
    })
  }, [allSecrets, search, providerFilter, injectionFilter, rotationFilter])

  const sortedSecrets = useMemo(() => {
    const list = [...filteredSecrets]
    const { key, direction } = sort
    const dir = direction === 'asc' ? 1 : -1
    list.sort((a, b) => {
      switch (key) {
        case 'name':
          return a.name.localeCompare(b.name) * dir
        case 'status':
          return a.status.localeCompare(b.status) * dir
        case 'version':
          return (a.version ?? '').localeCompare(b.version ?? '') * dir
        case 'last_updated_at':
          return ((a.last_updated_at ?? '') < (b.last_updated_at ?? '') ? -1 : 1) * dir
        default:
          return 0
      }
    })
    return list
  }, [filteredSecrets, sort])

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
      total: allSecrets.length,
      providerCount: providers.length,
      rotationOn: allSecrets.filter((s) => s.rotation_enabled).length,
      errored: allSecrets.filter((s) => Boolean(s.last_error) || s.status.toLowerCase().includes('error')).length,
    }),
    [allSecrets, providers]
  )

  return (
    <div className="px-6 py-5">
      <div className="mx-auto max-w-6xl space-y-4">
        <header>
          <h1 className="text-lg font-semibold text-foreground">Secrets</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">
            Manage Docker secrets, injection state, and rotation metadata.
          </p>
          <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground/70">
            <ShieldCheck className="h-3.5 w-3.5 flex-shrink-0" />
            Secret values stay in the DSO runtime. This console shows metadata and operational state only.
          </p>
        </header>

        {state.loading && (
          <div data-testid="secrets-loading" aria-busy="true" className="space-y-4">
            <span className="sr-only">Loading secrets</span>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Card key={i}>
                  <CardHeader className="flex-row items-center gap-2 space-y-0 p-3 pb-1.5">
                    <Skeleton className="h-4 w-4 rounded-full" />
                    <Skeleton className="h-3.5 w-16" />
                  </CardHeader>
                  <CardContent className="p-3 pt-0">
                    <Skeleton className="h-6 w-10" />
                  </CardContent>
                </Card>
              ))}
            </div>
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
            data-testid="secrets-error"
            role="alert"
            aria-live="assertive"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <p className="font-medium">Unable to load secrets</p>
            <p className="mt-0.5 flex items-center justify-between gap-3 text-red-400/80">
              <span>DSO could not retrieve secret metadata from the running agent.</span>
              <Button variant="link" size="sm" onClick={load} className="h-auto flex-shrink-0 p-0 text-red-300 hover:text-red-200">
                Retry
              </Button>
            </p>
          </div>
        )}

        {!state.loading && !state.error && state.data && (
          <>
            {allSecrets.length > 0 && (
              <div data-testid="secrets-summary" className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <StatTile label="Managed Secrets" value={summary.total} icon={KeyRound} />
                <StatTile label="Providers" value={summary.providerCount} icon={Boxes} />
                <StatTile
                  label="Rotation On"
                  value={summary.rotationOn}
                  icon={RefreshCcw}
                  tone={summary.rotationOn > 0 ? 'success' : 'neutral'}
                />
                <StatTile
                  label="In Error"
                  value={summary.errored}
                  icon={AlertCircle}
                  tone={summary.errored > 0 ? 'destructive' : 'neutral'}
                />
              </div>
            )}

            <Card data-testid="secrets-content">
              {allSecrets.length > 0 && (
                <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
                  <div className="relative w-full max-w-xs">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      data-testid="secrets-search"
                      aria-label="Search secrets by name or provider"
                      placeholder="Search by name or provider..."
                      value={search}
                      onChange={(e) => setSearch(e.target.value)}
                      className="h-8 pl-8 text-sm"
                    />
                  </div>

                  {providers.length > 1 && (
                    <select
                      aria-label="Filter by provider"
                      data-testid="filter-provider"
                      value={providerFilter}
                      onChange={(e) => setProviderFilter(e.target.value)}
                      className={selectClass}
                    >
                      <option value="all">All providers</option>
                      {providers.map((p) => (
                        <option key={p} value={p}>
                          {p}
                        </option>
                      ))}
                    </select>
                  )}

                  {injectionTypes.length > 1 && (
                    <select
                      aria-label="Filter by injection type"
                      data-testid="filter-injection"
                      value={injectionFilter}
                      onChange={(e) => setInjectionFilter(e.target.value)}
                      className={selectClass}
                    >
                      <option value="all">All injection types</option>
                      {injectionTypes.map((t) => (
                        <option key={t} value={t}>
                          {t}
                        </option>
                      ))}
                    </select>
                  )}

                  <select
                    aria-label="Filter by rotation"
                    data-testid="filter-rotation"
                    value={rotationFilter}
                    onChange={(e) => setRotationFilter(e.target.value as 'all' | 'on' | 'off')}
                    className={selectClass}
                  >
                    <option value="all">Rotation: all</option>
                    <option value="on">Rotation: on</option>
                    <option value="off">Rotation: off</option>
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
                {allSecrets.length === 0 ? (
                  <EmptyState
                    data-testid="secrets-empty"
                    icon={KeyRound}
                    title="No managed secrets yet"
                    description="DSO hasn't loaded any managed secrets from the current Docker Compose configuration."
                  />
                ) : sortedSecrets.length === 0 ? (
                  <EmptyState
                    data-testid="secrets-no-match"
                    icon={Search}
                    title={search.trim() ? `No secrets match "${search.trim()}"` : 'No secrets match the selected filters'}
                    description="Try a different search term or clear the filters."
                  />
                ) : (
                  <Table aria-label="Managed secrets">
                    <caption className="sr-only">Managed secrets</caption>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-8" />
                        <TableHead>
                          <SortButton
                            label="Secret"
                            active={sort.key === 'name'}
                            direction={sort.direction}
                            onClick={() => toggleSort('name')}
                          />
                        </TableHead>
                        <TableHead className="hidden sm:table-cell">Provider</TableHead>
                        <TableHead>Injection</TableHead>
                        <TableHead>
                          <SortButton
                            label="Status"
                            active={sort.key === 'status'}
                            direction={sort.direction}
                            onClick={() => toggleSort('status')}
                          />
                        </TableHead>
                        <TableHead className="hidden md:table-cell">Rotation</TableHead>
                        <TableHead className="hidden lg:table-cell">
                          <SortButton
                            label="Version"
                            active={sort.key === 'version'}
                            direction={sort.direction}
                            onClick={() => toggleSort('version')}
                          />
                        </TableHead>
                        <TableHead className="hidden lg:table-cell">
                          <SortButton
                            label="Last Updated"
                            active={sort.key === 'last_updated_at'}
                            direction={sort.direction}
                            onClick={() => toggleSort('last_updated_at')}
                          />
                        </TableHead>
                        <TableHead className="text-right pr-3">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {sortedSecrets.map((sec) => {
                        const rowId = `${sec.provider}:${sec.name}`
                        const isOpen = expanded.has(rowId)
                        return (
                          <SecretRow
                            key={rowId}
                            secret={sec}
                            isOpen={isOpen}
                            onToggle={() => toggleExpanded(rowId)}
                            phase={rotationPhase[sec.name] ?? 'idle'}
                            onRotateClick={() => setConfirmTarget(sec.name)}
                          />
                        )
                      })}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </>
        )}

        {confirmTarget && (
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="rotate-confirm-title"
            data-testid="rotate-confirm-dialog"
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 px-4"
          >
            <Card className="w-full max-w-sm">
              <CardContent className="p-5">
                <h2 id="rotate-confirm-title" className="text-sm font-semibold text-foreground">
                  Rotate {confirmTarget} now?
                </h2>
                <p className="mt-2 text-xs text-muted-foreground">
                  Rotation will affect every container currently using this secret.
                </p>
                <div className="mt-4 flex justify-end gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    data-testid="rotate-cancel"
                    onClick={() => setConfirmTarget(null)}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="destructive"
                    size="sm"
                    data-testid="rotate-confirm"
                    onClick={() => requestRotate(confirmTarget)}
                  >
                    Rotate Secret
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  )
}

const ROTATION_BADGE: Record<RotationPhase, { label: string; className: string } | null> = {
  idle: null,
  requesting: { label: 'Requesting…', className: 'text-muted-foreground' },
  in_progress: { label: 'Rotating…', className: 'text-amber-400' },
  succeeded: { label: '✓ Rotated', className: 'text-emerald-400' },
  failed: { label: '✕ Rotation failed', className: 'text-red-400' },
}

function SecretRow({
  secret,
  isOpen,
  onToggle,
  phase,
  onRotateClick,
}: {
  secret: SecretSummary
  isOpen: boolean
  onToggle: () => void
  phase: RotationPhase
  onRotateClick: () => void
}) {
  const badge = ROTATION_BADGE[phase]
  const busy = phase === 'requesting' || phase === 'in_progress'
  return (
    <>
      <TableRow className="cursor-pointer hover:bg-accent/40" onClick={onToggle}>
        <TableCell className="w-8 pr-0">
          <button
            type="button"
            aria-expanded={isOpen}
            aria-label={isOpen ? `Collapse details for ${secret.name}` : `Expand details for ${secret.name}`}
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
          <span className="block font-medium text-foreground">{secret.name}</span>
          {secret.mount_path && (
            <span className="block font-mono text-xs text-muted-foreground/70">{secret.mount_path}</span>
          )}
        </TableCell>
        <TableCell className="hidden sm:table-cell text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <ProviderIcon provider={secret.provider} />
            {secret.provider}
          </span>
        </TableCell>
        <TableCell className="text-muted-foreground">{secret.injection_type}</TableCell>
        <TableCell>
          <StatusBadge status={secret.status} />
          {secret.last_error && (
            <span
              title={secret.last_error}
              className="ml-1.5 inline-block align-middle text-red-400"
              aria-label={`Error: ${secret.last_error}`}
            >
              <AlertCircle className="h-3.5 w-3.5" />
            </span>
          )}
        </TableCell>
        <TableCell className="hidden md:table-cell">
          <StatusBadge status={secret.rotation_enabled ? 'on' : 'off'} />
        </TableCell>
        <TableCell className="hidden lg:table-cell text-muted-foreground">
          {secret.version ?? <span className="text-muted-foreground/60">Not available</span>}
        </TableCell>
        <TableCell className="hidden lg:table-cell">
          {secret.last_updated_at ? (
            <RelativeTime iso={secret.last_updated_at} />
          ) : (
            <span className="text-muted-foreground/60">Not available</span>
          )}
        </TableCell>
        <TableCell className="text-right pr-3" onClick={(e) => e.stopPropagation()}>
          <div className="flex items-center justify-end gap-2">
            {badge && (
              <span data-testid={`rotate-status-${secret.name}`} className={cn('text-xs whitespace-nowrap', badge.className)}>
                {badge.label}
              </span>
            )}
            <Button
              variant="outline"
              size="sm"
              data-testid={`rotate-button-${secret.name}`}
              disabled={busy}
              onClick={onRotateClick}
            >
              {busy ? 'Rotating…' : 'Rotate'}
            </Button>
          </div>
        </TableCell>
      </TableRow>
      {isOpen && (
        <TableRow className="hover:bg-transparent" data-testid={`secret-detail-${secret.name}`}>
          <TableCell colSpan={9} className="bg-muted/20 py-3">
            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 px-2 text-xs sm:grid-cols-4">
              <DetailField label="Provider" value={secret.provider} />
              <DetailField label="Injection type" value={secret.injection_type} />
              <DetailField label="Rotation" value={secret.rotation_enabled ? 'Enabled' : 'Off'} />
              <DetailField label="Auto sync" value={secret.auto_sync_enabled ? 'Enabled' : 'Off'} />
              <DetailField label="Version" value={secret.version ?? 'Not available'} />
              <DetailField
                label="Last updated"
                value={secret.last_updated_at ? new Date(secret.last_updated_at).toLocaleString() : 'Not available'}
              />
              <DetailField
                label="Last synced"
                value={secret.last_synced_at ? new Date(secret.last_synced_at).toLocaleString() : 'Not available'}
              />
              {secret.mount_path && <DetailField label="Mount path" value={secret.mount_path} mono />}
              {secret.last_error && (
                <div className="col-span-full">
                  <dt className="text-muted-foreground">Last error</dt>
                  <dd className="mt-0.5 text-red-400">{secret.last_error}</dd>
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
      <dd className={cn('mt-0.5 text-foreground', mono && 'font-mono text-[11px]')}>{value}</dd>
    </div>
  )
}

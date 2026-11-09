'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import {
  ShieldCheck,
  ShieldAlert,
  KeyRound,
  Radio,
  Bell,
  Boxes,
  RefreshCcw,
  AlertTriangle,
  ArrowRight,
  type LucideIcon,
} from 'lucide-react'
import { fetchHealth, fetchSecrets, fetchDiscovery, type SecretSummary, type SecretsResponse, type DiscoveryResponse } from '@/lib/api/dashboard'
import { fetchEvents } from '@/lib/api/events'
import { fetchAnalytics, type AnalyticsResponse } from '@/lib/api/analytics'
import type { Event } from '@/hooks/useWebSocket'
import { AuthenticationError } from '@/lib/errors'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { StatusBadge } from '@/components/ui/status-badge'
import { cn } from '@/lib/utils'
import Link from 'next/link'

interface CoreState {
  loading: boolean
  error: string | null
  healthUp: boolean | null
  secrets: SecretsResponse | null
  discovery: DiscoveryResponse | null
}

const initialCoreState: CoreState = {
  loading: true,
  error: null,
  healthUp: null,
  secrets: null,
  discovery: null,
}

interface AnalyticsState {
  loading: boolean
  error: string | null
  data: AnalyticsResponse | null
}

const initialAnalyticsState: AnalyticsState = { loading: true, error: null, data: null }

/** One merged, chronologically-sortable row built from analytics'
 * recent_activity arrays -- each array tells us which bucket (and therefore
 * tone/label) an Event belongs to; nothing here is inferred or fabricated. */
interface RotationActivityRow {
  event: Event
  label: string
  tone: 'success' | 'destructive'
}

function buildRotationActivity(data: AnalyticsResponse | null): RotationActivityRow[] {
  if (!data) return []
  const rows: RotationActivityRow[] = [
    ...data.recent_activity.successful_rotations.map((event) => ({ event, label: 'Rotation succeeded', tone: 'success' as const })),
    ...data.recent_activity.failed_rotations.map((event) => ({ event, label: 'Rotation failed', tone: 'destructive' as const })),
    ...data.recent_activity.injection_failures.map((event) => ({ event, label: 'Injection failed', tone: 'destructive' as const })),
  ]
  return rows
    .sort((a, b) => new Date(b.event.timestamp).getTime() - new Date(a.event.timestamp).getTime())
    .slice(0, 6)
}

/**
 * Real, honest dashboard content. Wired to every source the backend
 * actually provides for an overview: GET /health, GET /api/secrets,
 * GET /api/discovery, GET /api/analytics, GET /api/events. No
 * forecast/correlation/autonomy/scheduler/graph/plugins/policy/insights
 * data exists server-side, so no cards for any of that appear here.
 *
 * /api/analytics is fetched independently of the core Promise.all (health +
 * secrets + discovery): analytics is a richer, secondary view built from the
 * same underlying state, and a slow/failed analytics call must never blank
 * the primary agent-status/secrets-count/webhook cards that already render
 * from the core fetch. Same best-effort convention as the events feed below.
 */
export function DashboardContent() {
  const [core, setCore] = useState<CoreState>(initialCoreState)
  const [analytics, setAnalytics] = useState<AnalyticsState>(initialAnalyticsState)
  // Recent events is a secondary, best-effort widget: if it fails, the rest
  // of the dashboard still renders. Errors are swallowed to an empty list
  // rather than surfaced, matching "recent activity" being informational,
  // not load-bearing.
  const [events, setEvents] = useState<Event[]>([])
  const { logout } = useAuth()
  // Guards against a redirect storm: Promise.all can reject from more than
  // one of the three fetches racing to a 401 (and a Retry click could layer
  // another load() on top), but the operator should only be sent to /login
  // once per session expiry, not once per failed request.
  const redirectingRef = useRef(false)

  const loadCore = useCallback(async () => {
    setCore((s) => ({ ...s, loading: true, error: null }))
    try {
      const [health, secrets, discovery] = await Promise.all([
        fetchHealth(),
        fetchSecrets(),
        fetchDiscovery(),
      ])
      setCore({
        loading: false,
        error: null,
        healthUp: health.status === 'up',
        secrets,
        discovery,
      })
    } catch (err) {
      if (err instanceof AuthenticationError) {
        if (!redirectingRef.current) {
          redirectingRef.current = true
          void logout()
        }
        // Stay in the loading state rather than flashing the generic
        // retry-forever error banner -- logout() will navigate to /login.
        return
      }
      setCore((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load dashboard data',
      }))
    }
  }, [logout])

  const loadAnalytics = useCallback(async () => {
    setAnalytics((s) => ({ ...s, loading: true, error: null }))
    try {
      const data = await fetchAnalytics()
      setAnalytics({ loading: false, error: null, data })
    } catch (err) {
      if (err instanceof AuthenticationError) {
        // The core fetch's own 401 handling already redirects; avoid a
        // second competing error message here.
        return
      }
      setAnalytics({
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load runtime analytics',
        data: null,
      })
    }
  }, [])

  useEffect(() => {
    loadCore()
  }, [loadCore])

  useEffect(() => {
    loadAnalytics()
  }, [loadAnalytics])

  useEffect(() => {
    let cancelled = false
    fetchEvents(6)
      .then((ev) => {
        if (!cancelled) setEvents(ev)
      })
      .catch(() => {
        // Best-effort widget -- leave events empty on failure rather than
        // surfacing an error the operator can't act on.
      })
    return () => {
      cancelled = true
    }
  }, [])

  const rotationActivity = buildRotationActivity(analytics.data)
  const degradedServices = analytics.data?.degraded_services ?? []
  const previewSecrets: SecretSummary[] = core.secrets?.active_secrets.slice(0, 5) ?? []

  return (
    <div className="px-6 py-5">
      <div className="mx-auto max-w-6xl space-y-5">
        <header>
          <h1 className="text-lg font-semibold text-foreground">DSO Dashboard</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">Docker Secret Operator — runtime overview</p>
        </header>

        {core.loading && (
          <div data-testid="dashboard-loading" aria-busy="true" className="space-y-5">
            <span className="sr-only">Loading dashboard</span>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
              {Array.from({ length: 5 }).map((_, i) => (
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
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                <Skeleton className="h-4 w-4 rounded-full" />
                <Skeleton className="h-4 w-20" />
              </CardHeader>
              <CardContent className="space-y-2 p-4 pt-0">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-8 w-full" />
                ))}
              </CardContent>
            </Card>
          </div>
        )}

        {!core.loading && core.error && (
          <div
            data-testid="dashboard-error"
            role="alert"
            aria-live="assertive"
            className="flex items-center justify-between rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <span>{core.error}</span>
            <Button variant="link" size="sm" onClick={loadCore} className="h-auto p-0 text-red-300 hover:text-red-200">
              Retry
            </Button>
          </div>
        )}

        {!core.loading && !core.error && (
          <div data-testid="dashboard-content" className="space-y-5">
            {/* Status strip -- only fields the frontend can actually prove. No
                Docker/Proxy/Rotation-in-progress tile: no endpoint exposes
                those to the browser today (see docs/webui-phase1-notes.md). */}
            <div className="flex flex-wrap items-center gap-2">
              <StatusPill
                icon={core.healthUp ? ShieldCheck : ShieldAlert}
                label="Agent"
                value={core.healthUp ? 'Up' : 'Down'}
                tone={core.healthUp ? 'success' : 'destructive'}
              />
              <StatusPill
                icon={Radio}
                label="Webhook"
                value={core.discovery?.webhook_enabled ? 'Enabled' : 'Disabled'}
                tone={core.discovery?.webhook_enabled ? 'success' : 'neutral'}
              />
            </div>

            {/* Operational summary -- from /api/analytics, loaded and
                error-handled independently of the status strip above. */}
            {analytics.loading ? (
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-5" data-testid="analytics-loading">
                {Array.from({ length: 5 }).map((_, i) => (
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
            ) : analytics.error ? (
              <div
                data-testid="analytics-error"
                role="alert"
                className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 text-sm text-amber-400"
              >
                <p className="font-medium">Unable to load runtime analytics</p>
                <p className="mt-0.5 text-amber-400/80">
                  DSO could not retrieve analytics from the running agent. Check agent connectivity or run{' '}
                  <code className="rounded bg-black/20 px-1 py-0.5 font-mono text-xs">docker dso doctor</code>.
                </p>
              </div>
            ) : (
              <div className="grid grid-cols-2 gap-3 sm:grid-cols-5" data-testid="analytics-summary">
                <StatCard
                  icon={KeyRound}
                  label="Managed Secrets"
                  value={analytics.data?.current.managed_secrets}
                />
                <StatCard
                  icon={Boxes}
                  label="Containers"
                  value={analytics.data?.current.containers_targeted}
                />
                <StatCard
                  icon={RefreshCcw}
                  label="Rotations OK"
                  value={analytics.data?.since_restart.rotation_success_total}
                  tone="success"
                  hint="since restart"
                />
                <StatCard
                  icon={AlertTriangle}
                  label="Rotations Failed"
                  value={analytics.data?.since_restart.rotation_failure_total}
                  tone={
                    analytics.data && analytics.data.since_restart.rotation_failure_total > 0
                      ? 'destructive'
                      : undefined
                  }
                  hint="since restart"
                />
                <StatCard
                  icon={ShieldAlert}
                  label="Degraded"
                  value={analytics.data?.current.degraded}
                  tone={analytics.data && (analytics.data.current.degraded ?? 0) > 0 ? 'warning' : undefined}
                />
              </div>
            )}

            {/* Attention -- only rendered when there is something real to
                show. No "0 degraded services" card. */}
            {degradedServices.length > 0 && (
              <Card data-testid="dashboard-degraded" className="border-amber-500/25">
                <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                  <AlertTriangle className="h-4 w-4 text-amber-400" />
                  <CardTitle className="text-xs font-medium text-muted-foreground">
                    Attention required — {degradedServices.length} degraded
                  </CardTitle>
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  <ul className="divide-y divide-border">
                    {degradedServices.map((d) => (
                      <li key={d.service} className="flex items-center justify-between gap-3 py-2 text-sm">
                        <span className="font-mono text-foreground">{d.service}</span>
                        <span className="text-xs text-muted-foreground">{d.reason}</span>
                      </li>
                    ))}
                  </ul>
                </CardContent>
              </Card>
            )}

            <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
              {/* Rotation activity -- from /api/analytics recent_activity */}
              <Card>
                <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                  <RefreshCcw className="h-4 w-4 text-muted-foreground" />
                  <CardTitle className="text-xs font-medium text-muted-foreground">Rotation Activity</CardTitle>
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  {analytics.loading ? (
                    <div className="space-y-2">
                      {Array.from({ length: 3 }).map((_, i) => (
                        <Skeleton key={i} className="h-8 w-full" />
                      ))}
                    </div>
                  ) : rotationActivity.length === 0 ? (
                    <EmptyState
                      icon={RefreshCcw}
                      title="No rotation activity yet"
                      description="Rotation events will appear here when DSO processes a secret change."
                    />
                  ) : (
                    <ul className="divide-y divide-border">
                      {rotationActivity.map((row, i) => (
                        <li key={i} className="flex items-center justify-between gap-3 py-2 text-sm">
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-foreground">
                              {row.event.secret ?? row.event.container ?? 'unknown resource'}
                            </span>
                            <StatusBadge status={row.label} tone={row.tone} className="mt-1" />
                          </span>
                          <span className="flex-shrink-0 text-xs text-muted-foreground">
                            {new Date(row.event.timestamp).toLocaleString()}
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </CardContent>
              </Card>

              {/* Recent events -- from GET /api/events */}
              <Card>
                <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                  <Bell className="h-4 w-4 text-muted-foreground" />
                  <CardTitle className="text-xs font-medium text-muted-foreground">Recent Events</CardTitle>
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  {events.length === 0 ? (
                    <EmptyState
                      icon={Bell}
                      title="No recent activity"
                      description="DSO events will appear here as runtime activity occurs."
                    />
                  ) : (
                    <ul className="divide-y divide-border">
                      {events.map((ev, i) => (
                        <li key={i} className="flex items-center justify-between gap-3 py-2 text-sm">
                          <span className="min-w-0 truncate text-foreground">
                            {ev.event_type ?? 'event'}
                            {ev.secret ? ` — ${ev.secret}` : ''}
                            {ev.container ? ` (${ev.container})` : ''}
                          </span>
                          <span className="flex-shrink-0 text-xs text-muted-foreground">
                            {new Date(ev.timestamp).toLocaleString()}
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </CardContent>
              </Card>
            </div>

            {/* Secrets preview */}
            <Card data-testid="dashboard-secrets-card">
              <CardHeader className="flex-row items-center justify-between space-y-0 p-4 pb-2">
                <span className="flex items-center gap-2">
                  <KeyRound className="h-4 w-4 text-primary" />
                  <CardTitle className="text-xs font-medium text-muted-foreground">
                    {core.secrets?.total_count ?? 0} managed secret{core.secrets?.total_count === 1 ? '' : 's'}
                  </CardTitle>
                </span>
                {core.secrets && core.secrets.total_count > 0 && (
                  <Link
                    href="/secrets"
                    className="flex items-center gap-1 text-xs font-medium text-primary hover:underline"
                  >
                    View all <ArrowRight className="h-3 w-3" />
                  </Link>
                )}
              </CardHeader>
              <CardContent className="p-4 pt-0">
                {!core.secrets || core.secrets.active_secrets.length === 0 ? (
                  <EmptyState
                    data-testid="dashboard-secrets-empty"
                    icon={KeyRound}
                    title="No managed secrets yet"
                    description="DSO has not loaded any managed secrets from the current Docker Compose configuration."
                  />
                ) : (
                  <Table aria-label="Managed secrets">
                    <caption className="sr-only">Managed secrets</caption>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Provider</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead>Injection</TableHead>
                        <TableHead>Rotation</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {previewSecrets.map((sec) => (
                        <TableRow key={`${sec.provider}:${sec.name}`}>
                          <TableCell>{sec.name}</TableCell>
                          <TableCell className="text-muted-foreground">{sec.provider}</TableCell>
                          <TableCell>
                            <StatusBadge status={sec.status} />
                          </TableCell>
                          <TableCell className="text-muted-foreground">{sec.injection_type}</TableCell>
                          <TableCell className="text-muted-foreground">
                            {sec.rotation_enabled ? 'on' : 'off'}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </div>
    </div>
  )
}

function StatusPill({
  icon: Icon,
  label,
  value,
  tone,
}: {
  icon: LucideIcon
  label: string
  value: string
  tone: 'success' | 'destructive' | 'neutral'
}) {
  const toneClass =
    tone === 'success' ? 'text-emerald-400' : tone === 'destructive' ? 'text-red-400' : 'text-muted-foreground'
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border bg-card px-3 py-1.5 text-sm">
      <Icon className={cn('h-3.5 w-3.5', toneClass)} />
      <span className="text-muted-foreground">{label}</span>
      <span className={cn('font-medium', toneClass)}>{value}</span>
    </div>
  )
}

function StatCard({
  icon: Icon,
  label,
  value,
  tone,
  hint,
}: {
  icon: LucideIcon
  label: string
  value: number | null | undefined
  tone?: 'success' | 'destructive' | 'warning'
  hint?: string
}) {
  const toneClass =
    tone === 'success'
      ? 'text-emerald-400'
      : tone === 'destructive'
        ? 'text-red-400'
        : tone === 'warning'
          ? 'text-amber-400'
          : 'text-foreground'

  return (
    <Card>
      <CardHeader className="flex-row items-center gap-2 space-y-0 p-3 pb-1.5">
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
        <CardTitle className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-3 pt-0">
        {value === null || value === undefined ? (
          <p className="text-sm text-muted-foreground/70">Unavailable</p>
        ) : (
          <p className={cn('text-xl font-semibold', toneClass)}>
            {value}
            {hint && <span className="ml-1.5 text-[11px] font-normal text-muted-foreground">{hint}</span>}
          </p>
        )}
      </CardContent>
    </Card>
  )
}

'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import { Activity, ShieldCheck, ShieldAlert, KeyRound, Radio, Bell } from 'lucide-react'
import { fetchHealth, fetchSecrets, fetchDiscovery, type SecretsResponse, type DiscoveryResponse } from '@/lib/api/dashboard'
import { fetchEvents } from '@/lib/api/events'
import type { Event } from '@/hooks/useWebSocket'
import { AuthenticationError } from '@/lib/errors'
import { useAuth } from '@/contexts/AuthContext'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { StatusBadge } from '@/components/ui/status-badge'

interface DashboardState {
  loading: boolean
  error: string | null
  healthUp: boolean | null
  secrets: SecretsResponse | null
  discovery: DiscoveryResponse | null
}

const initialState: DashboardState = {
  loading: true,
  error: null,
  healthUp: null,
  secrets: null,
  discovery: null,
}

/**
 * Real, honest dashboard content. Only wired to data the backend actually
 * provides today (GET /health, GET /api/secrets, GET /api/discovery,
 * GET /api/events for the activity feed) -- no forecast/correlation/
 * autonomy/scheduler/graph/plugins/policy/insights data exists server-side,
 * so no cards for any of that are included here.
 */
export function DashboardContent() {
  const [state, setState] = useState<DashboardState>(initialState)
  // Recent activity is a secondary, best-effort widget sourced from the same
  // events feed used elsewhere (events-content.tsx). It's fetched
  // independently of the three core stats: if it fails, the dashboard's
  // primary stats still render -- a broken/slow activity feed must never
  // take down the whole page. Errors are swallowed to an empty list rather
  // than surfaced, matching "recent activity" being informational, not
  // load-bearing.
  const [events, setEvents] = useState<Event[]>([])
  const { logout } = useAuth()
  // Guards against a redirect storm: Promise.all can reject from more than
  // one of the three fetches racing to a 401 (and a Retry click could layer
  // another load() on top), but the operator should only be sent to /login
  // once per session expiry, not once per failed request.
  const redirectingRef = useRef(false)

  const load = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }))
    try {
      const [health, secrets, discovery] = await Promise.all([
        fetchHealth(),
        fetchSecrets(),
        fetchDiscovery(),
      ])
      setState({
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
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load dashboard data',
      }))
    }
  }, [logout])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    let cancelled = false
    fetchEvents(5)
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

  return (
    <div className="px-8 py-6">
      <div className="mx-auto max-w-5xl">
        <header className="mb-6">
          <h1 className="text-lg font-semibold text-foreground">DSO Dashboard</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">Docker Secret Operator — operator overview</p>
        </header>

        {state.loading && (
          <div
            data-testid="dashboard-loading"
            aria-busy="true"
            className="grid grid-cols-1 gap-3 sm:grid-cols-3"
          >
            <span className="sr-only">Loading dashboard</span>
            {Array.from({ length: 3 }).map((_, i) => (
              <Card key={i}>
                <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                  <Skeleton className="h-4 w-4 rounded-full" />
                  <Skeleton className="h-4 w-28" />
                </CardHeader>
                <CardContent className="p-4 pt-0">
                  <Skeleton className="h-7 w-16" />
                </CardContent>
              </Card>
            ))}
            <Card className="sm:col-span-3">
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

        {!state.loading && state.error && (
          <div
            data-testid="dashboard-error"
            role="alert"
            aria-live="assertive"
            className="flex items-center justify-between rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <span>{state.error}</span>
            <Button variant="link" size="sm" onClick={load} className="h-auto p-0 text-red-300 hover:text-red-200">
              Retry
            </Button>
          </div>
        )}

        {!state.loading && !state.error && (
          <div data-testid="dashboard-content" className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {/* Agent status */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-1.5">
                {state.healthUp ? (
                  <ShieldCheck className="h-4 w-4 text-emerald-400" />
                ) : (
                  <ShieldAlert className="h-4 w-4 text-red-400" />
                )}
                <CardTitle className="text-xs font-medium text-muted-foreground">Agent status</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                <p className="text-xl font-semibold text-foreground">{state.healthUp ? 'Up' : 'Down'}</p>
              </CardContent>
            </Card>

            {/* Secrets */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-1.5">
                <KeyRound className="h-4 w-4 text-primary" />
                <CardTitle className="text-xs font-medium text-muted-foreground">Managed secrets</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                <p className="text-xl font-semibold text-foreground">{state.secrets?.total_count ?? 0}</p>
                {state.secrets && state.secrets.total_count === 0 && (
                  <p data-testid="dashboard-secrets-empty" className="mt-1 text-xs text-muted-foreground">
                    None cached yet — secrets appear here once DSO loads them.
                  </p>
                )}
              </CardContent>
            </Card>

            {/* Capabilities */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-1.5">
                <Radio className="h-4 w-4 text-amber-400" />
                <CardTitle className="text-xs font-medium text-muted-foreground">Webhook capability</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                <p className="text-xl font-semibold text-foreground">
                  {state.discovery?.webhook_enabled ? 'Enabled' : 'Disabled'}
                </p>
              </CardContent>
            </Card>

            {/* Secrets table */}
            <Card className="sm:col-span-3">
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                <Activity className="h-4 w-4 text-muted-foreground" />
                <CardTitle className="text-xs font-medium text-muted-foreground">Secrets</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                {!state.secrets || state.secrets.active_secrets.length === 0 ? (
                  <EmptyState
                    icon={KeyRound}
                    title="No managed secrets yet"
                    description="Secrets will appear here once DSO loads them."
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
                      {state.secrets.active_secrets.map((sec) => (
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

            {/* Recent activity (real events from GET /api/events) */}
            <Card className="sm:col-span-3">
              <CardHeader className="flex-row items-center gap-2 space-y-0 p-4 pb-2">
                <Bell className="h-4 w-4 text-muted-foreground" />
                <CardTitle className="text-xs font-medium text-muted-foreground">Recent activity</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                {events.length === 0 ? (
                  <EmptyState
                    icon={Bell}
                    title="No recent activity"
                    description="Events DSO records will show up here as they happen."
                  />
                ) : (
                  <ul className="divide-y divide-border">
                    {events.map((ev, i) => (
                      <li key={i} className="flex items-center justify-between gap-3 py-2 text-sm">
                        <span className="text-foreground">
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
        )}
      </div>
    </div>
  )
}


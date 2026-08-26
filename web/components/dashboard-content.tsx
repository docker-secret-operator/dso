'use client'

import { useEffect, useState, useCallback } from 'react'
import { Activity, ShieldCheck, ShieldAlert, KeyRound, Radio } from 'lucide-react'
import { fetchHealth, fetchSecrets, fetchDiscovery, type SecretsResponse, type DiscoveryResponse } from '@/lib/api/dashboard'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'

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
 * provides today (GET /health, GET /api/secrets, GET /api/discovery) --
 * no forecast/correlation/autonomy/scheduler/graph/plugins/policy/insights
 * data exists server-side, so no cards for any of that are included here.
 */
export function DashboardContent() {
  const [state, setState] = useState<DashboardState>(initialState)

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
      setState((s) => ({
        ...s,
        loading: false,
        error: err instanceof Error ? err.message : 'Failed to load dashboard data',
      }))
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-8">
          <h1 className="text-xl font-semibold text-foreground">DSO Dashboard</h1>
          <p className="mt-1 text-sm text-muted-foreground">Docker Secret Operator — operator overview</p>
        </header>

        {state.loading && (
          <div data-testid="dashboard-loading" className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Card key={i}>
                <CardHeader className="flex-row items-center gap-2 space-y-0 pb-3">
                  <Skeleton className="h-5 w-5 rounded-full" />
                  <Skeleton className="h-4 w-28" />
                </CardHeader>
                <CardContent>
                  <Skeleton className="h-8 w-16" />
                </CardContent>
              </Card>
            ))}
            <Card className="sm:col-span-3">
              <CardHeader className="flex-row items-center gap-2 space-y-0">
                <Skeleton className="h-5 w-5 rounded-full" />
                <Skeleton className="h-4 w-20" />
              </CardHeader>
              <CardContent className="space-y-2">
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
            className="flex items-center justify-between rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            <span>{state.error}</span>
            <Button variant="link" size="sm" onClick={load} className="h-auto p-0 text-red-300 hover:text-red-200">
              Retry
            </Button>
          </div>
        )}

        {!state.loading && !state.error && (
          <div data-testid="dashboard-content" className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            {/* Agent status */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 pb-3">
                {state.healthUp ? (
                  <ShieldCheck className="h-5 w-5 text-emerald-400" />
                ) : (
                  <ShieldAlert className="h-5 w-5 text-red-400" />
                )}
                <CardTitle className="text-sm font-medium text-muted-foreground">Agent status</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold text-foreground">{state.healthUp ? 'Up' : 'Down'}</p>
              </CardContent>
            </Card>

            {/* Secrets */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 pb-3">
                <KeyRound className="h-5 w-5 text-primary" />
                <CardTitle className="text-sm font-medium text-muted-foreground">Managed secrets</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold text-foreground">{state.secrets?.total_count ?? 0}</p>
                {state.secrets && state.secrets.total_count === 0 && (
                  <p data-testid="dashboard-secrets-empty" className="mt-2 text-xs text-muted-foreground">
                    None cached yet — secrets appear here once DSO loads them.
                  </p>
                )}
              </CardContent>
            </Card>

            {/* Capabilities */}
            <Card>
              <CardHeader className="flex-row items-center gap-2 space-y-0 pb-3">
                <Radio className="h-5 w-5 text-amber-400" />
                <CardTitle className="text-sm font-medium text-muted-foreground">Webhook capability</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-semibold text-foreground">
                  {state.discovery?.webhook_enabled ? 'Enabled' : 'Disabled'}
                </p>
              </CardContent>
            </Card>

            {/* Secrets table */}
            <Card className="sm:col-span-3">
              <CardHeader className="flex-row items-center gap-2 space-y-0">
                <Activity className="h-5 w-5 text-muted-foreground" />
                <CardTitle className="text-sm font-medium text-muted-foreground">Secrets</CardTitle>
              </CardHeader>
              <CardContent>
                {!state.secrets || state.secrets.active_secrets.length === 0 ? (
                  <EmptyState
                    icon={KeyRound}
                    title="No secrets to display"
                    description="Secrets managed by DSO will show up here once they're loaded."
                  />
                ) : (
                  <Table>
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
                          <TableCell>{sec.status}</TableCell>
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


'use client'

import { useEffect, useState } from 'react'
import { BarChart3, Info, ShieldAlert, ShieldCheck } from 'lucide-react'

import { fetchAnalytics, type AnalyticsResponse } from '@/lib/api/analytics'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { useToast } from '@/components/toast-provider'

/** Renders a current-state count, distinguishing null (unavailable) from a real 0. */
function StatValue({ value }: { value: number | null }) {
  if (value === null) {
    return <span className="text-muted-foreground">unavailable</span>
  }
  return <span>{value}</span>
}

function EventRow({ ev }: { ev: Record<string, unknown> }) {
  const secret = typeof ev.secret === 'string' ? ev.secret : undefined
  const container = typeof ev.container === 'string' ? ev.container : undefined
  const timestamp = typeof ev.timestamp === 'string' ? ev.timestamp : undefined
  const error = typeof ev.error === 'string' ? ev.error : undefined
  return (
    <div className="flex items-center justify-between border-b border-border/60 px-4 py-2 text-sm last:border-0">
      <div className="min-w-0">
        <p className="truncate text-foreground">
          {(ev.event_type as string) ?? 'event'}
          {secret ? ` — ${secret}` : ''}
          {container ? ` (${container})` : ''}
        </p>
        {error && <p className="truncate text-xs text-red-400/80">{error}</p>}
      </div>
      {timestamp && (
        <span className="ml-4 flex-shrink-0 text-xs text-muted-foreground">
          {new Date(timestamp).toLocaleString()}
        </span>
      )}
    </div>
  )
}

/**
 * Operational Analytics Overview: GET /api/analytics. Current-state fields
 * (managed secrets, containers, drift, degraded) are live and real;
 * since_restart counters are process-lifetime-only and explicitly labeled
 * as such -- never presented as an all-time total. A failure loading this
 * page never touches rotation -- it's a pure read of already-computed
 * state.
 */
export function AnalyticsContent() {
  const [data, setData] = useState<AnalyticsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const toast = useToast()

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchAnalytics()
        if (!cancelled) setData(res)
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Failed to load analytics'
        if (!cancelled) {
          setError(message)
          toast.error('Analytics unavailable', message)
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps -- toast identity is stable per ToastProvider; including it would re-run this effect on every render
    })()
    return () => {
      cancelled = true
    }
  }, [])

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-6">
          <h1 className="text-xl font-semibold text-foreground">Operational Analytics</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            A current operational overview, computed live from existing DSO data -- not a separate metrics store.
          </p>
        </header>

        {loading && (
          <Card data-testid="analytics-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <Skeleton className="h-4 w-48" />
                <Skeleton className="h-4 w-16" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div
            data-testid="analytics-error"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            {error}
          </div>
        )}

        {!loading && !error && data && (
          <>
            {/* Summary cards */}
            <div className="mb-6 grid grid-cols-2 gap-4 sm:grid-cols-4">
              <Card className="px-5 py-4">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Managed Secrets</p>
                <p className="mt-1 text-2xl font-semibold text-foreground">
                  <StatValue value={data.current.managed_secrets} />
                </p>
              </Card>
              <Card className="px-5 py-4">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Containers</p>
                <p className="mt-1 text-2xl font-semibold text-foreground">
                  <StatValue value={data.current.containers_targeted} />
                </p>
              </Card>
              <Card className="px-5 py-4">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Drifted</p>
                <p className="mt-1 text-2xl font-semibold text-red-400">
                  <StatValue value={data.current.drifted} />
                </p>
              </Card>
              <Card className="px-5 py-4">
                <p className="text-xs uppercase tracking-wide text-muted-foreground">Degraded</p>
                <p className="mt-1 text-2xl font-semibold text-red-400">
                  <StatValue value={data.current.degraded} />
                </p>
              </Card>
            </div>

            {/* Operational activity / counters */}
            <Card className="mb-6 overflow-hidden">
              <div className="flex items-center justify-between border-b border-border px-5 py-3">
                <h2 className="text-sm font-semibold text-foreground">Operational Activity</h2>
                <Badge variant="secondary" data-testid="analytics-since-restart-label">
                  Since last restart
                </Badge>
              </div>
              <div className="grid grid-cols-2 gap-4 px-5 py-4 sm:grid-cols-4">
                <div>
                  <p className="text-xs text-muted-foreground">Successful rotations</p>
                  <p className="text-lg font-semibold text-emerald-400">{data.since_restart.rotation_success_total}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Failed rotations</p>
                  <p className="text-lg font-semibold text-red-400">{data.since_restart.rotation_failure_total}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Injection successes</p>
                  <p className="text-lg font-semibold text-emerald-400">{data.since_restart.injection_success_total}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Injection failures</p>
                  <p className="text-lg font-semibold text-red-400">{data.since_restart.injection_failure_total}</p>
                </div>
              </div>
              <div className="flex items-start gap-2 border-t border-border px-5 py-2.5 text-xs text-muted-foreground">
                <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
                <p>{data.since_restart.note}</p>
              </div>
            </Card>

            {/* Recent failures */}
            <Card className="mb-6 overflow-hidden">
              <div className="border-b border-border px-5 py-3">
                <h2 className="text-sm font-semibold text-foreground">Recent Failures</h2>
              </div>
              {data.recent_activity.recent_failures.length === 0 ? (
                <p className="px-5 py-6 text-center text-sm text-muted-foreground">No recent failures.</p>
              ) : (
                <div data-testid="analytics-recent-failures">
                  {data.recent_activity.recent_failures.map((ev, idx) => (
                    <EventRow key={idx} ev={ev} />
                  ))}
                </div>
              )}
            </Card>

            {/* Degraded services */}
            <Card className="overflow-hidden">
              <div className="border-b border-border px-5 py-3">
                <h2 className="text-sm font-semibold text-foreground">Degraded Services</h2>
              </div>
              {data.degraded_services.length === 0 ? (
                <EmptyState
                  data-testid="analytics-no-degraded"
                  icon={ShieldCheck}
                  title="No degraded services"
                  description="Every tracked service completed its last rotation successfully."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="pl-5">Service / Container</TableHead>
                      <TableHead className="pr-5">Reason</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.degraded_services.map((d) => (
                      <TableRow key={d.service}>
                        <TableCell className="pl-5">
                          <div className="flex items-center gap-2 text-foreground">
                            <ShieldAlert className="h-4 w-4 text-red-400" />
                            {d.service}
                          </div>
                        </TableCell>
                        <TableCell className="pr-5 text-xs text-muted-foreground">{d.reason}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </Card>
          </>
        )}

        {!loading && !error && !data && (
          <Card>
            <EmptyState icon={BarChart3} title="No analytics available" description="Try refreshing the page." />
          </Card>
        )}
      </div>
    </div>
  )
}

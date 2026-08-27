'use client'

import { useEffect, useState } from 'react'
import { BellRing, ShieldAlert, ShieldCheck } from 'lucide-react'

import { fetchAlerts, type Alert } from '@/lib/api/alerts'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState } from '@/components/ui/empty-state'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { useToast } from '@/components/toast-provider'

const TYPE_LABEL: Record<string, string> = {
  rotation_failed: 'Rotation Failed',
  injection_failed: 'Injection Failed',
  service_degraded: 'Service Degraded',
}

function typeLabel(type: string): string {
  return TYPE_LABEL[type] ?? type
}

function formatTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

/**
 * Phase 4.1 Alerts: GET /api/alerts, a read-only view of
 * internal/alert.Evaluator's current state. Alerts are event-driven
 * (rotation_failed, injection_failed, service_degraded, and their
 * resolutions), deduplicated, and cooldown-gated server-side -- this page
 * never polls or re-evaluates anything, it only renders what the backend
 * already decided. A failure loading this page never touches rotation --
 * it's a pure read of already-computed state.
 */
export function AlertsContent() {
  const [alerts, setAlerts] = useState<Alert[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const toast = useToast()

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetchAlerts()
        if (!cancelled) setAlerts(res.alerts)
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Failed to load alerts'
        if (!cancelled) {
          setError(message)
          toast.error('Alerts unavailable', message)
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

  const active = (alerts ?? []).filter((a) => a.status === 'firing')
  const recent = (alerts ?? [])
    .filter((a) => a.status !== 'firing')
    .sort((a, b) => new Date(b.last_triggered_at).getTime() - new Date(a.last_triggered_at).getTime())

  return (
    <div className="px-8 py-8">
      <div className="mx-auto max-w-5xl">
        <header className="mb-6">
          <h1 className="text-xl font-semibold text-foreground">Alerts</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            Deduplicated, cooldown-gated notifications for rotation failures, injection failures, and degraded
            services.
          </p>
        </header>

        {loading && (
          <Card data-testid="alerts-loading" className="divide-y divide-border overflow-hidden">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-5 py-3">
                <Skeleton className="h-4 w-64" />
                <Skeleton className="h-4 w-16" />
              </div>
            ))}
          </Card>
        )}

        {!loading && error && (
          <div
            data-testid="alerts-error"
            className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 text-sm text-red-400"
          >
            {error}
          </div>
        )}

        {!loading && !error && alerts && (
          <>
            <Card className="mb-6 overflow-hidden">
              <div className="flex items-center justify-between border-b border-border px-5 py-3">
                <h2 className="text-sm font-semibold text-foreground">Active Alerts</h2>
                <Badge variant={active.length > 0 ? 'destructive' : 'secondary'}>{active.length}</Badge>
              </div>
              {active.length === 0 ? (
                <EmptyState
                  data-testid="alerts-no-active"
                  icon={ShieldCheck}
                  title="No active alerts"
                  description="Every tracked secret rotation, injection, and container is currently healthy."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="pl-5">Type</TableHead>
                      <TableHead>Resource</TableHead>
                      <TableHead>Message</TableHead>
                      <TableHead>First Triggered</TableHead>
                      <TableHead className="pr-5">Last Triggered</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {active.map((a) => (
                      <TableRow key={a.id}>
                        <TableCell className="pl-5">
                          <div className="flex items-center gap-2 text-foreground">
                            <ShieldAlert className="h-4 w-4 text-red-400" />
                            {typeLabel(a.type)}
                          </div>
                        </TableCell>
                        <TableCell className="font-mono text-xs text-foreground">{a.resource}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">{a.message}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {formatTime(a.first_triggered_at)}
                        </TableCell>
                        <TableCell className="pr-5 text-xs text-muted-foreground">
                          {formatTime(a.last_triggered_at)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </Card>

            <Card className="overflow-hidden">
              <div className="border-b border-border px-5 py-3">
                <h2 className="text-sm font-semibold text-foreground">Recent / Resolved</h2>
              </div>
              {recent.length === 0 ? (
                <EmptyState
                  data-testid="alerts-no-recent"
                  icon={BellRing}
                  title="No recent alerts"
                  description="Resolved alerts will appear here."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="pl-5">Type</TableHead>
                      <TableHead>Resource</TableHead>
                      <TableHead>Message</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="pr-5">Last Triggered</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recent.map((a) => (
                      <TableRow key={a.id}>
                        <TableCell className="pl-5 text-foreground">{typeLabel(a.type)}</TableCell>
                        <TableCell className="font-mono text-xs text-foreground">{a.resource}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">{a.message}</TableCell>
                        <TableCell>
                          <Badge variant="success">Resolved</Badge>
                        </TableCell>
                        <TableCell className="pr-5 text-xs text-muted-foreground">
                          {formatTime(a.last_triggered_at)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </Card>
          </>
        )}
      </div>
    </div>
  )
}

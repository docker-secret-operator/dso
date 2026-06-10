'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, Download, TrendingUp, AlertTriangle, Clock, Activity } from 'lucide-react'
import { apiClient, MetricsPoint, TrendInfo } from '@/lib/api-client'
import { LineChart } from '@/components/charts/line-chart'
import { useVisibleInterval } from '@/hooks/useVisibilityRefetch'
import { useWebSocketContext } from '@/contexts/websocket-context'

type Period = '1h' | '24h' | '7d' | '30d'
type Granularity = '1m' | '5m' | '1h'

const PERIOD_GRANULARITY: Record<Period, Granularity> = {
  '1h':  '1m',
  '24h': '5m',
  '7d':  '1h',
  '30d': '1h',
}

function pct(v: number) { return `${(v * 100).toFixed(1)}%` }
function mb(v: number)  { return `${v.toFixed(1)} MB` }
function num(v: number) { return v.toFixed(0) }

function toPoints(data: MetricsPoint[], key: keyof MetricsPoint) {
  return data.map(p => ({ x: p.ts, y: p[key] as number }))
}

function statusColor(status: string) {
  switch (status) {
    case 'critical': return 'text-red-600 bg-red-50 border-red-200'
    case 'warning':  return 'text-yellow-700 bg-yellow-50 border-yellow-200'
    default:         return 'text-green-700 bg-green-50 border-green-200'
  }
}

function TrendBadge({ trend }: { trend: TrendInfo }) {
  const color =
    trend.direction === 'improving' ? 'text-green-600' :
    trend.direction === 'degrading' ? 'text-red-600' : 'text-muted-foreground'
  return (
    <span className={`text-sm font-bold ${color}`} title={`Slope: ${trend.slope}`}>
      {trend.arrow} <span className="text-xs font-normal capitalize">{trend.direction}</span>
    </span>
  )
}

function ChartCard({
  title,
  data,
  color,
  formatY,
  trend,
}: {
  title: string
  data: { x: number; y: number }[]
  color: string
  formatY: (v: number) => string
  trend?: TrendInfo
}) {
  return (
    <div className="rounded-lg border border-border bg-card p-4 space-y-2">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium">{title}</h3>
        {trend && <TrendBadge trend={trend} />}
      </div>
      <LineChart data={data} color={color} height={72} formatY={formatY} />
    </div>
  )
}

export default function AnalyticsPage() {
  const [period, setPeriod] = useState<Period>('24h')
  const { events: wsEvents } = useWebSocketContext()
  const refetchInterval = useVisibleInterval(60000)

  const granularity = PERIOD_GRANULARITY[period]

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['metrics', 'history', period, granularity],
    queryFn: () => apiClient.getMetricsHistory({ period, granularity }),
    refetchInterval,
  })

  const pts = data?.data ?? []
  const trends = data?.trends
  const forecast = data?.forecast
  const anomalies = data?.anomalies ?? []

  // FG7: check live WebSocket events for new anomaly signals
  const recentErrors = wsEvents.filter(e => e.severity === 'error').length

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Analytics</h1>
          <p className="text-sm text-muted-foreground mt-1">Historical metrics and operational intelligence</p>
        </div>
        <div className="flex gap-2 items-center">
          <a
            href={apiClient.getMetricsExportURL(period, 'csv')}
            download
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted"
          >
            <Download className="h-4 w-4" />
            Export CSV
          </a>
          <a
            href={apiClient.getMetricsExportURL(period, 'json')}
            download
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted"
          >
            <Download className="h-4 w-4" />
            Export JSON
          </a>
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      {/* Period selector */}
      <div className="flex gap-1 rounded-md border border-border w-fit overflow-hidden">
        {(['1h', '24h', '7d', '30d'] as Period[]).map(p => (
          <button
            key={p}
            onClick={() => setPeriod(p)}
            className={`px-4 py-2 text-sm font-medium transition-colors ${
              period === p ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
            }`}
          >
            {p}
          </button>
        ))}
      </div>

      {/* FG6: Capacity Forecast */}
      {forecast && (
        <div className="grid grid-cols-2 gap-4">
          <div className={`rounded-lg border p-4 ${statusColor(forecast.queue_status)}`}>
            <div className="flex items-center gap-2 mb-1">
              <Clock className="h-4 w-4" />
              <span className="text-sm font-semibold">Queue Capacity</span>
              <span className={`ml-auto text-xs font-bold uppercase px-1.5 py-0.5 rounded border ${statusColor(forecast.queue_status)}`}>
                {forecast.queue_status}
              </span>
            </div>
            <p className="text-xs mt-1">
              {forecast.queue_saturation_hours > 0
                ? `Saturation in ~${forecast.queue_saturation_hours.toFixed(1)}h at current growth rate`
                : 'No saturation predicted'}
            </p>
          </div>
          <div className={`rounded-lg border p-4 ${statusColor(forecast.worker_status)}`}>
            <div className="flex items-center gap-2 mb-1">
              <Activity className="h-4 w-4" />
              <span className="text-sm font-semibold">Worker Capacity</span>
              <span className={`ml-auto text-xs font-bold uppercase px-1.5 py-0.5 rounded border ${statusColor(forecast.worker_status)}`}>
                {forecast.worker_status}
              </span>
            </div>
            <p className="text-xs mt-1">
              {forecast.worker_exhaustion_hours > 0
                ? `Exhaustion in ~${forecast.worker_exhaustion_hours.toFixed(1)}h at current growth rate`
                : 'No exhaustion predicted'}
            </p>
          </div>
        </div>
      )}

      {/* FG7+FG8: Anomalies */}
      {(anomalies.length > 0 || recentErrors > 0) && (
        <div className="rounded-lg border border-yellow-200 bg-yellow-50 p-4 space-y-2">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 text-yellow-600" />
            <span className="text-sm font-semibold text-yellow-800">
              Anomalies Detected ({anomalies.length + (recentErrors > 0 ? 1 : 0)})
            </span>
          </div>
          <ul className="space-y-1">
            {anomalies.map((a, i) => (
              <li key={i} className="text-xs text-yellow-800 flex items-start gap-2">
                <span className="flex-shrink-0 font-mono font-semibold uppercase text-yellow-700">
                  {a.field}
                </span>
                <span>{a.message} — value {a.value.toFixed(3)} (baseline {a.baseline.toFixed(3)}, Δ {a.deviation > 0 ? '+' : ''}{a.deviation.toFixed(3)})</span>
              </li>
            ))}
            {recentErrors > 0 && (
              <li className="text-xs text-yellow-800">
                <span className="font-mono font-semibold uppercase text-yellow-700 mr-2">live</span>
                {recentErrors} real-time error event{recentErrors !== 1 ? 's' : ''} detected via WebSocket
              </li>
            )}
          </ul>
        </div>
      )}

      {/* FG4+FG5: Charts */}
      {isLoading ? (
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <div key={i} className="rounded-lg border border-border bg-card p-4 h-32 animate-pulse bg-muted/30" />
          ))}
        </div>
      ) : pts.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <TrendingUp className="h-12 w-12 text-muted-foreground/30 mb-3" />
          <p className="text-muted-foreground">No metrics data for this period yet.</p>
          <p className="text-xs text-muted-foreground mt-1">Snapshots are collected every 60 seconds.</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <ChartCard
            title="Success Rate"
            data={toPoints(pts, 'sr')}
            color="#22c55e"
            formatY={pct}
            trend={trends?.success_rate}
          />
          <ChartCard
            title="Failure Rate"
            data={toPoints(pts, 'fr')}
            color="#ef4444"
            formatY={pct}
            trend={trends?.failure_rate}
          />
          <ChartCard
            title="Throughput (ops/s)"
            data={toPoints(pts, 'tp')}
            color="#3b82f6"
            formatY={(v) => v.toFixed(3)}
            trend={trends?.queue_depth}
          />
          <ChartCard
            title="Queue Depth"
            data={toPoints(pts, 'qd')}
            color="#f59e0b"
            formatY={num}
            trend={trends?.queue_depth}
          />
          <ChartCard
            title="Worker Utilization"
            data={toPoints(pts, 'wu')}
            color="#8b5cf6"
            formatY={pct}
            trend={trends?.worker_utilization}
          />
          <ChartCard
            title="Memory Usage"
            data={toPoints(pts, 'mm')}
            color="#06b6d4"
            formatY={mb}
            trend={trends?.memory_mb}
          />
          <ChartCard
            title="Active Executions"
            data={toPoints(pts, 'ae')}
            color="#f97316"
            formatY={num}
          />
          <ChartCard
            title="Goroutines"
            data={toPoints(pts, 'gr')}
            color="#64748b"
            formatY={num}
          />
        </div>
      )}

      {/* FG8: Alert-metrics correlation hint */}
      {anomalies.length > 0 && (
        <div className="rounded-lg border border-border bg-muted/20 p-4 text-sm text-muted-foreground">
          <p className="font-medium text-foreground mb-1">Correlation context</p>
          {anomalies.map((a, i) => (
            <p key={i} className="text-xs">
              {a.field === 'queue_depth' && 'Queue depth increased before execution failures — check worker capacity.'}
              {a.field === 'failure_rate' && 'Error rate spike correlated with recent rotation events.'}
              {a.field === 'memory_mb' && 'Memory growth detected — consider reviewing active execution count.'}
              {a.field === 'worker_utilization' && 'Worker saturation may be causing queue build-up.'}
            </p>
          ))}
        </div>
      )}

      {/* Summary stats */}
      {pts.length > 0 && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 text-sm">
          {[
            { label: 'Avg Success Rate', value: pct(pts.reduce((a, p) => a + p.sr, 0) / pts.length) },
            { label: 'Avg Queue Depth', value: num(pts.reduce((a, p) => a + p.qd, 0) / pts.length) },
            { label: 'Peak Memory', value: mb(Math.max(...pts.map(p => p.mm))) },
            { label: 'Data Points', value: String(pts.length) },
          ].map(({ label, value }) => (
            <div key={label} className="rounded-lg border border-border bg-card p-3 text-center">
              <p className="text-xs text-muted-foreground">{label}</p>
              <p className="text-lg font-semibold mt-0.5 tabular-nums">{value}</p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

'use client'

import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { Logo } from '@/components/Logo'
import { cn } from '@/lib/utils'
import { ErrorBoundary } from '@/components/error-boundary'
import {
  Card,
  Badge,
  MetricCard,
  StatusIndicator,
  StatusBadge,
  EmptyState,
  Skeleton,
  PageHeader,
} from '@/components/ui-modern'
import {
  AlertCircle, Shield, Server, GitBranch, Bot,
  BarChart3, Lightbulb, ChevronRight, Zap,
  TrendingUp, TrendingDown, Activity, Cpu
} from 'lucide-react'
import * as systemApi from '@/lib/api/system'
import * as operationsApi from '@/lib/api/operations'
import * as metricsApi from '@/lib/api/metrics'
import * as auditApi from '@/lib/api/audit'
import * as dashboardApi from '@/lib/api/dashboard'

// ── Tiny sparkline using SVG ─────────────────────────────────────────────────

function Sparkline({ data, color = '#6366f1' }: { data: number[]; color?: string }) {
  if (!data || data.length < 2) return null
  const max = Math.max(...data)
  const min = Math.min(...data)
  const range = max - min || 1
  const W = 80, H = 28
  const pts = data.map((v, i) => {
    const x = (i / (data.length - 1)) * W
    const y = H - ((v - min) / range) * H
    return `${x},${y}`
  }).join(' ')

  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} className="overflow-visible">
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

// ── Mini progress bar ─────────────────────────────────────────────────────────

function MiniBar({ value, max = 100, color }: { value: number; max?: number; color: string }) {
  const pct = Math.min((value / max) * 100, 100)
  return (
    <div className="h-1 rounded-full bg-white/[0.06] overflow-hidden">
      <div className="h-full rounded-full transition-all duration-300" style={{ width: `${pct}%`, backgroundColor: color }} />
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────

function DashboardHero({ health, version }: { health: any, version: string }) {
  const isUp = health?.status === 'up'
  return (
    <div className="relative overflow-hidden rounded-2xl glass-panel p-8 mb-6 border border-indigo-500/20 shadow-glow-indigo">
      <div className="absolute inset-0 bg-mesh opacity-40 animate-mesh-gradient"></div>
      <div className="relative z-10 flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div className="flex items-start gap-4">
          <Logo href="/" size="lg" showText={false} className="flex-shrink-0 mt-1" />
          <div>
            <h1 className="text-[28px] font-semibold text-[#F9FAFB] tracking-tight mb-2">
              Operations <span className="gradient-text">Command Center</span>
            </h1>
            <p className="text-[14px] font-normal text-[#9CA3AF] max-w-xl">
              Real-time overview of system health, secrets, and autonomous operational status.
            </p>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-3 px-4 py-2 rounded-xl bg-black/40 border border-white/10 backdrop-blur-md">
            <div className="flex flex-col items-end">
              <span className="text-xs text-slate-400 font-medium">System Status</span>
              <span className={cn("text-sm font-semibold", isUp ? "text-emerald-400" : "text-red-400")}>
                {isUp ? 'Operational' : 'Degraded'}
              </span>
            </div>
            <span className={cn("w-3 h-3 rounded-full", isUp ? "bg-emerald-500 animate-pulse shadow-[0_0_12px_rgba(16,185,129,0.8)]" : "bg-red-500 animate-pulse shadow-[0_0_12px_rgba(239,68,68,0.8)]")} />
          </div>
        </div>
      </div>
    </div>
  )
}

function AgentUtilizationPanel({ goroutines, memoryMB, activeExec }: { goroutines: number, memoryMB: number, activeExec: number }) {
  // Use goroutines as a proxy for load (assuming baseline ~15, max ~200 for our simple UI scale)
  const loadPct = Math.round(Math.min((goroutines / 200) * 100, 100))
  const memPct = Math.round(Math.min((memoryMB / 2048) * 100, 100)) // Assuming 2GB max for visual gauge

  return (
    <div className="glass-panel p-6 rounded-2xl mb-6 flex flex-col md:flex-row items-center gap-8 border border-white/10 hover:border-indigo-500/30 transition-colors duration-300">
      <div className="flex items-center gap-4 w-full md:w-auto md:min-w-[200px]">
        <div className="w-12 h-12 rounded-xl bg-indigo-500/10 flex items-center justify-center border border-indigo-500/20 text-indigo-400">
          <Cpu className="w-6 h-6" />
        </div>
        <div>
          <h2 className="text-[14px] font-semibold text-[#F3F4F6]">DSO Agent Load</h2>
          <p className="text-[12px] font-normal text-[#9CA3AF]">Real-time utilization</p>
        </div>
      </div>

      <div className="flex-1 grid grid-cols-2 md:grid-cols-3 gap-6 w-full">
        {/* Load (Goroutines proxy) */}
        <div className="flex flex-col">
          <div className="flex justify-between items-end mb-2">
            <span className="text-[11px] font-semibold text-[#6B7280] uppercase tracking-wider">Agent Load</span>
            <span className={cn("text-[18px] font-semibold tabular-nums", loadPct > 85 ? "text-red-400" : loadPct > 60 ? "text-amber-400" : "text-emerald-400")}>
              {loadPct}%
            </span>
          </div>
          <div className="h-2 w-full bg-black/40 rounded-full overflow-hidden border border-white/5">
            <div 
              className={cn("h-full rounded-full transition-all duration-1000", loadPct > 85 ? "bg-red-500" : loadPct > 60 ? "bg-amber-500" : "bg-emerald-500")} 
              style={{ width: `${loadPct}%` }} 
            />
          </div>
        </div>

        {/* Memory */}
        <div className="flex flex-col">
          <div className="flex justify-between items-end mb-2">
            <span className="text-[11px] font-semibold text-[#6B7280] uppercase tracking-wider">Memory</span>
            <span className={cn("text-[18px] font-semibold tabular-nums", memoryMB > 1500 ? "text-red-400" : memoryMB > 1000 ? "text-amber-400" : "text-blue-400")}>
              {Math.round(memoryMB)} MB
            </span>
          </div>
          <div className="h-2 w-full bg-black/40 rounded-full overflow-hidden border border-white/5">
            <div 
              className={cn("h-full rounded-full transition-all duration-1000", memoryMB > 1500 ? "bg-red-500" : memoryMB > 1000 ? "bg-amber-500" : "bg-blue-500")} 
              style={{ width: `${memPct}%` }} 
            />
          </div>
        </div>

        {/* Active Goroutines */}
        <div className="col-span-2 md:col-span-1 flex flex-col md:items-end justify-center">
           <span className="text-[11px] font-semibold text-[#6B7280] uppercase tracking-wider mb-1">Active Routines</span>
           <span className="text-[28px] font-semibold text-[#F9FAFB] tabular-nums">{goroutines}</span>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────

function DashboardContent() {
  const router = useRouter()

  // ── Data fetching ────────────────────────────────────────────────────────

  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => systemApi.getHealth(),
    refetchInterval: 30000, // Poll every 30 seconds
  })

  const { data: opsData, isLoading: opsLoading } = useQuery({
    queryKey: ['operations-dashboard'],
    queryFn: () => operationsApi.getOperationsDashboard(),
    refetchInterval: 30000,
  })

  const { data: alerts, isLoading: alertsLoading } = useQuery({
    queryKey: ['alerts-dashboard'],
    queryFn: () => operationsApi.getAlerts({ limit: 5 }),
    refetchInterval: 30000,
  })

  const { data: metricsHistory } = useQuery({
    queryKey: ['metricsHistory'],
    queryFn: () => metricsApi.getHistory({ period: '1h', granularity: '1m' }),
    refetchInterval: 60000,
  })

  const { data: recentAudit } = useQuery({
    queryKey: ['audit-recent'],
    queryFn: () => auditApi.getAuditEvents({ limit: 5 }),
    refetchInterval: 30000,
  })

  const { data: dashboardMetrics } = useQuery({
    queryKey: ['dashboard-metrics'],
    queryFn: () => dashboardApi.getMetrics(),
    refetchInterval: 60000,
  })

  // ── Derived values — all from real API ──────────────────────────────────

  const isUp         = health?.status === 'up'
  const histPoints   = metricsHistory?.data ?? []
  const queueData    = histPoints.map((p: any) => p.qd ?? 0)
  const workerData   = histPoints.map((p: any) => Math.round((p.wu ?? 0) * 100))
  const memData      = histPoints.map((p: any) => p.mm ?? 0)
  const lastPoint    = histPoints[histPoints.length - 1] ?? {}
  const queueDepth   = lastPoint.qd ?? 0
  const workerUtil   = lastPoint.wu ?? 0
  const memoryMB     = lastPoint.mm ?? 0
  const activeExec   = lastPoint.ae ?? 0

  const activeAlerts = (alerts as any)?.alerts ?? []
  const totalAlerts  = (alerts as any)?.count ?? 0

  const rtGoroutines = health?.goroutines ?? 0
  const rtMemoryMB = health?.memory_mb ?? 0

  // Operations dashboard data
  const kpis = opsData?.overview_kpis
  const queueHealth = opsData?.queue_health
  const workerHealth = opsData?.worker_health
  const executionStatus = opsData?.execution_status

  return (
    <ErrorBoundary>
      <div className="relative min-h-[calc(100vh-4rem)]">
        <div className="absolute inset-0 bg-dashboard-orbs z-0 pointer-events-none"></div>
        <div className="p-6 space-y-4 max-w-[1400px] relative z-10">

        {/* ── Page hero & Utilization ── */}
        <DashboardHero health={health} version={health?.version ?? '—'} />

        {health && (
          <AgentUtilizationPanel goroutines={rtGoroutines} memoryMB={rtMemoryMB} activeExec={activeExec} />
        )}

        <div className="h-2" /> {/* Spacing */}

        {/* ── System health strip ── */}
        <Card className="p-4">
          <div className="flex items-center gap-6 flex-wrap">
            <div className="flex items-center gap-2">
              <span className="text-[11px] font-semibold text-[#6B7280] uppercase tracking-wider">System</span>
              <span className="text-[12px] font-normal text-[#9CA3AF]">{health?.status === 'up' ? 'Operational' : 'Degraded'}</span>
            </div>
            {health?.persistence && (
              <>
                <div className="flex items-center gap-1.5">
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>
                  <span className="text-[12px] font-normal text-[#9CA3AF]">Database: {health.persistence.driver}</span>
                </div>
                {health.persistence.wal_mode && (
                  <div className="text-[12px] font-normal text-[#9CA3AF]">WAL Enabled</div>
                )}
                <div className="text-[12px] font-normal text-[#9CA3AF]">Migration: v{health.persistence.migration_version}</div>
              </>
            )}
            {health?.uptime != null && (
              <div className="ml-auto text-[12px] font-normal text-[#9CA3AF]">
                Uptime: {Math.floor(health.uptime / 3600)}h {Math.floor((health.uptime % 3600) / 60)}m
              </div>
            )}
          </div>
        </Card>

        {/* ── KPI Cards from Operations Dashboard ── */}
        {opsLoading ? (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            {[...Array(4)].map((_, i) => (
              <Skeleton key={i} className="h-24 rounded-lg" />
            ))}
          </div>
        ) : kpis ? (
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <MetricCard
              label="Success Rate"
              value={`${Math.round(kpis.success_rate)}%`}
              sublabel="executions"
              icon={<TrendingUp className="w-4 h-4" />}
              accentColor="emerald"
            />
            <MetricCard
              label="Failure Rate"
              value={`${Math.round(kpis.failure_rate)}%`}
              sublabel="executions"
              icon={<TrendingDown className="w-4 h-4" />}
              accentColor={kpis.failure_rate > 10 ? 'red' : 'slate'}
            />
            <MetricCard
              label="Throughput"
              value={`${(kpis.throughput_per_second ?? 0).toFixed(1)}`}
              sublabel="executions/sec"
              icon={<Zap className="w-4 h-4" />}
              accentColor="blue"
            />
            <MetricCard
              label="Worker Util"
              value={`${Math.round(kpis.worker_utilization)}%`}
              sublabel="average"
              icon={<Cpu className="w-4 h-4" />}
              accentColor={kpis.worker_utilization > 85 ? 'red' : 'blue'}
            />
          </div>
        ) : null}

        {/* ── Queue & Worker Health ── */}
        {opsLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {[...Array(2)].map((_, i) => (
              <Skeleton key={i} className="h-32 rounded-lg" />
            ))}
          </div>
        ) : queueHealth || workerHealth ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Queue Health */}
            {queueHealth && (
              <Card className="p-5">
                <h3 className="text-[18px] font-semibold text-[#F3F4F6] mb-4">Queue Health</h3>
                <div className="space-y-3">
                  <div>
                    <div className="flex justify-between mb-1">
                      <span className="text-[12px] font-normal text-[#9CA3AF]">Depth</span>
                      <span className="text-[14px] font-semibold text-[#F9FAFB]">{queueHealth.depth}</span>
                    </div>
                    <MiniBar value={queueHealth.depth} max={100} color={queueHealth.health_score > 75 ? '#10b981' : queueHealth.health_score > 50 ? '#f59e0b' : '#ef4444'} />
                  </div>
                  <div>
                    <span className="text-[12px] font-normal text-[#9CA3AF]">Health Score: </span>
                    <span className={cn('text-[12px] font-semibold', queueHealth.status === 'healthy' ? 'text-emerald-400' : queueHealth.status === 'warning' ? 'text-amber-400' : 'text-red-400')}>
                      {queueHealth.health_score} {queueHealth.status}
                    </span>
                  </div>
                  <div className="text-[12px] font-normal text-[#9CA3AF]">Completion: {(queueHealth.completion_rate ?? 0).toFixed(1)}/s</div>
                </div>
              </Card>
            )}

            {/* Worker Health */}
            {workerHealth && (
              <Card className="p-5">
                <h3 className="text-[18px] font-semibold text-[#F3F4F6] mb-4">Worker Health</h3>
                <div className="space-y-3">
                  <div>
                    <div className="flex justify-between mb-1">
                      <span className="text-[12px] font-normal text-[#9CA3AF]">Workers</span>
                      <span className="text-[14px] font-semibold text-[#F9FAFB]">{workerHealth.healthy_workers}/{workerHealth.total_workers}</span>
                    </div>
                    <MiniBar value={workerHealth.healthy_workers} max={workerHealth.total_workers} color={workerHealth.health_score > 75 ? '#10b981' : '#f59e0b'} />
                  </div>
                  <div>
                    <span className="text-[12px] font-normal text-[#9CA3AF]">Utilization: </span>
                    <span className="text-[14px] font-semibold text-[#F9FAFB]">{Math.round(workerHealth.avg_utilization)}%</span>
                  </div>
                  <div className="text-[12px] font-normal text-[#9CA3AF]">{workerHealth.unhealthy_workers} unhealthy</div>
                </div>
              </Card>
            )}
          </div>
        ) : null}

        {/* ── Execution Status Distribution ── */}
        {opsLoading ? (
          <Skeleton className="h-24 rounded-lg" />
        ) : executionStatus ? (
          <Card className="p-5">
            <h3 className="text-[18px] font-semibold text-[#F3F4F6] mb-4">Execution Status</h3>
            <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-7 gap-3">
              {[
                { label: 'Queued', value: executionStatus.queued, color: '#3b82f6' },
                { label: 'Running', value: executionStatus.running, color: '#10b981' },
                { label: 'Completed', value: executionStatus.completed, color: '#6366f1' },
                { label: 'Failed', value: executionStatus.failed, color: '#ef4444' },
                { label: 'Cancelled', value: executionStatus.cancelled, color: '#f59e0b' },
                { label: 'Paused', value: executionStatus.paused, color: '#8b5cf6' },
                { label: 'Timed Out', value: executionStatus.timed_out, color: '#ec4899' },
              ].map(status => (
                <div key={status.label} className="text-center">
                  <div className="text-[22px] font-semibold text-[#F9FAFB] mb-1">{status.value}</div>
                  <div className="text-[11px] font-normal text-[#9CA3AF]">{status.label}</div>
                </div>
              ))}
            </div>
          </Card>
        ) : null}

        {/* ── Bottom grid: alerts + recent audit ── */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">

          {/* Active alerts */}
          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-[18px] font-semibold text-[#F3F4F6]">Active Alerts</h2>
              <button
                onClick={() => router.push('/alerts')}
                className="text-[12px] font-normal text-indigo-400 hover:text-indigo-300 transition-colors flex items-center gap-1"
              >
                {totalAlerts > 0 && <span className="tabular-nums">{totalAlerts}</span>}
                <ChevronRight className="w-3.5 h-3.5" />
              </button>
            </div>

            {alertsLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-10 w-full rounded" count={3} />
              </div>
            ) : activeAlerts.length === 0 ? (
              <EmptyState
                icon={<AlertCircle className="w-5 h-5" />}
                title="No active alerts"
                description="All systems are running normally."
              />
            ) : (
              <div className="space-y-2">
                {activeAlerts.slice(0, 5).map((alert: any, i: number) => (
                  <div key={i} className="flex items-start gap-3 p-2.5 rounded-lg hover:bg-white/[0.03] transition-colors">
                    <span className={`w-1.5 h-1.5 rounded-full mt-1.5 flex-shrink-0 ${
                      alert.severity === 'critical' ? 'bg-red-400' :
                      alert.severity === 'high'     ? 'bg-orange-400' :
                      alert.severity === 'warning'  ? 'bg-amber-400' : 'bg-blue-400'
                    }`} />
                    <div className="flex-1 min-w-0">
                      <p className="text-[13px] font-medium text-[#F3F4F6] truncate">{alert.message ?? 'Alert'}</p>
                      <p className="text-[11px] font-normal text-[#9CA3AF] mt-0.5">{alert.type}</p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>

          {/* Recent audit */}
          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-[18px] font-semibold text-[#F3F4F6]">Recent Activity</h2>
              <button
                onClick={() => router.push('/audit')}
                className="text-[12px] font-normal text-indigo-400 hover:text-indigo-300 transition-colors"
              >
                <ChevronRight className="w-3.5 h-3.5" />
              </button>
            </div>

            {!recentAudit ? (
              <div className="space-y-2">
                <Skeleton className="h-10 w-full rounded" count={3} />
              </div>
            ) : recentAudit.events.length === 0 ? (
              <EmptyState icon={<Activity className="w-5 h-5" />} title="No recent activity" />
            ) : (
              <div className="space-y-1">
                {recentAudit.events.slice(0, 5).map((e: any, i: number) => (
                  <div key={i} className="flex items-center gap-2.5 py-2 border-b border-white/[0.04] last:border-0">
                    <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                      e.severity === 'error' ? 'bg-red-400' :
                      e.severity === 'warning' ? 'bg-amber-400' : 'bg-blue-400'
                    }`} />
                    <span className="text-[12px] font-normal text-[#9CA3AF] truncate flex-1">{e.action}</span>
                    <span className="text-[11px] font-normal text-[#6B7280] flex-shrink-0">
                      {new Date(e.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>

      </div>
      </div>
    </ErrorBoundary>
  )
}

export default function DashboardPage() {
  return (
    <ProtectedRoute>
      <DashboardContent />
    </ProtectedRoute>
  )
}

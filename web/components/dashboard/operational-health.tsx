'use client'

import { cn } from '@/lib/utils'
import type { OperationsDashboard } from '@/lib/api/types'
import { Users, ListChecks, Activity, CheckCheck, Timer } from 'lucide-react'

interface OperationalHealthProps {
  data?: OperationsDashboard
}

type Tone = 'healthy' | 'warning' | 'critical' | 'neutral'

const toneClass: Record<Tone, string> = {
  healthy: 'text-emerald-400',
  warning: 'text-amber-400',
  critical: 'text-red-400',
  neutral: 'text-slate-100',
}

function statusTone(status?: 'healthy' | 'warning' | 'critical'): Tone {
  return status ?? 'neutral'
}

function Metric({
  icon: Icon,
  label,
  value,
  sublabel,
  tone = 'neutral',
}: {
  icon: typeof Users
  label: string
  value: React.ReactNode
  sublabel?: string
  tone?: Tone
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="mt-0.5 flex items-center justify-center w-8 h-8 rounded-lg bg-white/[0.04] text-slate-400 flex-shrink-0">
        <Icon className="w-4 h-4" />
      </span>
      <div className="min-w-0">
        <p className="text-[11px] font-medium uppercase tracking-wider text-slate-500">{label}</p>
        <p className={cn('mt-0.5 font-mono text-lg leading-tight font-semibold tabular-nums', toneClass[tone])}>
          {value}
        </p>
        {sublabel && <p className="text-xs text-slate-600 truncate">{sublabel}</p>}
      </div>
    </div>
  )
}

/**
 * Operational health in operator language — no goroutines, no "agent load".
 * Everything below maps to real OperationsDashboard fields.
 */
export function OperationalHealth({ data }: OperationalHealthProps) {
  const kpis = data?.overview_kpis
  const queue = data?.queue_health
  const workers = data?.worker_health
  const exec = data?.execution_status

  const successRate = kpis ? Math.round(kpis.success_rate) : null
  const latencyMs = kpis ? Math.round(kpis.avg_execution_time_seconds * 1000) : null

  return (
    <div className="grid grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-5">
      <Metric
        icon={Users}
        label="Workers active"
        value={workers ? `${workers.healthy_workers}/${workers.total_workers}` : '—'}
        sublabel={workers ? `${Math.round(workers.avg_utilization)}% utilized` : undefined}
        tone={statusTone(workers?.status)}
      />
      <Metric
        icon={ListChecks}
        label="Queue status"
        value={queue ? `${queue.depth} queued` : '—'}
        sublabel={queue ? `${queue.status} · ${queue.incoming_rate.toFixed(1)}/s in` : undefined}
        tone={statusTone(queue?.status)}
      />
      <Metric
        icon={Activity}
        label="Tasks executing"
        value={exec ? exec.running : '—'}
        sublabel={exec ? `${exec.queued} queued · ${exec.completed} done` : undefined}
      />
      <Metric
        icon={CheckCheck}
        label="Success rate"
        value={successRate === null ? '—' : `${successRate}%`}
        sublabel={kpis ? `${kpis.totals.failed} failed of ${kpis.totals.executed}` : undefined}
        tone={successRate === null ? 'neutral' : successRate >= 95 ? 'healthy' : successRate >= 80 ? 'warning' : 'critical'}
      />
      <Metric
        icon={Timer}
        label="Processing latency"
        value={latencyMs === null ? '—' : latencyMs >= 1000 ? `${(latencyMs / 1000).toFixed(1)}s` : `${latencyMs}ms`}
        sublabel={queue ? `${Math.round(queue.avg_wait_time_seconds)}s avg wait` : undefined}
      />
      <Metric
        icon={Activity}
        label="Throughput"
        value={kpis ? `${kpis.throughput_per_second.toFixed(1)}/s` : '—'}
        sublabel="executions per second"
      />
    </div>
  )
}

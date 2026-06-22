'use client'

import { cn } from '@/lib/utils'
import { ShieldCheck, ShieldAlert, ShieldX, RefreshCw } from 'lucide-react'

export interface PostureSummaryProps {
  managedSecrets: number
  needRotation: number
  /** Breakdown for the rotation sublabel. */
  overdue: number
  aging: number
  drifted: number
  /** Coverage percentage (0–100), or null when discovery data is unavailable. */
  coverage: number | null
  lastSyncLabel?: string
  loading?: boolean
}

type Posture = 'secured' | 'attention' | 'critical'

function derivePosture(needRotation: number, drifted: number): Posture {
  if (drifted > 0) return 'critical'
  if (needRotation > 0) return 'attention'
  return 'secured'
}

const postureMeta: Record<Posture, { label: string; icon: typeof ShieldCheck; tone: string; ring: string }> = {
  secured: { label: 'Secrets secured', icon: ShieldCheck, tone: 'text-emerald-400', ring: 'bg-emerald-500/10' },
  attention: { label: 'Attention needed', icon: ShieldAlert, tone: 'text-amber-400', ring: 'bg-amber-500/10' },
  critical: { label: 'Action required', icon: ShieldX, tone: 'text-red-400', ring: 'bg-red-500/10' },
}

function Stat({
  label,
  value,
  sublabel,
  tone = 'text-slate-100',
}: {
  label: string
  value: React.ReactNode
  sublabel?: string
  tone?: string
}) {
  return (
    <div className="rounded-xl border border-white/[0.07] bg-[#111827] px-5 py-4">
      <p className="text-[11px] font-medium uppercase tracking-wider text-slate-400">{label}</p>
      <p className={cn('mt-1.5 font-mono text-[26px] leading-none font-semibold tabular-nums', tone)}>{value}</p>
      <p className="mt-2 text-xs text-slate-400 h-4">{sublabel ?? ''}</p>
    </div>
  )
}

export function PostureSummary(props: PostureSummaryProps) {
  const { managedSecrets, needRotation, overdue, aging, drifted, coverage, lastSyncLabel, loading } = props
  const posture = derivePosture(needRotation, drifted)
  const meta = postureMeta[posture]
  const Icon = meta.icon

  const attentionCount = needRotation + drifted

  return (
    <div>
      <div className="flex items-center gap-3 mb-4">
        <span className={cn('flex items-center justify-center w-9 h-9 rounded-lg', meta.ring)}>
          <Icon className={cn('w-5 h-5', meta.tone)} />
        </span>
        <div>
          <p className={cn('text-[15px] font-semibold', meta.tone)}>{meta.label}</p>
          <p className="flex items-center gap-1.5 text-xs text-slate-400">
            {loading
              ? 'Loading secret estate…'
              : attentionCount > 0
                ? `${attentionCount} item${attentionCount === 1 ? '' : 's'} need attention`
                : 'All secrets healthy'}
            {lastSyncLabel && (
              <>
                <span className="text-slate-700">·</span>
                <RefreshCw className="w-3 h-3" />
                {lastSyncLabel}
              </>
            )}
          </p>
        </div>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <Stat label="Managed secrets" value={managedSecrets.toLocaleString()} />
        <Stat
          label="Need rotation"
          value={needRotation}
          tone={needRotation > 0 ? 'text-amber-400' : 'text-slate-100'}
          sublabel={needRotation > 0 ? `${overdue} overdue · ${aging} due soon` : 'none due'}
        />
        <Stat
          label="Drifted"
          value={drifted}
          tone={drifted > 0 ? 'text-red-400' : 'text-slate-100'}
          sublabel={drifted > 0 ? 'differs from provider' : 'in sync'}
        />
        <Stat
          label="Coverage"
          value={coverage === null ? '—' : `${Math.round(coverage)}%`}
          sublabel={coverage === null ? 'discovery unavailable' : 'containers managed'}
        />
      </div>
    </div>
  )
}

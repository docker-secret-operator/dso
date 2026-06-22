'use client'

import { cn } from '@/lib/utils'
import { ShieldCheck, ShieldAlert, ShieldX, RefreshCw } from 'lucide-react'
import { ROTATION_BUCKET_META, type RotationPosture } from '@/lib/dashboard/rotation'

export interface EstateHeroProps {
  posture: RotationPosture
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

// Best → worst, left to right, so accumulating risk reads from the right edge.
const BAND_ORDER = [...ROTATION_BUCKET_META].reverse()

/**
 * Signature hero: the entire secret estate as one rotation-health band, with
 * the supporting numbers orbiting it. This is the page's thesis — the thing
 * unique to a secrets-management tool leads, instead of generic KPI cards.
 */
export function EstateHero({ posture, coverage, lastSyncLabel, loading }: EstateHeroProps) {
  const meta = postureMeta[derivePosture(posture.needRotation, posture.drifted)]
  const Icon = meta.icon
  const total = posture.total || 1
  const pct = (n: number) => Math.round((n / total) * 100)

  return (
    <section className="rounded-xl border border-white/[0.07] bg-[#111827] p-6">
      {/* Header: identity + status (left), managed count (right) */}
      <div className="flex items-start justify-between gap-4 mb-5">
        <div className="flex items-center gap-3">
          <span className={cn('flex items-center justify-center w-9 h-9 rounded-lg', meta.ring)}>
            <Icon className={cn('w-5 h-5', meta.tone)} />
          </span>
          <div>
            <h1 className="text-[15px] font-semibold text-slate-100 leading-tight">Secret estate</h1>
            <p className="flex items-center gap-1.5 text-xs text-slate-400">
              <span className={meta.tone}>{loading ? 'Loading secret estate…' : meta.label}</span>
              {lastSyncLabel && (
                <>
                  <span className="text-slate-600">·</span>
                  <RefreshCw className="w-3 h-3" />
                  {lastSyncLabel}
                </>
              )}
            </p>
          </div>
        </div>
        <div className="text-right">
          <p className="font-mono text-2xl leading-none font-semibold tabular-nums text-slate-100">
            {posture.total.toLocaleString()}
          </p>
          <p className="text-[11px] uppercase tracking-wider text-slate-400 mt-1">managed</p>
        </div>
      </div>

      {/* Signature: the rotation-health band */}
      {loading ? (
        <div className="h-6 w-full rounded-md bg-white/[0.06] animate-pulse" />
      ) : (
        <div
          className="flex h-6 w-full overflow-hidden rounded-md bg-white/[0.04] gap-px"
          role="img"
          aria-label={
            posture.total === 0
              ? 'No secrets to display'
              : `Rotation health across ${posture.total} secrets: ` +
                BAND_ORDER.map((s) => `${posture[s.key]} ${s.label.toLowerCase()}`).join(', ')
          }
        >
          {BAND_ORDER.map((seg) => {
            const count = posture[seg.key]
            if (count === 0) return null
            return (
              <div
                key={seg.key}
                className={cn(seg.fill, 'h-full')}
                style={{ width: `${(count / total) * 100}%`, minWidth: 4 }}
                title={`${seg.label}: ${count} (${pct(count)}%)`}
              />
            )
          })}
        </div>
      )}

      {/* Legend: buckets with counts + percentages */}
      <div className="mt-4 grid grid-cols-2 sm:grid-cols-4 gap-3">
        {ROTATION_BUCKET_META.map((seg) => {
          const count = posture[seg.key]
          return (
            <div key={seg.key} className="flex items-center gap-2 min-w-0">
              <span className={cn('w-2.5 h-2.5 rounded-sm flex-shrink-0', seg.fill)} />
              <span className="text-xs text-slate-400 truncate">{seg.label}</span>
              <span className="ml-auto font-mono text-xs tabular-nums text-slate-200">
                {loading ? '—' : count}
                {!loading && posture.total > 0 && (
                  <span className="text-slate-500"> · {pct(count)}%</span>
                )}
              </span>
            </div>
          )
        })}
      </div>

      {/* Footer meta: coverage + actionable rotation count */}
      <div className="mt-4 pt-4 border-t border-white/[0.06] flex items-center gap-4 text-xs text-slate-400">
        <span>
          <span className="font-mono tabular-nums text-slate-200">
            {coverage === null ? '—' : `${Math.round(coverage)}%`}
          </span>{' '}
          coverage
        </span>
        <span className="text-slate-600">·</span>
        <span>
          <span className={cn('font-mono tabular-nums', posture.needRotation > 0 ? 'text-amber-400' : 'text-slate-200')}>
            {posture.needRotation}
          </span>{' '}
          need rotation
        </span>
      </div>
    </section>
  )
}

'use client'

import { cn } from '@/lib/utils'
import { ROTATION_BUCKET_META, type RotationPosture } from '@/lib/dashboard/rotation'

interface RotationHealthStripProps {
  posture: RotationPosture
}

/**
 * Signature component: the entire secret estate as one freshness band.
 * Ordered best → worst (fresh on the left, drifted on the right) so the amount
 * of risk reads at a glance. Color encodes rotation health only.
 */
export function RotationHealthStrip({ posture }: RotationHealthStripProps) {
  const total = posture.total || 1
  // Render fresh → aging → overdue → drifted (reverse of the severity-first meta).
  const segments = [...ROTATION_BUCKET_META].reverse()

  const pct = (n: number) => (n / total) * 100

  return (
    <div>
      <div
        className="flex h-3.5 w-full overflow-hidden rounded-md bg-white/[0.04] gap-px"
        role="img"
        aria-label={
          posture.total === 0
            ? 'No secrets to display'
            : `Rotation health across ${posture.total} secrets: ` +
              segments.map((s) => `${posture[s.key]} ${s.label.toLowerCase()}`).join(', ')
        }
      >
        {segments.map((seg) => {
          const count = posture[seg.key]
          if (count === 0) return null
          return (
            <div
              key={seg.key}
              className={cn(seg.fill, 'h-full')}
              style={{ width: `${pct(count)}%`, minWidth: count > 0 ? 3 : 0 }}
              title={`${seg.label}: ${count} (${Math.round(pct(count))}%)`}
            />
          )
        })}
      </div>

      <div className="mt-3.5 grid grid-cols-2 sm:grid-cols-4 gap-3">
        {ROTATION_BUCKET_META.map((seg) => {
          const count = posture[seg.key]
          return (
            <div key={seg.key} className="flex items-center gap-2">
              <span className={cn('w-2 h-2 rounded-sm flex-shrink-0', seg.fill)} />
              <span className="text-xs text-slate-400">{seg.label}</span>
              <span className="ml-auto font-mono text-xs tabular-nums text-slate-300">{count}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

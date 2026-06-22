'use client'

import { cn } from '@/lib/utils'
import Link from 'next/link'
import {
  CalendarClock,
  GitCompareArrows,
  RefreshCwOff,
  PlugZap,
  CheckCircle2,
  ChevronRight,
  type LucideIcon,
} from 'lucide-react'

export type AttentionSeverity = 'critical' | 'warning' | 'info'
export type AttentionKind = 'overdue' | 'drift' | 'failed-sync' | 'provider'

export interface AttentionItem {
  id: string
  kind: AttentionKind
  severity: AttentionSeverity
  /** Human-readable description of what needs attention. */
  message: string
  /** Machine identifier (rendered in mono). */
  target?: string
  href: string
}

const kindIcon: Record<AttentionKind, LucideIcon> = {
  overdue: CalendarClock,
  drift: GitCompareArrows,
  'failed-sync': RefreshCwOff,
  provider: PlugZap,
}

const severityTone: Record<AttentionSeverity, string> = {
  critical: 'text-red-400',
  warning: 'text-amber-400',
  info: 'text-blue-400',
}

/** Priority order for sorting the queue. Lower = more urgent. */
const SEVERITY_RANK: Record<AttentionSeverity, number> = { critical: 0, warning: 1, info: 2 }
const KIND_RANK: Record<AttentionKind, number> = {
  overdue: 0,
  drift: 1,
  'failed-sync': 2,
  provider: 3,
}

/** Sort by severity, then by kind priority (overdue → drift → failed sync → provider). */
export function sortAttentionItems(items: AttentionItem[]): AttentionItem[] {
  return [...items].sort(
    (a, b) =>
      SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] ||
      KIND_RANK[a.kind] - KIND_RANK[b.kind]
  )
}

export function NeedsAttention({ items, loading }: { items: AttentionItem[]; loading?: boolean }) {
  if (loading && items.length === 0) {
    return (
      <div className="space-y-2 -my-1" aria-busy="true" aria-label="Loading attention queue">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-[44px] rounded-md bg-white/[0.04] animate-pulse" />
        ))}
      </div>
    )
  }
  if (items.length === 0) {
    return (
      <div className="flex items-center gap-3 py-2">
        <CheckCircle2 className="w-5 h-5 text-emerald-400 flex-shrink-0" />
        <div>
          <p className="text-sm text-slate-300">Nothing needs attention</p>
          <p className="text-xs text-slate-400">All secrets are fresh, in sync, and syncing cleanly.</p>
        </div>
      </div>
    )
  }

  return (
    <ul className="divide-y divide-white/[0.06] -my-1">
      {items.map((item) => {
        const Icon = kindIcon[item.kind]
        return (
          <li key={item.id}>
            <Link
              href={item.href}
              className="group flex items-center gap-3 min-h-[44px] py-2.5 -mx-2 px-2 rounded-md hover:bg-white/[0.03] transition-colors"
            >
              <Icon className={cn('w-4 h-4 flex-shrink-0', severityTone[item.severity])} />
              <span className="text-[13px] text-slate-300 flex-1 min-w-0 truncate">{item.message}</span>
              {item.target && (
                <span className="font-mono text-[11px] text-slate-400 truncate max-w-[40%] hidden sm:block">
                  {item.target}
                </span>
              )}
              <ChevronRight className="w-4 h-4 text-slate-400 group-hover:text-slate-200 flex-shrink-0 transition-colors" />
            </Link>
          </li>
        )
      })}
    </ul>
  )
}

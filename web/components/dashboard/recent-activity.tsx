'use client'

import { cn } from '@/lib/utils'
import type { AuditEvent } from '@/lib/api/types'
import { EmptyState } from '@/components/ui-modern'
import { Activity } from 'lucide-react'

interface RecentActivityProps {
  events: AuditEvent[]
}

/** Map an audit event's status/severity to a result label + tone. */
function resultMeta(event: AuditEvent): { label: string; tone: string } {
  const status = (event.status || '').toLowerCase()
  const severity = event.severity

  if (severity === 'error' || severity === 'critical' || status === 'failed' || status === 'failure') {
    return { label: 'ERROR', tone: 'text-red-400' }
  }
  if (severity === 'warning' || status === 'pending') {
    return { label: status === 'pending' ? 'PENDING' : 'WARN', tone: 'text-amber-400' }
  }
  return { label: 'SUCCESS', tone: 'text-emerald-400' }
}

/** UTC HH:MM:SSZ — stable, machine-style timestamp. */
function formatTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return '--:--:--Z'
  return d.toISOString().slice(11, 19) + 'Z'
}

/** ACTION verbs shown uppercased; collapse dotted/namespaced actions to the leaf. */
function formatAction(action: string): string {
  const leaf = action.includes('.') ? action.split('.').pop()! : action
  return leaf.replace(/[_-]/g, ' ').toUpperCase()
}

export function RecentActivity({ events }: RecentActivityProps) {
  if (!Array.isArray(events) || events.length === 0) {
    return <EmptyState icon={<Activity className="w-5 h-5" />} title="No recent activity" />
  }

  return (
    <div className="font-mono text-xs -my-1">
      {events.map((event) => {
        const result = resultMeta(event)
        const target = event.resource_id || event.resource || event.resource_type || '—'
        return (
          <div
            key={event.id}
            className="flex items-center gap-3 py-1.5 border-b border-white/[0.05] last:border-0"
          >
            <span className="text-slate-600 tabular-nums flex-shrink-0">{formatTime(event.timestamp)}</span>
            <span className="text-slate-300 w-20 flex-shrink-0 truncate" title={event.action}>
              {formatAction(event.action)}
            </span>
            <span className="text-slate-400 flex-1 min-w-0 truncate" title={target}>
              {target}
            </span>
            <span className={cn('flex-shrink-0 font-medium', result.tone)}>{result.label}</span>
          </div>
        )
      })}
    </div>
  )
}

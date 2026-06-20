'use client'

import { AuditEvent } from '@/lib/api/types'
import { Badge, StatusBadge, EmptyState, Skeleton } from '@/components/ui-modern'
import { Search, Clock, ChevronRight, AlertTriangle, Info, AlertCircle, CheckCircle2 } from 'lucide-react'

function SevIcon({ s }: { s: string }) {
  if (s === 'critical' || s === 'error') return <AlertCircle className="w-3.5 h-3.5" />
  if (s === 'warning') return <AlertTriangle className="w-3.5 h-3.5" />
  return <Info className="w-3.5 h-3.5" />
}

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(ts))
}

function EventRow({ e, onCorrelation, onActor }: {
  e: AuditEvent
  onCorrelation: (id: string) => void
  onActor: (id: string) => void
}) {
  return (
    <div className="flex items-start gap-3 px-4 py-3 border-b border-white/[0.05] last:border-0 hover:bg-white/[0.02] transition-colors rounded-sm">
      <StatusBadge status={e.severity} />

      <div className="flex-1 min-w-0 space-y-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-[13px] font-semibold text-[#F3F4F6]">{e.action}</span>
          <StatusBadge status={e.status} />
          {e.resource_type && (
            <Badge variant="outline" size="sm">{e.resource_type}</Badge>
          )}
        </div>

        {e.details && (
          <p className="text-[12px] font-normal text-[#9CA3AF] truncate max-w-2xl">{e.details}</p>
        )}

        <div className="flex flex-wrap items-center gap-3 text-[11px] font-normal text-[#9CA3AF]">
          <span className="flex items-center gap-1">
            <Clock className="w-3 h-3" />{relTime(e.timestamp)}
          </span>

          {e.actor && (
            <button
              className="font-mono text-indigo-400 hover:text-indigo-300 transition-colors hover:underline"
              onClick={() => onActor(e.actor_id)}
            >
              {e.actor}
            </button>
          )}

          {e.resource && (
            <span className="font-mono">{e.resource}/{e.resource_id?.slice(0, 8)}</span>
          )}

          {e.correlation_id && (
            <button
              className="font-mono text-blue-400/80 hover:text-blue-400 transition-colors hover:underline flex items-center gap-0.5"
              onClick={() => onCorrelation(e.correlation_id)}
              title="View correlation chain"
            >
              {e.correlation_id.slice(0, 16)}…
              <ChevronRight className="w-3 h-3 inline" />
            </button>
          )}

          {e.ip_address && <span>{e.ip_address}</span>}
        </div>
      </div>
    </div>
  )
}

interface AuditTableProps {
  events: AuditEvent[]
  isLoading: boolean
  isEmpty: boolean
  searchTerm: string
  onCorrelation: (id: string) => void
  onActor: (id: string) => void
}

export function AuditTable({
  events,
  isLoading,
  isEmpty,
  searchTerm,
  onCorrelation,
  onActor,
}: AuditTableProps) {
  if (isLoading) {
    return (
      <div className="p-5 space-y-3">
        <Skeleton className="h-16 w-full rounded" count={5} />
      </div>
    )
  }

  if (isEmpty) {
    return (
      <EmptyState
        icon={<CheckCircle2 className="w-5 h-5" />}
        title={searchTerm ? 'No events match' : 'No audit events found'}
        description={searchTerm ? 'Try a different search term.' : 'System activity will appear here.'}
      />
    )
  }

  return (
    <div>
      {events.map(e => (
        <EventRow key={e.id} e={e} onCorrelation={onCorrelation} onActor={onActor} />
      ))}
    </div>
  )
}

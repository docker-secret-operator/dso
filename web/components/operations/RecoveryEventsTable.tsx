import { Card, Badge } from '@/components/ui-modern'
import { RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { RecoveryEvent } from '@/lib/api/types'

interface RecoveryEventsTableProps {
  events: RecoveryEvent[]
}

/**
 * Table displaying recovery events and auto-recovery actions
 */
export function RecoveryEventsTable({ events }: RecoveryEventsTableProps) {
  if (!events || events.length === 0) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <RefreshCw className="w-5 h-5 text-blue-400" />
          <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        </div>
        <div className="text-slate-400 text-sm">No recovery events</div>
      </Card>
    )
  }

  const getEventTypeColor = (type: string) => {
    switch (type) {
      case 'worker_failure':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      case 'queue_recovery':
        return 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
      case 'execution_cancelled':
        return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20'
      case 'execution_paused':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/20'
      case 'auto_recovery':
        return 'bg-blue-500/10 text-blue-400 border-blue-500/20'
      default:
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
    }
  }

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    } catch {
      return dateStr
    }
  }

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-4">
        <RefreshCw className="w-5 h-5 text-blue-400" />
        <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        <span className="ml-auto text-xs font-medium text-slate-500">
          {events.length} event{events.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10">
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Event Type</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Execution ID</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Worker ID</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Timestamp</th>
            </tr>
          </thead>
          <tbody>
            {events.slice(0, 10).map((event) => (
              <tr
                key={event.id}
                className="border-b border-white/5 hover:bg-white/[0.02] transition-colors"
              >
                <td className="py-3 px-3">
                  <Badge className={getEventTypeColor(event.type)}>
                    {event.type.replace(/_/g, ' ')}
                  </Badge>
                </td>
                <td className="py-3 px-3">
                  <code className="text-xs text-slate-400">
                    {event.execution_id?.substring(0, 8)}...
                  </code>
                </td>
                <td className="py-3 px-3">
                  <code className="text-xs text-slate-400">
                    {event.worker_id?.substring(0, 8)}...
                  </code>
                </td>
                <td className="py-3 px-3 text-slate-400 text-xs">
                  {formatDate(event.timestamp)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

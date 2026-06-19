import { Card, Badge } from '@/components/ui-modern'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Execution } from '@/lib/api/types'

interface ExecutionTableProps {
  executions: Execution[]
  onSelectExecution?: (execution: Execution) => void
}

/**
 * Table displaying recent executions with status and duration
 */
export function ExecutionTable({ executions, onSelectExecution }: ExecutionTableProps) {
  if (!executions || executions.length === 0) {
    return (
      <Card className="p-6">
        <h3 className="text-sm font-semibold text-slate-300 mb-4">Recent Executions</h3>
        <div className="text-slate-400 text-sm">No executions found</div>
      </Card>
    )
  }

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
      case 'failed':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      case 'running':
        return 'bg-blue-500/10 text-blue-400 border-blue-500/20'
      case 'queued':
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
      case 'cancelled':
        return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20'
      case 'paused':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/20'
      case 'timed_out':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      default:
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
    }
  }

  const formatDuration = (durationMs?: number) => {
    if (!durationMs) return '—'
    const seconds = Math.floor(durationMs / 1000)
    if (seconds < 60) return `${seconds}s`
    const minutes = Math.floor(seconds / 60)
    return `${minutes}m ${seconds % 60}s`
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
      <h3 className="text-sm font-semibold text-slate-300 mb-4">Recent Executions</h3>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-white/10">
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">ID</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Status</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Duration</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Started</th>
              <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Correlation ID</th>
              <th className="text-center py-3 px-3 font-medium text-xs text-slate-400 uppercase">Action</th>
            </tr>
          </thead>
          <tbody>
            {executions.map((execution) => (
              <tr
                key={execution.id}
                className="border-b border-white/5 hover:bg-white/[0.02] transition-colors cursor-pointer"
                onClick={() => onSelectExecution?.(execution)}
              >
                <td className="py-3 px-3">
                  <code className="text-xs text-slate-300 bg-black/40 px-2 py-1 rounded">
                    {execution.id.substring(0, 8)}...
                  </code>
                </td>
                <td className="py-3 px-3">
                  <Badge className={getStatusColor(execution.status)}>
                    {execution.status}
                  </Badge>
                </td>
                <td className="py-3 px-3 text-slate-400">
                  {formatDuration(execution.duration_ms)}
                </td>
                <td className="py-3 px-3 text-slate-400">
                  {execution.started_at ? formatDate(execution.started_at) : execution.created_at ? formatDate(execution.created_at) : '—'}
                </td>
                <td className="py-3 px-3">
                  <code className="text-xs text-slate-500">
                    {execution.correlation_id.substring(0, 12)}...
                  </code>
                </td>
                <td className="py-3 px-3 text-center">
                  <button
                    className="inline-flex items-center justify-center w-6 h-6 rounded hover:bg-white/10 transition-colors"
                    onClick={(e) => {
                      e.stopPropagation()
                      onSelectExecution?.(execution)
                    }}
                  >
                    <ChevronRight className="w-4 h-4 text-slate-400" />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

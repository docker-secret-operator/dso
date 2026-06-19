import { Card, Skeleton } from '@/components/ui-modern'
import { Activity } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { QueueHealth } from '@/lib/api/types'

interface QueueHealthCardProps {
  data?: QueueHealth
  isLoading?: boolean
  error?: string | null
}

/**
 * Queue health card showing depth, oldest item age, incoming rate, completion rate, and health score
 */
export function QueueHealthCard({ data, isLoading, error }: QueueHealthCardProps) {
  if (isLoading) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-lg bg-blue-500/10" />
          <div>
            <Skeleton className="h-4 w-24 rounded mb-1" />
            <Skeleton className="h-3 w-32 rounded" />
          </div>
        </div>
        <div className="space-y-4">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-3 w-full rounded" />
          ))}
        </div>
      </Card>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <p className="text-red-400 text-sm">{error}</p>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card className="p-6">
        <p className="text-slate-400 text-sm">No queue health data available</p>
      </Card>
    )
  }

  const healthColor = data.health_score > 75 ? '#10b981' : data.health_score > 50 ? '#f59e0b' : '#ef4444'
  const healthStatus = data.status === 'healthy' ? 'text-emerald-400' : data.status === 'warning' ? 'text-amber-400' : 'text-red-400'

  const formatAge = (seconds: number) => {
    if (seconds < 60) return `${Math.round(seconds)}s`
    if (seconds < 3600) return `${Math.round(seconds / 60)}m`
    return `${Math.round(seconds / 3600)}h`
  }

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="w-10 h-10 rounded-lg bg-blue-500/10 flex items-center justify-center border border-blue-500/20">
          <Activity className="w-5 h-5 text-blue-400" />
        </div>
        <div>
          <h3 className="text-sm font-semibold text-slate-300">Queue Health</h3>
          <p className="text-xs text-slate-500">Real-time queue status</p>
        </div>
      </div>

      <div className="space-y-4">
        {/* Queue Depth */}
        <div>
          <div className="flex justify-between mb-2">
            <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">Depth</span>
            <span className="text-sm font-semibold text-slate-200">{data.depth} items</span>
          </div>
          <div className="h-2 w-full bg-black/40 rounded-full overflow-hidden border border-white/5">
            <div
              className="h-full rounded-full transition-all duration-300 bg-blue-500"
              style={{ width: `${Math.min((data.depth / 100) * 100, 100)}%` }}
            />
          </div>
        </div>

        {/* Oldest Item Age */}
        <div className="flex justify-between text-xs">
          <span className="text-slate-400">Oldest Item Age</span>
          <span className="font-semibold text-slate-200">{formatAge(data.oldest_item_age_seconds)}</span>
        </div>

        {/* Incoming Rate */}
        <div className="flex justify-between text-xs">
          <span className="text-slate-400">Incoming Rate</span>
          <span className="font-semibold text-slate-200">{data.incoming_rate.toFixed(2)}/s</span>
        </div>

        {/* Completion Rate */}
        <div className="flex justify-between text-xs">
          <span className="text-slate-400">Completion Rate</span>
          <span className="font-semibold text-slate-200">{data.completion_rate.toFixed(2)}/s</span>
        </div>

        {/* Health Score */}
        <div className="flex items-center justify-between pt-2 border-t border-white/5">
          <span className="text-xs font-medium text-slate-400">Health Score</span>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full" style={{ backgroundColor: healthColor }} />
            <span className={cn('text-sm font-semibold', healthStatus)}>
              {data.health_score}
            </span>
          </div>
        </div>

        {/* Avg Wait Time */}
        <div className="text-xs text-slate-500">
          Avg wait: {data.avg_wait_time_seconds.toFixed(2)}s
        </div>
      </div>
    </Card>
  )
}

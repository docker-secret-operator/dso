import { Card } from '@/components/ui-modern'
import { Cpu } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { WorkerHealth } from '@/lib/api/types'

interface WorkerHealthCardProps {
  data?: WorkerHealth
}

/**
 * Worker health card showing worker count, utilization, and health score
 */
export function WorkerHealthCard({ data }: WorkerHealthCardProps) {
  if (!data) {
    return (
      <Card className="p-6">
        <p className="text-slate-400 text-sm">No worker health data available</p>
      </Card>
    )
  }

  const healthColor = data.health_score > 75 ? '#10b981' : data.health_score > 50 ? '#f59e0b' : '#ef4444'
  const healthStatus = data.status === 'healthy' ? 'text-emerald-400' : data.status === 'warning' ? 'text-amber-400' : 'text-red-400'

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="w-10 h-10 rounded-lg bg-purple-500/10 flex items-center justify-center border border-purple-500/20">
          <Cpu className="w-5 h-5 text-purple-400" />
        </div>
        <div>
          <h3 className="text-sm font-semibold text-slate-300">Worker Health</h3>
          <p className="text-xs text-slate-500">Active worker status</p>
        </div>
      </div>

      <div className="space-y-4">
        {/* Worker Count */}
        <div>
          <div className="flex justify-between mb-2">
            <span className="text-xs font-medium text-slate-400 uppercase tracking-wider">Workers</span>
            <span className="text-sm font-semibold text-slate-200">
              {data.healthy_workers}/{data.total_workers}
            </span>
          </div>
          <div className="h-2 w-full bg-black/40 rounded-full overflow-hidden border border-white/5">
            <div
              className="h-full rounded-full transition-all duration-300 bg-purple-500"
              style={{ width: `${(data.healthy_workers / data.total_workers) * 100}%` }}
            />
          </div>
        </div>

        {/* Utilization */}
        <div>
          <div className="flex justify-between mb-1">
            <span className="text-xs text-slate-400">Avg Utilization</span>
            <span className="text-xs font-semibold text-slate-200">{Math.round(data.avg_utilization)}%</span>
          </div>
          <div className="h-2 w-full bg-black/40 rounded-full overflow-hidden border border-white/5">
            <div
              className="h-full rounded-full transition-all duration-300 bg-purple-500"
              style={{ width: `${Math.min(data.avg_utilization, 100)}%` }}
            />
          </div>
        </div>

        {/* Health Score */}
        <div className="flex items-center justify-between pt-2 border-t border-white/5">
          <span className="text-xs font-medium text-slate-400">Health Score</span>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full" style={{ backgroundColor: healthColor }} />
            <span className={cn('text-sm font-semibold', healthStatus)}>
              {data.health_score} {data.status}
            </span>
          </div>
        </div>

        {/* Unhealthy count */}
        {data.unhealthy_workers > 0 && (
          <div className="text-xs text-red-400">
            {data.unhealthy_workers} unhealthy worker{data.unhealthy_workers !== 1 ? 's' : ''}
          </div>
        )}
      </div>
    </Card>
  )
}

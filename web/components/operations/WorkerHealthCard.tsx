'use client'

import { useState } from 'react'
import { Card, Badge, Skeleton } from '@/components/ui-modern'
import { Cpu, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { WorkerHealth } from '@/lib/api/types'

interface WorkerHealthCardProps {
  data?: WorkerHealth
  isLoading?: boolean
  error?: string | null
}

/**
 * Worker health card with expandable worker list
 * Shows total workers, healthy count, unhealthy count, average utilization
 * Expandable section displays worker details
 */
export function WorkerHealthCard({ data, isLoading, error }: WorkerHealthCardProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  if (isLoading) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-6">
          <div className="w-10 h-10 rounded-lg bg-purple-500/10" />
          <div>
            <Skeleton className="h-4 w-24 rounded mb-1" />
            <Skeleton className="h-3 w-32 rounded" />
          </div>
        </div>
        <div className="space-y-4">
          {Array.from({ length: 4 }).map((_, i) => (
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
        <p className="text-slate-400 text-sm">No worker health data available</p>
      </Card>
    )
  }

  const healthColor = data.health_score > 75 ? '#10b981' : data.health_score > 50 ? '#f59e0b' : '#ef4444'
  const healthStatus = data.status === 'healthy' ? 'text-emerald-400' : data.status === 'warning' ? 'text-amber-400' : 'text-red-400'

  const healthyPercent = data.total_workers > 0 ? Math.round((data.healthy_workers / data.total_workers) * 100) : 0

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
              style={{ width: `${healthyPercent}%` }}
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
              {data.health_score}
            </span>
          </div>
        </div>

        {/* Unhealthy count */}
        {data.unhealthy_workers > 0 && (
          <div className="text-xs text-red-400">
            {data.unhealthy_workers} unhealthy worker{data.unhealthy_workers !== 1 ? 's' : ''}
          </div>
        )}

        {/* Expandable Workers List */}
        {data.workers && data.workers.length > 0 && (
          <div className="pt-2 border-t border-white/5">
            <button
              onClick={() => setIsExpanded(!isExpanded)}
              className="w-full flex items-center justify-between text-xs font-medium text-slate-400 hover:text-slate-300 transition-colors py-1"
            >
              <span>Worker Details ({data.workers.length})</span>
              <ChevronDown className={cn('w-4 h-4 transition-transform', isExpanded && 'rotate-180')} />
            </button>

            {isExpanded && (
              <div className="mt-3 space-y-2 max-h-64 overflow-y-auto border-t border-white/5 pt-3">
                {data.workers.slice(0, 10).map((worker) => (
                  <div key={worker.id} className="text-xs rounded-lg bg-white/[0.02] p-2.5 space-y-1">
                    <div className="flex items-center justify-between">
                      <code className="text-slate-400 truncate">{worker.id.substring(0, 12)}…</code>
                      <Badge variant={worker.healthy ? 'success' : 'danger'} size="sm">
                        {worker.healthy ? 'healthy' : 'unhealthy'}
                      </Badge>
                    </div>
                    <div className="grid grid-cols-2 gap-2 text-slate-500">
                      <div>Util: <span className="text-slate-400">{Math.round(worker.utilization)}%</span></div>
                      <div>Running: <span className="text-slate-400">{worker.running}</span></div>
                    </div>
                  </div>
                ))}
                {data.workers.length > 10 && (
                  <p className="text-center text-xs text-slate-500 py-2">
                    +{data.workers.length - 10} more workers
                  </p>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </Card>
  )
}

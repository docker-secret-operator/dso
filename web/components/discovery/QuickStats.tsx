'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card } from '@/components/ui-modern'
import { AlertCircle, CheckCircle2, TrendingUp } from 'lucide-react'

interface QuickStatsProps {
  containers?: ContainerMetadata[]
  lastRefreshTime?: Date
}

export function QuickStats({ containers, lastRefreshTime }: QuickStatsProps) {
  if (!containers || containers.length === 0) return null

  const managed = containers.filter(c => c.dso_awareness?.status === 'managed').length
  const partial = containers.filter(c => c.dso_awareness?.status === 'partial').length
  const unmanaged = containers.filter(c => c.dso_awareness?.status === 'unmanaged').length
  const needsMapping = containers.filter(c => (c.dso_awareness?.missing_mappings?.length ?? 0) > 0).length
  const coverage = Math.round((managed / containers.length) * 100)

  let statusIcon = <CheckCircle2 className="w-4 h-4 text-emerald-400" />
  let statusLabel = 'Excellent'
  let statusColor = 'text-emerald-400'

  if (coverage < 80) statusLabel = 'Good'
  if (coverage < 60) statusLabel = 'Warning'
  if (coverage < 40) {
    statusLabel = 'Critical'
    statusIcon = <AlertCircle className="w-4 h-4 text-red-400" />
    statusColor = 'text-red-400'
  }

  return (
    <Card className="p-4 border-indigo-500/20 bg-indigo-500/5">
      <div className="flex items-start justify-between">
        <div className="space-y-3 flex-1">
          <div className="flex items-center gap-2">
            {statusIcon}
            <span className={`text-sm font-semibold ${statusColor}`}>{statusLabel} Coverage</span>
            <span className="text-xs text-slate-500">({coverage}%)</span>
          </div>
          <div className="text-xs text-slate-400 space-y-1">
            <p>
              <span className="font-medium">{managed}</span> managed • <span className="font-medium">{partial}</span> partial •{' '}
              <span className="font-medium">{unmanaged}</span> unmanaged
            </p>
            {needsMapping > 0 && (
              <p className="text-amber-400">
                <TrendingUp className="w-3 h-3 inline mr-1" />
                <span className="font-medium">{needsMapping}</span> containers need secret mapping
              </p>
            )}
          </div>
        </div>
        {lastRefreshTime && (
          <div className="text-right">
            <p className="text-xs text-slate-500">Last scan</p>
            <p className="text-xs text-slate-400">
              {new Date(lastRefreshTime).toLocaleTimeString()}
            </p>
          </div>
        )}
      </div>
    </Card>
  )
}

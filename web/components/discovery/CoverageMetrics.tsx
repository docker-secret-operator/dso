'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { TrendingUp } from 'lucide-react'

interface CoverageMetricsProps {
  containers?: ContainerMetadata[]
  isLoading: boolean
}

interface MetricCard {
  label: string
  value: number
  percentage: number
  color: string
}

export function CoverageMetrics({ containers, isLoading }: CoverageMetricsProps) {
  if (isLoading) {
    return (
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[...Array(4)].map((_, i) => (
          <Skeleton key={i} className="h-24 rounded-lg" />
        ))}
      </div>
    )
  }

  const total = containers?.length ?? 0
  const managed = containers?.filter(c => c.dso_awareness?.status === 'managed').length ?? 0
  const partial = containers?.filter(c => c.dso_awareness?.status === 'partial').length ?? 0
  const unmanaged = containers?.filter(c => c.dso_awareness?.status === 'unmanaged').length ?? 0

  const metrics: MetricCard[] = [
    { label: 'Total', value: total, percentage: 100, color: 'blue' },
    {
      label: 'Managed',
      value: managed,
      percentage: total > 0 ? Math.round((managed / total) * 100) : 0,
      color: 'emerald',
    },
    {
      label: 'Partial',
      value: partial,
      percentage: total > 0 ? Math.round((partial / total) * 100) : 0,
      color: 'amber',
    },
    {
      label: 'Unmanaged',
      value: unmanaged,
      percentage: total > 0 ? Math.round((unmanaged / total) * 100) : 0,
      color: 'slate',
    },
  ]

  const colorClasses: Record<string, string> = {
    blue: 'text-blue-400',
    emerald: 'text-emerald-400',
    amber: 'text-amber-400',
    slate: 'text-slate-400',
  }

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
      {metrics.map(metric => (
        <Card key={metric.label} className="p-4">
          <div className="space-y-2">
            <p className="text-[11px] font-semibold text-[#6B7280] uppercase tracking-wide">{metric.label}</p>
            <div className="flex items-baseline justify-between">
              <p className={`text-[28px] font-semibold ${colorClasses[metric.color]}`}>{metric.value}</p>
              <p className="text-[11px] font-normal text-[#9CA3AF]">{metric.percentage}%</p>
            </div>
          </div>
        </Card>
      ))}
    </div>
  )
}

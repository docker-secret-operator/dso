'use client'

import { useState } from 'react'
import { DiscoveryMetrics } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { ChevronDown } from 'lucide-react'

interface DiscoveryMetricsSectionProps {
  metrics?: DiscoveryMetrics
  isLoading: boolean
}

export function DiscoveryMetricsSection({
  metrics,
  isLoading,
}: DiscoveryMetricsSectionProps) {
  const [isExpanded, setIsExpanded] = useState(false)

  if (isLoading) {
    return (
      <Card className="p-4">
        <Skeleton className="h-12 w-full rounded" />
      </Card>
    )
  }

  if (!metrics) {
    return (
      <Card className="p-4">
        <p className="text-sm text-slate-500">Unable to load discovery metrics</p>
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full px-4 py-3 flex items-center justify-between hover:bg-white/[0.02] transition-colors"
      >
        <h3 className="text-sm font-semibold text-slate-300">Discovery Metrics</h3>
        <ChevronDown
          className={`w-4 h-4 text-slate-500 transition-transform ${
            isExpanded ? 'rotate-180' : ''
          }`}
        />
      </button>

      {isExpanded && (
        <div className="border-t border-white/[0.06] px-4 py-3 space-y-3">
          <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Hits</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_hits}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Misses</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_misses}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Refresh Count</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.refresh_count}</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Cache Age</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.cache_age_seconds}s</p>
            </div>
            <div>
              <p className="text-xs text-slate-500 mb-1">Latency</p>
              <p className="text-lg font-semibold text-slate-200">{metrics.avg_latency_ms}ms</p>
            </div>
          </div>
        </div>
      )}
    </Card>
  )
}

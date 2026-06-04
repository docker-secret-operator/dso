'use client'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Zap } from 'lucide-react'

export interface CacheMetrics {
  hits: number
  misses: number
  hitRate: number
  age: number
  isFresh: boolean
}

export interface CacheMetricsCardProps {
  title?: string
  metrics?: CacheMetrics
  loading?: boolean
}

export function CacheMetricsCard({
  title = 'Cache Performance',
  metrics,
  loading = false,
}: CacheMetricsCardProps) {
  const hitRate = metrics ? Math.round(metrics.hitRate) : 0
  const totalRequests = metrics ? metrics.hits + metrics.misses : 0

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Zap className={`w-5 h-5 ${metrics?.isFresh ? 'text-green-600' : 'text-yellow-600'}`} />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-3">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-4 bg-gray-200 rounded animate-pulse" />
            ))}
          </div>
        ) : metrics ? (
          <div className="space-y-4">
            {/* Hit Rate Progress */}
            <div>
              <div className="flex justify-between items-center mb-2">
                <span className="text-sm font-medium text-gray-700">Hit Rate</span>
                <span className="text-sm font-bold text-gray-900">{hitRate}%</span>
              </div>
              <div className="w-full bg-gray-200 rounded-full h-2">
                <div
                  className="bg-green-600 h-2 rounded-full"
                  style={{ width: `${Math.min(hitRate, 100)}%` }}
                />
              </div>
            </div>

            {/* Stats Grid */}
            <div className="grid grid-cols-2 gap-3">
              <div className="bg-gray-50 p-3 rounded">
                <p className="text-xs text-gray-600">Hits</p>
                <p className="text-lg font-bold text-gray-900">{metrics.hits}</p>
              </div>
              <div className="bg-gray-50 p-3 rounded">
                <p className="text-xs text-gray-600">Misses</p>
                <p className="text-lg font-bold text-gray-900">{metrics.misses}</p>
              </div>
              <div className="bg-gray-50 p-3 rounded">
                <p className="text-xs text-gray-600">Total Requests</p>
                <p className="text-lg font-bold text-gray-900">{totalRequests}</p>
              </div>
              <div className="bg-gray-50 p-3 rounded">
                <p className="text-xs text-gray-600">Age</p>
                <p className="text-lg font-bold text-gray-900">{Math.round(metrics.age / 1000)}s</p>
              </div>
            </div>

            {/* Status */}
            <div className="pt-2 border-t">
              <p className="text-xs text-gray-600">
                Status:{' '}
                <span className={metrics.isFresh ? 'text-green-600 font-semibold' : 'text-yellow-600 font-semibold'}>
                  {metrics.isFresh ? '✓ Fresh' : '⏱ Stale'}
                </span>
              </p>
            </div>
          </div>
        ) : (
          <p className="text-center text-sm text-gray-500 py-4">No metrics available</p>
        )}
      </CardContent>
    </Card>
  )
}

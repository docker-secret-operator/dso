'use client'

import { Card, Button } from '@/components/ui-modern'
import { BarChart3 } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { MetricsHistory, DataPoint } from '@/lib/api/types'

interface MetricsHistoryChartProps {
  data?: MetricsHistory
  isLoading?: boolean
  error?: string
}

type TimeRange = '1h' | '6h' | '24h' | '7d'

/**
 * 4-line chart displaying metrics history
 * Task 8: Shows throughput, queue depth, worker utilization, and success rate
 * with client-side time range filtering
 */
export function MetricsHistoryChart({ data, isLoading, error }: MetricsHistoryChartProps) {
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')

  // Filter data based on time range (client-side)
  const filteredData = useMemo(() => {
    if (!data?.data || data.data.length === 0) return []

    const now = Date.now() / 1000
    const ranges: Record<TimeRange, number> = {
      '1h': 3600,
      '6h': 21600,
      '24h': 86400,
      '7d': 604800,
    }

    const cutoffTime = now - ranges[timeRange]
    return data.data.filter(
      (point) => point.timestamp >= cutoffTime
    )
  }, [data?.data, timeRange])

  // Calculate min/max for each metric
  const metrics = useMemo(() => {
    if (filteredData.length === 0) {
      return {
        throughput: { min: 0, max: 10 },
        queueDepth: { min: 0, max: 100 },
        utilization: { min: 0, max: 100 },
        successRate: { min: 0, max: 100 },
      }
    }

    const throughputs = filteredData.map((d) => d.throughput)
    const depths = filteredData.map((d) => d.queue_depth)
    const utils = filteredData.map((d) => d.worker_utilization)
    const rates = filteredData.map((d) => d.success_rate)

    return {
      throughput: {
        min: Math.min(...throughputs),
        max: Math.max(...throughputs, 10),
      },
      queueDepth: {
        min: Math.min(...depths),
        max: Math.max(...depths, 100),
      },
      utilization: {
        min: Math.min(...utils),
        max: Math.max(...utils, 100),
      },
      successRate: {
        min: Math.min(...rates),
        max: Math.max(...rates, 100),
      },
    }
  }, [filteredData])

  const buildPath = (points: number[], metric: { min: number; max: number }) => {
    if (points.length === 0) return ''
    const width = 800
    const height = 160
    const padding = 40
    const graphWidth = width - padding * 2
    const graphHeight = height - padding * 2
    const range = metric.max - metric.min || 1

    return points
      .map((val, i) => {
        const x = padding + (i / Math.max(points.length - 1, 1)) * graphWidth
        const normalized = (val - metric.min) / range
        const y = padding + graphHeight - normalized * graphHeight
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  }

  if (isLoading) {
    return (
      <Card className="p-6">
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <BarChart3 className="w-5 h-5 text-indigo-400" />
            <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
          </div>
        </div>
        <div className="h-64 bg-white/[0.02] rounded-lg animate-pulse" />
      </Card>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <BarChart3 className="w-5 h-5 text-red-400" />
          <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
        </div>
        <div className="text-red-400 text-xs">{error}</div>
      </Card>
    )
  }

  if (!filteredData || filteredData.length === 0) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <BarChart3 className="w-5 h-5 text-indigo-400" />
          <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
        </div>
        <div className="py-16 text-center">
          <p className="text-slate-500 text-sm">No metrics data for selected period</p>
        </div>
      </Card>
    )
  }

  const width = 800
  const height = 160
  const padding = 40

  const throughputPath = buildPath(
    filteredData.map((d) => d.throughput),
    metrics.throughput
  )
  const queuePath = buildPath(
    filteredData.map((d) => d.queue_depth),
    metrics.queueDepth
  )
  const utilizationPath = buildPath(
    filteredData.map((d) => d.worker_utilization),
    metrics.utilization
  )
  const successPath = buildPath(
    filteredData.map((d) => d.success_rate),
    metrics.successRate
  )

  return (
    <Card className="p-6">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-3">
          <BarChart3 className="w-5 h-5 text-indigo-400" />
          <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
        </div>
        <div className="flex gap-2">
          {(['1h', '6h', '24h', '7d'] as const).map((range) => (
            <Button
              key={range}
              variant={timeRange === range ? 'primary' : 'ghost'}
              size="sm"
              onClick={() => setTimeRange(range)}
            >
              {range}
            </Button>
          ))}
        </div>
      </div>

      <div className="space-y-6">
        {/* Chart */}
        <div className="overflow-x-auto">
          <svg
            viewBox={`0 0 ${width} ${height}`}
            className="w-full h-auto min-w-full"
            style={{ minHeight: '160px' }}
          >
            {/* Grid lines */}
            {Array.from({ length: 4 }).map((_, i) => (
              <line
                key={`grid-${i}`}
                x1={padding}
                y1={padding + (i * (height - padding * 2)) / 3}
                x2={width - padding}
                y2={padding + (i * (height - padding * 2)) / 3}
                stroke="rgba(255, 255, 255, 0.05)"
                strokeWidth="1"
              />
            ))}

            {/* Axes */}
            <line
              x1={padding}
              y1={padding}
              x2={padding}
              y2={height - padding}
              stroke="rgba(255, 255, 255, 0.15)"
              strokeWidth="1"
            />
            <line
              x1={padding}
              y1={height - padding}
              x2={width - padding}
              y2={height - padding}
              stroke="rgba(255, 255, 255, 0.15)"
              strokeWidth="1"
            />

            {/* Throughput (blue) */}
            {throughputPath && (
              <path
                d={throughputPath}
                fill="none"
                stroke="#3b82f6"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            )}

            {/* Queue depth (purple) */}
            {queuePath && (
              <path
                d={queuePath}
                fill="none"
                stroke="#a855f7"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            )}

            {/* Worker utilization (orange) */}
            {utilizationPath && (
              <path
                d={utilizationPath}
                fill="none"
                stroke="#f97316"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            )}

            {/* Success rate (emerald) */}
            {successPath && (
              <path
                d={successPath}
                fill="none"
                stroke="#10b981"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            )}
          </svg>
        </div>

        {/* Legend */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 pt-4 border-t border-white/10">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-blue-500" />
            <span className="text-xs text-slate-400">Throughput (exec/s)</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-purple-500" />
            <span className="text-xs text-slate-400">Queue Depth</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-orange-500" />
            <span className="text-xs text-slate-400">Utilization (%)</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 rounded-full bg-emerald-500" />
            <span className="text-xs text-slate-400">Success Rate (%)</span>
          </div>
        </div>
      </div>
    </Card>
  )
}

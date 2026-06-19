import { Card } from '@/components/ui-modern'
import { BarChart3 } from 'lucide-react'
import type { MetricsHistory } from '@/lib/api/types'

interface MetricsHistoryChartProps {
  data?: MetricsHistory
}

/**
 * Chart displaying metrics history trends
 * Shows success rate, failure rate, throughput, and queue depth over time
 */
export function MetricsHistoryChart({ data }: MetricsHistoryChartProps) {
  if (!data || !data.data || data.data.length === 0) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <BarChart3 className="w-5 h-5 text-indigo-400" />
          <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
        </div>
        <div className="text-slate-400 text-sm">No metrics data available</div>
      </Card>
    )
  }

  const dataPoints = data.data
  const maxDepth = Math.max(...dataPoints.map((d) => d.queue_depth), 100)
  const maxThroughput = Math.max(...dataPoints.map((d) => d.throughput), 10)

  // Simple SVG sparkline visualization
  const width = 800
  const height = 200
  const padding = 40

  // Calculate graph area
  const graphWidth = width - padding * 2
  const graphHeight = height - padding * 2

  // Build path points for throughput line
  const throughputPoints = dataPoints.map((d, i) => {
    const x = padding + (i / (dataPoints.length - 1)) * graphWidth
    const y = padding + graphHeight - (d.throughput / maxThroughput) * graphHeight
    return `${x},${y}`
  })

  // Build path for queue depth
  const queuePoints = dataPoints.map((d, i) => {
    const x = padding + (i / (dataPoints.length - 1)) * graphWidth
    const y = padding + graphHeight - (d.queue_depth / maxDepth) * graphHeight
    return `${x},${y}`
  })

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <BarChart3 className="w-5 h-5 text-indigo-400" />
        <div>
          <h3 className="text-sm font-semibold text-slate-300">Metrics History</h3>
          <p className="text-xs text-slate-500">
            {data.period} • {data.granularity} granularity
          </p>
        </div>
      </div>

      <div className="space-y-6">
        {/* SVG Chart */}
        <div className="overflow-x-auto">
          <svg
            viewBox={`0 0 ${width} ${height}`}
            className="w-full h-auto min-w-full"
            style={{ minHeight: '200px' }}
          >
            {/* Grid lines */}
            {Array.from({ length: 5 }).map((_, i) => (
              <line
                key={`grid-${i}`}
                x1={padding}
                y1={padding + (i * graphHeight) / 4}
                x2={width - padding}
                y2={padding + (i * graphHeight) / 4}
                stroke="rgba(255, 255, 255, 0.05)"
                strokeWidth="1"
              />
            ))}

            {/* Axes */}
            <line x1={padding} y1={padding} x2={padding} y2={height - padding} stroke="rgba(255, 255, 255, 0.2)" strokeWidth="1" />
            <line x1={padding} y1={height - padding} x2={width - padding} y2={height - padding} stroke="rgba(255, 255, 255, 0.2)" strokeWidth="1" />

            {/* Throughput line */}
            <polyline
              points={throughputPoints.join(' ')}
              fill="none"
              stroke="#3b82f6"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />

            {/* Queue depth line */}
            <polyline
              points={queuePoints.join(' ')}
              fill="none"
              stroke="#f59e0b"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />

            {/* Y-axis labels */}
            <text x="15" y={padding + 5} fontSize="12" fill="rgba(255, 255, 255, 0.5)" textAnchor="end">
              {maxThroughput.toFixed(1)}
            </text>
            <text x="15" y={height - padding + 5} fontSize="12" fill="rgba(255, 255, 255, 0.5)" textAnchor="end">
              0
            </text>
          </svg>
        </div>

        {/* Legend and stats */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5">
            <p className="text-xs text-slate-400">Avg Success Rate</p>
            <p className="text-lg font-semibold text-emerald-400">
              {(
                dataPoints.reduce((sum, d) => sum + d.success_rate, 0) / dataPoints.length
              ).toFixed(1)}%
            </p>
          </div>
          <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5">
            <p className="text-xs text-slate-400">Avg Failure Rate</p>
            <p className="text-lg font-semibold text-red-400">
              {(
                dataPoints.reduce((sum, d) => sum + d.failure_rate, 0) / dataPoints.length
              ).toFixed(1)}%
            </p>
          </div>
          <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5">
            <p className="text-xs text-slate-400">Peak Throughput</p>
            <p className="text-lg font-semibold text-blue-400">{maxThroughput.toFixed(2)}/s</p>
          </div>
          <div className="p-3 rounded-lg bg-white/[0.02] border border-white/5">
            <p className="text-xs text-slate-400">Max Queue Depth</p>
            <p className="text-lg font-semibold text-amber-400">{Math.ceil(maxDepth)}</p>
          </div>
        </div>

        {/* Legend */}
        <div className="flex gap-6 pt-4 border-t border-white/10">
          <div className="flex items-center gap-2">
            <div className="w-3 h-0.5 bg-blue-500" />
            <span className="text-xs text-slate-400">Throughput (exec/s)</span>
          </div>
          <div className="flex items-center gap-2">
            <div className="w-3 h-0.5 bg-amber-500" />
            <span className="text-xs text-slate-400">Queue Depth</span>
          </div>
        </div>
      </div>
    </Card>
  )
}

'use client'

import { useState, useEffect } from 'react'
import { TrendingUp, TrendingDown, Zap, Activity } from 'lucide-react'

interface Forecast {
  id: string
  resource_type: string
  resource_id: string
  metric: string
  current_value: number
  predicted_value: number
  growth_rate: number
  confidence: number
  horizon: string
  severity: string
  trend: string
  created_at: number
}

interface ForecastMetrics {
  total_forecasts: number
  critical_forecasts: number
  average_confidence: number
  prediction_accuracy: number
  forecast_runs: number
  last_updated: string
}

export default function ForecastsPage() {
  const [forecasts, setForecasts] = useState<Forecast[]>([])
  const [metrics, setMetrics] = useState<ForecastMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [forecastsRes, metricsRes] = await Promise.all([
          fetch('/api/forecasts'),
          fetch('/api/forecasts/metrics'),
        ])

        if (!forecastsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch forecasts data')
        }

        const forecastsData = await forecastsRes.json()
        const metricsData = await metricsRes.json()

        setForecasts(forecastsData.forecasts || [])
        setMetrics(metricsData)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 30000)
    return () => clearInterval(interval)
  }, [])

  const handleRunForecasts = async () => {
    try {
      const res = await fetch('/api/forecasts/run', { method: 'POST' })
      if (res.ok) {
        // Refresh data
        const forecastsRes = await fetch('/api/forecasts')
        const metricsRes = await fetch('/api/forecasts/metrics')
        if (forecastsRes.ok && metricsRes.ok) {
          const forecastsData = await forecastsRes.json()
          const metricsData = await metricsRes.json()
          setForecasts(forecastsData.forecasts || [])
          setMetrics(metricsData)
        }
      }
    } catch (err) {
      console.error('Failed to run forecasts:', err)
    }
  }

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  const severityColors: Record<string, string> = {
    critical: 'bg-red-100 text-red-800',
    high: 'bg-orange-100 text-orange-800',
    medium: 'bg-yellow-100 text-yellow-800',
    low: 'bg-blue-100 text-blue-800',
    info: 'bg-gray-100 text-gray-800',
  }

  const trendIcon = (trend: string) => {
    switch (trend) {
      case 'increasing':
        return <TrendingUp className="h-4 w-4 text-red-600" />
      case 'decreasing':
        return <TrendingDown className="h-4 w-4 text-green-600" />
      default:
        return <Activity className="h-4 w-4 text-blue-600" />
    }
  }

  return (
    <div className="space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Forecasts</h1>
          <p className="mt-2 text-gray-600">Predict future operational conditions and capacity risks</p>
        </div>
        <button
          onClick={handleRunForecasts}
          className="rounded bg-blue-600 px-4 py-2 text-white font-medium hover:bg-blue-700"
        >
          Run Forecasts
        </button>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800">
          {error}
        </div>
      )}

      {/* Metrics Summary */}
      {metrics && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-5">
          <MetricCard
            label="Total Forecasts"
            value={metrics.total_forecasts}
            icon={<Activity className="h-5 w-5" />}
          />
          <MetricCard
            label="Critical"
            value={metrics.critical_forecasts}
            valueClass="text-red-600"
            icon={<Zap className="h-5 w-5" />}
          />
          <MetricCard
            label="Avg Confidence"
            value={(metrics.average_confidence * 100).toFixed(0) + '%'}
            valueClass="text-blue-600"
          />
          <MetricCard
            label="Accuracy"
            value={(metrics.prediction_accuracy * 100).toFixed(0) + '%'}
            valueClass="text-green-600"
          />
          <MetricCard
            label="Runs"
            value={metrics.forecast_runs}
          />
        </div>
      )}

      {/* Forecasts Table */}
      <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Active Forecasts ({forecasts.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Resource</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Metric</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Current</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Predicted</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Growth</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Confidence</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Severity</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Trend</th>
              </tr>
            </thead>
            <tbody>
              {forecasts.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-6 py-8 text-center text-gray-500">
                    No forecasts available
                  </td>
                </tr>
              ) : (
                forecasts.slice(0, 50).map(forecast => (
                  <tr key={forecast.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4 text-sm font-medium text-gray-900">{forecast.resource_type}</td>
                    <td className="px-6 py-4 text-sm text-gray-600">{forecast.metric}</td>
                    <td className="px-6 py-4 text-sm text-gray-900">
                      {typeof forecast.current_value === 'number' ? forecast.current_value.toFixed(2) : '—'}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-900">
                      {typeof forecast.predicted_value === 'number' ? forecast.predicted_value.toFixed(2) : '—'}
                    </td>
                    <td className="px-6 py-4 text-sm">
                      <span className={forecast.growth_rate > 0 ? 'text-red-600' : 'text-green-600'}>
                        {(forecast.growth_rate * 100).toFixed(1)}%
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm">
                      <div className="flex items-center gap-1">
                        <div className="flex-1 h-2 bg-gray-200 rounded w-12">
                          <div
                            className="h-2 bg-blue-600 rounded"
                            style={{ width: `${forecast.confidence * 100}%` }}
                          />
                        </div>
                        <span className="text-xs text-gray-600">{(forecast.confidence * 100).toFixed(0)}%</span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className={`rounded px-2 py-1 text-xs font-semibold ${severityColors[forecast.severity]}`}>
                        {forecast.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm">
                      <div className="flex items-center gap-1">
                        {trendIcon(forecast.trend)}
                        <span className="text-xs text-gray-600 capitalize">{forecast.trend}</span>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {forecasts.length > 50 && (
          <div className="border-t border-gray-200 px-6 py-4 text-sm text-gray-600">
            Showing 50 of {forecasts.length} forecasts
          </div>
        )}
      </div>

      {/* Forecast Categories */}
      <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
        <ForecastPanel
          title="Queue Saturation"
          description="Predicts queue depth growth and worker utilization"
          forecasts={forecasts.filter(f => f.resource_type === 'queue')}
        />
        <ForecastPanel
          title="Memory Usage"
          description="Predicts exhaustion risk based on trends"
          forecasts={forecasts.filter(f => f.resource_type === 'memory')}
        />
        <ForecastPanel
          title="Backup Storage"
          description="Predicts filesystem usage growth"
          forecasts={forecasts.filter(f => f.resource_type === 'backup_storage')}
        />
      </div>

      <div className="grid grid-cols-1 gap-6 md:grid-cols-3">
        <ForecastPanel
          title="Incident Growth"
          description="Predicts incident count trends"
          forecasts={forecasts.filter(f => f.resource_type === 'incident')}
        />
        <ForecastPanel
          title="Alert Volume"
          description="Predicts alert frequency patterns"
          forecasts={forecasts.filter(f => f.resource_type === 'alert')}
        />
        <ForecastPanel
          title="Scheduler Load"
          description="Predicts job execution volume"
          forecasts={forecasts.filter(f => f.resource_type === 'scheduler')}
        />
      </div>
    </div>
  )
}

interface MetricCardProps {
  label: string
  value: string | number
  icon?: React.ReactNode
  valueClass?: string
}

function MetricCard({ label, value, icon, valueClass = 'text-gray-900' }: MetricCardProps) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="flex items-center justify-between">
        <span className="text-sm text-gray-600">{label}</span>
        {icon && <div className="text-gray-400">{icon}</div>}
      </div>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}

interface ForecastPanelProps {
  title: string
  description: string
  forecasts: Forecast[]
}

function ForecastPanel({ title, description, forecasts }: ForecastPanelProps) {
  const latestForecast = forecasts[0]

  return (
    <div className="rounded-lg border border-gray-200 bg-white p-6">
      <h3 className="font-semibold text-gray-900">{title}</h3>
      <p className="mt-1 text-sm text-gray-600">{description}</p>

      {latestForecast ? (
        <div className="mt-4 space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-gray-600">Current</span>
            <span className="font-medium text-gray-900">{latestForecast.current_value.toFixed(2)}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-600">Predicted</span>
            <span className="font-medium text-gray-900">{latestForecast.predicted_value.toFixed(2)}</span>
          </div>
          <div className="flex justify-between">
            <span className="text-gray-600">Growth</span>
            <span className={`font-medium ${latestForecast.growth_rate > 0 ? 'text-red-600' : 'text-green-600'}`}>
              {(latestForecast.growth_rate * 100).toFixed(1)}%
            </span>
          </div>
          <div className="flex justify-between pt-2 border-t border-gray-200">
            <span className="text-gray-600">Confidence</span>
            <span className="font-medium text-blue-600">{(latestForecast.confidence * 100).toFixed(0)}%</span>
          </div>
        </div>
      ) : (
        <div className="mt-4 text-sm text-gray-500">No forecasts available</div>
      )}
    </div>
  )
}

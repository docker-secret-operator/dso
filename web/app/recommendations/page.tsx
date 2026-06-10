'use client'

import { useState, useEffect } from 'react'
import { CheckCircle, AlertCircle, AlertTriangle, TrendingUp, Zap } from 'lucide-react'

interface Recommendation {
  id: string
  title: string
  description: string
  priority: string
  category: string
  status: string
  resource_id?: string
  incident_id?: string
  suggested_action: string
  confidence: number
  created_at: number
}

interface RecommendationMetrics {
  total_recommendations: number
  open_recommendations: number
  acknowledged_recommendations: number
  implemented_recommendations: number
  dismissed_recommendations: number
  average_confidence: number
  last_updated: string
}

export default function RecommendationsPage() {
  const [recommendations, setRecommendations] = useState<Recommendation[]>([])
  const [metrics, setMetrics] = useState<RecommendationMetrics | null>(null)
  const [filter, setFilter] = useState<'open' | 'implemented' | 'dismissed'>('open')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [recsRes, metricsRes] = await Promise.all([
          fetch(`/api/recommendations?status=${filter}`),
          fetch('/api/recommendations/metrics'),
        ])

        if (!recsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch recommendations data')
        }

        const recsData = await recsRes.json()
        const metricsData = await metricsRes.json()

        setRecommendations(recsData.recommendations || [])
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
  }, [filter])

  const handleAcknowledge = async (recId: string) => {
    try {
      const res = await fetch(`/api/recommendations/${recId}/acknowledge`, { method: 'POST' })
      if (res.ok) {
        setRecommendations(recommendations.map(r => r.id === recId ? { ...r, status: 'acknowledged' } : r))
      }
    } catch (err) {
      console.error('Failed to acknowledge:', err)
    }
  }

  const handleImplement = async (recId: string) => {
    try {
      const res = await fetch(`/api/recommendations/${recId}/implement`, { method: 'POST' })
      if (res.ok) {
        setRecommendations(recommendations.filter(r => r.id !== recId))
      }
    } catch (err) {
      console.error('Failed to implement:', err)
    }
  }

  const handleDismiss = async (recId: string) => {
    try {
      const res = await fetch(`/api/recommendations/${recId}/dismiss`, { method: 'POST' })
      if (res.ok) {
        setRecommendations(recommendations.filter(r => r.id !== recId))
      }
    } catch (err) {
      console.error('Failed to dismiss:', err)
    }
  }

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  const priorityColors: Record<string, string> = {
    critical: 'bg-red-100 text-red-800 border-red-300',
    high: 'bg-orange-100 text-orange-800 border-orange-300',
    medium: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    low: 'bg-blue-100 text-blue-800 border-blue-300',
  }

  const priorityIcons: Record<string, React.ReactNode> = {
    critical: <Zap className="h-4 w-4" />,
    high: <AlertTriangle className="h-4 w-4" />,
    medium: <AlertCircle className="h-4 w-4" />,
    low: <CheckCircle className="h-4 w-4" />,
  }

  const categoryColors: Record<string, string> = {
    backup: 'bg-purple-100 text-purple-800',
    security: 'bg-red-100 text-red-800',
    plugin: 'bg-indigo-100 text-indigo-800',
    integration: 'bg-green-100 text-green-800',
    scheduler: 'bg-blue-100 text-blue-800',
    policy: 'bg-yellow-100 text-yellow-800',
    drift: 'bg-orange-100 text-orange-800',
    performance: 'bg-cyan-100 text-cyan-800',
  }

  return (
    <div className="space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Recommendations</h1>
          <p className="mt-2 text-gray-600">Operational advisory layer for DSO</p>
        </div>
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
            label="Total"
            value={metrics.total_recommendations}
            icon={<TrendingUp className="h-5 w-5" />}
          />
          <MetricCard
            label="Open"
            value={metrics.open_recommendations}
            valueClass="text-red-600"
            icon={<AlertTriangle className="h-5 w-5" />}
          />
          <MetricCard
            label="Acknowledged"
            value={metrics.acknowledged_recommendations}
            valueClass="text-yellow-600"
          />
          <MetricCard
            label="Implemented"
            value={metrics.implemented_recommendations}
            valueClass="text-green-600"
            icon={<CheckCircle className="h-5 w-5" />}
          />
          <MetricCard
            label="Avg Confidence"
            value={(metrics.average_confidence * 100).toFixed(0) + '%'}
            valueClass="text-blue-600"
          />
        </div>
      )}

      {/* Filter Tabs */}
      <div className="flex gap-2 border-b border-gray-200">
        <button
          onClick={() => setFilter('open')}
          className={`px-4 py-2 font-medium border-b-2 ${
            filter === 'open'
              ? 'border-blue-600 text-blue-600'
              : 'border-transparent text-gray-600 hover:text-gray-900'
          }`}
        >
          Open ({metrics?.open_recommendations || 0})
        </button>
        <button
          onClick={() => setFilter('implemented')}
          className={`px-4 py-2 font-medium border-b-2 ${
            filter === 'implemented'
              ? 'border-blue-600 text-blue-600'
              : 'border-transparent text-gray-600 hover:text-gray-900'
          }`}
        >
          Implemented ({metrics?.implemented_recommendations || 0})
        </button>
        <button
          onClick={() => setFilter('dismissed')}
          className={`px-4 py-2 font-medium border-b-2 ${
            filter === 'dismissed'
              ? 'border-blue-600 text-blue-600'
              : 'border-transparent text-gray-600 hover:text-gray-900'
          }`}
        >
          Dismissed ({metrics?.dismissed_recommendations || 0})
        </button>
      </div>

      {/* Recommendations List */}
      <div className="space-y-4">
        {recommendations.length === 0 ? (
          <div className="rounded-lg border border-gray-200 bg-gray-50 p-8 text-center text-gray-500">
            No {filter} recommendations
          </div>
        ) : (
          recommendations.map(rec => (
            <div
              key={rec.id}
              className="rounded-lg border border-gray-200 bg-white p-6 hover:shadow-lg transition-shadow"
            >
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-3">
                    <div className={`rounded border px-2 py-1 ${priorityColors[rec.priority]}`}>
                      <div className="flex items-center gap-1">
                        {priorityIcons[rec.priority]}
                        <span className="text-xs font-semibold uppercase">{rec.priority}</span>
                      </div>
                    </div>
                    <span className={`rounded px-2 py-1 text-xs font-semibold ${categoryColors[rec.category]}`}>
                      {rec.category}
                    </span>
                  </div>

                  <h3 className="mt-2 text-lg font-semibold text-gray-900">{rec.title}</h3>

                  {rec.description && (
                    <p className="mt-2 text-sm text-gray-600">{rec.description}</p>
                  )}

                  <p className="mt-3 text-sm text-gray-700">
                    <span className="font-medium">Action:</span> {rec.suggested_action}
                  </p>

                  <div className="mt-4 grid grid-cols-3 gap-4 md:grid-cols-4">
                    <div>
                      <p className="text-xs text-gray-600">Confidence</p>
                      <div className="mt-1 flex items-center gap-1">
                        <div className="flex-1 h-2 bg-gray-200 rounded">
                          <div
                            className="h-2 bg-blue-600 rounded"
                            style={{ width: `${rec.confidence * 100}%` }}
                          />
                        </div>
                        <span className="text-sm font-semibold text-gray-900">
                          {(rec.confidence * 100).toFixed(0)}%
                        </span>
                      </div>
                    </div>
                    <div>
                      <p className="text-xs text-gray-600">Status</p>
                      <p className="mt-1 text-sm font-medium text-gray-900 capitalize">{rec.status}</p>
                    </div>
                    {rec.incident_id && (
                      <div>
                        <p className="text-xs text-gray-600">Incident</p>
                        <p className="mt-1 text-xs font-mono text-gray-600 truncate">{rec.incident_id.slice(0, 8)}</p>
                      </div>
                    )}
                  </div>
                </div>

                {/* Actions */}
                <div className="ml-4 flex gap-2 flex-wrap">
                  {rec.status === 'open' && (
                    <>
                      <button
                        onClick={() => handleAcknowledge(rec.id)}
                        className="rounded bg-yellow-600 px-3 py-2 text-sm font-medium text-white hover:bg-yellow-700"
                      >
                        Ack
                      </button>
                      <button
                        onClick={() => handleImplement(rec.id)}
                        className="rounded bg-green-600 px-3 py-2 text-sm font-medium text-white hover:bg-green-700"
                      >
                        Impl
                      </button>
                      <button
                        onClick={() => handleDismiss(rec.id)}
                        className="rounded bg-gray-600 px-3 py-2 text-sm font-medium text-white hover:bg-gray-700"
                      >
                        Dismiss
                      </button>
                    </>
                  )}
                </div>
              </div>
            </div>
          ))
        )}
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

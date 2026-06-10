'use client'

import { useState, useEffect } from 'react'
import { AlertTriangle, AlertCircle, CheckCircle, Clock, Zap } from 'lucide-react'

interface Incident {
  id: string
  title: string
  severity: string
  status: string
  root_cause: string
  affected_nodes: string[]
  event_count: number
  correlation_score: number
  first_seen: number
  last_seen: number
  acknowledged_at?: number
  resolved_at?: number
}

interface IncidentMetrics {
  total_incidents: number
  open_incidents: number
  resolved_incidents: number
  acknowledged_incidents: number
  average_correlation_score: number
  events_processed: number
  merges_performed: number
  last_updated: string
}

export default function IncidentsPage() {
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [metrics, setMetrics] = useState<IncidentMetrics | null>(null)
  const [filter, setFilter] = useState<'open' | 'resolved'>('open')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [incidentsRes, metricsRes] = await Promise.all([
          fetch(`/api/incidents?status=${filter}`),
          fetch('/api/incidents/metrics'),
        ])

        if (!incidentsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch incidents data')
        }

        const incidentsData = await incidentsRes.json()
        const metricsData = await metricsRes.json()

        setIncidents(incidentsData.incidents || [])
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

  const handleAcknowledge = async (incidentId: string) => {
    try {
      const res = await fetch(`/api/incidents/${incidentId}/acknowledge`, { method: 'POST' })
      if (res.ok) {
        setIncidents(incidents.map(i => i.id === incidentId ? { ...i, status: 'acknowledged' } : i))
      }
    } catch (err) {
      console.error('Failed to acknowledge incident:', err)
    }
  }

  const handleResolve = async (incidentId: string) => {
    try {
      const res = await fetch(`/api/incidents/${incidentId}/resolve`, { method: 'POST' })
      if (res.ok) {
        setIncidents(incidents.filter(i => i.id !== incidentId))
      }
    } catch (err) {
      console.error('Failed to resolve incident:', err)
    }
  }

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  const severityColors: Record<string, string> = {
    critical: 'bg-red-100 text-red-800 border-red-300',
    high: 'bg-orange-100 text-orange-800 border-orange-300',
    medium: 'bg-yellow-100 text-yellow-800 border-yellow-300',
    low: 'bg-blue-100 text-blue-800 border-blue-300',
    info: 'bg-gray-100 text-gray-800 border-gray-300',
  }

  const severityIcons: Record<string, React.ReactNode> = {
    critical: <Zap className="h-4 w-4" />,
    high: <AlertTriangle className="h-4 w-4" />,
    medium: <AlertCircle className="h-4 w-4" />,
    low: <Clock className="h-4 w-4" />,
    info: <Clock className="h-4 w-4" />,
  }

  const statusColors: Record<string, string> = {
    open: 'bg-red-50 border-red-200',
    acknowledged: 'bg-yellow-50 border-yellow-200',
    resolved: 'bg-green-50 border-green-200',
  }

  const statusBadges: Record<string, string> = {
    open: 'bg-red-100 text-red-800',
    acknowledged: 'bg-yellow-100 text-yellow-800',
    resolved: 'bg-green-100 text-green-800',
  }

  return (
    <div className="space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Incidents</h1>
          <p className="mt-2 text-gray-600">Manage correlated incidents and root causes</p>
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
            label="Total Incidents"
            value={metrics.total_incidents}
            icon={<AlertTriangle className="h-5 w-5" />}
          />
          <MetricCard
            label="Open"
            value={metrics.open_incidents}
            valueClass="text-red-600"
            icon={<AlertTriangle className="h-5 w-5" />}
          />
          <MetricCard
            label="Acknowledged"
            value={metrics.acknowledged_incidents}
            valueClass="text-yellow-600"
          />
          <MetricCard
            label="Resolved"
            value={metrics.resolved_incidents}
            valueClass="text-green-600"
            icon={<CheckCircle className="h-5 w-5" />}
          />
          <MetricCard
            label="Avg Score"
            value={metrics.average_correlation_score.toFixed(1)}
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
          Open ({metrics?.open_incidents || 0})
        </button>
        <button
          onClick={() => setFilter('resolved')}
          className={`px-4 py-2 font-medium border-b-2 ${
            filter === 'resolved'
              ? 'border-blue-600 text-blue-600'
              : 'border-transparent text-gray-600 hover:text-gray-900'
          }`}
        >
          Resolved ({metrics?.resolved_incidents || 0})
        </button>
      </div>

      {/* Incidents List */}
      <div className="space-y-4">
        {incidents.length === 0 ? (
          <div className="rounded-lg border border-gray-200 bg-gray-50 p-8 text-center text-gray-500">
            No {filter} incidents
          </div>
        ) : (
          incidents.map(incident => (
            <div
              key={incident.id}
              className={`rounded-lg border-2 p-6 ${statusColors[incident.status] || statusColors.open}`}
            >
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-3">
                    <div className={`rounded border px-2 py-1 ${severityColors[incident.severity]}`}>
                      <div className="flex items-center gap-1">
                        {severityIcons[incident.severity]}
                        <span className="text-xs font-semibold uppercase">{incident.severity}</span>
                      </div>
                    </div>
                    <span className={`rounded-full px-3 py-1 text-xs font-semibold ${statusBadges[incident.status]}`}>
                      {incident.status}
                    </span>
                  </div>

                  <h3 className="mt-2 text-lg font-semibold text-gray-900">{incident.title}</h3>

                  <p className="mt-2 text-sm text-gray-700">
                    <span className="font-medium">Root Cause:</span> {incident.root_cause}
                  </p>

                  <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-4">
                    <div>
                      <p className="text-xs text-gray-600">Correlation Score</p>
                      <p className="mt-1 text-lg font-semibold text-gray-900">
                        {incident.correlation_score.toFixed(1)}
                      </p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-600">Events</p>
                      <p className="mt-1 text-lg font-semibold text-gray-900">{incident.event_count}</p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-600">Affected Nodes</p>
                      <p className="mt-1 text-lg font-semibold text-gray-900">
                        {incident.affected_nodes.length}
                      </p>
                    </div>
                    <div>
                      <p className="text-xs text-gray-600">Duration</p>
                      <p className="mt-1 text-sm text-gray-900">
                        {formatDuration(incident.last_seen - incident.first_seen)}
                      </p>
                    </div>
                  </div>

                  {incident.affected_nodes.length > 0 && (
                    <div className="mt-4">
                      <p className="text-sm font-medium text-gray-700">Affected Nodes:</p>
                      <div className="mt-2 flex flex-wrap gap-2">
                        {incident.affected_nodes.slice(0, 5).map(node => (
                          <span
                            key={node}
                            className="rounded-full bg-gray-200 px-3 py-1 text-xs text-gray-700"
                          >
                            {node}
                          </span>
                        ))}
                        {incident.affected_nodes.length > 5 && (
                          <span className="text-xs text-gray-600">
                            +{incident.affected_nodes.length - 5} more
                          </span>
                        )}
                      </div>
                    </div>
                  )}
                </div>

                {/* Actions */}
                <div className="ml-4 flex gap-2">
                  {incident.status === 'open' && (
                    <button
                      onClick={() => handleAcknowledge(incident.id)}
                      className="rounded bg-yellow-600 px-3 py-2 text-sm font-medium text-white hover:bg-yellow-700"
                    >
                      Acknowledge
                    </button>
                  )}
                  {incident.status !== 'resolved' && (
                    <button
                      onClick={() => handleResolve(incident.id)}
                      className="rounded bg-green-600 px-3 py-2 text-sm font-medium text-white hover:bg-green-700"
                    >
                      Resolve
                    </button>
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

function formatDuration(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  return `${Math.round(seconds / 3600)}h`
}

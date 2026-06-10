'use client'

import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle, XCircle, Clock, ChevronLeft, ChevronRight } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface MetricAlert {
  id: string
  rule_id: string
  state: string
  severity: string
  metric: string
  message: string
  value: number
  threshold: number
  acknowledged_by?: string
  acknowledged_at?: string
  resolved_by?: string
  resolved_at?: string
  suppressed_by?: string
  suppressed_until?: string
  last_fired_at: string
  created_at: string
}

function getSeverityIcon(severity: string) {
  switch (severity) {
    case 'critical':
      return <AlertTriangle className="w-5 h-5 text-red-600" />
    case 'high':
      return <AlertTriangle className="w-5 h-5 text-orange-600" />
    case 'medium':
      return <AlertTriangle className="w-5 h-5 text-yellow-600" />
    default:
      return <AlertTriangle className="w-5 h-5 text-blue-600" />
  }
}

function getStateIcon(state: string) {
  switch (state) {
    case 'active':
      return <XCircle className="w-5 h-5 text-red-600" />
    case 'acknowledged':
      return <Clock className="w-5 h-5 text-yellow-600" />
    case 'resolved':
      return <CheckCircle className="w-5 h-5 text-green-600" />
    case 'suppressed':
      return <XCircle className="w-5 h-5 text-gray-400" />
    default:
      return <XCircle className="w-5 h-5 text-gray-400" />
  }
}

export default function AlertsPage() {
  const [alerts, setAlerts] = useState<MetricAlert[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [state, setState] = useState<string>('')
  const [severity, setSeverity] = useState<string>('')
  const [actingAlert, setActingAlert] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    fetchAlerts()
  }, [page, state, severity])

  const fetchAlerts = async () => {
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (state) params.append('state', state)
      params.append('limit', pageSize.toString())
      params.append('offset', ((page - 1) * pageSize).toString())

      const response = await fetch(`/api/alerts?${params}`)
      if (!response.ok) {
        if (response.status === 403) {
          router.push('/login')
          return
        }
        throw new Error('Failed to fetch alerts')
      }
      const data = await response.json()
      setAlerts(data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }

  const handleAcknowledge = async (alertId: string) => {
    setActingAlert(alertId)
    try {
      const response = await fetch(`/api/alerts/${alertId}/acknowledge`, { method: 'POST' })
      if (!response.ok) throw new Error('Failed to acknowledge')
      fetchAlerts()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to acknowledge')
    } finally {
      setActingAlert(null)
    }
  }

  const handleResolve = async (alertId: string) => {
    setActingAlert(alertId)
    try {
      const response = await fetch(`/api/alerts/${alertId}/resolve`, { method: 'POST' })
      if (!response.ok) throw new Error('Failed to resolve')
      fetchAlerts()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to resolve')
    } finally {
      setActingAlert(null)
    }
  }

  const handleSuppress = async (alertId: string) => {
    setActingAlert(alertId)
    try {
      const response = await fetch(`/api/alerts/${alertId}/suppress`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ suppress_until: new Date(Date.now() + 24 * 60 * 60 * 1000) }),
      })
      if (!response.ok) throw new Error('Failed to suppress')
      fetchAlerts()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to suppress')
    } finally {
      setActingAlert(null)
    }
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString()
  }

  const activeCritical = alerts.filter((a) => a.state === 'active' && a.severity === 'critical').length
  const activeHigh = alerts.filter((a) => a.state === 'active' && a.severity === 'high').length
  const acknowledged = alerts.filter((a) => a.state === 'acknowledged').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Active Alerts Dashboard</h1>
        <div className="flex gap-2">
          <Link href="/alerts/rules" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 text-sm">
            Manage Rules
          </Link>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="bg-red-50 border border-red-200 rounded-lg p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-red-700 text-sm font-medium">Critical</p>
              <p className="text-3xl font-bold text-red-900">{activeCritical}</p>
            </div>
            <AlertTriangle className="w-8 h-8 text-red-600" />
          </div>
        </div>

        <div className="bg-orange-50 border border-orange-200 rounded-lg p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-orange-700 text-sm font-medium">High</p>
              <p className="text-3xl font-bold text-orange-900">{activeHigh}</p>
            </div>
            <AlertTriangle className="w-8 h-8 text-orange-600" />
          </div>
        </div>

        <div className="bg-yellow-50 border border-yellow-200 rounded-lg p-6">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-yellow-700 text-sm font-medium">Acknowledged</p>
              <p className="text-3xl font-bold text-yellow-900">{acknowledged}</p>
            </div>
            <Clock className="w-8 h-8 text-yellow-600" />
          </div>
        </div>
      </div>

      <div className="bg-white border border-gray-200 rounded-lg p-4 space-y-4">
        <div className="flex gap-4">
          <div className="flex-1">
            <label className="block text-sm font-medium mb-1">State Filter</label>
            <select
              value={state}
              onChange={(e) => {
                setState(e.target.value)
                setPage(1)
              }}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg"
            >
              <option value="">All States</option>
              <option value="active">Active</option>
              <option value="acknowledged">Acknowledged</option>
              <option value="resolved">Resolved</option>
              <option value="suppressed">Suppressed</option>
            </select>
          </div>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading alerts...</div>
        ) : alerts.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No alerts</div>
        ) : (
          <div className="space-y-2">
            {alerts.map((alert) => (
              <div key={alert.id} className="border-b last:border-b-0 p-6 hover:bg-gray-50">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex items-start gap-4 flex-1">
                    <div className="flex flex-col gap-2 mt-1">
                      {getSeverityIcon(alert.severity)}
                      {getStateIcon(alert.state)}
                    </div>
                    <div className="flex-1">
                      <h3 className="font-bold text-lg">{alert.message}</h3>
                      <p className="text-sm text-gray-600 mt-1">
                        Metric: {alert.metric} | Value: {alert.value.toFixed(2)} | Threshold: {alert.threshold.toFixed(2)}
                      </p>
                      <p className="text-xs text-gray-500 mt-2">
                        Created: {formatDate(alert.created_at)} | Last fired: {formatDate(alert.last_fired_at)}
                      </p>
                      {alert.acknowledged_by && (
                        <p className="text-xs text-gray-500">Acknowledged by {alert.acknowledged_by}</p>
                      )}
                    </div>
                  </div>

                  <div className="flex flex-col gap-2 whitespace-nowrap">
                    <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${
                      alert.severity === 'critical'
                        ? 'bg-red-100 text-red-800'
                        : alert.severity === 'high'
                        ? 'bg-orange-100 text-orange-800'
                        : alert.severity === 'medium'
                        ? 'bg-yellow-100 text-yellow-800'
                        : 'bg-blue-100 text-blue-800'
                    }`}>
                      {alert.severity}
                    </span>
                    <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${
                      alert.state === 'active'
                        ? 'bg-red-100 text-red-800'
                        : alert.state === 'acknowledged'
                        ? 'bg-yellow-100 text-yellow-800'
                        : alert.state === 'resolved'
                        ? 'bg-green-100 text-green-800'
                        : 'bg-gray-100 text-gray-800'
                    }`}>
                      {alert.state}
                    </span>
                  </div>
                </div>

                {alert.state === 'active' && (
                  <div className="flex gap-2 mt-4 pt-4 border-t">
                    <button
                      onClick={() => handleAcknowledge(alert.id)}
                      disabled={actingAlert === alert.id}
                      className="px-3 py-1 bg-yellow-100 text-yellow-800 rounded text-sm hover:bg-yellow-200 disabled:opacity-50"
                    >
                      Acknowledge
                    </button>
                    <button
                      onClick={() => handleResolve(alert.id)}
                      disabled={actingAlert === alert.id}
                      className="px-3 py-1 bg-green-100 text-green-800 rounded text-sm hover:bg-green-200 disabled:opacity-50"
                    >
                      Resolve
                    </button>
                    <button
                      onClick={() => handleSuppress(alert.id)}
                      disabled={actingAlert === alert.id}
                      className="px-3 py-1 bg-gray-100 text-gray-800 rounded text-sm hover:bg-gray-200 disabled:opacity-50"
                    >
                      Suppress
                    </button>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}

        {!loading && alerts.length > 0 && (
          <div className="flex items-center justify-between px-6 py-4 bg-gray-50 border-t border-gray-200">
            <div className="text-sm text-gray-600">
              Page <span className="font-medium">{page}</span>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setPage(Math.max(1, page - 1))}
                disabled={page === 1}
                className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
              >
                <ChevronLeft className="w-5 h-5" />
              </button>
              <button
                onClick={() => setPage(page + 1)}
                disabled={alerts.length < pageSize}
                className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
              >
                <ChevronRight className="w-5 h-5" />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

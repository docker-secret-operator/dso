'use client'

import { useState, useEffect } from 'react'
import { Play, X, RotateCcw, TrendingUp, CheckCircle, AlertCircle, Clock } from 'lucide-react'

interface Action {
  id: string
  action_type: string
  status: string
  safety_level: string
  resource_id: string
  trigger: string
  reason: string
  rollback_supported: boolean
  started_at?: number
  completed_at?: number
  created_at: number
  result?: string
  error?: string
}

interface Metrics {
  total_actions: number
  successful_actions: number
  failed_actions: number
  rollback_count: number
  automatic_actions: number
  manual_actions: number
  success_rate: number
  last_updated: string
}

export default function AutonomyPage() {
  const [actions, setActions] = useState<Action[]>([])
  const [metrics, setMetrics] = useState<Metrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [actionsRes, metricsRes] = await Promise.all([
          fetch('/api/autonomy/actions'),
          fetch('/api/autonomy/metrics'),
        ])

        if (!actionsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch autonomy data')
        }

        const actionsData = await actionsRes.json()
        const metricsData = await metricsRes.json()

        setActions(actionsData.actions || [])
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

  const handleExecute = async (actionId: string) => {
    try {
      const res = await fetch(`/api/autonomy/actions/${actionId}/execute`, { method: 'POST' })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'running' } : a))
      }
    } catch (err) {
      console.error('Failed to execute:', err)
    }
  }

  const handleCancel = async (actionId: string) => {
    try {
      const res = await fetch(`/api/autonomy/actions/${actionId}/cancel`, { method: 'POST' })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'cancelled' } : a))
      }
    } catch (err) {
      console.error('Failed to cancel:', err)
    }
  }

  const handleRollback = async (actionId: string) => {
    try {
      const res = await fetch(`/api/autonomy/actions/${actionId}/rollback`, { method: 'POST' })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'rolled_back' } : a))
      }
    } catch (err) {
      console.error('Failed to rollback:', err)
    }
  }

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  const statusColors: Record<string, string> = {
    pending: 'bg-gray-100 text-gray-800',
    running: 'bg-blue-100 text-blue-800',
    succeeded: 'bg-green-100 text-green-800',
    failed: 'bg-red-100 text-red-800',
    rolled_back: 'bg-yellow-100 text-yellow-800',
    cancelled: 'bg-gray-100 text-gray-800',
  }

  const safetyColors: Record<string, string> = {
    manual_only: 'bg-red-100 text-red-800',
    approval_required: 'bg-yellow-100 text-yellow-800',
    automatic: 'bg-green-100 text-green-800',
  }

  return (
    <div className="space-y-8 p-8">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Autonomous Operations</h1>
        <p className="mt-2 text-gray-600">Self-healing remediation with human control and auditability</p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800">
          {error}
        </div>
      )}

      {metrics && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-6">
          <MetricCard label="Total" value={metrics.total_actions} />
          <MetricCard label="Succeeded" value={metrics.successful_actions} valueClass="text-green-600" />
          <MetricCard label="Failed" value={metrics.failed_actions} valueClass="text-red-600" />
          <MetricCard label="Rollbacks" value={metrics.rollback_count} />
          <MetricCard label="Automatic" value={metrics.automatic_actions} />
          <MetricCard label="Success Rate" value={(metrics.success_rate * 100).toFixed(0) + '%'} valueClass="text-blue-600" />
        </div>
      )}

      {/* Actions Table */}
      <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Autonomous Actions ({actions.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Action</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Resource</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Trigger</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Safety</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Status</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Rollback</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Actions</th>
              </tr>
            </thead>
            <tbody>
              {actions.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-8 text-center text-gray-500">
                    No actions
                  </td>
                </tr>
              ) : (
                actions.slice(0, 50).map(action => (
                  <tr key={action.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4 text-sm font-medium text-gray-900">{action.action_type}</td>
                    <td className="px-6 py-4 text-sm text-gray-600">{action.resource_id}</td>
                    <td className="px-6 py-4 text-sm text-gray-600">{action.trigger}</td>
                    <td className="px-6 py-4">
                      <span className={`rounded px-2 py-1 text-xs font-semibold ${safetyColors[action.safety_level]}`}>
                        {action.safety_level}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span className={`rounded px-2 py-1 text-xs font-semibold ${statusColors[action.status]}`}>
                        {action.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm">
                      {action.rollback_supported ? (
                        <span className="text-green-600">✓</span>
                      ) : (
                        <span className="text-gray-400">—</span>
                      )}
                    </td>
                    <td className="px-6 py-4 flex gap-2">
                      {action.status === 'pending' && (
                        <button
                          onClick={() => handleExecute(action.id)}
                          className="rounded bg-blue-600 px-2 py-1 text-xs text-white hover:bg-blue-700"
                          title="Execute"
                        >
                          <Play className="h-3 w-3" />
                        </button>
                      )}
                      {(action.status === 'pending' || action.status === 'running') && (
                        <button
                          onClick={() => handleCancel(action.id)}
                          className="rounded bg-gray-600 px-2 py-1 text-xs text-white hover:bg-gray-700"
                          title="Cancel"
                        >
                          <X className="h-3 w-3" />
                        </button>
                      )}
                      {action.rollback_supported && (action.status === 'succeeded' || action.status === 'failed') && (
                        <button
                          onClick={() => handleRollback(action.id)}
                          className="rounded bg-yellow-600 px-2 py-1 text-xs text-white hover:bg-yellow-700"
                          title="Rollback"
                        >
                          <RotateCcw className="h-3 w-3" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="rounded-lg border border-gray-200 bg-blue-50 p-6">
        <h3 className="font-semibold text-gray-900">Safety Features</h3>
        <ul className="mt-4 space-y-2 text-sm text-gray-700">
          <li>✓ Deterministic: All actions are fully predictable and repeatable</li>
          <li>✓ Auditable: Complete audit trail with timestamps and correlation IDs</li>
          <li>✓ Reversible: Rollback support for all critical operations</li>
          <li>✓ Policy-Driven: Actions triggered by rules matching incidents, drift, forecasts</li>
          <li>✓ Human Override: Manual-only and approval-required safety levels</li>
          <li>✓ Panic Recovery: Failures isolated, never crash DSO</li>
        </ul>
      </div>
    </div>
  )
}

interface MetricCardProps {
  label: string
  value: string | number
  valueClass?: string
}

function MetricCard({ label, value, valueClass = 'text-gray-900' }: MetricCardProps) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <span className="text-sm text-gray-600">{label}</span>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}

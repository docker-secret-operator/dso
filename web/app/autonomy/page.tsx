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

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token ? { Authorization: `Bearer ${token}` } : {}
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
        const headers = getAuthHeaders()
        const [actionsRes, metricsRes] = await Promise.all([
          fetch('/api/autonomy/actions', { headers }),
          fetch('/api/autonomy/metrics', { headers }),
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
      const res = await fetch(`/api/autonomy/actions/${actionId}/execute`, { method: 'POST', headers: getAuthHeaders() })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'running' } : a))
      }
    } catch (err) {
      console.error('Failed to execute:', err)
    }
  }

  const handleCancel = async (actionId: string) => {
    try {
      const res = await fetch(`/api/autonomy/actions/${actionId}/cancel`, { method: 'POST', headers: getAuthHeaders() })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'cancelled' } : a))
      }
    } catch (err) {
      console.error('Failed to cancel:', err)
    }
  }

  const handleRollback = async (actionId: string) => {
    try {
      const res = await fetch(`/api/autonomy/actions/${actionId}/rollback`, { method: 'POST', headers: getAuthHeaders() })
      if (res.ok) {
        setActions(actions.map(a => a.id === actionId ? { ...a, status: 'rolled_back' } : a))
      }
    } catch (err) {
      console.error('Failed to rollback:', err)
    }
  }

  if (loading && !metrics) {
    return <div className="p-8 text-slate-200">Loading...</div>
  }

  const statusColors: Record<string, string> = {
    pending: 'bg-slate-700/30 text-slate-300 border border-slate-600/50',
    running: 'bg-blue-500/15 text-blue-300 border border-blue-500/30',
    succeeded: 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
    failed: 'bg-red-500/15 text-red-300 border border-red-500/30',
    rolled_back: 'bg-amber-500/15 text-amber-300 border border-amber-500/30',
    cancelled: 'bg-slate-700/30 text-slate-400 border border-slate-600/50',
  }

  const safetyColors: Record<string, string> = {
    manual_only: 'bg-red-500/15 text-red-300 border border-red-500/30',
    approval_required: 'bg-amber-500/15 text-amber-300 border border-amber-500/30',
    automatic: 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
  }

  return (
    <div className="space-y-8 p-8">
      <div>
        <h1 className="text-3xl font-bold text-slate-100">Autonomous Operations</h1>
        <p className="mt-2 text-slate-400">Self-healing remediation with human control and auditability</p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-red-300">
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
          <MetricCard label="Success Rate" value={((metrics.success_rate ?? 0) * 100).toFixed(0) + '%'} valueClass="text-blue-600" />
        </div>
      )}

      {/* Actions Table */}
      <div className="rounded-lg border border-slate-700/50 bg-[#111827] overflow-hidden">
        <div className="border-b border-slate-700/50 px-6 py-4">
          <h2 className="font-semibold text-slate-200">Autonomous Actions ({actions.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-slate-700/50 bg-[#0B1020]">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Action</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Resource</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Trigger</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Safety</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Status</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Rollback</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Actions</th>
              </tr>
            </thead>
            <tbody>
              {actions.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-8 text-center text-slate-500">
                    No actions
                  </td>
                </tr>
              ) : (
                actions.slice(0, 50).map(action => (
                  <tr key={action.id} className="border-b border-slate-700/30 hover:bg-slate-800/50/[0.02]">
                    <td className="px-6 py-4 text-sm font-medium text-slate-200">{action.action_type}</td>
                    <td className="px-6 py-4 text-sm text-slate-400">{action.resource_id}</td>
                    <td className="px-6 py-4 text-sm text-slate-400">{action.trigger}</td>
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
                        <span className="text-emerald-400">✓</span>
                      ) : (
                        <span className="text-slate-600">—</span>
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

      <div className="rounded-lg border border-blue-500/20 bg-blue-500/5 p-6">
        <h3 className="font-semibold text-slate-200">Safety Features</h3>
        <ul className="mt-4 space-y-2 text-sm text-slate-300">
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

function MetricCard({ label, value, valueClass = 'text-slate-100' }: MetricCardProps) {
  return (
    <div className="rounded-lg border border-slate-700/50 bg-[#111827] p-4">
      <span className="text-sm text-slate-400">{label}</span>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}

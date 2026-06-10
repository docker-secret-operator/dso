'use client'

import { useState, useEffect } from 'react'
import { AlertCircle, Play, RotateCw, Trash2, TrendingUp } from 'lucide-react'

interface Rule {
  id: string
  name: string
  description?: string
  enabled: boolean
  severity: string
  trigger: string
  event_type?: string
  last_run?: number
  last_result?: string
}

interface RuleMetrics {
  total_rules: number
  enabled_rules: number
  executions: number
  failures: number
  average_duration: number
  last_execution?: number
}

const severityColor: Record<string, string> = {
  info: 'bg-blue-50 text-blue-800 border-blue-200',
  low: 'bg-green-50 text-green-800 border-green-200',
  medium: 'bg-yellow-50 text-yellow-800 border-yellow-200',
  high: 'bg-orange-50 text-orange-800 border-orange-200',
  critical: 'bg-red-50 text-red-800 border-red-200',
}

const triggerColor: Record<string, string> = {
  scheduled: 'bg-purple-100 text-purple-800',
  event: 'bg-blue-100 text-blue-800',
  manual: 'bg-gray-100 text-gray-800',
}

export default function PoliciesPage() {
  const [rules, setRules] = useState<Rule[]>([])
  const [metrics, setMetrics] = useState<RuleMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [rulesRes, metricsRes] = await Promise.all([
          fetch('/api/policies'),
          fetch('/api/policies/metrics'),
        ])

        if (!rulesRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch policy data')
        }

        const rulesData = await rulesRes.json()
        const metricsData = await metricsRes.json()

        setRules(rulesData.rules || [])
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

  const handleRun = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}/run`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to run policy')
      // Refresh data
      const rulesRes = await fetch('/api/policies')
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleEnable = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}/enable`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to enable policy')
      const rulesRes = await fetch('/api/policies')
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleDisable = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}/disable`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to disable policy')
      const rulesRes = await fetch('/api/policies')
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleDelete = async (ruleId: string) => {
    if (!confirm(`Delete policy ${ruleId}?`)) return
    try {
      const res = await fetch(`/api/policies/${ruleId}`, {
        method: 'DELETE',
      })
      if (!res.ok) throw new Error('Failed to delete policy')
      setRules(rules.filter(r => r.id !== ruleId))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const formatTime = (ms?: number) => {
    if (!ms) return '—'
    return new Date(ms).toLocaleString()
  }

  const criticalCount = rules.filter(r => r.severity === 'critical' && r.enabled).length

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  return (
    <div className="space-y-8 p-8">
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Policies</h1>
        <p className="mt-2 text-gray-600">Manage decision rules and automated actions</p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800">
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4" />
            <span>{error}</span>
          </div>
        </div>
      )}

      {/* Metrics Summary */}
      {metrics && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-5">
          <MetricCard
            label="Total Policies"
            value={metrics.total_rules}
            icon={<TrendingUp className="h-5 w-5" />}
          />
          <MetricCard
            label="Enabled"
            value={metrics.enabled_rules}
            valueClass="text-green-600"
          />
          <MetricCard
            label="Executions"
            value={metrics.executions}
            valueClass="text-blue-600"
          />
          <MetricCard
            label="Failures"
            value={metrics.failures}
            valueClass="text-red-600"
          />
          <MetricCard
            label="Critical"
            value={criticalCount}
            valueClass={criticalCount > 0 ? 'text-red-600' : 'text-gray-600'}
          />
        </div>
      )}

      {/* Policies Table */}
      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Policies ({rules.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Name
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Severity
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Trigger
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Status
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Last Run
                </th>
                <th className="px-6 py-3 text-right text-sm font-medium text-gray-700">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {rules.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-6 py-8 text-center text-gray-500">
                    No policies configured
                  </td>
                </tr>
              ) : (
                rules.map(rule => (
                  <tr key={rule.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4">
                      <div>
                        <div className="font-medium text-gray-900">{rule.name}</div>
                        <div className="text-xs text-gray-500">{rule.id}</div>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded border px-2 py-1 text-xs font-medium ${
                          severityColor[rule.severity] || 'bg-gray-50 text-gray-800'
                        }`}
                      >
                        {rule.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          triggerColor[rule.trigger] || 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {rule.trigger}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          rule.enabled
                            ? 'bg-green-100 text-green-800'
                            : 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {rule.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">
                      {formatTime(rule.last_run)}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => handleRun(rule.id)}
                          className="p-1 hover:bg-gray-200 rounded"
                          title="Run now"
                        >
                          <Play className="h-4 w-4 text-blue-600" />
                        </button>
                        {rule.enabled ? (
                          <button
                            onClick={() => handleDisable(rule.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Disable"
                          >
                            <RotateCw className="h-4 w-4 text-yellow-600" />
                          </button>
                        ) : (
                          <button
                            onClick={() => handleEnable(rule.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Enable"
                          >
                            <RotateCw className="h-4 w-4 text-green-600" />
                          </button>
                        )}
                        <button
                          onClick={() => handleDelete(rule.id)}
                          className="p-1 hover:bg-gray-200 rounded"
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4 text-red-600" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Execution Stats */}
      {metrics && (
        <div className="rounded-lg border border-gray-200 bg-white p-6">
          <h3 className="font-semibold text-gray-900">Execution Stats</h3>
          <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-4">
            <div>
              <div className="text-sm text-gray-600">Total Executions</div>
              <div className="mt-1 text-2xl font-bold text-gray-900">{metrics.executions}</div>
            </div>
            <div>
              <div className="text-sm text-gray-600">Failures</div>
              <div className="mt-1 text-2xl font-bold text-red-600">{metrics.failures}</div>
            </div>
            <div>
              <div className="text-sm text-gray-600">Success Rate</div>
              <div className="mt-1 text-2xl font-bold text-green-600">
                {metrics.executions > 0
                  ? (((metrics.executions - metrics.failures) / metrics.executions) * 100).toFixed(1)
                  : '0'}
                %
              </div>
            </div>
            <div>
              <div className="text-sm text-gray-600">Avg Duration</div>
              <div className="mt-1 text-2xl font-bold text-gray-900">
                {metrics.average_duration.toFixed(0)}ms
              </div>
            </div>
          </div>
        </div>
      )}
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

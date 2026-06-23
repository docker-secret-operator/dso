'use client'

import { useState, useEffect } from 'react'
import { AlertCircle, Play, RotateCw, Trash2, TrendingUp } from 'lucide-react'
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar, type BulkAction } from '@/components/common/BulkToolbar'
import { ConfirmModal } from '@/components/common/ConfirmModal'
import * as bulkApi from '@/lib/api/bulk'
import type { BulkIdResult } from '@/lib/api/bulk'

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
  info: 'bg-blue-500/15 text-blue-300 border-blue-500/30',
  low: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30',
  medium: 'bg-amber-500/15 text-amber-300 border-amber-500/30',
  high: 'bg-orange-500/15 text-orange-300 border-orange-500/30',
  critical: 'bg-red-500/15 text-red-300 border-red-500/30',
}

const triggerColor: Record<string, string> = {
  scheduled: 'bg-purple-500/15 text-purple-300',
  event: 'bg-blue-500/15 text-blue-300',
  manual: 'bg-slate-700/30 text-slate-400',
}

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export default function PoliciesPage() {
  const [rules, setRules] = useState<Rule[]>([])
  const [metrics, setMetrics] = useState<RuleMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [confirmDeletePolicy, setConfirmDeletePolicy] = useState<string | null>(null)
  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkIdResult | null>(null)
  const [bulkPending, setBulkPending] = useState(false)
  const [confirmBulkAction, setConfirmBulkAction] = useState<null | 'disable' | 'delete'>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const headers = getAuthHeaders()
        const [rulesRes, metricsRes] = await Promise.all([
          fetch('/api/policies', { headers }),
          fetch('/api/policies/metrics', { headers }),
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
      const res = await fetch(`/api/policies/${ruleId}/run`, { method: 'POST', headers: getAuthHeaders() })
      if (!res.ok) throw new Error('Failed to run policy')
      const rulesRes = await fetch('/api/policies', { headers: getAuthHeaders() })
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleEnable = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}/enable`, { method: 'POST', headers: getAuthHeaders() })
      if (!res.ok) throw new Error('Failed to enable policy')
      const rulesRes = await fetch('/api/policies', { headers: getAuthHeaders() })
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleDisable = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}/disable`, { method: 'POST', headers: getAuthHeaders() })
      if (!res.ok) throw new Error('Failed to disable policy')
      const rulesRes = await fetch('/api/policies', { headers: getAuthHeaders() })
      const rulesData = await rulesRes.json()
      setRules(rulesData.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleDelete = async (ruleId: string) => {
    try {
      const res = await fetch(`/api/policies/${ruleId}`, { method: 'DELETE', headers: getAuthHeaders() })
      if (!res.ok) throw new Error('Failed to delete policy')
      setRules(rules.filter(r => r.id !== ruleId))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleBulkEnable = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyEnable(ids)
      setBulkStatus(result)
      sel.clear()
      const res = await fetch('/api/policies', { headers: getAuthHeaders() })
      const data = await res.json()
      setRules(data.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk enable failed')
    } finally {
      setBulkPending(false)
    }
  }

  const handleBulkDisable = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyDisable(ids)
      setBulkStatus(result)
      sel.clear()
      const res = await fetch('/api/policies', { headers: getAuthHeaders() })
      const data = await res.json()
      setRules(data.rules || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk disable failed')
    } finally {
      setBulkPending(false)
    }
  }

  const handleBulkDelete = async () => {
    const ids = Array.from(sel.selected)
    setBulkPending(true)
    try {
      const result = await bulkApi.bulkPolicyDelete(ids)
      setBulkStatus(result)
      sel.clear()
      setRules(prev => prev.filter(r => !ids.includes(r.id)))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Bulk delete failed')
    } finally {
      setBulkPending(false)
    }
  }

  const formatTime = (ms?: number) => {
    if (!ms) return '—'
    return new Date(ms).toLocaleString()
  }

  const criticalCount = rules.filter(r => r.severity === 'critical' && r.enabled).length

  if (loading && !metrics) {
    return <div className="p-8 text-slate-200">Loading...</div>
  }

  return (
    <div className="space-y-8 p-8">
      <div>
        <h1 className="text-3xl font-bold text-slate-100">Policies</h1>
        <p className="mt-2 text-slate-400">Manage decision rules and automated actions</p>
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-red-300">
          <div className="flex items-center gap-2">
            <AlertCircle className="h-4 w-4" />
            <span>{error}</span>
          </div>
        </div>
      )}

      {confirmDeletePolicy && (
        <div className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-red-300">Delete this policy? This action cannot be undone.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button onClick={() => setConfirmDeletePolicy(null)} className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors">Cancel</button>
            <button onClick={() => { const id = confirmDeletePolicy; setConfirmDeletePolicy(null); handleDelete(id) }} className="px-3 py-1.5 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors">Delete</button>
          </div>
        </div>
      )}

      {confirmBulkAction === 'disable' && (
        <ConfirmModal
          title={`Disable ${sel.size} policies?`}
          message={`You are about to disable ${sel.size} policies. Disabled policies will not evaluate or fire until re-enabled.`}
          confirmLabel={`Disable ${sel.size} policies`}
          onConfirm={() => { setConfirmBulkAction(null); handleBulkDisable() }}
          onCancel={() => setConfirmBulkAction(null)}
        />
      )}
      {confirmBulkAction === 'delete' && (
        <ConfirmModal
          title={`Delete ${sel.size} policies?`}
          message={`You are about to permanently delete ${sel.size} policies. This action cannot be undone.`}
          confirmLabel={`Delete ${sel.size} policies`}
          onConfirm={() => { setConfirmBulkAction(null); handleBulkDelete() }}
          onCancel={() => setConfirmBulkAction(null)}
        />
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
            valueClass={criticalCount > 0 ? 'text-red-600' : 'text-slate-400'}
          />
        </div>
      )}

      {/* Bulk toolbar */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          bulkPending
            ? `Processing ${sel.size} policies…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} updated`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed: ${bulkStatus.failures.map((f) => f.id).join(', ')}`
            : undefined
        }
        actions={[
          {
            label: 'Enable',
            onClick: handleBulkEnable,
            disabled: bulkPending,
          },
          {
            label: 'Disable',
            onClick: () => setConfirmBulkAction('disable'),
            variant: 'danger',
            disabled: bulkPending,
          },
          {
            label: 'Delete',
            onClick: () => setConfirmBulkAction('delete'),
            variant: 'danger',
            disabled: bulkPending,
          },
        ] satisfies BulkAction[]}
      />

      {/* Policies Table */}
      <div className="rounded-lg border border-slate-700/50 bg-[#111827]">
        <div className="border-b border-slate-700/50 px-6 py-4">
          <h2 className="font-semibold text-slate-200">Policies ({rules.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-slate-700/50 bg-[#0B1020]">
              <tr>
                <th className="pl-6 pr-2 py-3 w-8">
                  <input
                    type="checkbox"
                    aria-label="Select page"
                    checked={rules.length > 0 && rules.every(r => sel.isSelected(r.id))}
                    onChange={() => sel.togglePage(rules.map(r => r.id))}
                    className="rounded border-slate-600 bg-transparent accent-indigo-500 cursor-pointer"
                  />
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Name</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Severity</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Trigger</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Status</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-slate-400">Last Run</th>
                <th className="px-6 py-3 text-right text-sm font-medium text-slate-400">Actions</th>
              </tr>
            </thead>
            <tbody>
              {rules.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-8 text-center text-slate-500">
                    No policies configured
                  </td>
                </tr>
              ) : (
                rules.map(rule => (
                  <tr key={rule.id} className="border-b border-slate-700/30 hover:bg-slate-800/50/[0.02]">
                    <td className="pl-6 pr-2 py-4">
                      <input
                        type="checkbox"
                        aria-label={`Select ${rule.name}`}
                        checked={sel.isSelected(rule.id)}
                        onChange={() => sel.toggle(rule.id)}
                        className="rounded border-slate-600 bg-transparent accent-indigo-500 cursor-pointer"
                      />
                    </td>
                    <td className="px-6 py-4">
                      <div>
                        <div className="font-medium text-slate-200">{rule.name}</div>
                        <div className="text-xs text-slate-500">{rule.id}</div>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded border px-2 py-1 text-xs font-medium ${
                          severityColor[rule.severity] || 'bg-slate-900/50 text-slate-200'
                        }`}
                      >
                        {rule.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          triggerColor[rule.trigger] || 'bg-slate-700/30 text-slate-200'
                        }`}
                      >
                        {rule.trigger}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          rule.enabled
                            ? 'bg-emerald-500/15 text-emerald-300'
                            : 'bg-slate-700/30 text-slate-400'
                        }`}
                      >
                        {rule.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-slate-400">
                      {formatTime(rule.last_run)}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => handleRun(rule.id)}
                          className="p-1 hover:bg-slate-800/50/[0.05] rounded"
                          title="Run now"
                        >
                          <Play className="h-4 w-4 text-blue-400" />
                        </button>
                        {rule.enabled ? (
                          <button
                            onClick={() => handleDisable(rule.id)}
                            className="p-1 hover:bg-slate-800/50/[0.05] rounded"
                            title="Disable"
                          >
                            <RotateCw className="h-4 w-4 text-amber-400" />
                          </button>
                        ) : (
                          <button
                            onClick={() => handleEnable(rule.id)}
                            className="p-1 hover:bg-slate-800/50/[0.05] rounded"
                            title="Enable"
                          >
                            <RotateCw className="h-4 w-4 text-emerald-400" />
                          </button>
                        )}
                        <button
                          onClick={() => handleDelete(rule.id)}
                          className="p-1 hover:bg-slate-800/50/[0.05] rounded"
                          title="Delete"
                        >
                          <Trash2 className="h-4 w-4 text-red-400" />
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
        <div className="rounded-lg border border-slate-700/50 bg-slate-800/50 p-6">
          <h3 className="font-semibold text-slate-100">Execution Stats</h3>
          <div className="mt-4 grid grid-cols-2 gap-4 md:grid-cols-4">
            <div>
              <div className="text-sm text-slate-400">Total Executions</div>
              <div className="mt-1 text-2xl font-bold text-slate-100">{metrics.executions}</div>
            </div>
            <div>
              <div className="text-sm text-slate-400">Failures</div>
              <div className="mt-1 text-2xl font-bold text-red-400">{metrics.failures}</div>
            </div>
            <div>
              <div className="text-sm text-slate-400">Success Rate</div>
              <div className="mt-1 text-2xl font-bold text-emerald-400">
                {metrics.executions > 0
                  ? (((metrics.executions - metrics.failures) / metrics.executions) * 100).toFixed(1)
                  : '0'}
                %
              </div>
            </div>
            <div>
              <div className="text-sm text-slate-400">Avg Duration</div>
              <div className="mt-1 text-2xl font-bold text-slate-100">
                {(metrics.average_duration ?? 0).toFixed(0)}ms
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

function MetricCard({ label, value, icon, valueClass = 'text-slate-100' }: MetricCardProps) {
  return (
    <div className="rounded-lg border border-slate-700/50 bg-slate-800/50 p-4">
      <div className="flex items-center justify-between">
        <span className="text-sm text-slate-400">{label}</span>
        {icon && <div className="text-slate-500">{icon}</div>}
      </div>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}

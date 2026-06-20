'use client'

import { useEffect, useState } from 'react'
import { Plus, Edit, Trash2, ToggleRight, ChevronLeft, ChevronRight } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface AlertRule {
  id: string
  name: string
  description?: string
  enabled: boolean
  severity: string
  metric: string
  operator: string
  threshold: number
  duration_seconds: number
  cooldown_seconds: number
  is_builtin: boolean
  created_at: string
  updated_at: string
}

const METRICS = [
  'queue_depth',
  'failure_rate',
  'worker_utilization',
  'memory_usage',
  'login_failures_24h',
  'brute_force_attempts',
]

const OPERATORS = ['>', '>=', '<', '<=', '==', '!=']
const SEVERITIES = ['low', 'medium', 'high', 'critical']

function getSeverityColor(severity: string) {
  const colors = {
    low: 'bg-blue-500/15 text-blue-300',
    medium: 'bg-amber-500/15 text-amber-300',
    high: 'bg-orange-500/15 text-orange-300',
    critical: 'bg-red-500/15 text-red-300',
  }
  return colors[severity as keyof typeof colors] || 'bg-slate-700/30 text-slate-400'
}

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export default function AlertRulesPage() {
  const [rules, setRules] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [confirmDeleteRule, setConfirmDeleteRule] = useState<string | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    description: '',
    enabled: true,
    severity: 'medium',
    metric: 'queue_depth',
    operator: '>',
    threshold: 100,
    duration_seconds: 60,
    cooldown_seconds: 300,
  })
  const router = useRouter()

  useEffect(() => {
    fetchRules()
  }, [page])

  const fetchRules = async () => {
    setLoading(true)
    try {
      const offset = (page - 1) * pageSize
      const response = await fetch(`/api/alerts/rules?limit=${pageSize}&offset=${offset}`, { headers: getAuthHeaders() })
      if (!response.ok) {
        if (response.status === 403) {
          router.push('/login')
          return
        }
        throw new Error('Failed to fetch rules')
      }
      const data = await response.json()
      setRules(data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError(null)
    try {
      const method = editingId ? 'PUT' : 'POST'
      const path = editingId ? `/api/alerts/rules/${editingId}` : '/api/alerts/rules'
      const response = await fetch(path, {
        method,
        headers: { 'Content-Type': 'application/json', ...getAuthHeaders() },
        body: JSON.stringify(formData),
      })
      if (!response.ok) {
        throw new Error('Failed to save rule')
      }
      setShowForm(false)
      setEditingId(null)
      setFormData({
        name: '',
        description: '',
        enabled: true,
        severity: 'medium',
        metric: 'queue_depth',
        operator: '>',
        threshold: 100,
        duration_seconds: 60,
        cooldown_seconds: 300,
      })
      fetchRules()
    } catch (err) {
      setFormError(err instanceof Error ? err.message : 'Failed to save rule')
    }
  }

  const handleDelete = async (ruleId: string) => {
    try {
      const response = await fetch(`/api/alerts/rules/${ruleId}`, { method: 'DELETE', headers: getAuthHeaders() })
      if (!response.ok) throw new Error('Failed to delete rule')
      fetchRules()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete rule')
    }
  }

  const handleEdit = (rule: AlertRule) => {
    setFormData({
      name: rule.name,
      description: rule.description || '',
      enabled: rule.enabled,
      severity: rule.severity,
      metric: rule.metric,
      operator: rule.operator,
      threshold: rule.threshold,
      duration_seconds: rule.duration_seconds,
      cooldown_seconds: rule.cooldown_seconds,
    })
    setEditingId(rule.id)
    setShowForm(true)
  }

  const inputCls = 'w-full px-3 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-200 focus:outline-none focus:border-indigo-500/60 focus:ring-1 focus:ring-indigo-500/30'
  const labelCls = 'block text-sm font-medium text-slate-300 mb-1.5'

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-slate-100">Alert Rules</h1>
        <div className="flex gap-2">
          <button
            onClick={() => { setShowForm(!showForm); if (showForm) setEditingId(null) }}
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-500 text-sm flex items-center gap-2 transition-colors"
          >
            <Plus className="w-4 h-4" />
            {showForm ? 'Cancel' : 'New Rule'}
          </button>
          <Link href="/alerts" className="px-4 py-2 border border-white/[0.09] text-slate-300 rounded-lg hover:bg-white/5 text-sm transition-colors">
            View Alerts
          </Link>
        </div>
      </div>

      {showForm && (
        <form onSubmit={handleSubmit} className="rounded-xl border border-white/[0.07] bg-[#111827] p-6 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>Name</label>
              <input type="text" required value={formData.name} onChange={(e) => setFormData({ ...formData, name: e.target.value })} className={inputCls} />
            </div>
            <div>
              <label className={labelCls}>Severity</label>
              <select value={formData.severity} onChange={(e) => setFormData({ ...formData, severity: e.target.value })} className={inputCls}>
                {SEVERITIES.map((s) => <option key={s} value={s}>{s.charAt(0).toUpperCase() + s.slice(1)}</option>)}
              </select>
            </div>
            <div>
              <label className={labelCls}>Metric</label>
              <select value={formData.metric} onChange={(e) => setFormData({ ...formData, metric: e.target.value })} className={inputCls}>
                {METRICS.map((m) => <option key={m} value={m}>{m.replace(/_/g, ' ')}</option>)}
              </select>
            </div>
            <div className="flex gap-2">
              <div className="flex-1">
                <label className={labelCls}>Operator</label>
                <select value={formData.operator} onChange={(e) => setFormData({ ...formData, operator: e.target.value })} className={inputCls}>
                  {OPERATORS.map((op) => <option key={op} value={op}>{op}</option>)}
                </select>
              </div>
              <div className="flex-1">
                <label className={labelCls}>Threshold</label>
                <input type="number" required value={formData.threshold} onChange={(e) => setFormData({ ...formData, threshold: parseFloat(e.target.value) })} className={inputCls} />
              </div>
            </div>
            <div>
              <label className={labelCls}>Duration (seconds)</label>
              <input type="number" required value={formData.duration_seconds} onChange={(e) => setFormData({ ...formData, duration_seconds: parseInt(e.target.value) })} className={inputCls} />
            </div>
            <div>
              <label className={labelCls}>Cooldown (seconds)</label>
              <input type="number" required value={formData.cooldown_seconds} onChange={(e) => setFormData({ ...formData, cooldown_seconds: parseInt(e.target.value) })} className={inputCls} />
            </div>
            <div className="md:col-span-2">
              <label className={labelCls}>Description</label>
              <textarea value={formData.description} onChange={(e) => setFormData({ ...formData, description: e.target.value })} className={inputCls} rows={2} />
            </div>
            {formError && (
              <div className="md:col-span-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-sm text-red-300">
                {formError}
              </div>
            )}
            <div className="md:col-span-2 flex gap-2">
              <button type="submit" className="px-4 py-2 text-sm bg-indigo-600 text-white rounded-lg hover:bg-indigo-500 transition-colors">
                {editingId ? 'Update Rule' : 'Create Rule'}
              </button>
              {editingId && (
                <button type="button" onClick={() => { setShowForm(false); setEditingId(null) }} className="px-4 py-2 text-sm border border-white/[0.09] text-slate-300 rounded-lg hover:bg-white/5 transition-colors">
                  Cancel
                </button>
              )}
            </div>
          </div>
        </form>
      )}

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-red-300 text-sm">{error}</div>
      )}

      {/* Inline delete confirm */}
      {confirmDeleteRule && (
        <div className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-red-300">Delete this alert rule? This action cannot be undone.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button onClick={() => setConfirmDeleteRule(null)} className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors">Cancel</button>
            <button onClick={() => { const id = confirmDeleteRule; setConfirmDeleteRule(null); handleDelete(id) }} className="px-3 py-1.5 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors">Delete</button>
          </div>
        </div>
      )}

      <div className="rounded-xl border border-white/[0.07] bg-[#111827] overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-slate-400">Loading rules…</div>
        ) : rules.length === 0 ? (
          <div className="p-8 text-center text-slate-500">No rules configured</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-[#0B1020] border-b border-white/[0.07]">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Name</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Severity</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Metric</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Condition</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.05]">
                {rules.map((rule) => (
                  <tr key={rule.id} className="hover:bg-white/[0.03] transition-colors">
                    <td className="px-6 py-4 text-sm font-medium text-slate-100">{rule.name}</td>
                    <td className="px-6 py-4 text-sm">
                      <span className={`inline-block px-2.5 py-0.5 rounded-full text-xs font-medium ${getSeverityColor(rule.severity)}`}>
                        {rule.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-slate-400">{rule.metric.replace(/_/g, ' ')}</td>
                    <td className="px-6 py-4 text-sm font-mono text-slate-300">
                      {rule.operator} {rule.threshold}
                    </td>
                    <td className="px-6 py-4 text-sm">
                      {rule.is_builtin && <span className="text-xs bg-slate-700/30 text-slate-400 px-2 py-0.5 rounded">builtin</span>}
                      <span className={`ml-2 inline-block text-xs ${rule.enabled ? 'text-emerald-400' : 'text-slate-600'}`}>
                        {rule.enabled ? '● enabled' : '● disabled'}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm flex gap-2">
                      <button onClick={() => handleEdit(rule)} className="text-slate-500 hover:text-blue-400 transition-colors" title="Edit">
                        <Edit className="w-4 h-4" />
                      </button>
                      {!rule.is_builtin && (
                        <button onClick={() => setConfirmDeleteRule(rule.id)} className="text-slate-500 hover:text-red-400 transition-colors" title="Delete">
                          <Trash2 className="w-4 h-4" />
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex items-center justify-between px-6 py-4 bg-[#0B1020] border-t border-white/[0.07]">
          <div className="text-sm text-slate-500">Page <span className="font-medium text-slate-300">{page}</span></div>
          <div className="flex gap-2">
            <button onClick={() => setPage(Math.max(1, page - 1))} disabled={page === 1} className="p-2 hover:bg-white/5 rounded-lg disabled:opacity-50 text-slate-400 transition-colors">
              <ChevronLeft className="w-5 h-5" />
            </button>
            <button onClick={() => setPage(page + 1)} disabled={rules.length < pageSize} className="p-2 hover:bg-white/5 rounded-lg disabled:opacity-50 text-slate-400 transition-colors">
              <ChevronRight className="w-5 h-5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

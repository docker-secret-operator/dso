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
    low: 'bg-blue-100 text-blue-800',
    medium: 'bg-yellow-100 text-yellow-800',
    high: 'bg-orange-100 text-orange-800',
    critical: 'bg-red-100 text-red-800',
  }
  return colors[severity as keyof typeof colors] || 'bg-gray-100 text-gray-800'
}

export default function AlertRulesPage() {
  const [rules, setRules] = useState<AlertRule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
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
      const response = await fetch(`/api/alerts/rules?limit=${pageSize}&offset=${offset}`)
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
    try {
      const method = editingId ? 'PUT' : 'POST'
      const path = editingId ? `/api/alerts/rules/${editingId}` : '/api/alerts/rules'
      const response = await fetch(path, {
        method,
        headers: { 'Content-Type': 'application/json' },
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
      alert(err instanceof Error ? err.message : 'Failed to save rule')
    }
  }

  const handleDelete = async (ruleId: string) => {
    if (!confirm('Delete this rule?')) return
    try {
      const response = await fetch(`/api/alerts/rules/${ruleId}`, { method: 'DELETE' })
      if (!response.ok) throw new Error('Failed to delete rule')
      fetchRules()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete rule')
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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Alert Rules Management</h1>
        <div className="flex gap-2">
          <button
            onClick={() => {
              setShowForm(!showForm)
              if (showForm) setEditingId(null)
            }}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 text-sm flex items-center gap-2"
          >
            <Plus className="w-4 h-4" />
            {showForm ? 'Cancel' : 'New Rule'}
          </button>
          <Link href="/alerts" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300 text-sm">
            View Alerts
          </Link>
        </div>
      </div>

      {showForm && (
        <form onSubmit={handleSubmit} className="bg-white border border-gray-200 rounded-lg p-6 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Name</label>
              <input
                type="text"
                required
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Severity</label>
              <select
                value={formData.severity}
                onChange={(e) => setFormData({ ...formData, severity: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
              >
                {SEVERITIES.map((s) => (
                  <option key={s} value={s}>
                    {s.charAt(0).toUpperCase() + s.slice(1)}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Metric</label>
              <select
                value={formData.metric}
                onChange={(e) => setFormData({ ...formData, metric: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
              >
                {METRICS.map((m) => (
                  <option key={m} value={m}>
                    {m.replace(/_/g, ' ')}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex gap-2">
              <div className="flex-1">
                <label className="block text-sm font-medium mb-1">Operator</label>
                <select
                  value={formData.operator}
                  onChange={(e) => setFormData({ ...formData, operator: e.target.value })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg"
                >
                  {OPERATORS.map((op) => (
                    <option key={op} value={op}>
                      {op}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex-1">
                <label className="block text-sm font-medium mb-1">Threshold</label>
                <input
                  type="number"
                  required
                  value={formData.threshold}
                  onChange={(e) => setFormData({ ...formData, threshold: parseFloat(e.target.value) })}
                  className="w-full px-3 py-2 border border-gray-300 rounded-lg"
                />
              </div>
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Duration (seconds)</label>
              <input
                type="number"
                required
                value={formData.duration_seconds}
                onChange={(e) => setFormData({ ...formData, duration_seconds: parseInt(e.target.value) })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Cooldown (seconds)</label>
              <input
                type="number"
                required
                value={formData.cooldown_seconds}
                onChange={(e) => setFormData({ ...formData, cooldown_seconds: parseInt(e.target.value) })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
              />
            </div>
            <div className="md:col-span-2">
              <label className="block text-sm font-medium mb-1">Description</label>
              <textarea
                value={formData.description}
                onChange={(e) => setFormData({ ...formData, description: e.target.value })}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg"
                rows={2}
              />
            </div>
            <div className="md:col-span-2 flex gap-2">
              <button type="submit" className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700">
                {editingId ? 'Update Rule' : 'Create Rule'}
              </button>
              {editingId && (
                <button
                  type="button"
                  onClick={() => {
                    setShowForm(false)
                    setEditingId(null)
                  }}
                  className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300"
                >
                  Cancel
                </button>
              )}
            </div>
          </div>
        </form>
      )}

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading rules...</div>
        ) : rules.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No rules configured</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Name</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Severity</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Metric</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Condition</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {rules.map((rule) => (
                  <tr key={rule.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 text-sm font-medium text-gray-900">{rule.name}</td>
                    <td className="px-6 py-4 text-sm">
                      <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${getSeverityColor(rule.severity)}`}>
                        {rule.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700">{rule.metric.replace(/_/g, ' ')}</td>
                    <td className="px-6 py-4 text-sm font-mono text-gray-700">
                      {rule.operator} {rule.threshold}
                    </td>
                    <td className="px-6 py-4 text-sm">
                      {rule.is_builtin && <span className="text-xs bg-gray-100 text-gray-700 px-2 py-1 rounded">builtin</span>}
                      <span className={`ml-2 inline-block text-xs ${rule.enabled ? 'text-green-600' : 'text-gray-400'}`}>
                        {rule.enabled ? '● enabled' : '● disabled'}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm flex gap-2">
                      <button
                        onClick={() => handleEdit(rule)}
                        className="text-blue-600 hover:text-blue-800"
                      >
                        <Edit className="w-4 h-4" />
                      </button>
                      {!rule.is_builtin && (
                        <button
                          onClick={() => handleDelete(rule.id)}
                          className="text-red-600 hover:text-red-800"
                        >
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
              disabled={rules.length < pageSize}
              className="p-2 hover:bg-gray-200 rounded disabled:opacity-50"
            >
              <ChevronRight className="w-5 h-5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

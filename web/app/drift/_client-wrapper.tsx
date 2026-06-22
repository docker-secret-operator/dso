'use client'

import { useState, useEffect } from 'react'
import { apiFetch } from "@/lib/api-fetch"
import { AlertCircle, Play, CheckCircle2, TrendingUp } from 'lucide-react'

interface Finding {
  id: string
  type: string
  severity: string
  status: string
  resource: string
  description: string
  detected_at: number
  acknowledged_at?: number
  resolved_at?: number
}

interface DriftMetrics {
  total_findings: number
  critical_findings: number
  open_findings: number
  scans: number
  average_duration: number
  last_scan?: number
}

const severityColor: Record<string, string> = {
  info: 'bg-blue-50 text-blue-800 border-blue-200',
  low: 'bg-green-50 text-green-800 border-green-200',
  medium: 'bg-yellow-50 text-yellow-800 border-yellow-200',
  high: 'bg-orange-50 text-orange-800 border-orange-200',
  critical: 'bg-red-50 text-red-800 border-red-200',
}

const statusColor: Record<string, string> = {
  detected: 'bg-blue-100 text-blue-800',
  acknowledged: 'bg-yellow-100 text-yellow-800',
  resolved: 'bg-green-100 text-green-800',
}

const driftTypeColor: Record<string, string> = {
  secret: 'bg-purple-100 text-purple-800',
  policy: 'bg-blue-100 text-blue-800',
  plugin: 'bg-indigo-100 text-indigo-800',
  user: 'bg-red-100 text-red-800',
  configuration: 'bg-gray-100 text-gray-800',
  backup: 'bg-orange-100 text-orange-800',
  integration: 'bg-green-100 text-green-800',
  scheduler: 'bg-cyan-100 text-cyan-800',
}

export function DriftDashboardClient() {
  const [findings, setFindings] = useState<Finding[]>([])
  const [metrics, setMetrics] = useState<DriftMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [findingsRes, metricsRes] = await Promise.all([
          apiFetch('/api/drift'),
          apiFetch('/api/drift/metrics'),
        ])

        if (!findingsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch drift data')
        }

        const findingsData = await findingsRes.json()
        const metricsData = await metricsRes.json()

        setFindings(findingsData.findings || [])
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

  const handleScan = async () => {
    try {
      setScanning(true)
      const res = await apiFetch('/api/drift/scan', {
        method: 'POST',
      })

      if (!res.ok) throw new Error('Scan failed')

      // Refresh data
      const findingsRes = await apiFetch('/api/drift')
      const findingsData = await findingsRes.json()
      setFindings(findingsData.findings || [])

      const metricsRes = await apiFetch('/api/drift/metrics')
      const metricsData = await metricsRes.json()
      setMetrics(metricsData)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setScanning(false)
    }
  }

  const handleAcknowledge = async (findingId: string) => {
    try {
      const res = await apiFetch(`/api/drift/${findingId}/acknowledge`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to acknowledge')
      // Refresh data
      const findingsRes = await apiFetch('/api/drift')
      const findingsData = await findingsRes.json()
      setFindings(findingsData.findings || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleResolve = async (findingId: string) => {
    try {
      const res = await apiFetch(`/api/drift/${findingId}/resolve`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to resolve')
      // Refresh data
      const findingsRes = await apiFetch('/api/drift')
      const findingsData = await findingsRes.json()
      setFindings(findingsData.findings || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const formatTime = (ms?: number) => {
    if (!ms) return '—'
    return new Date(ms).toLocaleString()
  }

  if (loading && !metrics) {
    return <div className="p-8">Loading...</div>
  }

  return (
    <div className="space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Drift Detection</h1>
          <p className="mt-2 text-gray-600">Monitor configuration changes and divergence</p>
        </div>
        <button
          onClick={handleScan}
          disabled={scanning}
          className="flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:opacity-50"
        >
          <Play className="h-4 w-4" />
          {scanning ? 'Scanning...' : 'Run Scan'}
        </button>
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
            label="Total Findings"
            value={metrics.total_findings}
            icon={<TrendingUp className="h-5 w-5" />}
          />
          <MetricCard
            label="Critical"
            value={metrics.critical_findings}
            valueClass="text-red-600"
          />
          <MetricCard
            label="Open"
            value={metrics.open_findings}
            valueClass="text-orange-600"
          />
          <MetricCard
            label="Scans"
            value={metrics.scans}
            valueClass="text-blue-600"
          />
          <MetricCard
            label="Avg Duration"
            value={`${metrics.average_duration.toFixed(0)}ms`}
            valueClass="text-gray-600"
          />
        </div>
      )}

      {/* Findings Table */}
      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Findings ({findings.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Type</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Severity</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Status</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Resource</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Description</th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">Detected</th>
                <th className="px-6 py-3 text-right text-sm font-medium text-gray-700">Actions</th>
              </tr>
            </thead>
            <tbody>
              {findings.length === 0 ? (
                <tr>
                  <td colSpan={7} className="px-6 py-8 text-center text-gray-500">
                    No drift findings
                  </td>
                </tr>
              ) : (
                findings.map(finding => (
                  <tr key={finding.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          driftTypeColor[finding.type] || 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {finding.type}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded border px-2 py-1 text-xs font-medium ${
                          severityColor[finding.severity] || 'bg-gray-50 text-gray-800'
                        }`}
                      >
                        {finding.severity}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded px-2 py-1 text-xs font-medium ${
                          statusColor[finding.status] || 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {finding.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">{finding.resource}</td>
                    <td className="px-6 py-4 text-sm text-gray-600">{finding.description}</td>
                    <td className="px-6 py-4 text-sm text-gray-600">{formatTime(finding.detected_at)}</td>
                    <td className="px-6 py-4">
                      <div className="flex justify-end gap-2">
                        {finding.status !== 'acknowledged' && (
                          <button
                            onClick={() => handleAcknowledge(finding.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Acknowledge"
                          >
                            <CheckCircle2 className="h-4 w-4 text-yellow-600" />
                          </button>
                        )}
                        {finding.status !== 'resolved' && (
                          <button
                            onClick={() => handleResolve(finding.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Resolve"
                          >
                            <CheckCircle2 className="h-4 w-4 text-green-600" />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Statistics */}
      {metrics && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div className="rounded-lg border border-gray-200 bg-white p-6">
            <h3 className="font-semibold text-gray-900">Status Distribution</h3>
            <div className="mt-4 space-y-2">
              <StatRow
                label="Detected"
                value={findings.filter(f => f.status === 'detected').length}
              />
              <StatRow
                label="Acknowledged"
                value={findings.filter(f => f.status === 'acknowledged').length}
              />
              <StatRow
                label="Resolved"
                value={findings.filter(f => f.status === 'resolved').length}
              />
            </div>
          </div>

          <div className="rounded-lg border border-gray-200 bg-white p-6">
            <h3 className="font-semibold text-gray-900">Severity Distribution</h3>
            <div className="mt-4 space-y-2">
              <StatRow
                label="Critical"
                value={findings.filter(f => f.severity === 'critical').length}
                valueClass="text-red-600"
              />
              <StatRow
                label="High"
                value={findings.filter(f => f.severity === 'high').length}
                valueClass="text-orange-600"
              />
              <StatRow
                label="Medium"
                value={findings.filter(f => f.severity === 'medium').length}
                valueClass="text-yellow-600"
              />
            </div>
          </div>

          <div className="rounded-lg border border-gray-200 bg-white p-6">
            <h3 className="font-semibold text-gray-900">Type Distribution</h3>
            <div className="mt-4 space-y-2 text-sm">
              {Array.from(new Set(findings.map(f => f.type))).map(type => (
                <StatRow
                  key={type}
                  label={type}
                  value={findings.filter(f => f.type === type).length}
                />
              ))}
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

function StatRow({
  label,
  value,
  valueClass = 'text-gray-900',
}: {
  label: string
  value: number
  valueClass?: string
}) {
  return (
    <div className="flex justify-between text-sm">
      <span className="text-gray-600">{label}</span>
      <span className={`font-medium ${valueClass}`}>{value}</span>
    </div>
  )
}

'use client'

import { useState, useEffect } from 'react'
import { AlertCircle, Play, Pause, RotateCcw, Trash2, TrendingUp } from 'lucide-react'

interface Job {
  id: string
  name: string
  type: string
  enabled: boolean
  status: string
  next_run?: number
  last_run?: number
  metadata?: Record<string, string>
}

interface SystemMetrics {
  total_jobs: number
  running_jobs: number
  successful_runs: number
  failed_runs: number
  average_duration: number
  last_execution?: number
  worker_utilization: number
  active_workers: number
  queued_jobs: number
  completed_jobs: number
}

const statusColor: Record<string, string> = {
  pending: 'bg-yellow-50 text-yellow-800 border-yellow-200',
  running: 'bg-blue-50 text-blue-800 border-blue-200',
  success: 'bg-green-50 text-green-800 border-green-200',
  failed: 'bg-red-50 text-red-800 border-red-200',
  paused: 'bg-gray-50 text-gray-800 border-gray-200',
  disabled: 'bg-gray-50 text-gray-800 border-gray-200',
}

export default function SchedulerPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [metrics, setMetrics] = useState<SystemMetrics | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true)
        const [jobsRes, metricsRes] = await Promise.all([
          fetch('/api/scheduler/jobs'),
          fetch('/api/scheduler/metrics'),
        ])

        if (!jobsRes.ok || !metricsRes.ok) {
          throw new Error('Failed to fetch scheduler data')
        }

        const jobsData = await jobsRes.json()
        const metricsData = await metricsRes.json()

        setJobs(jobsData.jobs || [])
        setMetrics(metricsData)
        setError(null)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unknown error')
      } finally {
        setLoading(false)
      }
    }

    fetchData()
    const interval = setInterval(fetchData, 5000)
    return () => clearInterval(interval)
  }, [])

  const handleRunNow = async (jobId: string) => {
    try {
      const res = await fetch(`/api/scheduler/jobs/${jobId}/run`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to run job')
      // Refresh data
      const jobsRes = await fetch('/api/scheduler/jobs')
      const jobsData = await jobsRes.json()
      setJobs(jobsData.jobs || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handlePause = async (jobId: string) => {
    try {
      const res = await fetch(`/api/scheduler/jobs/${jobId}/pause`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to pause job')
      const jobsRes = await fetch('/api/scheduler/jobs')
      const jobsData = await jobsRes.json()
      setJobs(jobsData.jobs || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleResume = async (jobId: string) => {
    try {
      const res = await fetch(`/api/scheduler/jobs/${jobId}/resume`, {
        method: 'POST',
      })
      if (!res.ok) throw new Error('Failed to resume job')
      const jobsRes = await fetch('/api/scheduler/jobs')
      const jobsData = await jobsRes.json()
      setJobs(jobsData.jobs || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    }
  }

  const handleDelete = async (jobId: string) => {
    if (!confirm(`Delete job ${jobId}?`)) return
    try {
      const res = await fetch(`/api/scheduler/jobs/${jobId}`, {
        method: 'DELETE',
      })
      if (!res.ok) throw new Error('Failed to delete job')
      setJobs(jobs.filter(j => j.id !== jobId))
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
      <div>
        <h1 className="text-3xl font-bold text-gray-900">Scheduler</h1>
        <p className="mt-2 text-gray-600">Manage internal jobs and scheduling</p>
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
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3 lg:grid-cols-5">
          <MetricCard
            label="Total Jobs"
            value={metrics.total_jobs}
            icon={<TrendingUp className="h-5 w-5" />}
          />
          <MetricCard
            label="Running"
            value={metrics.running_jobs}
            valueClass="text-blue-600"
          />
          <MetricCard
            label="Successful"
            value={metrics.successful_runs}
            valueClass="text-green-600"
          />
          <MetricCard
            label="Failed"
            value={metrics.failed_runs}
            valueClass="text-red-600"
          />
          <MetricCard
            label="Worker Util"
            value={`${metrics.worker_utilization.toFixed(1)}%`}
            valueClass="text-purple-600"
          />
        </div>
      )}

      {/* Jobs Table */}
      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Jobs ({jobs.length})</h2>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Name
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Type
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Status
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Last Run
                </th>
                <th className="px-6 py-3 text-left text-sm font-medium text-gray-700">
                  Next Run
                </th>
                <th className="px-6 py-3 text-right text-sm font-medium text-gray-700">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {jobs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-6 py-8 text-center text-gray-500">
                    No jobs configured
                  </td>
                </tr>
              ) : (
                jobs.map(job => (
                  <tr key={job.id} className="border-b border-gray-200 hover:bg-gray-50">
                    <td className="px-6 py-4">
                      <div>
                        <div className="font-medium text-gray-900">{job.name}</div>
                        <div className="text-xs text-gray-500">{job.id}</div>
                      </div>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">
                      <span className="rounded-full bg-gray-100 px-2 py-1 text-xs font-medium">
                        {job.type}
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <span
                        className={`rounded border px-2 py-1 text-xs font-medium ${
                          statusColor[job.status] || 'bg-gray-50 text-gray-800'
                        }`}
                      >
                        {job.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">
                      {formatTime(job.last_run)}
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-600">
                      {formatTime(job.next_run)}
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => handleRunNow(job.id)}
                          className="p-1 hover:bg-gray-200 rounded"
                          title="Run now"
                        >
                          <Play className="h-4 w-4 text-blue-600" />
                        </button>
                        {job.status !== 'paused' && (
                          <button
                            onClick={() => handlePause(job.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Pause"
                          >
                            <Pause className="h-4 w-4 text-yellow-600" />
                          </button>
                        )}
                        {job.status === 'paused' && (
                          <button
                            onClick={() => handleResume(job.id)}
                            className="p-1 hover:bg-gray-200 rounded"
                            title="Resume"
                          >
                            <RotateCcw className="h-4 w-4 text-green-600" />
                          </button>
                        )}
                        <button
                          onClick={() => handleDelete(job.id)}
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

      {/* Worker Pool Stats */}
      {metrics && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div className="rounded-lg border border-gray-200 bg-white p-6">
            <h3 className="font-semibold text-gray-900">Worker Pool</h3>
            <div className="mt-4 space-y-2">
              <div className="flex justify-between text-sm">
                <span className="text-gray-600">Active Workers</span>
                <span className="font-medium text-gray-900">{metrics.active_workers}</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-600">Queued Jobs</span>
                <span className="font-medium text-gray-900">{metrics.queued_jobs}</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-600">Completed Jobs</span>
                <span className="font-medium text-gray-900">{metrics.completed_jobs}</span>
              </div>
            </div>
          </div>

          <div className="rounded-lg border border-gray-200 bg-white p-6">
            <h3 className="font-semibold text-gray-900">Execution Stats</h3>
            <div className="mt-4 space-y-2">
              <div className="flex justify-between text-sm">
                <span className="text-gray-600">Avg Duration</span>
                <span className="font-medium text-gray-900">
                  {metrics.average_duration.toFixed(2)}ms
                </span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-gray-600">Success Rate</span>
                <span className="font-medium text-green-600">
                  {metrics.successful_runs + metrics.failed_runs > 0
                    ? (
                        (metrics.successful_runs /
                          (metrics.successful_runs + metrics.failed_runs)) *
                        100
                      ).toFixed(1)
                    : '0'}
                  %
                </span>
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

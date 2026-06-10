'use client'

import { useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle, Clock, Database, Activity } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface BackupStatus {
  latest_backup_age_minutes?: number
  total_backups: number
  failed_backups: number
  completed_backups: number
  worker_running: boolean
  last_backup_size_bytes?: number
  retention_days: number
}

function formatBytes(bytes?: number) {
  if (!bytes) return 'N/A'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
}

function getHealthStatus(status: BackupStatus) {
  const warnings = []

  if (!status.latest_backup_age_minutes) {
    warnings.push('No backups created yet')
  } else if (status.latest_backup_age_minutes > 24 * 60) {
    warnings.push('Last backup is older than 24 hours')
  }

  if (status.failed_backups > 0) {
    warnings.push(`${status.failed_backups} failed backup(s)`)
  }

  if (!status.worker_running) {
    warnings.push('Backup worker not running')
  }

  return warnings
}

export default function RecoveryDashboard() {
  const [status, setStatus] = useState<BackupStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    fetchStatus()
    const interval = setInterval(fetchStatus, 30000)
    return () => clearInterval(interval)
  }, [])

  const fetchStatus = async () => {
    try {
      const response = await fetch('/api/backups/status')
      if (!response.ok) {
        if (response.status === 403) {
          router.push('/login')
          return
        }
        throw new Error('Failed to fetch backup status')
      }
      const data = await response.json()
      setStatus(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }

  if (loading) {
    return <div className="text-center p-8">Loading...</div>
  }

  if (error || !status) {
    return <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error || 'Failed to load backup status'}</div>
  }

  const warnings = getHealthStatus(status)
  const isHealthy = warnings.length === 0

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Disaster Recovery Dashboard</h1>
        <Link href="/backups" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300">
          Manage Backups
        </Link>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className={`border rounded-lg p-6 ${isHealthy ? 'bg-green-50 border-green-200' : 'bg-yellow-50 border-yellow-200'}`}>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-bold">{isHealthy ? 'System Healthy' : 'Attention Required'}</h2>
            {isHealthy ? (
              <CheckCircle className="w-6 h-6 text-green-600" />
            ) : (
              <AlertTriangle className="w-6 h-6 text-yellow-600" />
            )}
          </div>
          <div className="space-y-2">
            {isHealthy ? (
              <p className="text-green-700">All backup systems operational</p>
            ) : (
              <div className="space-y-1">
                {warnings.map((warning, idx) => (
                  <p key={idx} className="text-yellow-700 text-sm">• {warning}</p>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <h2 className="text-lg font-bold mb-4">Status</h2>
          <div className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-gray-700">Worker Status</span>
              <span className={`inline-block px-3 py-1 rounded text-xs font-medium ${status.worker_running ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800'}`}>
                {status.worker_running ? '● Running' : '● Stopped'}
              </span>
            </div>
            <div className="flex justify-between items-center">
              <span className="text-gray-700">Retention Period</span>
              <span className="text-gray-900 font-medium">{status.retention_days} days</span>
            </div>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-gray-700">Total Backups</span>
            <Database className="w-5 h-5 text-blue-600" />
          </div>
          <div className="text-3xl font-bold text-gray-900">{status.total_backups}</div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-gray-700">Completed</span>
            <CheckCircle className="w-5 h-5 text-green-600" />
          </div>
          <div className="text-3xl font-bold text-gray-900">{status.completed_backups}</div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-gray-700">Failed</span>
            <AlertTriangle className="w-5 h-5 text-red-600" />
          </div>
          <div className="text-3xl font-bold text-gray-900">{status.failed_backups}</div>
        </div>

        <div className="bg-white border border-gray-200 rounded-lg p-6">
          <div className="flex items-center justify-between mb-3">
            <span className="text-sm font-medium text-gray-700">Latest Size</span>
            <Activity className="w-5 h-5 text-purple-600" />
          </div>
          <div className="text-2xl font-bold text-gray-900">{formatBytes(status.last_backup_size_bytes)}</div>
        </div>
      </div>

      <div className="bg-white border border-gray-200 rounded-lg p-6">
        <h2 className="text-lg font-bold mb-4 flex items-center gap-2">
          <Clock className="w-5 h-5" />
          Latest Backup
        </h2>
        {status.latest_backup_age_minutes ? (
          <div>
            <p className="text-gray-700">
              {status.latest_backup_age_minutes < 60
                ? `${status.latest_backup_age_minutes} minutes ago`
                : status.latest_backup_age_minutes < 24 * 60
                ? `${Math.floor(status.latest_backup_age_minutes / 60)} hours ago`
                : `${Math.floor(status.latest_backup_age_minutes / (24 * 60))} days ago`}
            </p>
            {status.latest_backup_age_minutes > 24 * 60 && (
              <p className="text-yellow-700 mt-2 text-sm">⚠️ No backup in the last 24 hours</p>
            )}
          </div>
        ) : (
          <p className="text-gray-500">No backups created yet</p>
        )}
      </div>

      <div className="bg-blue-50 border border-blue-200 rounded-lg p-6">
        <h3 className="font-bold text-blue-900 mb-2">Recovery Information</h3>
        <ul className="text-sm text-blue-800 space-y-1">
          <li>• Backups are compressed with gzip and include checksums for integrity</li>
          <li>• Backups are created daily at 2 AM</li>
          <li>• Backups older than {status.retention_days} days are automatically deleted</li>
          <li>• You can manually create backups and restore from any backup</li>
          <li>• Database is backed up before restore (dso.db.old)</li>
        </ul>
      </div>
    </div>
  )
}

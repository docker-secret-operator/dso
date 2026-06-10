'use client'

import { useEffect, useState } from 'react'
import { Download, Trash2, RotateCcw, Plus, ChevronLeft, ChevronRight } from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'

interface Backup {
  id: string
  filename: string
  size_bytes: number
  checksum: string
  backup_type: string
  status: string
  duration_ms: number
  error_msg?: string
  created_at: string
  completed_at?: string
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
}

function formatDate(dateStr: string) {
  return new Date(dateStr).toLocaleString()
}

function getStatusColor(status: string) {
  const colors = {
    completed: 'bg-green-100 text-green-800',
    running: 'bg-blue-100 text-blue-800',
    failed: 'bg-red-100 text-red-800',
  }
  return colors[status as keyof typeof colors] || 'bg-gray-100 text-gray-800'
}

export default function BackupsPage() {
  const [backups, setBackups] = useState<Backup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [creatingBackup, setCreatingBackup] = useState(false)
  const [actingBackup, setActingBackup] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    fetchBackups()
  }, [page])

  const fetchBackups = async () => {
    setLoading(true)
    try {
      const offset = (page - 1) * pageSize
      const response = await fetch(`/api/backups?limit=${pageSize}&offset=${offset}`)
      if (!response.ok) {
        if (response.status === 403) {
          router.push('/login')
          return
        }
        throw new Error('Failed to fetch backups')
      }
      const data = await response.json()
      setBackups(data || [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error')
    } finally {
      setLoading(false)
    }
  }

  const handleCreateBackup = async () => {
    setCreatingBackup(true)
    try {
      const response = await fetch('/api/backups', { method: 'POST' })
      if (!response.ok) throw new Error('Failed to create backup')
      alert('Backup creation started')
      fetchBackups()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to create backup')
    } finally {
      setCreatingBackup(false)
    }
  }

  const handleDelete = async (backupId: string) => {
    if (!confirm('Delete this backup?')) return
    setActingBackup(backupId)
    try {
      const response = await fetch(`/api/backups/${backupId}`, { method: 'DELETE' })
      if (!response.ok) throw new Error('Failed to delete backup')
      fetchBackups()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete backup')
    } finally {
      setActingBackup(null)
    }
  }

  const handleRestore = async (backupId: string) => {
    if (!confirm('Restore from this backup? The current database will be backed up.')) return
    setActingBackup(backupId)
    try {
      const response = await fetch(`/api/backups/${backupId}/restore`, { method: 'POST' })
      if (!response.ok) throw new Error('Failed to restore backup')
      alert('Database restored successfully')
      fetchBackups()
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to restore backup')
    } finally {
      setActingBackup(null)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Backup Management</h1>
        <div className="flex gap-2">
          <button
            onClick={handleCreateBackup}
            disabled={creatingBackup}
            className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50 flex items-center gap-2"
          >
            <Plus className="w-4 h-4" />
            Create Backup
          </button>
          <Link href="/backups/recovery" className="px-4 py-2 bg-gray-200 text-gray-800 rounded-lg hover:bg-gray-300">
            Recovery Dashboard
          </Link>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 text-red-700">{error}</div>
      )}

      <div className="bg-white border border-gray-200 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-gray-500">Loading backups...</div>
        ) : backups.length === 0 ? (
          <div className="p-8 text-center text-gray-500">No backups yet</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Filename</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Size</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Type</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Duration</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Created</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {backups.map((backup) => (
                  <tr key={backup.id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 text-sm font-mono text-gray-900">{backup.filename}</td>
                    <td className="px-6 py-4 text-sm text-gray-700">{formatBytes(backup.size_bytes)}</td>
                    <td className="px-6 py-4 text-sm">
                      <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${getStatusColor(backup.status)}`}>
                        {backup.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700 capitalize">{backup.backup_type}</td>
                    <td className="px-6 py-4 text-sm text-gray-700">{backup.duration_ms}ms</td>
                    <td className="px-6 py-4 text-sm text-gray-700 whitespace-nowrap">{formatDate(backup.created_at)}</td>
                    <td className="px-6 py-4 text-sm flex gap-2">
                      {backup.status === 'completed' && (
                        <>
                          <a
                            href={`/api/backups/${backup.id}/download`}
                            className="text-blue-600 hover:text-blue-800"
                            title="Download"
                          >
                            <Download className="w-4 h-4" />
                          </a>
                          <button
                            onClick={() => handleRestore(backup.id)}
                            disabled={actingBackup === backup.id}
                            className="text-green-600 hover:text-green-800 disabled:opacity-50"
                            title="Restore"
                          >
                            <RotateCcw className="w-4 h-4" />
                          </button>
                        </>
                      )}
                      <button
                        onClick={() => handleDelete(backup.id)}
                        disabled={actingBackup === backup.id}
                        className="text-red-600 hover:text-red-800 disabled:opacity-50"
                        title="Delete"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
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
              disabled={backups.length < pageSize}
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

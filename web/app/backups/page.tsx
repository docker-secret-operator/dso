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
    completed: 'bg-emerald-500/15 text-emerald-300',
    running: 'bg-blue-500/15 text-blue-300',
    failed: 'bg-red-500/15 text-red-300',
  }
  return colors[status as keyof typeof colors] || 'bg-slate-700/30 text-slate-400'
}

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export default function BackupsPage() {
  const [backups, setBackups] = useState<Backup[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionMsg, setActionMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(10)
  const [creatingBackup, setCreatingBackup] = useState(false)
  const [actingBackup, setActingBackup] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const [confirmRestore, setConfirmRestore] = useState<string | null>(null)
  const router = useRouter()

  useEffect(() => {
    fetchBackups()
  }, [page])

  const fetchBackups = async () => {
    setLoading(true)
    try {
      const offset = (page - 1) * pageSize
      const response = await fetch(`/api/backups?limit=${pageSize}&offset=${offset}`, {
        headers: getAuthHeaders(),
      })
      if (!response.ok) {
        if (response.status === 401 || response.status === 403) {
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
    setActionMsg(null)
    try {
      const response = await fetch('/api/backups', { method: 'POST', headers: getAuthHeaders() })
      if (!response.ok) throw new Error('Failed to create backup')
      setActionMsg({ type: 'success', text: 'Backup creation started' })
      fetchBackups()
    } catch (err) {
      setActionMsg({ type: 'error', text: err instanceof Error ? err.message : 'Failed to create backup' })
    } finally {
      setCreatingBackup(false)
    }
  }

  const handleDelete = async (backupId: string) => {
    setActingBackup(backupId)
    setActionMsg(null)
    try {
      const response = await fetch(`/api/backups/${backupId}`, { method: 'DELETE', headers: getAuthHeaders() })
      if (!response.ok) throw new Error('Failed to delete backup')
      setActionMsg({ type: 'success', text: 'Backup deleted' })
      fetchBackups()
    } catch (err) {
      setActionMsg({ type: 'error', text: err instanceof Error ? err.message : 'Failed to delete backup' })
    } finally {
      setActingBackup(null)
    }
  }

  const handleRestore = async (backupId: string) => {
    setActingBackup(backupId)
    setActionMsg(null)
    try {
      const response = await fetch(`/api/backups/${backupId}/restore`, { method: 'POST', headers: getAuthHeaders() })
      if (!response.ok) throw new Error('Failed to restore backup')
      setActionMsg({ type: 'success', text: 'Database restore initiated — the system will reload shortly' })
      fetchBackups()
    } catch (err) {
      setActionMsg({ type: 'error', text: err instanceof Error ? err.message : 'Failed to restore backup' })
    } finally {
      setActingBackup(null)
    }
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-slate-100">Backup Management</h1>
        <div className="flex gap-2">
          <button
            onClick={handleCreateBackup}
            disabled={creatingBackup}
            className="px-4 py-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-500 disabled:opacity-50 flex items-center gap-2 transition-colors text-sm"
          >
            <Plus className="w-4 h-4" />
            Create Backup
          </button>
          <Link href="/backups/recovery" className="px-4 py-2 bg-white/5 border border-white/[0.09] text-slate-300 rounded-lg hover:bg-white/10 text-sm transition-colors">
            Recovery Dashboard
          </Link>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-red-300 text-sm">{error}</div>
      )}

      {/* Inline delete confirm */}
      {confirmDelete && (
        <div className="rounded-lg border border-red-500/25 bg-red-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-red-300">Delete this backup? This action cannot be undone.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button onClick={() => setConfirmDelete(null)} className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors">Cancel</button>
            <button onClick={() => { const id = confirmDelete; setConfirmDelete(null); handleDelete(id) }} className="px-3 py-1.5 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors">Delete</button>
          </div>
        </div>
      )}

      {/* Inline restore confirm */}
      {confirmRestore && (
        <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-amber-300">Restore from this backup? This will overwrite the current database. A safety backup will be created first.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button onClick={() => setConfirmRestore(null)} className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors">Cancel</button>
            <button onClick={() => { const id = confirmRestore; setConfirmRestore(null); handleRestore(id) }} className="px-3 py-1.5 text-xs rounded-lg bg-amber-600 text-white hover:bg-amber-500 transition-colors">Restore</button>
          </div>
        </div>
      )}

      <div className="bg-slate-800/50 border border-slate-700/50 rounded-lg overflow-hidden">
        {loading ? (
          <div className="p-8 text-center text-slate-400">Loading backups...</div>
        ) : backups.length === 0 ? (
          <div className="p-8 text-center text-slate-400">No backups yet</div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead className="bg-slate-900/50 border-b border-slate-700/50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Filename</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Size</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Status</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Type</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Duration</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Created</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-300 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-700/50">
                {backups.map((backup) => (
                  <tr key={backup.id} className="hover:bg-slate-900/50">
                    <td className="px-6 py-4 text-sm font-mono text-slate-100">{backup.filename}</td>
                    <td className="px-6 py-4 text-sm text-slate-300">{formatBytes(backup.size_bytes)}</td>
                    <td className="px-6 py-4 text-sm">
                      <span className={`inline-block px-3 py-1 rounded-full text-xs font-medium ${getStatusColor(backup.status)}`}>
                        {backup.status}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-slate-300 capitalize">{backup.backup_type}</td>
                    <td className="px-6 py-4 text-sm text-slate-300">{backup.duration_ms}ms</td>
                    <td className="px-6 py-4 text-sm text-slate-300 whitespace-nowrap">{formatDate(backup.created_at)}</td>
                    <td className="px-6 py-4 text-sm flex gap-2">
                      {backup.status === 'completed' && (
                        <>
                          <a
                            href={`/api/backups/${backup.id}/download`}
                            className="text-blue-400 hover:text-blue-300 transition-colors"
                            title="Download"
                          >
                            <Download className="w-4 h-4" />
                          </a>
                          <button
                            onClick={() => setConfirmRestore(backup.id)}
                            disabled={actingBackup === backup.id}
                            className="text-emerald-400 hover:text-emerald-300 disabled:opacity-50 transition-colors"
                            title="Restore"
                          >
                            <RotateCcw className="w-4 h-4" />
                          </button>
                        </>
                      )}
                      <button
                        onClick={() => setConfirmDelete(backup.id)}
                        disabled={actingBackup === backup.id}
                        className="text-red-400 hover:text-red-300 disabled:opacity-50 transition-colors"
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

        <div className="flex items-center justify-between px-6 py-4 bg-slate-900/50 border-t border-slate-700/50">
          <div className="text-sm text-slate-400">
            Page <span className="font-medium">{page}</span>
          </div>
          <div className="flex gap-2">
            <button
              onClick={() => setPage(Math.max(1, page - 1))}
              disabled={page === 1}
              className="p-2 hover:bg-slate-700/50 rounded disabled:opacity-50"
            >
              <ChevronLeft className="w-5 h-5" />
            </button>
            <button
              onClick={() => setPage(page + 1)}
              disabled={backups.length < pageSize}
              className="p-2 hover:bg-slate-700/50 rounded disabled:opacity-50"
            >
              <ChevronRight className="w-5 h-5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

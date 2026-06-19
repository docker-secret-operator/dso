'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, LogOut, Trash2 } from 'lucide-react'
import { apiClient, type Session } from '@/lib/api-client'

function truncateUA(ua: string): string {
  return ua.length > 60 ? ua.slice(0, 57) + '…' : ua
}

export default function SessionsPage() {
  const queryClient = useQueryClient()
  const [confirmRevokeAll, setConfirmRevokeAll] = useState(false)

  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => apiClient.listSessions(),
    refetchInterval: 30_000,
  })

  const sessions: (Session & { is_current?: boolean })[] = data?.sessions ?? []

  const revokeMutation = useMutation({
    mutationFn: (id: string) => apiClient.revokeSession(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['sessions'] }),
  })

  const revokeAllMutation = useMutation({
    mutationFn: () => apiClient.revokeAllSessions(),
    onSuccess: () => {
      // Clear token and redirect to login — all sessions including current are gone
      if (typeof window !== 'undefined') {
        localStorage.removeItem('dso_api_token')
        window.location.href = '/login'
      }
    },
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-slate-100">Active Sessions</h1>
          <p className="text-sm text-slate-400 mt-1">Devices currently signed in to your account</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 disabled:opacity-50 transition-colors"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={() => setConfirmRevokeAll(true)}
            disabled={revokeAllMutation.isPending || sessions.length === 0}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-lg border border-red-500/30 text-red-400 hover:bg-red-500/10 disabled:opacity-50 transition-colors"
          >
            <LogOut className="h-4 w-4" />
            {revokeAllMutation.isPending ? 'Signing out…' : 'Sign out all devices'}
          </button>
        </div>
      </div>

      {/* Inline confirm instead of browser confirm() */}
      {confirmRevokeAll && (
        <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 px-4 py-3 flex items-center justify-between gap-4">
          <p className="text-sm text-amber-300">Sign out from all devices? You will be redirected to login.</p>
          <div className="flex gap-2 flex-shrink-0">
            <button
              onClick={() => setConfirmRevokeAll(false)}
              className="px-3 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-300 hover:bg-white/5 transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={() => { setConfirmRevokeAll(false); revokeAllMutation.mutate() }}
              className="px-3 py-1.5 text-xs rounded-lg bg-red-600 text-white hover:bg-red-500 transition-colors"
            >
              Confirm sign out
            </button>
          </div>
        </div>
      )}

      {isError && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">
          Failed to load sessions.
        </div>
      )}

      {revokeMutation.isError && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-sm text-red-300">
          Failed to revoke session.
        </div>
      )}

      {isLoading ? (
        <div className="space-y-2">
          {[...Array(3)].map((_, i) => <div key={i} className="h-16 rounded-lg bg-white/5 animate-pulse" />)}
        </div>
      ) : sessions.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center space-y-2">
          <p className="text-slate-500">No active sessions.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-white/[0.07] overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-[#0f1015] border-b border-white/[0.07]">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Device / Browser</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">IP Address</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Created</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Last Active</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-slate-400 uppercase tracking-wider">Expires</th>
                <th className="px-4 py-3 text-right text-xs font-medium text-slate-400 uppercase tracking-wider">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/[0.05]">
              {sessions.map(s => (
                <tr key={s.id} className="hover:bg-white/[0.03] transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <span className="text-slate-400 font-mono text-xs truncate max-w-[260px]" title={s.user_agent}>
                        {truncateUA(s.user_agent || 'Unknown')}
                      </span>
                      {s.is_current && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-emerald-500/15 text-emerald-400 shrink-0">
                          current
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{s.ip_address || '—'}</td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {new Date(s.created_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {new Date(s.last_activity).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">
                    {new Date(s.expires_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => revokeMutation.mutate(s.id)}
                      disabled={revokeMutation.isPending}
                      className="flex items-center gap-1 ml-auto px-2.5 py-1.5 text-xs rounded-lg border border-white/[0.09] text-slate-400 hover:text-red-400 hover:border-red-500/30 hover:bg-red-500/10 disabled:opacity-50 transition-colors"
                      title="Revoke this session"
                    >
                      <Trash2 className="h-3 w-3" />
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

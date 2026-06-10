'use client'

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, LogOut, Trash2 } from 'lucide-react'
import { apiClient, type Session } from '@/lib/api-client'

function truncateUA(ua: string): string {
  return ua.length > 60 ? ua.slice(0, 57) + '…' : ua
}

export default function SessionsPage() {
  const queryClient = useQueryClient()

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
          <h1 className="text-2xl font-semibold">Active Sessions</h1>
          <p className="text-sm text-muted-foreground mt-1">Devices currently signed in to your account</p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </button>
          <button
            onClick={() => {
              if (confirm('Sign out from all devices? You will be redirected to login.')) {
                revokeAllMutation.mutate()
              }
            }}
            disabled={revokeAllMutation.isPending || sessions.length === 0}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-red-300 text-red-600 hover:bg-red-50 disabled:opacity-50"
          >
            <LogOut className="h-4 w-4" />
            {revokeAllMutation.isPending ? 'Signing out…' : 'Sign out all devices'}
          </button>
        </div>
      </div>

      {isError && (
        <div className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
          Failed to load sessions.
        </div>
      )}

      {revokeMutation.isError && (
        <div className="rounded-md bg-red-50 border border-red-200 px-4 py-3 text-sm text-red-700">
          Failed to revoke session.
        </div>
      )}

      {isLoading ? (
        <div className="space-y-2">
          {[...Array(3)].map((_, i) => <div key={i} className="h-16 rounded-md bg-muted animate-pulse" />)}
        </div>
      ) : sessions.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center space-y-2">
          <p className="text-muted-foreground">No active sessions.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-3 text-left font-medium">Device / Browser</th>
                <th className="px-4 py-3 text-left font-medium">IP Address</th>
                <th className="px-4 py-3 text-left font-medium">Created</th>
                <th className="px-4 py-3 text-left font-medium">Last Active</th>
                <th className="px-4 py-3 text-left font-medium">Expires</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {sessions.map(s => (
                <tr key={s.id} className="hover:bg-muted/30 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground font-mono text-xs truncate max-w-[260px]" title={s.user_agent}>
                        {truncateUA(s.user_agent || 'Unknown')}
                      </span>
                      {s.is_current && (
                        <span className="inline-flex px-1.5 py-0.5 rounded text-xs font-medium bg-green-100 text-green-700 shrink-0">
                          current
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{s.ip_address || '—'}</td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {new Date(s.created_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {new Date(s.last_activity).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {new Date(s.expires_at).toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button
                      onClick={() => revokeMutation.mutate(s.id)}
                      disabled={revokeMutation.isPending}
                      className="flex items-center gap-1 ml-auto px-2.5 py-1.5 text-xs rounded border border-border hover:bg-muted disabled:opacity-50"
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

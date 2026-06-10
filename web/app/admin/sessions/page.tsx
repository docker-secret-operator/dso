'use client'

import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Trash2 } from 'lucide-react'
import { apiClient, type Session } from '@/lib/api-client'

type AdminSession = Session & { username?: string }

function truncateUA(ua: string): string {
  return ua.length > 55 ? ua.slice(0, 52) + '…' : ua
}

export default function AdminSessionsPage() {
  const queryClient = useQueryClient()

  const { data, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ['admin-sessions'],
    queryFn: () => apiClient.listAdminSessions(),
    refetchInterval: 30_000,
  })

  const sessions: AdminSession[] = data?.sessions ?? []

  const revokeMutation = useMutation({
    mutationFn: (id: string) => apiClient.adminRevokeSession(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['admin-sessions'] }),
  })

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">All Active Sessions</h1>
          <p className="text-sm text-muted-foreground mt-1">Admin view of all user sessions across the system</p>
        </div>
        <button
          onClick={() => refetch()}
          disabled={isFetching}
          className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
          Refresh
        </button>
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
          {[...Array(4)].map((_, i) => <div key={i} className="h-16 rounded-md bg-muted animate-pulse" />)}
        </div>
      ) : sessions.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center">
          <p className="text-muted-foreground">No active sessions.</p>
        </div>
      ) : (
        <div className="rounded-lg border border-border overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-4 py-3 text-left font-medium">User</th>
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
                  <td className="px-4 py-3 font-mono text-xs">{s.username || s.user_id}</td>
                  <td className="px-4 py-3 text-muted-foreground font-mono text-xs truncate max-w-[240px]" title={s.user_agent}>
                    {truncateUA(s.user_agent || 'Unknown')}
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
                      onClick={() => {
                        if (confirm('Revoke this session?')) revokeMutation.mutate(s.id)
                      }}
                      disabled={revokeMutation.isPending}
                      className="flex items-center gap-1 ml-auto px-2.5 py-1.5 text-xs rounded border border-border hover:bg-muted disabled:opacity-50"
                      title="Revoke session"
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

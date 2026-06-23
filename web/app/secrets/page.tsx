'use client'

import { useState, useMemo, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient, type Secret } from '@/lib/api-client'
import * as auditApi from '@/lib/api/audit'
import { Pagination } from '@/components/common/Pagination'
import { PageHeader, Card, Badge, StatusBadge, Button, Input, EmptyState, Skeleton } from '@/components/ui-modern'
import {
  RefreshCw, RotateCcw, Database, Search, ChevronUp, ChevronDown,
  ChevronsUpDown, Shield, X, Server, Clock, Hash,
} from 'lucide-react'

// ── Sort helpers ──────────────────────────────────────────────────────────────

type SortKey = 'name' | 'provider' | 'status' | 'last_rotated' | 'next_rotation'
type SortDir = 'asc' | 'desc'

function SortIcon({ col, active, dir }: { col: string; active: boolean; dir: SortDir }) {
  if (!active) return <ChevronsUpDown className="w-3 h-3 text-slate-700 ml-1 inline" />
  return dir === 'asc'
    ? <ChevronUp className="w-3 h-3 text-indigo-400 ml-1 inline" />
    : <ChevronDown className="w-3 h-3 text-indigo-400 ml-1 inline" />
}

// ── Detail drawer ─────────────────────────────────────────────────────────────

function SecretDrawer({ secret, onClose }: { secret: Secret; onClose: () => void }) {
  const qc = useQueryClient()
  const [rotating, setRotating] = useState(false)
  const [rotateError, setRotateError] = useState<string | null>(null)
  const [rotateSuccess, setRotateSuccess] = useState(false)

  const { data: auditData } = useQuery({
    queryKey: ['secret-audit', secret.name],
    queryFn: () => auditApi.getAuditEvents({ resource_id: secret.name, resource_type: 'secret', limit: 3 }),
  })
  const recentAudit = auditData?.events ?? []

  const rotate = async () => {
    setRotating(true)
    setRotateError(null)
    setRotateSuccess(false)
    try {
      await apiClient.rotateSecret(secret.name)
      await qc.invalidateQueries({ queryKey: ['secrets'] })
      setRotateSuccess(true)
    } catch (e: any) {
      setRotateError(e?.message ?? 'Rotation failed')
    } finally {
      setRotating(false)
    }
  }

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 backdrop-blur-sm z-40 animate-fade-in"
        onClick={onClose}
      />

      {/* Drawer */}
      <div className="fixed right-0 top-0 h-full w-[420px] max-w-full bg-[#111827] border-l border-white/[0.09] z-50 flex flex-col animate-slide-in shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.07]">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded-lg bg-blue-500/15 flex items-center justify-center">
              <Shield className="w-3.5 h-3.5 text-blue-400" />
            </div>
            <div>
              <p className="text-sm font-semibold text-slate-100 font-mono">{secret.name}</p>
              <p className="text-[11px] text-slate-500 capitalize">{secret.provider}</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg text-slate-600 hover:text-slate-300 hover:bg-white/5 transition-colors"
            aria-label="Close"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5">

          {/* Status row */}
          <div className="flex items-center gap-3">
            <StatusBadge status={secret.status} label={secret.status.toUpperCase()} />
            {secret.rotation_strategy && (
              <Badge variant="outline">{secret.rotation_strategy}</Badge>
            )}
            {secret.version && (
              <span className="text-xs font-mono text-slate-600">v{secret.version}</span>
            )}
          </div>

          {/* Details grid */}
          <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
            {[
              {
                label: 'Provider',
                value: <span className="capitalize">{secret.provider}</span>,
                icon: <Database className="w-3.5 h-3.5" />,
              },
              {
                label: 'Last Rotated',
                value: secret.last_rotated
                  ? new Date(secret.last_rotated).toLocaleString()
                  : <span className="text-slate-600">Never</span>,
                icon: <Clock className="w-3.5 h-3.5" />,
              },
              {
                label: 'Next Rotation',
                value: secret.next_rotation
                  ? new Date(secret.next_rotation).toLocaleString()
                  : <span className="text-slate-600">Not scheduled</span>,
                icon: <Clock className="w-3.5 h-3.5" />,
              },
              {
                label: 'Containers',
                value: secret.container_count != null ? (
                  <a
                    href={`/discovery?secret=${encodeURIComponent(secret.name)}`}
                    className="text-blue-400 hover:text-blue-300 hover:underline transition-colors"
                  >
                    {secret.container_count} container{secret.container_count === 1 ? '' : 's'} →
                  </a>
                ) : <span className="text-slate-600">—</span>,
                icon: <Server className="w-3.5 h-3.5" />,
              },
              {
                label: 'Version',
                value: secret.version
                  ? <span className="font-mono">{secret.version}</span>
                  : <span className="text-slate-600">—</span>,
                icon: <Hash className="w-3.5 h-3.5" />,
              },
            ].map(row => (
              <div key={row.label} className="flex items-center gap-3 px-4 py-3">
                <span className="text-slate-600">{row.icon}</span>
                <span className="text-xs text-slate-500 w-28 flex-shrink-0">{row.label}</span>
                <span className="text-xs text-slate-300">{row.value}</span>
              </div>
            ))}
          </div>

          {/* Recent audit activity */}
          {recentAudit.length > 0 && (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Recent Activity</p>
                <a
                  href={`/audit?q=${encodeURIComponent(secret.name)}`}
                  className="text-[11px] text-blue-400/70 hover:text-blue-400 transition-colors"
                >
                  View all →
                </a>
              </div>
              <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
                {recentAudit.map(ev => (
                  <div key={ev.id} className="px-3 py-2 space-y-0.5">
                    <p className="text-xs text-slate-300 truncate">{ev.action}</p>
                    <p className="text-[11px] text-slate-600">
                      {ev.actor} · {new Date(ev.timestamp).toLocaleString()}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Rotation feedback */}
          {rotateSuccess && (
            <div className="rounded-lg bg-emerald-500/10 border border-emerald-500/20 px-4 py-3 text-xs text-emerald-400">
              Rotation triggered successfully.
            </div>
          )}
          {rotateError && (
            <div className="rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-3 text-xs text-red-400">
              {rotateError}
            </div>
          )}
        </div>

        {/* Footer action */}
        <div className="border-t border-white/[0.07] p-5">
          <Button
            variant="secondary"
            className="w-full"
            onClick={rotate}
            isLoading={rotating}
            disabled={rotating}
          >
            <RotateCcw className="w-3.5 h-3.5" />
            Rotate Secret
          </Button>
        </div>
      </div>
    </>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

const PAGE_SIZE = 50

export default function SecretsPage() {
  const qc = useQueryClient()
  const [sortKey, setSortKey]     = useState<SortKey>('name')
  const [sortDir, setSortDir]     = useState<SortDir>('asc')
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch]       = useState('')
  const [statusFilter, setStatus] = useState('')
  const [page, setPage]           = useState(1)
  const [selected, setSelected]   = useState<Secret | null>(null)
  const [globalError, setGlobalError] = useState<string | null>(null)

  // Debounce search input 300ms before hitting the server
  useEffect(() => {
    const t = setTimeout(() => { setSearch(searchInput); setPage(1) }, 300)
    return () => clearTimeout(t)
  }, [searchInput])

  const { data: secretsPage, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['secrets', page, PAGE_SIZE, search, statusFilter, sortKey, sortDir],
    queryFn: () => apiClient.getSecretsPage({
      page,
      pageSize: PAGE_SIZE,
      search: search || undefined,
      status: statusFilter || undefined,
      sortBy: sortKey,
      sortOrder: sortDir,
    }),
    refetchInterval: 30_000,
    placeholderData: (prev) => prev,
  })

  const secrets = secretsPage?.items ?? []
  const total   = secretsPage?.total ?? 0

  const rotateMutation = useMutation({
    mutationFn: (name: string) => apiClient.rotateSecret(name),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['secrets'] }) },
    onError: (e: any) => setGlobalError(e?.message ?? 'Rotation failed'),
  })

  // Sort toggle — resets page since sorted order changes results
  const handleSort = (key: SortKey) => {
    if (sortKey === key) setSortDir(d => d === 'asc' ? 'desc' : 'asc')
    else { setSortKey(key); setSortDir('asc') }
    setPage(1)
  }

  const ThCell = ({ col, label }: { col: SortKey; label: string }) => (
    <th
      className="px-4 py-3 text-left text-[11px] font-semibold text-slate-500 uppercase tracking-wider cursor-pointer hover:text-slate-300 transition-colors select-none whitespace-nowrap"
      onClick={() => handleSort(col)}
    >
      {label}
      <SortIcon col={col} active={sortKey === col} dir={sortDir} />
    </th>
  )

  return (
    <div className="p-6 space-y-5">
      <PageHeader
        title="Secrets"
        description="Manage and rotate secrets across all providers."
        actions={
          <Button variant="ghost" size="sm" onClick={() => refetch()} disabled={isFetching}>
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        }
      />

      {globalError && (
        <div className="rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-3 text-sm text-red-400 flex items-center justify-between">
          {globalError}
          <button onClick={() => setGlobalError(null)}><X className="w-4 h-4" /></button>
        </div>
      )}

      {/* Filter bar */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-600" />
          <input
            value={searchInput}
            onChange={e => setSearchInput(e.target.value)}
            placeholder="Search by name or provider…"
            className="w-full pl-9 pr-3 py-2 text-sm bg-[#1a1d24] border border-white/[0.09] rounded-lg text-slate-300 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20"
          />
          {searchInput && (
            <button onClick={() => { setSearchInput(''); setSearch(''); setPage(1) }} className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-600 hover:text-slate-400">
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
        <select
          value={statusFilter}
          onChange={e => { setStatus(e.target.value); setPage(1) }}
          className="px-3 py-2 text-sm bg-[#1a1d24] border border-white/[0.09] rounded-lg text-slate-400 focus:outline-none focus:border-indigo-500/50"
        >
          <option value="">All statuses</option>
          <option value="ok">OK</option>
          <option value="pending">Pending</option>
          <option value="error">Error</option>
        </select>
        {(search || statusFilter) && (
          <button
            onClick={() => { setSearchInput(''); setSearch(''); setStatus(''); setPage(1) }}
            className="text-xs text-slate-500 hover:text-slate-300 transition-colors"
          >
            Clear
          </button>
        )}
        <span className="ml-auto text-xs text-slate-600 tabular-nums">{secrets.length} of {total}</span>
      </div>

      {/* Table */}
      <Card className="overflow-hidden">
        {isLoading ? (
          <div className="p-6 space-y-3">
            <Skeleton className="h-10 w-full rounded" count={5} />
          </div>
        ) : total === 0 && !isLoading ? (
          <EmptyState
            icon={<Database className="w-5 h-5" />}
            title="No secrets found"
            description="Secrets appear here once the DSO agent has synced them from a provider."
          />
        ) : secrets.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Search className="w-5 h-5" />}
            title="No matches"
            description="Try adjusting your search or filter."
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/[0.07] bg-white/[0.02]">
                  <ThCell col="name"         label="Name" />
                  <ThCell col="provider"     label="Provider" />
                  <ThCell col="status"       label="Status" />
                  <ThCell col="last_rotated" label="Last Rotated" />
                  <ThCell col="next_rotation" label="Next Rotation" />
                  <th className="px-4 py-3 text-right text-[11px] font-semibold text-slate-500 uppercase tracking-wider">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/[0.04]">
                {secrets.map(secret => (
                  <tr
                    key={secret.name}
                    className="hover:bg-white/[0.03] transition-colors cursor-pointer"
                    onClick={() => setSelected(secret)}
                  >
                    <td className="px-4 py-3 font-mono text-xs text-slate-200">{secret.name}</td>
                    <td className="px-4 py-3 text-slate-400 capitalize">{secret.provider}</td>
                    <td className="px-4 py-3">
                      <StatusBadge status={secret.status} />
                    </td>
                    <td className="px-4 py-3 text-slate-500 text-xs whitespace-nowrap">
                      {secret.last_rotated ? new Date(secret.last_rotated).toLocaleString() : <span className="text-slate-700">—</span>}
                    </td>
                    <td className="px-4 py-3 text-xs whitespace-nowrap">
                      {secret.next_rotation ? (
                        <span className={
                          new Date(secret.next_rotation).getTime() - Date.now() < 7 * 86400 * 1000
                            ? 'text-amber-400' : 'text-slate-500'
                        }>
                          {new Date(secret.next_rotation).toLocaleString()}
                        </span>
                      ) : <span className="text-slate-700">—</span>}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={e => {
                          e.stopPropagation()
                          rotateMutation.mutate(secret.name)
                        }}
                        disabled={rotateMutation.isPending && rotateMutation.variables === secret.name}
                        className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-md border border-white/[0.09] text-slate-400 hover:text-slate-200 hover:border-white/20 hover:bg-white/5 transition-all disabled:opacity-50"
                        title="Rotate secret"
                        aria-label={`Rotate ${secret.name}`}
                      >
                        <RotateCcw className={`w-3 h-3 ${rotateMutation.isPending && rotateMutation.variables === secret.name ? 'animate-spin' : ''}`} />
                        Rotate
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div className="px-4">
              <Pagination page={page} pageSize={PAGE_SIZE} total={total} onPageChange={setPage} urlState={false} />
            </div>
          </div>
        )}
      </Card>

      {/* Detail drawer */}
      {selected && (
        <SecretDrawer secret={selected} onClose={() => setSelected(null)} />
      )}
    </div>
  )
}

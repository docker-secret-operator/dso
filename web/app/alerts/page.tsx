'use client'

import { useState, useCallback } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import Link from 'next/link'
import {
  AlertTriangle, CheckCircle, XCircle, Clock,
  ChevronLeft, ChevronRight, X, Bell,
} from 'lucide-react'
import { PageHeader, Card, StatusBadge, Badge, Button, EmptyState, Skeleton } from '@/components/ui-modern'
import { apiClient } from '@/lib/api-client'

// ── Helpers ───────────────────────────────────────────────────────────────────

function alertAge(createdAt: string) {
  const diffMs = Date.now() - new Date(createdAt).getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 60) return `${diffMins}m`
  const diffH = Math.floor(diffMins / 60)
  if (diffH < 24) return `${diffH}h ${diffMins % 60}m`
  return `${Math.floor(diffH / 24)}d`
}

function severityVariant(s: string): 'danger' | 'warning' | 'info' | 'default' {
  if (s === 'critical' || s === 'high') return 'danger'
  if (s === 'medium') return 'warning'
  if (s === 'low') return 'info'
  return 'default'
}

// ── Alert card ────────────────────────────────────────────────────────────────

interface AlertCardProps {
  alert: any
  onAck: (id: string) => void
  onResolve: (id: string) => void
  onSuppress: (id: string) => void
  acting: boolean
  actionError: string | null
}

function AlertCard({ alert, onAck, onResolve, onSuppress, acting, actionError }: AlertCardProps) {
  const isActive = alert.state === 'active'

  return (
    <div className="p-4 border-b border-white/[0.05] last:border-0 hover:bg-white/[0.02] transition-colors">
      <div className="flex items-start gap-4">
        {/* Severity indicator */}
        <div className="flex flex-col items-center gap-1.5 pt-0.5 flex-shrink-0">
          <span className={`w-2 h-2 rounded-full ${
            alert.severity === 'critical' ? 'bg-red-400 shadow-[0_0_6px_rgba(248,113,113,0.6)]' :
            alert.severity === 'high'     ? 'bg-orange-400' :
            alert.severity === 'medium'   ? 'bg-amber-400' : 'bg-blue-400'
          } ${isActive ? 'animate-pulse' : ''}`} />
        </div>

        {/* Content */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <p className="text-sm font-medium text-slate-200">{alert.message}</p>
            <Badge variant={severityVariant(alert.severity)} size="sm">{alert.severity}</Badge>
            <Badge
              variant={alert.state === 'active' ? 'danger' : alert.state === 'acknowledged' ? 'warning' : 'success'}
              size="sm"
            >
              {alert.state}
            </Badge>
          </div>

          <div className="flex items-center gap-4 mt-1.5 text-xs text-slate-600 flex-wrap">
            <span className="font-mono">{alert.metric}</span>
            <span>Value: <span className="text-slate-400">{alert.value?.toFixed(2)}</span></span>
            <span>Threshold: <span className="text-slate-400">{alert.threshold?.toFixed(2)}</span></span>
            <span className="flex items-center gap-1">
              <Clock className="w-3 h-3" />
              Active for {alertAge(alert.created_at)}
            </span>
            {alert.acknowledged_by && (
              <span>Ack by <span className="text-slate-400">{alert.acknowledged_by}</span></span>
            )}
          </div>

          {actionError && (
            <div className="mt-2 px-3 py-1.5 rounded-md bg-red-500/10 border border-red-500/20 text-xs text-red-400">
              {actionError}
            </div>
          )}

          {/* Actions — only for active */}
          {isActive && (
            <div className="flex items-center gap-2 mt-3">
              <button
                onClick={() => onAck(alert.id)}
                disabled={acting}
                className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-md border border-amber-500/30 text-amber-400 hover:bg-amber-500/10 transition-colors disabled:opacity-50"
              >
                <Clock className="w-3 h-3" />
                Acknowledge
              </button>
              <button
                onClick={() => onResolve(alert.id)}
                disabled={acting}
                className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-md border border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/10 transition-colors disabled:opacity-50"
              >
                <CheckCircle className="w-3 h-3" />
                Resolve
              </button>
              <button
                onClick={() => onSuppress(alert.id)}
                disabled={acting}
                className="inline-flex items-center gap-1.5 px-2.5 py-1.5 text-xs rounded-md border border-white/10 text-slate-500 hover:bg-white/5 transition-colors disabled:opacity-50"
              >
                <XCircle className="w-3 h-3" />
                Suppress 24h
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

const PAGE_SIZE = 20

export default function AlertsPage() {
  const qc = useQueryClient()
  const [page, setPage]           = useState(1)
  const [stateFilter, setStateFilter] = useState('')
  const [actingId, setActingId]   = useState<string | null>(null)
  const [errors, setErrors]       = useState<Record<string, string>>({})

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['alerts', stateFilter, page],
    queryFn: async () => {
      const params: any = { limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE }
      if (stateFilter) params.state = stateFilter
      const res = await apiClient.getAlerts(params)
      return res
    },
    refetchInterval: 15000,
  })

  const { data: statsData } = useQuery({
    queryKey: ['alerts-stats'],
    queryFn: async () => {
      const res = await apiClient.getAlerts({ limit: 1, summary: true } as any)
      return res
    },
    refetchInterval: 30000,
  })

  const alerts: any[] = (data as any)?.alerts ?? []
  const total: number = (data as any)?.total  ?? 0

  const mutate = useCallback(async (id: string, fn: () => Promise<any>) => {
    setActingId(id)
    setErrors(prev => { const n = { ...prev }; delete n[id]; return n })
    try {
      await fn()
      await qc.invalidateQueries({ queryKey: ['alerts'] })
    } catch (e: any) {
      setErrors(prev => ({ ...prev, [id]: e?.message ?? 'Action failed' }))
    } finally {
      setActingId(null)
    }
  }, [qc])

  const authHeader = (): Record<string, string> => {
    const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  const handleAck      = (id: string) => mutate(id, () => fetch(`/api/alerts/${id}/acknowledge`, { method: 'POST', headers: authHeader() }).then(r => { if (!r.ok) throw new Error('Failed to acknowledge') }))
  const handleResolve  = (id: string) => mutate(id, () => fetch(`/api/alerts/${id}/resolve`,     { method: 'POST', headers: authHeader() }).then(r => { if (!r.ok) throw new Error('Failed to resolve') }))
  const handleSuppress = (id: string) => mutate(id, () =>
    fetch(`/api/alerts/${id}/suppress`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeader() },
      body: JSON.stringify({ suppress_until: new Date(Date.now() + 86400 * 1000).toISOString() }),
    }).then(r => { if (!r.ok) throw new Error('Failed to suppress') })
  )

  // Summary counts: prefer server-side stats when available, fall back to current page data
  const statsFromServer = (statsData as any)?.stats
  const critical    = statsFromServer?.critical    ?? alerts.filter(a => a.state === 'active' && a.severity === 'critical').length
  const high        = statsFromServer?.high        ?? alerts.filter(a => a.state === 'active' && a.severity === 'high').length
  const acknowledged = statsFromServer?.acknowledged ?? alerts.filter(a => a.state === 'acknowledged').length
  const totalPages  = Math.ceil(total / PAGE_SIZE)

  return (
    <div className="p-6 space-y-5">
      <PageHeader
        title="Alerts"
        description="Monitor and respond to active metric alerts."
        actions={
          <Link href="/alerts/rules">
            <Button variant="secondary" size="sm">Manage Rules</Button>
          </Link>
        }
      />

      {/* Summary strip */}
      <div className="grid grid-cols-3 gap-3">
        <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-4 flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-red-500/15 flex items-center justify-center">
            <AlertTriangle className="w-4 h-4 text-red-400" />
          </div>
          <div>
            <p className="text-xl font-semibold text-red-400 tabular-nums">{critical}</p>
            <p className="text-[11px] text-slate-600">Critical active{!statsFromServer ? ' (this page)' : ''}</p>
          </div>
        </div>
        <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-4 flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-orange-500/15 flex items-center justify-center">
            <AlertTriangle className="w-4 h-4 text-orange-400" />
          </div>
          <div>
            <p className="text-xl font-semibold text-orange-400 tabular-nums">{high}</p>
            <p className="text-[11px] text-slate-600">High active{!statsFromServer ? ' (this page)' : ''}</p>
          </div>
        </div>
        <div className="rounded-xl border border-white/[0.07] bg-[#111318] p-4 flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-amber-500/15 flex items-center justify-center">
            <Clock className="w-4 h-4 text-amber-400" />
          </div>
          <div>
            <p className="text-xl font-semibold text-amber-400 tabular-nums">{acknowledged}</p>
            <p className="text-[11px] text-slate-600">Acknowledged{!statsFromServer ? ' (this page)' : ''}</p>
          </div>
        </div>
      </div>

      {/* Filter bar */}
      <div className="flex items-center gap-3">
        <select
          value={stateFilter}
          onChange={e => { setStateFilter(e.target.value); setPage(1) }}
          className="px-3 py-2 text-sm bg-[#1a1d24] border border-white/[0.09] rounded-lg text-slate-400 focus:outline-none focus:border-indigo-500/50"
        >
          <option value="">All states</option>
          <option value="active">Active</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="resolved">Resolved</option>
          <option value="suppressed">Suppressed</option>
        </select>
        {isFetching && <span className="text-xs text-slate-600 animate-pulse">Refreshing…</span>}
        <span className="ml-auto text-xs text-slate-600 tabular-nums">{total} total</span>
      </div>

      {/* Alert list */}
      <Card className="overflow-hidden">
        {isLoading ? (
          <div className="p-5 space-y-3">
            <Skeleton className="h-24 w-full rounded" count={3} />
          </div>
        ) : alerts.length === 0 ? (
          <EmptyState
            icon={<Bell className="w-5 h-5" />}
            title={stateFilter ? `No ${stateFilter} alerts` : 'No alerts'}
            description="The system is running normally."
          />
        ) : (
          <div>
            {alerts.map(alert => (
              <AlertCard
                key={alert.id}
                alert={alert}
                onAck={handleAck}
                onResolve={handleResolve}
                onSuppress={handleSuppress}
                acting={actingId === alert.id}
                actionError={errors[alert.id] ?? null}
              />
            ))}
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-white/[0.06]">
            <Button
              variant="ghost" size="sm"
              disabled={page === 1}
              onClick={() => setPage(p => Math.max(1, p - 1))}
            >
              <ChevronLeft className="w-4 h-4" />
              Previous
            </Button>
            <span className="text-xs text-slate-600">
              Page {page} of {totalPages}
            </span>
            <Button
              variant="ghost" size="sm"
              disabled={page >= totalPages}
              onClick={() => setPage(p => p + 1)}
            >
              Next
              <ChevronRight className="w-4 h-4" />
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}

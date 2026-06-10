'use client'

import { useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { RefreshCw, AlertCircle, AlertTriangle, Info, CheckCircle, Bell } from 'lucide-react'
import { apiClient, Event } from '@/lib/api-client'
import { useVisibleInterval } from '@/hooks/useVisibilityRefetch'
import { useWebSocketContext } from '@/contexts/websocket-context'

type SeverityFilter = 'all' | 'error' | 'warning' | 'info'
type StatusFilter = 'active' | 'acknowledged' | 'all'

const ACK_KEY = 'dso_ack_alerts'

function loadAcked(): Set<string> {
  if (typeof window === 'undefined') return new Set()
  try {
    const raw = localStorage.getItem(ACK_KEY)
    return new Set(raw ? (JSON.parse(raw) as string[]) : [])
  } catch {
    return new Set()
  }
}

function saveAcked(set: Set<string>) {
  try {
    localStorage.setItem(ACK_KEY, JSON.stringify([...set].slice(-500)))
  } catch {
    localStorage.removeItem(ACK_KEY)
  }
}

function severityIcon(severity: string) {
  switch (severity) {
    case 'error': return <AlertCircle className="w-4 h-4 text-red-500 flex-shrink-0" />
    case 'warning': return <AlertTriangle className="w-4 h-4 text-yellow-500 flex-shrink-0" />
    default: return <Info className="w-4 h-4 text-blue-400 flex-shrink-0" />
  }
}

function severityBadge(severity: string) {
  switch (severity) {
    case 'error': return 'bg-red-100 text-red-700 border-red-200'
    case 'warning': return 'bg-yellow-100 text-yellow-700 border-yellow-200'
    default: return 'bg-blue-100 text-blue-700 border-blue-200'
  }
}

function alertKey(e: Event) {
  return `${e.timestamp}:${e.action}:${e.message}`
}

export default function AlertsPage() {
  const refetchInterval = useVisibleInterval(10000)
  const { events: wsEvents } = useWebSocketContext()

  const { data: polledEvents = [], isLoading, refetch, isFetching } = useQuery({
    queryKey: ['events', 'alerts'],
    queryFn: () => apiClient.getEvents(200),
    refetchInterval,
  })

  const [severityFilter, setSeverityFilter] = useState<SeverityFilter>('all')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('active')
  const [search, setSearch] = useState('')
  const [timeRange, setTimeRange] = useState<'1h' | '6h' | '24h' | 'all'>('24h')
  const [acked, setAcked] = useState<Set<string>>(loadAcked)

  // Merge polled + live WS events, deduplicate by key
  const allEvents: Event[] = (() => {
    const seen = new Set<string>()
    const merged: Event[] = []
    for (const e of [...wsEvents, ...polledEvents]) {
      const k = alertKey(e)
      if (!seen.has(k)) { seen.add(k); merged.push(e) }
    }
    return merged.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
  })()

  const now = Date.now()
  const rangeMs: Record<typeof timeRange, number> = { '1h': 3600000, '6h': 21600000, '24h': 86400000, 'all': Infinity }

  const filtered = allEvents.filter(e => {
    if (severityFilter !== 'all' && e.severity !== severityFilter) return false
    const key = alertKey(e)
    if (statusFilter === 'acknowledged' && !acked.has(key)) return false
    if (statusFilter === 'active' && acked.has(key)) return false
    if (search && !e.message.toLowerCase().includes(search.toLowerCase()) && !e.action?.toLowerCase().includes(search.toLowerCase())) return false
    const age = now - new Date(e.timestamp).getTime()
    if (age > rangeMs[timeRange]) return false
    return true
  })

  const counts = {
    error: allEvents.filter(e => e.severity === 'error').length,
    warning: allEvents.filter(e => e.severity === 'warning').length,
    info: allEvents.filter(e => e.severity === 'info').length,
    active: allEvents.filter(e => !acked.has(alertKey(e))).length,
  }

  const acknowledge = useCallback((e: Event) => {
    setAcked(prev => {
      const next = new Set(prev)
      next.add(alertKey(e))
      saveAcked(next)
      return next
    })
  }, [])

  const acknowledgeAll = useCallback(() => {
    setAcked(prev => {
      const next = new Set(prev)
      filtered.forEach(e => next.add(alertKey(e)))
      saveAcked(next)
      return next
    })
  }, [filtered])

  function formatTime(ts: string) {
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }

  return (
    <div className="p-6 space-y-5">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Alert Timeline</h1>
          <p className="text-sm text-muted-foreground mt-1">System events and operational alerts</p>
        </div>
        <div className="flex gap-2">
          {counts.active > 0 && statusFilter === 'active' && (
            <button
              onClick={acknowledgeAll}
              className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted"
            >
              <CheckCircle className="h-4 w-4 text-green-600" />
              Ack All ({counts.active})
            </button>
          )}
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="flex items-center gap-2 px-3 py-2 text-sm rounded-md border border-border hover:bg-muted disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            Refresh
          </button>
        </div>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-4 gap-4">
        {[
          { label: 'Errors', count: counts.error, color: 'text-red-600', bg: 'bg-red-50 border-red-200' },
          { label: 'Warnings', count: counts.warning, color: 'text-yellow-600', bg: 'bg-yellow-50 border-yellow-200' },
          { label: 'Info', count: counts.info, color: 'text-blue-600', bg: 'bg-blue-50 border-blue-200' },
          { label: 'Active', count: counts.active, color: 'text-foreground', bg: 'bg-muted/40 border-border' },
        ].map(({ label, count, color, bg }) => (
          <div key={label} className={`rounded-lg border p-4 ${bg}`}>
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">{label}</p>
            <p className={`text-2xl font-bold mt-1 ${color}`}>{count}</p>
          </div>
        ))}
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-3 items-center">
        {/* Severity */}
        <div className="flex gap-1 rounded-md border border-border overflow-hidden">
          {(['all', 'error', 'warning', 'info'] as SeverityFilter[]).map(s => (
            <button
              key={s}
              onClick={() => setSeverityFilter(s)}
              className={`px-3 py-1.5 text-xs font-medium capitalize transition-colors ${
                severityFilter === s ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
              }`}
            >
              {s}
            </button>
          ))}
        </div>

        {/* Status */}
        <div className="flex gap-1 rounded-md border border-border overflow-hidden">
          {(['active', 'acknowledged', 'all'] as StatusFilter[]).map(s => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              className={`px-3 py-1.5 text-xs font-medium capitalize transition-colors ${
                statusFilter === s ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
              }`}
            >
              {s}
            </button>
          ))}
        </div>

        {/* Time range */}
        <div className="flex gap-1 rounded-md border border-border overflow-hidden">
          {(['1h', '6h', '24h', 'all'] as const).map(t => (
            <button
              key={t}
              onClick={() => setTimeRange(t)}
              className={`px-3 py-1.5 text-xs font-medium transition-colors ${
                timeRange === t ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
              }`}
            >
              {t}
            </button>
          ))}
        </div>

        {/* Search */}
        <input
          type="text"
          placeholder="Search alerts…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="flex-1 min-w-[160px] max-w-xs px-3 py-1.5 text-sm rounded-md border border-border bg-background focus:outline-none focus:ring-1 focus:ring-primary"
        />
      </div>

      {/* Timeline */}
      {isLoading ? (
        <div className="space-y-2">
          {[...Array(5)].map((_, i) => <div key={i} className="h-14 rounded-md bg-muted animate-pulse" />)}
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 text-center space-y-2">
          <Bell className="h-10 w-10 text-muted-foreground/30" />
          <p className="text-muted-foreground">No alerts match the current filters.</p>
        </div>
      ) : (
        <div className="space-y-1.5">
          {filtered.map((e, i) => {
            const key = alertKey(e)
            const isAcked = acked.has(key)
            return (
              <div
                key={`${key}-${i}`}
                className={`flex items-start gap-3 rounded-lg border px-4 py-3 transition-colors ${
                  isAcked
                    ? 'border-border bg-muted/20 opacity-60'
                    : e.severity === 'error'
                    ? 'border-red-200 bg-red-50/50'
                    : e.severity === 'warning'
                    ? 'border-yellow-200 bg-yellow-50/50'
                    : 'border-border bg-background'
                }`}
              >
                {severityIcon(e.severity)}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className={`inline-flex px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase border ${severityBadge(e.severity)}`}>
                      {e.severity}
                    </span>
                    {e.action && (
                      <span className="text-xs text-muted-foreground font-mono">{e.action}</span>
                    )}
                    {e.secret_name && (
                      <span className="text-xs text-muted-foreground">· {e.secret_name}</span>
                    )}
                    {isAcked && (
                      <span className="inline-flex items-center gap-1 text-[10px] text-green-600">
                        <CheckCircle className="w-3 h-3" /> Acknowledged
                      </span>
                    )}
                  </div>
                  <p className="text-sm mt-1 text-foreground">{e.message}</p>
                  <p className="text-xs text-muted-foreground mt-0.5">{formatTime(e.timestamp)}</p>
                </div>
                {!isAcked && (
                  <button
                    onClick={() => acknowledge(e)}
                    className="flex-shrink-0 px-2 py-1 text-xs rounded border border-border hover:bg-muted transition-colors"
                    title="Acknowledge"
                  >
                    Ack
                  </button>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

'use client'

import { useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { apiClient, AuditEvent, AuditFilters } from '@/lib/api-client'
import { useVisibleInterval } from '@/hooks/useVisibilityRefetch'
import {
  Search, Download, Filter, X, ChevronRight,
  AlertTriangle, Info, AlertCircle, CheckCircle2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'

const SEVERITY_COLORS: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 border-red-200',
  error:    'bg-red-50 text-red-700 border-red-100',
  warning:  'bg-yellow-50 text-yellow-700 border-yellow-100',
  info:     'bg-blue-50 text-blue-700 border-blue-100',
}

const STATUS_COLORS: Record<string, string> = {
  success: 'bg-green-50 text-green-700 border-green-100',
  failure: 'bg-red-50 text-red-700 border-red-100',
}

function SeverityIcon({ s }: { s: string }) {
  switch (s) {
    case 'critical':
    case 'error': return <AlertCircle className="h-3.5 w-3.5" />
    case 'warning': return <AlertTriangle className="h-3.5 w-3.5" />
    default: return <Info className="h-3.5 w-3.5" />
  }
}

function Badge({ label, colorClass }: { label: string; colorClass: string }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs font-medium ${colorClass}`}>
      {label}
    </span>
  )
}

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Date(ts).toLocaleString()
}

function EventRow({ e, onCorrelation, onActor }: {
  e: AuditEvent
  onCorrelation: (id: string) => void
  onActor: (id: string) => void
}) {
  const sevClass = SEVERITY_COLORS[e.severity] ?? SEVERITY_COLORS.info
  const statusClass = STATUS_COLORS[e.status] ?? 'bg-muted text-muted-foreground border-border'

  return (
    <div className="flex items-start gap-3 border-b border-border py-3 last:border-0 hover:bg-muted/20 px-2 rounded">
      <div className={`mt-0.5 flex items-center gap-1 rounded border px-1.5 py-0.5 text-xs font-medium ${sevClass}`}>
        <SeverityIcon s={e.severity} />
        {e.severity}
      </div>

      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium">{e.action}</span>
          <Badge label={e.status} colorClass={statusClass} />
          {e.resource_type && (
            <Badge label={e.resource_type} colorClass="bg-muted text-muted-foreground border-border" />
          )}
        </div>

        {e.details && (
          <p className="text-xs text-muted-foreground truncate max-w-xl">{e.details}</p>
        )}

        <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
          <span>{relTime(e.timestamp)}</span>

          {e.actor && (
            <button
              className="font-mono text-primary hover:underline"
              onClick={() => onActor(e.actor_id)}
            >
              {e.actor}
            </button>
          )}

          {e.resource && (
            <span className="font-mono">{e.resource}/{e.resource_id?.slice(0, 12)}</span>
          )}

          {e.correlation_id && (
            <button
              className="font-mono text-xs text-blue-600 hover:underline"
              onClick={() => onCorrelation(e.correlation_id)}
              title="View correlation chain"
            >
              {e.correlation_id.slice(0, 20)}… <ChevronRight className="inline h-3 w-3" />
            </button>
          )}

          {e.ip_address && (
            <span>{e.ip_address}</span>
          )}
        </div>
      </div>
    </div>
  )
}

export default function AuditPage() {
  const router = useRouter()
  const [filters, setFilters] = useState<AuditFilters>({ limit: 50, offset: 0 })
  const [search, setSearch] = useState('')
  const [showFilters, setShowFilters] = useState(false)

  const refetchInterval = useVisibleInterval(30000)

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['audit', filters],
    queryFn: () => apiClient.getAuditEvents(filters),
    refetchInterval,
  })

  const events = data?.events ?? []
  const total = data?.total ?? 0

  // Client-side search overlay
  const visible = search
    ? events.filter(e =>
        e.action.includes(search) ||
        e.actor.toLowerCase().includes(search.toLowerCase()) ||
        e.resource.toLowerCase().includes(search.toLowerCase()) ||
        e.correlation_id.includes(search) ||
        e.resource_id.includes(search) ||
        e.details.toLowerCase().includes(search.toLowerCase())
      )
    : events

  const setFilter = useCallback((key: keyof AuditFilters, value: string) => {
    setFilters(f => ({ ...f, [key]: value || undefined, offset: 0 }))
  }, [])

  const clearFilters = () => {
    setFilters({ limit: 50, offset: 0 })
    setSearch('')
  }

  const hasActiveFilters = Object.keys(filters).some(k => k !== 'limit' && k !== 'offset' && (filters as any)[k])

  const onCorrelation = (id: string) => router.push(`/audit?correlation_id=${encodeURIComponent(id)}`)
  const onActor = (id: string) => router.push(`/users/activity?id=${encodeURIComponent(id)}`)

  return (
    <div className="p-6 space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Audit Log</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {total > 0 ? `${total.toLocaleString()} events` : 'All system activities and operations'}
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={() => setShowFilters(v => !v)}
            className={`flex items-center gap-1.5 rounded-md border px-3 py-2 text-sm hover:bg-muted ${hasActiveFilters ? 'border-primary text-primary' : 'border-border'}`}
          >
            <Filter className="h-4 w-4" />
            Filters
            {hasActiveFilters && <span className="ml-0.5 rounded-full bg-primary text-primary-foreground px-1.5 text-xs">!</span>}
          </button>
          <a
            href={apiClient.getAuditExportURL(filters, 'csv')}
            download
            className="flex items-center gap-1.5 rounded-md border border-border px-3 py-2 text-sm hover:bg-muted"
          >
            <Download className="h-4 w-4" />
            CSV
          </a>
          <a
            href={apiClient.getAuditExportURL(filters, 'json')}
            download
            className="flex items-center gap-1.5 rounded-md border border-border px-3 py-2 text-sm hover:bg-muted"
          >
            <Download className="h-4 w-4" />
            JSON
          </a>
        </div>
      </div>

      {/* Search bar */}
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <input
          className="w-full rounded-md border border-border bg-background pl-9 pr-4 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
          placeholder="Search actions, actors, resources, correlation IDs…"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        {search && (
          <button className="absolute right-3 top-1/2 -translate-y-1/2" onClick={() => setSearch('')}>
            <X className="h-4 w-4 text-muted-foreground" />
          </button>
        )}
      </div>

      {/* Filter panel */}
      {showFilters && (
        <div className="rounded-lg border border-border bg-card p-4 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          {[
            { key: 'actor', label: 'Actor name' },
            { key: 'actor_id', label: 'Actor ID' },
            { key: 'action', label: 'Action' },
            { key: 'resource', label: 'Resource type' },
            { key: 'correlation_id', label: 'Correlation ID' },
            { key: 'execution_id', label: 'Execution ID' },
            { key: 'start_time', label: 'Start time (ISO)' },
            { key: 'end_time', label: 'End time (ISO)' },
          ].map(({ key, label }) => (
            <div key={key} className="space-y-1">
              <label className="text-xs text-muted-foreground font-medium">{label}</label>
              <input
                className="w-full rounded border border-border bg-background px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-primary"
                value={(filters as any)[key] ?? ''}
                onChange={e => setFilter(key as keyof AuditFilters, e.target.value)}
              />
            </div>
          ))}
          <div className="col-span-full flex justify-end">
            <Button size="sm" variant="ghost" onClick={clearFilters}>Clear all</Button>
          </div>
        </div>
      )}

      {/* Active filter chips */}
      {hasActiveFilters && (
        <div className="flex flex-wrap gap-2">
          {Object.entries(filters).filter(([k, v]) => k !== 'limit' && k !== 'offset' && v).map(([k, v]) => (
            <span key={k} className="inline-flex items-center gap-1 rounded-full bg-primary/10 text-primary text-xs px-2.5 py-1">
              {k}: {String(v)}
              <button onClick={() => setFilter(k as keyof AuditFilters, '')}><X className="h-3 w-3" /></button>
            </span>
          ))}
        </div>
      )}

      {/* Event list */}
      <div className="rounded-lg border border-border bg-card">
        {/* summary bar */}
        <div className="flex items-center justify-between border-b border-border px-4 py-2.5">
          <span className="text-sm text-muted-foreground">
            {isFetching ? 'Refreshing…' : `Showing ${visible.length}${search ? ' filtered' : ''} of ${events.length} loaded`}
          </span>
          <button onClick={() => refetch()} className="text-xs text-primary hover:underline">Refresh</button>
        </div>

        <div className="px-2 py-1 divide-y divide-transparent">
          {isLoading ? (
            <div className="flex justify-center py-16">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            </div>
          ) : visible.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <CheckCircle2 className="h-10 w-10 mb-3 opacity-20" />
              <p>{search ? 'No events match the search.' : 'No audit events found.'}</p>
            </div>
          ) : (
            visible.map(e => (
              <EventRow key={e.id} e={e} onCorrelation={onCorrelation} onActor={onActor} />
            ))
          )}
        </div>

        {/* Pagination */}
        {total > (filters.limit ?? 50) && (
          <div className="flex items-center justify-between border-t border-border px-4 py-3">
            <Button
              size="sm" variant="outline"
              disabled={(filters.offset ?? 0) === 0}
              onClick={() => setFilters(f => ({ ...f, offset: Math.max(0, (f.offset ?? 0) - (f.limit ?? 50)) }))}
            >
              Previous
            </Button>
            <span className="text-xs text-muted-foreground">
              {(filters.offset ?? 0) + 1}–{Math.min((filters.offset ?? 0) + (filters.limit ?? 50), total)} of {total}
            </span>
            <Button
              size="sm" variant="outline"
              disabled={(filters.offset ?? 0) + (filters.limit ?? 50) >= total}
              onClick={() => setFilters(f => ({ ...f, offset: (f.offset ?? 0) + (f.limit ?? 50) }))}
            >
              Next
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

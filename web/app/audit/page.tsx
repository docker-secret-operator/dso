'use client'

import { useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'
import { PageHeader, Card, Button } from '@/components/ui-modern'
import { Search, X } from 'lucide-react'
import * as auditApi from '@/lib/api/audit'
import { AuditFilters as AuditFiltersType, AuditEvent } from '@/lib/api/types'
import { AuditTable } from '@/components/audit/AuditTable'
import { AuditFilters } from '@/components/audit/AuditFilters'
import { AuditExportButton } from '@/components/audit/AuditExportButton'
import { CorrelationTimeline } from '@/components/audit/CorrelationTimeline'
import { ActorTimeline } from '@/components/audit/ActorTimeline'

function AuditContent() {
  const [filters, setFilters] = useState<AuditFiltersType>({ limit: 50, offset: 0 })
  const [search, setSearch] = useState('')
  const [showFilters, setShowFilters] = useState(false)
  const [correlationId, setCorrelationId] = useState<string | null>(null)
  const [actorId, setActorId] = useState<string | null>(null)
  const [actorPeriod, setActorPeriod] = useState<'24h' | '7d' | '30d'>('24h')

  // Main audit events query
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['audit', filters],
    queryFn: () => auditApi.getAuditEvents(filters),
    refetchInterval: 30000,
  })

  // Correlation chain query
  const { data: correlationData, isLoading: correlationLoading } = useQuery({
    queryKey: ['audit-correlation', correlationId],
    queryFn: () => correlationId ? auditApi.getCorrelationChain(correlationId) : null,
    enabled: !!correlationId,
  })

  // Actor timeline query
  const { data: actorData, isLoading: actorLoading, refetch: refetchActor } = useQuery({
    queryKey: ['audit-actor', actorId, actorPeriod],
    queryFn: () => actorId ? auditApi.getActorTimeline(actorId, actorPeriod) : null,
    enabled: !!actorId,
  })

  const events = data?.events ?? []
  const total = data?.total ?? 0

  const visible = search
    ? events.filter(e =>
        e.action.toLowerCase().includes(search.toLowerCase()) ||
        e.actor.toLowerCase().includes(search.toLowerCase()) ||
        e.resource.toLowerCase().includes(search.toLowerCase()) ||
        e.correlation_id?.includes(search) ||
        e.resource_id?.includes(search) ||
        e.details?.toLowerCase().includes(search.toLowerCase())
      )
    : events

  const setFilter = useCallback((key: keyof AuditFiltersType, value: string | undefined) => {
    setFilters(f => ({ ...f, [key]: value, offset: 0 }))
  }, [])

  const clearFilters = () => {
    setFilters({ limit: 50, offset: 0 })
    setSearch('')
  }

  const handlePeriodChange = (period: '24h' | '7d' | '30d') => {
    setActorPeriod(period)
  }

  return (
    <div className="p-6 space-y-5">
      <PageHeader
        title="Audit Log"
        description={total > 0 ? `${total.toLocaleString()} events recorded` : 'All system activities and operations'}
        actions={
          <div className="flex items-center gap-2">
            <AuditExportButton filters={filters} format="csv" />
            <AuditExportButton filters={filters} format="json" />
          </div>
        }
      />

      {/* Search bar */}
      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-lg">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-600" />
          <input
            className="w-full pl-9 pr-4 py-2 text-[13px] font-normal rounded-lg border border-white/[0.09] bg-[#1a1f2e] text-[#F3F4F6] placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20"
            placeholder="Search actions, actors, resources, IDs…"
            value={search}
            onChange={e => setSearch(e.target.value)}
          />
          {search && (
            <button
              className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-600 hover:text-slate-400"
              onClick={() => setSearch('')}
            >
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>

        <AuditFilters
          filters={filters}
          showFilters={showFilters}
          onToggleFilters={() => setShowFilters(v => !v)}
          onFilterChange={setFilter}
          onClearFilters={clearFilters}
        />

        {isFetching && <span className="text-xs text-slate-600 animate-pulse">Refreshing…</span>}
      </div>

      {/* Event list */}
      <Card className="overflow-hidden">
        {/* Summary bar */}
        <div className="flex items-center justify-between px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
          <span className="text-[11px] font-normal text-[#9CA3AF]">
            {isFetching ? 'Refreshing…' : `${visible.length}${search ? ' filtered' : ''} of ${events.length} loaded`}
          </span>
          <button
            onClick={() => refetch()}
            className="text-[11px] font-normal text-indigo-400 hover:text-indigo-300 transition-colors"
          >
            Refresh
          </button>
        </div>

        {/* Event table */}
        <AuditTable
          events={visible}
          isLoading={isLoading}
          isEmpty={visible.length === 0}
          searchTerm={search}
          onCorrelation={setCorrelationId}
          onActor={setActorId}
        />

        {/* Pagination */}
        {total > (filters.limit ?? 50) && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-white/[0.06]">
            <Button
              variant="ghost"
              size="sm"
              disabled={(filters.offset ?? 0) === 0}
              onClick={() => setFilters(f => ({ ...f, offset: Math.max(0, (f.offset ?? 0) - (f.limit ?? 50)) }))}
            >
              Previous
            </Button>
            <span className="text-[11px] font-normal text-[#9CA3AF] tabular-nums">
              {(filters.offset ?? 0) + 1}–{Math.min((filters.offset ?? 0) + (filters.limit ?? 50), total)} of {total.toLocaleString()}
            </span>
            <Button
              variant="ghost"
              size="sm"
              disabled={(filters.offset ?? 0) + (filters.limit ?? 50) >= total}
              onClick={() => setFilters(f => ({ ...f, offset: (f.offset ?? 0) + (f.limit ?? 50) }))}
            >
              Next
            </Button>
          </div>
        )}
      </Card>

      {/* Modals */}
      <CorrelationTimeline
        data={correlationData}
        isLoading={correlationLoading}
        onClose={() => setCorrelationId(null)}
      />

      <ActorTimeline
        data={actorData}
        isLoading={actorLoading}
        period={actorPeriod}
        onPeriodChange={handlePeriodChange}
        onClose={() => setActorId(null)}
      />
    </div>
  )
}

export default function AuditPage() {
  return (
    <ProtectedRoute>
      <ErrorBoundary>
        <AuditContent />
      </ErrorBoundary>
    </ProtectedRoute>
  )
}

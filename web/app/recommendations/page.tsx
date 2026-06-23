'use client'

import { useState, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CheckCircle,
  AlertCircle,
  AlertTriangle,
  TrendingUp,
  Zap,
  X,
  ExternalLink,
  Info,
} from 'lucide-react'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import * as recApi from '@/lib/api/recommendations'
import type { Recommendation } from '@/lib/api/recommendations'
import Link from 'next/link'

const PRIORITY_COLOR: Record<string, string> = {
  critical: 'bg-red-500/15 text-red-300 border-red-500/40',
  high:     'bg-orange-500/15 text-orange-300 border-orange-500/40',
  medium:   'bg-amber-500/15 text-amber-300 border-amber-500/40',
  low:      'bg-blue-500/15 text-blue-300 border-blue-500/40',
}
const PRIORITY_ICON: Record<string, React.ReactNode> = {
  critical: <Zap className="h-4 w-4" />,
  high:     <AlertTriangle className="h-4 w-4" />,
  medium:   <AlertCircle className="h-4 w-4" />,
  low:      <CheckCircle className="h-4 w-4" />,
}
const CATEGORY_COLOR: Record<string, string> = {
  rotation:    'bg-purple-500/15 text-purple-300',
  drift:       'bg-orange-500/15 text-orange-300',
  compliance:  'bg-red-500/15 text-red-300',
  policy:      'bg-amber-500/15 text-amber-300',
  operational: 'bg-cyan-500/15 text-cyan-300',
  backup:      'bg-purple-500/15 text-purple-300',
  security:    'bg-red-500/15 text-red-300',
  plugin:      'bg-indigo-500/15 text-indigo-300',
  integration: 'bg-emerald-500/15 text-emerald-300',
  scheduler:   'bg-blue-500/15 text-blue-300',
  performance: 'bg-cyan-500/15 text-cyan-300',
}

type StatusFilter = 'open' | 'implemented' | 'dismissed'

function RecommendationDrawer({
  rec,
  onClose,
  onAcknowledge,
  onDismiss,
}: {
  rec: Recommendation
  onClose: () => void
  onAcknowledge: (id: string) => void
  onDismiss: (id: string) => void
}) {
  return (
    <div className="fixed inset-0 z-50 flex justify-end" onClick={onClose}>
      <div
        className="relative w-full max-w-md bg-[#111827] border-l border-slate-700/60 h-full overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-3 p-5 border-b border-slate-700/50">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <span className={`inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs font-semibold ${PRIORITY_COLOR[rec.priority] ?? ''}`}>
                {PRIORITY_ICON[rec.priority]}
                {rec.priority.toUpperCase()}
              </span>
              <span className={`rounded px-2 py-0.5 text-xs font-semibold ${CATEGORY_COLOR[rec.category] ?? 'bg-slate-500/15 text-slate-300'}`}>
                {rec.category}
              </span>
            </div>
            <h2 className="mt-2 text-base font-semibold text-slate-100 leading-snug">{rec.title}</h2>
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200 mt-0.5">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="p-5 space-y-5">
          {/* Description */}
          {rec.description && (
            <p className="text-sm text-slate-400 leading-relaxed">{rec.description}</p>
          )}

          {/* Evidence / Reason */}
          {rec.reason && (
            <div className="rounded-md border border-slate-700/50 bg-slate-800/40 p-3">
              <div className="flex items-center gap-1.5 text-xs text-slate-400 font-medium mb-1">
                <Info className="h-3.5 w-3.5" />
                Why this recommendation exists
              </div>
              <p className="text-sm text-slate-300">{rec.reason}</p>
            </div>
          )}

          {/* Suggested action */}
          <div>
            <p className="text-xs text-slate-400 font-medium mb-1">Suggested action</p>
            <p className="text-sm text-slate-200">{rec.suggested_action}</p>
          </div>

          {/* Metadata grid */}
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <p className="text-xs text-slate-400">Resource</p>
              <p className="mt-0.5 font-mono text-slate-200 truncate">{rec.resource ?? rec.resource_id ?? '—'}</p>
            </div>
            <div>
              <p className="text-xs text-slate-400">Status</p>
              <p className="mt-0.5 text-slate-200 capitalize">{rec.status}</p>
            </div>
            <div>
              <p className="text-xs text-slate-400">Confidence</p>
              <div className="mt-0.5 flex items-center gap-1.5">
                <div className="flex-1 h-1.5 bg-slate-700 rounded">
                  <div className="h-1.5 bg-indigo-500 rounded" style={{ width: `${(rec.confidence ?? 0) * 100}%` }} />
                </div>
                <span className="text-xs text-slate-300">{((rec.confidence ?? 0) * 100).toFixed(0)}%</span>
              </div>
            </div>
          </div>

          {/* Cross-links */}
          {(rec.driftId || rec.policyId || rec.auditId) && (
            <div className="space-y-2">
              <p className="text-xs text-slate-400 font-medium">Linked evidence</p>
              {rec.driftId && (
                <Link href={`/drift?highlight=${rec.driftId}`} className="flex items-center gap-1.5 text-xs text-indigo-400 hover:text-indigo-300">
                  <ExternalLink className="h-3 w-3" />
                  Drift finding: <span className="font-mono">{rec.driftId.slice(0, 12)}…</span>
                </Link>
              )}
              {rec.policyId && (
                <Link href={`/policies?highlight=${rec.policyId}`} className="flex items-center gap-1.5 text-xs text-indigo-400 hover:text-indigo-300">
                  <ExternalLink className="h-3 w-3" />
                  Policy rule: <span className="font-mono">{rec.policyId.slice(0, 12)}…</span>
                </Link>
              )}
              {rec.auditId && (
                <Link href={`/audit?highlight=${rec.auditId}`} className="flex items-center gap-1.5 text-xs text-indigo-400 hover:text-indigo-300">
                  <ExternalLink className="h-3 w-3" />
                  Audit event: <span className="font-mono">{rec.auditId.slice(0, 12)}…</span>
                </Link>
              )}
            </div>
          )}

          {/* Actions */}
          {rec.status === 'open' && (
            <div className="flex gap-2 pt-2 border-t border-slate-700/50">
              <button
                onClick={() => onAcknowledge(rec.id)}
                className="flex-1 rounded bg-amber-600 px-3 py-2 text-sm font-medium text-white hover:bg-amber-700 transition-colors"
              >
                Acknowledge
              </button>
              <button
                onClick={() => onDismiss(rec.id)}
                className="flex-1 rounded bg-slate-600 px-3 py-2 text-sm font-medium text-white hover:bg-slate-500 transition-colors"
              >
                Dismiss
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function RecommendationsContent() {
  const qc = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('open')
  const [severityFilter, setSeverityFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [selected, setSelected] = useState<Recommendation | null>(null)

  const { data: recsData, isLoading } = useQuery({
    queryKey: ['recommendations', statusFilter, severityFilter, categoryFilter],
    queryFn: () => recApi.listRecommendations({
      severity: severityFilter || undefined,
      category: categoryFilter || undefined,
      pageSize: 200,
    }),
    refetchInterval: 30000,
  })

  const { data: metrics } = useQuery({
    queryKey: ['recommendation-metrics'],
    queryFn: () => recApi.getRecommendationMetrics(),
    refetchInterval: 30000,
  })

  const invalidate = useCallback(() => {
    qc.invalidateQueries({ queryKey: ['recommendations'] })
    qc.invalidateQueries({ queryKey: ['recommendation-metrics'] })
  }, [qc])

  const handleAcknowledge = async (id: string) => {
    await recApi.acknowledgeRecommendation(id)
    invalidate()
    setSelected(null)
  }
  const handleDismiss = async (id: string) => {
    await recApi.dismissRecommendation(id)
    invalidate()
    setSelected(null)
  }

  // Filter client-side by status (server doesn't separate live recs by status)
  const recs = (recsData?.recommendations ?? []).filter((r) => {
    if (statusFilter === 'open') return r.status === 'open'
    if (statusFilter === 'implemented') return r.status === 'implemented'
    if (statusFilter === 'dismissed') return r.status === 'dismissed'
    return true
  })

  return (
    <div className="space-y-6 p-8">
      <div>
        <h1 className="text-2xl font-bold text-slate-100">Recommendations</h1>
        <p className="mt-1 text-sm text-slate-400">Deterministic, evidence-based operational advisories</p>
      </div>

      {/* Metrics */}
      {metrics && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
          {[
            { label: 'Total', value: metrics.total_recommendations, icon: <TrendingUp className="h-4 w-4" /> },
            { label: 'Open', value: metrics.open_recommendations, cls: 'text-red-400', icon: <AlertTriangle className="h-4 w-4" /> },
            { label: 'Acknowledged', value: metrics.acknowledged_recommendations, cls: 'text-amber-400' },
            { label: 'Implemented', value: metrics.implemented_recommendations, cls: 'text-emerald-400', icon: <CheckCircle className="h-4 w-4" /> },
            { label: 'Avg Confidence', value: `${((metrics.average_confidence ?? 0) * 100).toFixed(0)}%` },
          ].map((m) => (
            <div key={m.label} className="rounded-lg border border-slate-700/50 bg-[#111827] p-3">
              <div className="flex items-center justify-between">
                <span className="text-xs text-slate-400">{m.label}</span>
                {m.icon && <span className="text-slate-500">{m.icon}</span>}
              </div>
              <div className={`mt-1 text-xl font-bold ${m.cls ?? 'text-slate-100'}`}>{m.value}</div>
            </div>
          ))}
        </div>
      )}

      {/* Filters */}
      <div className="flex flex-wrap gap-3 items-center border-b border-slate-700/50 pb-4">
        <div className="flex gap-1">
          {(['open', 'implemented', 'dismissed'] as StatusFilter[]).map((s) => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              className={`px-3 py-1.5 rounded text-sm font-medium transition-colors ${
                statusFilter === s
                  ? 'bg-indigo-600/30 text-indigo-300 border border-indigo-500/40'
                  : 'text-slate-400 hover:text-slate-200 border border-transparent'
              }`}
            >
              {s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>
        <select
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value)}
          className="rounded bg-slate-800 border border-slate-700/50 text-slate-300 text-sm px-2 py-1.5"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
        <select
          value={categoryFilter}
          onChange={(e) => setCategoryFilter(e.target.value)}
          className="rounded bg-slate-800 border border-slate-700/50 text-slate-300 text-sm px-2 py-1.5"
        >
          <option value="">All categories</option>
          <option value="rotation">Rotation</option>
          <option value="drift">Drift</option>
          <option value="compliance">Compliance</option>
          <option value="policy">Policy</option>
          <option value="operational">Operational</option>
        </select>
      </div>

      {/* List */}
      {isLoading && recs.length === 0 ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-24 rounded-lg bg-slate-800/40 animate-pulse" />
          ))}
        </div>
      ) : recs.length === 0 ? (
        <div className="rounded-lg border border-slate-700/50 bg-[#0B1020] p-10 text-center text-slate-500">
          No {statusFilter} recommendations
        </div>
      ) : (
        <div className="space-y-2">
          {recs.map((rec) => (
            <button
              key={rec.id}
              onClick={() => setSelected(rec)}
              className="w-full text-left rounded-lg border border-slate-700/50 bg-[#111827] px-5 py-4 hover:border-slate-600/60 transition-colors"
            >
              <div className="flex items-start gap-3">
                <div className="flex items-center gap-2 flex-shrink-0 mt-0.5">
                  <span className={`inline-flex items-center gap-1 rounded border px-2 py-0.5 text-xs font-semibold ${PRIORITY_COLOR[rec.priority] ?? ''}`}>
                    {PRIORITY_ICON[rec.priority]}
                    {rec.priority.toUpperCase()}
                  </span>
                  <span className={`rounded px-2 py-0.5 text-xs font-semibold ${CATEGORY_COLOR[rec.category] ?? 'bg-slate-500/15 text-slate-300'}`}>
                    {rec.category}
                  </span>
                </div>
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-slate-100 truncate">{rec.title}</p>
                  {rec.resource && (
                    <p className="mt-0.5 text-xs font-mono text-slate-400 truncate">{rec.resource}</p>
                  )}
                </div>
              </div>
            </button>
          ))}
        </div>
      )}

      {selected && (
        <RecommendationDrawer
          rec={selected}
          onClose={() => setSelected(null)}
          onAcknowledge={handleAcknowledge}
          onDismiss={handleDismiss}
        />
      )}
    </div>
  )
}

export default function RecommendationsPage() {
  return (
    <ProtectedRoute>
      <RecommendationsContent />
    </ProtectedRoute>
  )
}

'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  TrendingUp,
  AlertTriangle,
  RotateCcw,
  GitFork,
  ShieldCheck,
  Activity,
  X,
  ChevronRight,
  Info,
  FlaskConical,
} from 'lucide-react'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import * as forecastsApi from '@/lib/api/forecasts'
import type { OperationalForecast, ForecastCategory, ForecastSeverity } from '@/lib/api/forecasts'

// ── Visual constants ──────────────────────────────────────────────────────────

const SEVERITY_COLOR: Record<ForecastSeverity, string> = {
  critical: 'bg-red-500/15 text-red-300 border-red-500/40',
  high:     'bg-orange-500/15 text-orange-300 border-orange-500/40',
  medium:   'bg-amber-500/15 text-amber-300 border-amber-500/40',
  low:      'bg-blue-500/15 text-blue-300 border-blue-500/40',
  info:     'bg-slate-500/15 text-slate-300 border-slate-500/40',
}

const SEVERITY_DOT: Record<ForecastSeverity, string> = {
  critical: 'bg-red-400',
  high:     'bg-orange-400',
  medium:   'bg-amber-400',
  low:      'bg-blue-400',
  info:     'bg-slate-400',
}

const CATEGORY_META: Record<ForecastCategory, { label: string; Icon: React.ElementType; color: string }> = {
  rotation:    { label: 'Rotation',    Icon: RotateCcw,   color: 'text-purple-400' },
  drift:       { label: 'Drift',       Icon: GitFork,     color: 'text-orange-400' },
  compliance:  { label: 'Compliance',  Icon: ShieldCheck, color: 'text-red-400'    },
  operational: { label: 'Operational', Icon: Activity,    color: 'text-cyan-400'   },
}

// ── Beta disclaimer ───────────────────────────────────────────────────────────

function BetaNotice() {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-indigo-500/20 bg-indigo-500/5 px-4 py-3">
      <FlaskConical className="h-4 w-4 text-indigo-400 mt-0.5 flex-shrink-0" />
      <p className="text-xs text-slate-400 leading-relaxed">
        <span className="font-semibold text-indigo-300">Beta — predictions only.</span>{' '}
        These forecasts are statistical estimates derived from rotation history, drift recurrence, and compliance evidence.
        They are <em>not</em> measurements. Confidence scores are statistical probabilities, not guarantees.
        Forecasts disappear automatically when the underlying evidence resolves.
      </p>
    </div>
  )
}

// ── Confidence bar ────────────────────────────────────────────────────────────

function ConfidenceBar({ value }: { value: number }) {
  const pct = Math.round(value * 100)
  const color = pct >= 80 ? 'bg-emerald-500' : pct >= 60 ? 'bg-amber-500' : 'bg-slate-500'
  return (
    <div className="flex items-center gap-2">
      <div className="flex-1 h-1.5 rounded bg-slate-700">
        <div className={`h-1.5 rounded ${color}`} style={{ width: `${pct}%` }} />
      </div>
      <span className="text-xs tabular-nums text-slate-400 w-8 text-right">{pct}%</span>
    </div>
  )
}

// ── Detail drawer ─────────────────────────────────────────────────────────────

function ForecastDrawer({ fc, onClose }: { fc: OperationalForecast; onClose: () => void }) {
  const catMeta = CATEGORY_META[fc.category]
  const CatIcon = catMeta?.Icon ?? TrendingUp

  return (
    <div className="fixed inset-0 z-50 flex justify-end" onClick={onClose}>
      <div
        className="relative w-full max-w-md bg-[#111827] border-l border-slate-700/60 h-full overflow-y-auto shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-3 p-5 border-b border-slate-700/50">
          <div className="flex-1 min-w-0">
            <div className="flex flex-wrap gap-2 items-center">
              <span className="inline-flex items-center gap-1 rounded border border-indigo-500/40 bg-indigo-500/10 px-2 py-0.5 text-[10px] font-bold text-indigo-300 uppercase tracking-wider">
                <FlaskConical className="h-3 w-3" /> Beta
              </span>
              <span className={`inline-flex items-center gap-1.5 rounded border px-2 py-0.5 text-xs font-semibold ${SEVERITY_COLOR[fc.severity] ?? ''}`}>
                <span className={`h-1.5 w-1.5 rounded-full ${SEVERITY_DOT[fc.severity] ?? 'bg-slate-400'}`} />
                {fc.severity.toUpperCase()}
              </span>
              <span className={`inline-flex items-center gap-1 text-xs font-medium ${catMeta?.color ?? 'text-slate-400'}`}>
                <CatIcon className="h-3.5 w-3.5" />
                {catMeta?.label ?? fc.category}
              </span>
            </div>
            <h2 className="mt-2 text-sm font-semibold text-slate-100 leading-snug">{fc.title}</h2>
            {fc.resource && (
              <p className="mt-0.5 text-xs font-mono text-slate-400 truncate">{fc.resource}</p>
            )}
          </div>
          <button onClick={onClose} className="text-slate-400 hover:text-slate-200 mt-0.5">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="p-5 space-y-5">
          {fc.description && (
            <p className="text-sm text-slate-400 leading-relaxed">{fc.description}</p>
          )}

          {fc.reason && (
            <div className="rounded-md border border-slate-700/50 bg-slate-800/40 p-3">
              <div className="flex items-center gap-1.5 text-xs text-slate-400 font-medium mb-1">
                <Info className="h-3.5 w-3.5" />
                Why this forecast was generated
              </div>
              <p className="text-sm text-slate-300 leading-relaxed">{fc.reason}</p>
            </div>
          )}

          <div>
            <p className="text-xs text-slate-400 font-medium mb-1.5">Statistical confidence</p>
            <ConfidenceBar value={fc.confidence} />
            <p className="mt-1.5 text-[11px] text-slate-500">
              Derived from evidence count and historical consistency — not an AI score.
            </p>
          </div>

          {fc.evidence && fc.evidence.length > 0 && (
            <div>
              <p className="text-xs text-slate-400 font-medium mb-2">Evidence</p>
              <ul className="space-y-1.5">
                {fc.evidence.map((e, i) => (
                  <li key={i} className="flex items-start gap-2 text-sm text-slate-300">
                    <ChevronRight className="h-3.5 w-3.5 text-slate-500 mt-0.5 flex-shrink-0" />
                    <span>{e}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="rounded-md border border-amber-500/15 bg-amber-500/5 px-3 py-2.5">
            <p className="text-[11px] text-amber-300/80 leading-relaxed">
              <strong>Prediction, not measurement.</strong> This forecast is an estimate.
              Use it to guide investigation — not to trigger automated action.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Forecast card ─────────────────────────────────────────────────────────────

function ForecastCard({ fc, onClick }: { fc: OperationalForecast; onClick: () => void }) {
  const catMeta = CATEGORY_META[fc.category]
  const CatIcon = catMeta?.Icon ?? TrendingUp

  return (
    <button
      onClick={onClick}
      className="w-full text-left rounded-lg border border-slate-700/50 bg-[#111827] px-5 py-4 hover:border-slate-600/60 transition-colors group"
    >
      <div className="flex items-start gap-3">
        <span className={`mt-1.5 h-2 w-2 flex-shrink-0 rounded-full ${SEVERITY_DOT[fc.severity] ?? 'bg-slate-400'}`} />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className={`inline-flex items-center gap-1 text-xs font-medium ${catMeta?.color ?? 'text-slate-400'}`}>
              <CatIcon className="h-3 w-3" />
              {catMeta?.label ?? fc.category}
            </span>
            <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold uppercase ${SEVERITY_COLOR[fc.severity] ?? ''}`}>
              {fc.severity}
            </span>
          </div>
          <p className="mt-1 text-sm font-medium text-slate-200 leading-snug group-hover:text-white transition-colors">
            {fc.title}
          </p>
          {fc.resource && (
            <p className="mt-0.5 text-xs font-mono text-slate-400 truncate">{fc.resource}</p>
          )}
          <div className="mt-2 max-w-[200px]">
            <ConfidenceBar value={fc.confidence} />
          </div>
        </div>
        <ChevronRight className="h-4 w-4 text-slate-500 group-hover:text-slate-300 flex-shrink-0 mt-1 transition-colors" />
      </div>
    </button>
  )
}

// ── Severity summary ──────────────────────────────────────────────────────────

function ForecastSummary({ forecasts }: { forecasts: OperationalForecast[] }) {
  const bySeverity = forecasts.reduce((acc, fc) => {
    acc[fc.severity] = (acc[fc.severity] ?? 0) + 1
    return acc
  }, {} as Record<string, number>)

  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {(['critical', 'high', 'medium', 'low'] as ForecastSeverity[]).map((sev) => (
        <div key={sev} className="rounded-lg border border-slate-700/50 bg-[#111827] px-4 py-3">
          <p className="text-xs text-slate-400 capitalize">{sev}</p>
          <p className={`mt-1 text-2xl font-bold ${
            sev === 'critical' ? 'text-red-400' :
            sev === 'high'     ? 'text-orange-400' :
            sev === 'medium'   ? 'text-amber-400' :
                                 'text-blue-400'
          }`}>{bySeverity[sev] ?? 0}</p>
        </div>
      ))}
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

function ForecastsContent() {
  const [categoryFilter, setCategoryFilter] = useState<ForecastCategory | ''>('')
  const [severityFilter, setSeverityFilter] = useState<ForecastSeverity | ''>('')
  const [selected, setSelected] = useState<OperationalForecast | null>(null)

  const { data, isLoading, error } = useQuery({
    queryKey: ['forecasts', categoryFilter, severityFilter],
    queryFn: () => forecastsApi.listForecasts({
      category: categoryFilter || undefined,
      severity: severityFilter || undefined,
      pageSize: 100,
    }),
    refetchInterval: 60000,
  })

  const forecasts = data?.forecasts ?? []

  return (
    <div className="space-y-6 p-8">
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-bold text-slate-100">Forecasts</h1>
            <span className="inline-flex items-center gap-1 rounded border border-indigo-500/40 bg-indigo-500/10 px-2 py-0.5 text-[10px] font-bold text-indigo-300 uppercase tracking-wider">
              <FlaskConical className="h-3 w-3" /> Beta
            </span>
          </div>
          <p className="mt-1 text-sm text-slate-400">Statistical risk predictions — evidence-based, never AI-generated</p>
        </div>
        <AlertTriangle className="h-5 w-5 text-amber-400 flex-shrink-0 mt-1" />
      </div>

      <BetaNotice />

      {!isLoading && forecasts.length > 0 && <ForecastSummary forecasts={forecasts} />}

      <div className="flex flex-wrap gap-3 items-center">
        <select
          value={categoryFilter}
          onChange={(e) => setCategoryFilter(e.target.value as ForecastCategory | '')}
          className="rounded bg-slate-800 border border-slate-700/50 text-slate-300 text-sm px-2 py-1.5"
        >
          <option value="">All categories</option>
          <option value="rotation">Rotation</option>
          <option value="drift">Drift</option>
          <option value="compliance">Compliance</option>
          <option value="operational">Operational</option>
        </select>
        <select
          value={severityFilter}
          onChange={(e) => setSeverityFilter(e.target.value as ForecastSeverity | '')}
          className="rounded bg-slate-800 border border-slate-700/50 text-slate-300 text-sm px-2 py-1.5"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="high">High</option>
          <option value="medium">Medium</option>
          <option value="low">Low</option>
        </select>
        {(categoryFilter || severityFilter) && (
          <button
            onClick={() => { setCategoryFilter(''); setSeverityFilter('') }}
            className="text-xs text-slate-400 hover:text-slate-200 transition-colors"
          >
            Clear
          </button>
        )}
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-300">
          {error instanceof Error ? error.message : 'Failed to load forecasts'}
        </div>
      )}

      {isLoading && (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="h-20 rounded-lg bg-slate-800/40 animate-pulse" />
          ))}
        </div>
      )}

      {!isLoading && !error && forecasts.length === 0 && (
        <div className="rounded-lg border border-slate-700/50 bg-[#0B1020] p-10 text-center">
          <ShieldCheck className="h-8 w-8 text-emerald-400 mx-auto mb-3" />
          <p className="text-slate-300 font-medium">No forecasts at this time</p>
          <p className="mt-1 text-sm text-slate-500">
            No statistical risk signals detected. Forecasts appear automatically when evidence accumulates.
          </p>
        </div>
      )}

      {!isLoading && forecasts.length > 0 && (
        <div className="space-y-2">
          {forecasts.map((fc) => (
            <ForecastCard key={fc.id} fc={fc} onClick={() => setSelected(fc)} />
          ))}
        </div>
      )}

      {!isLoading && (
        <div className="rounded-lg border border-slate-700/30 bg-slate-800/20 px-4 py-3">
          <p className="text-xs text-slate-500 leading-relaxed">
            <strong className="text-slate-400">How forecasts are generated:</strong>{' '}
            Rotation — version history intervals and elapsed-cycle fraction.
            Drift — 14-day recurrence window with count-based confidence.
            Compliance — current status distribution across the managed estate.
            All forecasts are computed live from current evidence; none are stored.
          </p>
        </div>
      )}

      {selected && <ForecastDrawer fc={selected} onClose={() => setSelected(null)} />}
    </div>
  )
}

export default function ForecastsPage() {
  return (
    <ProtectedRoute>
      <ForecastsContent />
    </ProtectedRoute>
  )
}

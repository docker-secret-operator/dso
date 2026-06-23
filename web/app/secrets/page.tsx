'use client'

import { useState, useMemo, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient, type Secret } from '@/lib/api-client'
import * as auditApi from '@/lib/api/audit'
import * as bulkApi from '@/lib/api/bulk'
import * as historyApi from '@/lib/api/secret-history'
import * as complianceApi from '@/lib/api/compliance'
import type { BulkRotateResult } from '@/lib/api/bulk'
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar } from '@/components/common/BulkToolbar'
import { ConfirmModal } from '@/components/common/ConfirmModal'
import { Pagination } from '@/components/common/Pagination'
import { PageHeader, Card, Badge, StatusBadge, Button, Input, EmptyState, Skeleton } from '@/components/ui-modern'
import {
  RefreshCw, RotateCcw, Database, Search, ChevronUp, ChevronDown,
  ChevronsUpDown, Shield, X, Server, Clock, Hash,
  History, Activity, GitCompare, CheckCircle, AlertTriangle, XCircle,
  FileText,
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

type DrawerTab = 'overview' | 'history' | 'timeline' | 'compliance'

function sourceLabel(src: string) {
  const map: Record<string, string> = {
    manual: 'Manual',
    bulk_rotate: 'Bulk',
    scheduler: 'Scheduler',
    provider_sync: 'Provider',
  }
  return map[src] ?? src
}

function relativeTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  return `${d}d ago`
}

function timelineEventIcon(type: string) {
  switch (type) {
    case 'rotation': return '↻'
    case 'drift':    return '⚠'
    case 'audit':    return '✎'
    default:         return '•'
  }
}

function timelineEventColor(type: string) {
  switch (type) {
    case 'rotation': return 'text-blue-400'
    case 'drift':    return 'text-amber-400'
    case 'audit':    return 'text-slate-400'
    default:         return 'text-slate-500'
  }
}

function SecretDrawer({ secret, onClose }: { secret: Secret; onClose: () => void }) {
  const qc = useQueryClient()
  const [tab, setTab] = useState<DrawerTab>('overview')
  const [rotating, setRotating] = useState(false)
  const [rotateError, setRotateError] = useState<string | null>(null)
  const [rotateSuccess, setRotateSuccess] = useState(false)
  const [diffV1, setDiffV1] = useState<number | null>(null)
  const [diffV2, setDiffV2] = useState<number | null>(null)

  const { data: auditData } = useQuery({
    queryKey: ['secret-audit', secret.name],
    queryFn: () => auditApi.getAuditEvents({ resource_id: secret.name, resource_type: 'secret', limit: 3 }),
  })
  const recentAudit = auditData?.events ?? []

  const { data: historyData, isLoading: histLoading } = useQuery({
    queryKey: ['secret-history', secret.name],
    queryFn: () => historyApi.getSecretHistory(secret.name),
    enabled: tab === 'history',
  })

  const { data: timelineData, isLoading: timelineLoading } = useQuery({
    queryKey: ['secret-timeline', secret.name],
    queryFn: () => historyApi.getSecretTimeline(secret.name),
    enabled: tab === 'timeline',
  })

  const { data: diffData } = useQuery({
    queryKey: ['secret-diff', secret.name, diffV1, diffV2],
    queryFn: () => historyApi.getSecretDiff(secret.name, diffV1!, diffV2!),
    enabled: diffV1 !== null && diffV2 !== null && diffV1 !== diffV2,
  })

  const { data: complianceData, isLoading: complianceLoading } = useQuery({
    queryKey: ['secret-compliance', secret.name],
    queryFn: () => complianceApi.getSecretCompliance(secret.name),
    enabled: tab === 'compliance',
  })

  const rotate = async () => {
    setRotating(true)
    setRotateError(null)
    setRotateSuccess(false)
    try {
      await apiClient.rotateSecret(secret.name)
      await qc.invalidateQueries({ queryKey: ['secrets'] })
      await qc.invalidateQueries({ queryKey: ['secret-history', secret.name] })
      await qc.invalidateQueries({ queryKey: ['secret-timeline', secret.name] })
      setRotateSuccess(true)
    } catch (e: any) {
      setRotateError(e?.message ?? 'Rotation failed')
    } finally {
      setRotating(false)
    }
  }

  const versions = historyData?.versions ?? []
  const currentVersion = historyData?.currentVersion ?? 0
  const timeline = timelineData ?? []

  const tabs: { id: DrawerTab; label: string; icon: React.ReactNode }[] = [
    { id: 'overview',    label: 'Overview',    icon: <Shield className="w-3 h-3" /> },
    { id: 'history',     label: 'History',     icon: <History className="w-3 h-3" /> },
    { id: 'timeline',    label: 'Timeline',    icon: <Activity className="w-3 h-3" /> },
    { id: 'compliance',  label: 'Compliance',  icon: <FileText className="w-3 h-3" /> },
  ]

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 backdrop-blur-sm z-40 animate-fade-in"
        onClick={onClose}
      />

      {/* Drawer */}
      <div className="fixed right-0 top-0 h-full w-[460px] max-w-full bg-[#111827] border-l border-white/[0.09] z-50 flex flex-col animate-slide-in shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-white/[0.07]">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 rounded-lg bg-blue-500/15 flex items-center justify-center">
              <Shield className="w-3.5 h-3.5 text-blue-400" />
            </div>
            <div>
              <p className="text-sm font-semibold text-slate-100 font-mono">{secret.name}</p>
              <p className="text-[11px] text-slate-500 capitalize">
                {secret.provider}
                {currentVersion > 0 && (
                  <span className="ml-2 text-blue-400/70 font-mono">v{currentVersion}</span>
                )}
              </p>
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

        {/* Tabs */}
        <div className="flex border-b border-white/[0.07] px-5">
          {tabs.map(t => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={`flex items-center gap-1.5 px-3 py-2.5 text-xs font-medium border-b-2 transition-colors -mb-px ${
                tab === t.id
                  ? 'border-blue-500 text-blue-400'
                  : 'border-transparent text-slate-500 hover:text-slate-300'
              }`}
            >
              {t.icon}
              {t.label}
            </button>
          ))}
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5">

          {/* ── Overview tab ── */}
          {tab === 'overview' && (
            <>
              {/* Status row */}
              <div className="flex items-center gap-3">
                <StatusBadge status={secret.status} label={secret.status.toUpperCase()} />
                {secret.rotation_strategy && (
                  <Badge variant="outline">{secret.rotation_strategy}</Badge>
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
                    label: 'Version',
                    value: currentVersion > 0
                      ? <span className="font-mono text-blue-400">v{currentVersion}</span>
                      : (secret.version ? <span className="font-mono">{secret.version}</span> : <span className="text-slate-600">—</span>),
                    icon: <Hash className="w-3.5 h-3.5" />,
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
            </>
          )}

          {/* ── History tab ── */}
          {tab === 'history' && (
            <div className="space-y-4">
              {histLoading && (
                <div className="space-y-2">
                  {[1,2,3].map(i => <Skeleton key={i} className="h-12 w-full" />)}
                </div>
              )}

              {!histLoading && versions.length === 0 && (
                <div className="text-center py-8 text-slate-600 text-xs">
                  No rotation history yet. Rotate this secret to start tracking versions.
                </div>
              )}

              {!histLoading && versions.length > 0 && (
                <>
                  {/* Diff selector hint */}
                  {versions.length >= 2 && (
                    <div className="text-[11px] text-slate-600 flex items-center gap-1">
                      <GitCompare className="w-3 h-3" />
                      Click two versions to compare
                    </div>
                  )}

                  <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
                    {versions.map(v => {
                      const isSelected = diffV1 === v.version || diffV2 === v.version
                      return (
                        <button
                          key={v.version}
                          onClick={() => {
                            if (diffV1 === null) { setDiffV1(v.version); return }
                            if (diffV2 === null && v.version !== diffV1) { setDiffV2(v.version); return }
                            // Reset
                            setDiffV1(v.version); setDiffV2(null)
                          }}
                          className={`w-full text-left px-4 py-3 flex items-center gap-3 transition-colors hover:bg-white/[0.03] ${
                            isSelected ? 'bg-blue-500/5 border-l-2 border-l-blue-500' : ''
                          }`}
                        >
                          <span className="text-xs font-mono text-blue-400 w-8 flex-shrink-0">
                            v{v.version}
                          </span>
                          <div className="flex-1 min-w-0">
                            <p className="text-xs text-slate-300 truncate">
                              {sourceLabel(v.rotationSource)}
                              {v.rotatedBy && v.rotatedBy !== 'system' && (
                                <span className="text-slate-500"> by {v.rotatedBy}</span>
                              )}
                            </p>
                            <p className="text-[11px] text-slate-600">{relativeTime(v.createdAt)}</p>
                          </div>
                          <span className="text-[10px] text-slate-700 capitalize flex-shrink-0">
                            {v.provider}
                          </span>
                        </button>
                      )
                    })}
                  </div>

                  {/* Diff result */}
                  {diffV1 !== null && diffV2 !== null && diffData && (
                    <div className="rounded-lg border border-white/[0.07] p-4 space-y-3">
                      <p className="text-xs font-semibold text-slate-400 uppercase tracking-wider flex items-center gap-2">
                        <GitCompare className="w-3 h-3" />
                        v{Math.min(diffV1,diffV2)} → v{Math.max(diffV1,diffV2)}
                      </p>
                      <div className="space-y-1.5 text-xs">
                        {[
                          { label: 'Provider changed',         val: diffData.providerChanged },
                          { label: 'Rotation source changed',  val: diffData.rotationSourceChanged },
                          { label: 'Hash changed',             val: diffData.hashChanged },
                          { label: 'Execution changed',        val: diffData.executionChanged },
                        ].map(row => (
                          <div key={row.label} className="flex items-center justify-between">
                            <span className="text-slate-500">{row.label}</span>
                            <span className={row.val ? 'text-amber-400' : 'text-slate-600'}>
                              {row.val ? 'yes' : 'no'}
                            </span>
                          </div>
                        ))}
                        <div className="flex items-center justify-between pt-1 border-t border-white/[0.05]">
                          <span className="text-slate-500">Containers affected</span>
                          <span className="text-slate-300 font-mono">{diffData.containersAffected}</span>
                        </div>
                      </div>
                      <button
                        onClick={() => { setDiffV1(null); setDiffV2(null) }}
                        className="text-[11px] text-slate-600 hover:text-slate-400 transition-colors"
                      >
                        Clear comparison
                      </button>
                    </div>
                  )}
                </>
              )}
            </div>
          )}

          {/* ── Timeline tab ── */}
          {tab === 'timeline' && (
            <div className="space-y-1">
              {timelineLoading && (
                <div className="space-y-2">
                  {[1,2,3,4].map(i => <Skeleton key={i} className="h-10 w-full" />)}
                </div>
              )}

              {!timelineLoading && timeline.length === 0 && (
                <div className="text-center py-8 text-slate-600 text-xs">
                  No events recorded for this secret yet.
                </div>
              )}

              {!timelineLoading && timeline.length > 0 && (
                <div className="relative pl-5">
                  {/* Vertical line */}
                  <div className="absolute left-2 top-0 bottom-0 w-px bg-white/[0.06]" />

                  {timeline.map((ev, i) => (
                    <div key={i} className="relative mb-4">
                      {/* Dot */}
                      <div className={`absolute -left-3 top-1 w-2 h-2 rounded-full bg-current ${timelineEventColor(ev.type)} opacity-70`} />

                      <div className="space-y-0.5">
                        <div className="flex items-baseline gap-2">
                          <span className={`text-[11px] font-mono ${timelineEventColor(ev.type)}`}>
                            {timelineEventIcon(ev.type)} {ev.type}
                            {ev.version != null && <span className="text-slate-500"> v{ev.version}</span>}
                          </span>
                          <span className="text-[10px] text-slate-700 ml-auto flex-shrink-0">
                            {relativeTime(ev.timestamp)}
                          </span>
                        </div>
                        <p className="text-[11px] text-slate-400 truncate">{ev.description}</p>
                        {ev.actor && (
                          <p className="text-[10px] text-slate-600">
                            {ev.actor}
                            {ev.source && ` · ${sourceLabel(ev.source)}`}
                          </p>
                        )}
                        {ev.driftId && (
                          <a
                            href={`/drift?id=${ev.driftId}`}
                            className="text-[10px] text-amber-400/70 hover:text-amber-400 transition-colors"
                          >
                            Drift #{ev.driftId.slice(0, 8)} →
                          </a>
                        )}
                        {ev.auditId && (
                          <a
                            href={`/audit?id=${ev.auditId}`}
                            className="text-[10px] text-slate-500 hover:text-slate-300 transition-colors"
                          >
                            Audit record →
                          </a>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* ── Compliance tab ── */}
          {tab === 'compliance' && (
            <div className="space-y-5">
              {complianceLoading && (
                <div className="space-y-3">
                  {[1,2,3].map(i => <Skeleton key={i} className="h-16 w-full" />)}
                </div>
              )}

              {!complianceLoading && complianceData && (
                <>
                  {/* Overall badge */}
                  <div className="flex items-center gap-3">
                    {complianceData.overallStatus === 'compliant' && (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                        <CheckCircle className="w-3 h-3" /> Compliant
                      </span>
                    )}
                    {complianceData.overallStatus === 'warning' && (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20">
                        <AlertTriangle className="w-3 h-3" /> Warning
                      </span>
                    )}
                    {complianceData.overallStatus === 'non_compliant' && (
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
                        <XCircle className="w-3 h-3" /> Non-Compliant
                      </span>
                    )}
                  </div>

                  {/* Rotation section */}
                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider">Rotation</p>
                    <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-xs text-slate-500">Status</span>
                        <span className={`text-xs font-medium ${
                          complianceData.rotationStatus === 'compliant' ? 'text-emerald-400' :
                          complianceData.rotationStatus === 'overdue'   ? 'text-red-400' :
                          complianceData.rotationStatus === 'never_rotated' ? 'text-red-400' :
                          'text-slate-500'
                        }`}>
                          {complianceData.rotationStatus === 'compliant'     && 'Compliant'}
                          {complianceData.rotationStatus === 'overdue'       && 'Overdue'}
                          {complianceData.rotationStatus === 'never_rotated' && 'Never rotated'}
                          {complianceData.rotationStatus === 'unknown'       && 'Unknown'}
                        </span>
                      </div>
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-xs text-slate-500">Last rotation</span>
                        <span className="text-xs text-slate-300">
                          {complianceData.lastRotation
                            ? relativeTime(complianceData.lastRotation)
                            : <span className="text-slate-600">Never</span>
                          }
                        </span>
                      </div>
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-xs text-slate-500">Version</span>
                        <span className="text-xs font-mono text-blue-400">
                          {complianceData.versionCount > 0
                            ? `v${complianceData.versionCount}`
                            : <span className="text-slate-600">—</span>
                          }
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Drift section */}
                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider">Drift</p>
                    <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
                      <div className="flex items-center justify-between px-4 py-3">
                        <span className="text-xs text-slate-500">Open findings</span>
                        <span className={`text-xs font-medium ${complianceData.openDrift > 0 ? 'text-amber-400' : 'text-emerald-400'}`}>
                          {complianceData.openDrift > 0 ? `${complianceData.openDrift} open` : '0 — clean'}
                        </span>
                      </div>
                    </div>
                  </div>

                  {/* Evidence section */}
                  <div className="space-y-2">
                    <p className="text-[10px] font-semibold text-slate-500 uppercase tracking-wider">Evidence</p>
                    <div className="rounded-lg border border-white/[0.07] divide-y divide-white/[0.05]">
                      {[
                        { label: 'Versions recorded',  value: complianceData.versionCount },
                        { label: 'Audit events',       value: complianceData.auditCount },
                      ].map(row => (
                        <div key={row.label} className="flex items-center justify-between px-4 py-3">
                          <span className="text-xs text-slate-500">{row.label}</span>
                          <span className="text-xs font-mono text-slate-300">{row.value}</span>
                        </div>
                      ))}
                    </div>
                    <div className="flex gap-2 pt-1">
                      <a
                        href={`/audit?q=${encodeURIComponent(secret.name)}`}
                        className="text-[11px] text-blue-400/70 hover:text-blue-400 transition-colors"
                      >
                        Audit trail →
                      </a>
                      <span className="text-[11px] text-slate-700">·</span>
                      <a
                        href={`/drift?resource=${encodeURIComponent(secret.name)}`}
                        className="text-[11px] text-amber-400/70 hover:text-amber-400 transition-colors"
                      >
                        Drift findings →
                      </a>
                    </div>
                  </div>
                </>
              )}

              {!complianceLoading && !complianceData && (
                <div className="text-center py-8 text-slate-600 text-xs">
                  Compliance data unavailable for this secret.
                </div>
              )}
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

  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkRotateResult | null>(null)
  const [confirmBulk, setConfirmBulk] = useState(false)

  const bulkRotateMutation = useMutation({
    mutationFn: (names: string[]) => bulkApi.bulkRotate(names),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: ['secrets'] })
    },
  })

  const handleBulkRotate = () => {
    const names = Array.from(sel.selected)
    if (names.length > 50) {
      setConfirmBulk(true)
    } else {
      bulkRotateMutation.mutate(names)
    }
  }

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

      {/* Bulk toolbar — visible when any rows selected */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          bulkRotateMutation.isPending
            ? `Rotating ${sel.size} secrets…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} rotated successfully`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed: ${bulkStatus.failures.map(f => f.name).join(', ')}`
            : undefined
        }
        actions={[
          {
            label: bulkRotateMutation.isPending ? 'Rotating…' : 'Rotate',
            onClick: handleBulkRotate,
            disabled: bulkRotateMutation.isPending || sel.size === 0,
          },
        ]}
      />

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
                  <th className="pl-4 pr-2 py-3 w-8">
                    <input
                      type="checkbox"
                      aria-label="Select page"
                      checked={secrets.length > 0 && secrets.every(s => sel.isSelected(s.name))}
                      onChange={() => sel.togglePage(secrets.map(s => s.name))}
                      className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                    />
                  </th>
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
                    <td className="pl-4 pr-2 py-3" onClick={e => e.stopPropagation()}>
                      <input
                        type="checkbox"
                        aria-label={`Select ${secret.name}`}
                        checked={sel.isSelected(secret.name)}
                        onChange={() => sel.toggle(secret.name)}
                        className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                      />
                    </td>
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

      {confirmBulk && (
        <ConfirmModal
          title={`Rotate ${sel.size} secrets?`}
          message={`You are about to rotate ${sel.size} secrets. This will trigger provider webhooks for each secret simultaneously. Continue?`}
          confirmLabel={`Rotate ${sel.size} secrets`}
          onConfirm={() => {
            setConfirmBulk(false)
            bulkRotateMutation.mutate(Array.from(sel.selected))
          }}
          onCancel={() => setConfirmBulk(false)}
        />
      )}

      {/* Detail drawer */}
      {selected && (
        <SecretDrawer secret={selected} onClose={() => setSelected(null)} />
      )}
    </div>
  )
}

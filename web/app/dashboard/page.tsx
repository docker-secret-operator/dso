'use client'

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { FlaskConical, ChevronRight } from 'lucide-react'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'

import { Section } from '@/components/dashboard/section'
import { EstateHero } from '@/components/dashboard/estate-hero'
import {
  NeedsAttention,
  sortAttentionItems,
  type AttentionItem,
} from '@/components/dashboard/needs-attention'
import { OperationalHealth } from '@/components/dashboard/operational-health'
import { RecentActivity } from '@/components/dashboard/recent-activity'

import { classifySecret } from '@/lib/dashboard/rotation'
import { apiClient, type Secret } from '@/lib/api-client'
import type { FailureEvent, DriftFinding } from '@/lib/api/types'
import * as systemApi from '@/lib/api/system'
import * as operationsApi from '@/lib/api/operations'
import * as auditApi from '@/lib/api/audit'
import * as discoveryApi from '@/lib/api/discovery'
import * as driftApi from '@/lib/api/drift'
import * as recommendationsApi from '@/lib/api/recommendations'
import type { Recommendation } from '@/lib/api/recommendations'
import * as forecastsApi from '@/lib/api/forecasts'

const MAX_ATTENTION_ITEMS = 7

function relativeSync(ts?: string): string | undefined {
  if (!ts) return undefined
  const diff = Date.now() - new Date(ts).getTime()
  const minutes = Math.floor(diff / 60000)
  if (!Number.isFinite(minutes) || minutes < 1) return 'synced just now'
  if (minutes < 60) return `synced ${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `synced ${hours}h ago`
}

const CATEGORY_KIND: Record<string, AttentionItem['kind']> = {
  rotation: 'overdue',
  drift: 'drift',
  compliance: 'error',
  policy: 'provider',
  operational: 'failed-sync',
}

function buildAttentionItems(
  secrets: Secret[],
  recentFailures: FailureEvent[] | undefined,
  alerts: Array<{ id: string; type: string; severity: string; message: string }> | undefined,
  driftFindings: DriftFinding[] = [],
  recommendations: Recommendation[] = []
): AttentionItem[] {
  const items: AttentionItem[] = []
  const failures = Array.isArray(recentFailures) ? recentFailures : []
  const alertList = Array.isArray(alerts) ? alerts : []

  for (const s of secrets) {
    const bucket = classifySecret(s)
    if (bucket === 'overdue') {
      items.push({
        id: `overdue-${s.name}`,
        kind: 'overdue',
        severity: 'critical',
        message: 'Overdue for rotation',
        target: s.name,
        href: '/secrets',
      })
    } else if (bucket === 'errored') {
      items.push({
        id: `error-${s.name}`,
        kind: 'error',
        severity: 'warning',
        message: 'Secret is reporting an error',
        target: `${s.provider}/${s.name}`,
        href: '/secrets',
      })
    }
  }

  for (const f of failures) {
    items.push({
      id: `fail-${f.id}`,
      kind: 'failed-sync',
      severity: 'warning',
      message: f.reason || 'Execution failed',
      target: f.worker_id || f.execution_id,
      href: '/executions',
    })
  }

  for (const a of alertList) {
    if (!a.type?.toLowerCase().includes('provider')) continue
    items.push({
      id: `provider-${a.id}`,
      kind: 'provider',
      severity: a.severity === 'critical' || a.severity === 'error' ? 'critical' : 'warning',
      message: a.message,
      target: a.type,
      href: '/alerts',
    })
  }

  // High/critical open drift findings surface in the attention queue.
  for (const f of driftFindings) {
    if (f.status !== 'detected') continue
    if (f.severity !== 'critical' && f.severity !== 'high') continue
    items.push({
      id: `drift-${f.id}`,
      kind: 'drift',
      severity: f.severity === 'critical' ? 'critical' : 'warning',
      message: f.description,
      target: f.secret_name || f.resource,
      href: '/drift',
    })
  }

  // P8 live recommendations: only critical/high, not already covered by drift/overdue above.
  const existingIds = new Set(items.map((i) => i.id))
  for (const rec of recommendations) {
    if (rec.priority !== 'critical' && rec.priority !== 'high') continue
    const itemId = `rec-${rec.id}`
    if (existingIds.has(itemId)) continue
    const kind = CATEGORY_KIND[rec.category] ?? 'error'
    items.push({
      id: itemId,
      kind,
      severity: rec.priority === 'critical' ? 'critical' : 'warning',
      message: rec.title,
      target: rec.resource,
      href: '/recommendations',
    })
  }

  return sortAttentionItems(items).slice(0, MAX_ATTENTION_ITEMS)
}

function DashboardContent() {
  const { data: postureData, isLoading: postureLoading } = useQuery({
    queryKey: ['dashboard-posture'],
    queryFn: () => apiClient.getPosture(),
    refetchInterval: 60000,
  })

  const { data: opsData, isLoading: opsLoading } = useQuery({
    queryKey: ['operations-dashboard'],
    queryFn: () => operationsApi.getOperationsDashboard(),
    refetchInterval: 30000,
  })

  const { data: alerts, isLoading: alertsLoading } = useQuery({
    queryKey: ['alerts-dashboard'],
    queryFn: () => operationsApi.getAlerts({ limit: 20 }),
    refetchInterval: 30000,
  })

  const { data: recentAudit, isLoading: auditLoading } = useQuery({
    queryKey: ['audit-recent'],
    queryFn: () => auditApi.getAuditEvents({ limit: 8 }),
    refetchInterval: 30000,
  })

  const { data: discovery } = useQuery({
    queryKey: ['discovery-summary'],
    queryFn: () => discoveryApi.getDiscoverySummary(),
    refetchInterval: 60000,
  })

  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: () => systemApi.getHealth(),
    refetchInterval: 30000,
  })

  const { data: driftData } = useQuery({
    queryKey: ['drift', 'findings'],
    queryFn: () => driftApi.listFindings(),
    refetchInterval: 60000,
  })

  const { data: recsData } = useQuery({
    queryKey: ['recommendations', 'dashboard'],
    queryFn: () => recommendationsApi.listRecommendations({ pageSize: 50 }),
    refetchInterval: 60000,
  })

  const { data: forecastData } = useQuery({
    queryKey: ['forecasts', 'dashboard'],
    queryFn: () => forecastsApi.listForecasts({ pageSize: 5 }),
    refetchInterval: 120000,
  })

  const posture = useMemo(() => ({
    fresh:        postureData?.fresh        ?? 0,
    aging:        postureData?.aging        ?? 0,
    overdue:      postureData?.overdue      ?? 0,
    errored:      postureData?.secretErrors ?? 0,
    unknown:      postureData?.unknown      ?? 0,
    total:        postureData?.managedSecrets ?? 0,
    needRotation: postureData?.needRotation ?? 0,
  }), [postureData])

  const driftFindings = driftData?.findings ?? []
  const driftedCount = postureData?.driftedCount ?? driftFindings.filter(f => f.status === 'detected').length

  const attentionItems = useMemo(
    () => buildAttentionItems([], opsData?.recent_failures, alerts, driftFindings, recsData?.recommendations ?? []),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [opsData, alerts, driftData, recsData]
  )

  const coverage =
    discovery && discovery.total > 0 ? (discovery.managed / discovery.total) * 100 : discovery ? 100 : null

  const lastSync = relativeSync(opsData?.timestamp ?? (health as any)?.timestamp)

  return (
    <ErrorBoundary>
      <div className="min-h-[calc(100vh-3rem)] bg-[#0B1020]">
        <div className="mx-auto max-w-[1400px] p-6 space-y-5">
          {/* 1 — Secret estate hero (signature: posture + rotation band fused) */}
          <EstateHero
            posture={posture}
            coverage={coverage}
            lastSyncLabel={lastSync}
            loading={postureLoading}
            drifted={driftedCount}
          />

          {/* 2 — Needs attention */}
          <Section
            title="Needs attention"
            meta={attentionItems.length > 0 ? `${attentionItems.length} item${attentionItems.length === 1 ? '' : 's'}` : undefined}
          >
            <NeedsAttention
              items={attentionItems}
              loading={opsLoading || alertsLoading}
            />
          </Section>

          {/* 3 + 4 — Operational health & recent activity */}
          <div className="grid grid-cols-1 lg:grid-cols-5 gap-5">
            <div className="lg:col-span-3">
              <Section title="Operational health" href="/operations" className="h-full">
                <OperationalHealth data={opsData} loading={opsLoading} />
              </Section>
            </div>
            <div className="lg:col-span-2">
              <Section title="Recent activity" href="/audit" className="h-full">
                <RecentActivity events={recentAudit?.events ?? []} loading={auditLoading} />
              </Section>
            </div>
          </div>

          {/* 5 — Labs: Forecasts (Beta) — predictions never outrank measurements */}
          {(() => {
            const topForecasts = (forecastData?.forecasts ?? [])
              .filter(f => f.severity === 'critical' || f.severity === 'high')
              .slice(0, 3)
            if (topForecasts.length === 0) return null
            return (
              <div className="rounded-xl border border-indigo-500/15 bg-indigo-500/[0.03] p-5">
                <div className="flex items-center justify-between mb-4">
                  <div className="flex items-center gap-2">
                    <FlaskConical className="h-4 w-4 text-indigo-400" />
                    <span className="text-sm font-medium text-slate-300">Labs — Risk Forecasts</span>
                    <span className="rounded border border-indigo-500/40 bg-indigo-500/10 px-1.5 py-0.5 text-[10px] font-bold text-indigo-300 uppercase tracking-wider">Beta</span>
                  </div>
                  <Link href="/forecasts" className="text-xs text-indigo-400 hover:text-indigo-300 transition-colors">
                    View all →
                  </Link>
                </div>
                <p className="text-[11px] text-slate-500 mb-3">
                  Statistical estimates — not measurements. Forecasts disappear when evidence resolves.
                </p>
                <ul className="space-y-1.5">
                  {topForecasts.map(fc => (
                    <li key={fc.id}>
                      <Link
                        href="/forecasts"
                        className="flex items-center gap-3 rounded-md px-3 py-2 hover:bg-white/[0.03] transition-colors group"
                      >
                        <span className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${fc.severity === 'critical' ? 'bg-red-400' : 'bg-orange-400'}`} />
                        <span className="text-xs text-slate-300 flex-1 truncate">{fc.title}</span>
                        <span className="text-[10px] text-slate-500 tabular-nums">{Math.round(fc.confidence * 100)}%</span>
                        <ChevronRight className="h-3 w-3 text-slate-500 group-hover:text-slate-300 transition-colors" />
                      </Link>
                    </li>
                  ))}
                </ul>
              </div>
            )
          })()}
        </div>
      </div>
    </ErrorBoundary>
  )
}

export default function DashboardPage() {
  return (
    <ProtectedRoute>
      <DashboardContent />
    </ProtectedRoute>
  )
}

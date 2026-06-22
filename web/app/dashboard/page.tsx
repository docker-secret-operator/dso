'use client'

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'

import { Section } from '@/components/dashboard/section'
import { PostureSummary } from '@/components/dashboard/posture-summary'
import { RotationHealthStrip } from '@/components/dashboard/rotation-health-strip'
import {
  NeedsAttention,
  sortAttentionItems,
  type AttentionItem,
} from '@/components/dashboard/needs-attention'
import { OperationalHealth } from '@/components/dashboard/operational-health'
import { RecentActivity } from '@/components/dashboard/recent-activity'
import { Skeleton } from '@/components/ui-modern'

import { deriveRotationPosture, classifySecret } from '@/lib/dashboard/rotation'
import { apiClient, type Secret } from '@/lib/api-client'
import type { FailureEvent } from '@/lib/api/types'
import * as systemApi from '@/lib/api/system'
import * as operationsApi from '@/lib/api/operations'
import * as auditApi from '@/lib/api/audit'
import * as discoveryApi from '@/lib/api/discovery'

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

function buildAttentionItems(
  secrets: Secret[],
  recentFailures: FailureEvent[] | undefined,
  alerts: Array<{ id: string; type: string; severity: string; message: string }> | undefined
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
    } else if (bucket === 'drifted') {
      items.push({
        id: `drift-${s.name}`,
        kind: 'drift',
        severity: 'warning',
        message: 'Differs from provider',
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

  return sortAttentionItems(items).slice(0, MAX_ATTENTION_ITEMS)
}

function DashboardContent() {
  const { data: secrets, isLoading: secretsLoading } = useQuery({
    queryKey: ['secrets-all'],
    queryFn: () => apiClient.getSecrets(),
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

  const secretList = useMemo(() => (Array.isArray(secrets) ? secrets : []), [secrets])
  const posture = useMemo(() => deriveRotationPosture(secretList), [secretList])

  const attentionItems = useMemo(
    () => buildAttentionItems(secretList, opsData?.recent_failures, alerts),
    [secretList, opsData, alerts]
  )

  const coverage =
    discovery && discovery.total > 0 ? (discovery.managed / discovery.total) * 100 : discovery ? 100 : null

  const lastSync = relativeSync(opsData?.timestamp ?? (health as any)?.timestamp)

  return (
    <ErrorBoundary>
      <div className="min-h-[calc(100vh-3rem)] bg-[#0B1020]">
        <div className="mx-auto max-w-[1400px] p-6 space-y-5">
          {/* 1 — Posture summary */}
          <PostureSummary
            managedSecrets={posture.total}
            needRotation={posture.needRotation}
            overdue={posture.overdue}
            aging={posture.aging}
            drifted={posture.drifted}
            coverage={coverage}
            lastSyncLabel={lastSync}
            loading={secretsLoading}
          />

          {/* 2 — Rotation health strip (signature) */}
          <Section title="Rotation health" meta={`${posture.total.toLocaleString()} secrets`} href="/secrets">
            {secretsLoading ? (
              <Skeleton className="h-16 w-full rounded" />
            ) : (
              <RotationHealthStrip posture={posture} />
            )}
          </Section>

          {/* 3 — Needs attention */}
          <Section
            title="Needs attention"
            meta={attentionItems.length > 0 ? `${attentionItems.length} item${attentionItems.length === 1 ? '' : 's'}` : undefined}
          >
            <NeedsAttention
              items={attentionItems}
              loading={secretsLoading || opsLoading || alertsLoading}
            />
          </Section>

          {/* 4 + 5 — Operational health & recent activity */}
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

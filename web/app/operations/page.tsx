'use client'

import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'next/navigation'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'
import { Skeleton, Card } from '@/components/ui-modern'
import {
  OperationsOverview,
  QueueHealthCard,
  WorkerHealthCard,
  ExecutionTable,
  AlertsPanel,
  RecoveryEventsTable,
  MetricsHistoryChart,
  ExecutionDetailsDrawer,
} from '@/components/operations'
import * as operationsApi from '@/lib/api/operations'
import type { Execution, FailureEvent } from '@/lib/api/types'

/**
 * Shared query configuration for all operations queries
 */
const QUERY_CONFIG = {
  refetchInterval: 30000,      // 30s auto-refresh
  staleTime: 25000,            // Stale after 25s
  retry: 2,                    // Retry twice
  refetchOnWindowFocus: false,
}

/**
 * Main Operations Console page
 * Displays operations dashboard with real-time monitoring
 */
function OperationsContent() {
  const [selectedExecution, setSelectedExecution] = useState<Execution | null>(null)
  const searchParams = useSearchParams()

  useEffect(() => {
    const execId = searchParams?.get('exec')
    if (!execId) return
    operationsApi.getExecution(execId)
      .then(setSelectedExecution)
      .catch(() => {})
  }, [searchParams])

  /**
   * Query 1: Operations Dashboard (KPIs, queue health, worker health, execution status)
   */
  const {
    data: operationsDashboard,
    isLoading: dashboardLoading,
    error: dashboardError,
  } = useQuery({
    queryKey: ['operations/dashboard'],
    queryFn: () => operationsApi.getOperationsDashboard(),
    ...QUERY_CONFIG,
  })

  /**
   * Query 2: Alerts
   */
  const {
    data: alerts,
    isLoading: alertsLoading,
    error: alertsError,
  } = useQuery({
    queryKey: ['operations/alerts'],
    queryFn: () => operationsApi.getAlerts({ limit: 10 }),
    ...QUERY_CONFIG,
  })

  /**
   * Query 3: Recovery Events
   */
  const {
    data: recoveryEvents,
    isLoading: recoveryLoading,
    error: recoveryError,
  } = useQuery({
    queryKey: ['operations/recovery-events'],
    queryFn: () => operationsApi.getRecoveryEvents({ limit: 10 }),
    ...QUERY_CONFIG,
  })

  /**
   * Query 4: Metrics History
   */
  const {
    data: metricsHistory,
    isLoading: metricsLoading,
    error: metricsError,
  } = useQuery({
    queryKey: ['operations/metrics-history'],
    queryFn: () => operationsApi.getMetricsHistory(),
    ...QUERY_CONFIG,
  })

  /**
   * Query 5: Executions
   */
  const {
    data: executionList,
    isLoading: executionsLoading,
    error: executionsError,
  } = useQuery({
    queryKey: ['executions'],
    queryFn: () => operationsApi.getExecutions({ limit: 20 }),
    ...QUERY_CONFIG,
  })

  return (
    <ErrorBoundary>
      <div className="min-h-screen bg-slate-950">
        <div className="mx-auto max-w-7xl space-y-6 px-4 py-8">
          {/* ── Header ── */}
          <div>
            <h1 className="text-3xl font-bold text-white mb-2">Operations Console</h1>
            <p className="text-slate-400">Monitor real-time system operations, queue health, and execution status</p>
          </div>

          {/* ── Operations Overview ── */}
          <OperationsOverview
            data={operationsDashboard}
            isLoading={dashboardLoading}
            error={dashboardError ? 'Failed to load operations overview' : null}
          />

          {/* ── Health Section (Queue & Worker) - 2 columns ── */}
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            {/* Queue Health Card */}
            <QueueHealthCard
              data={operationsDashboard?.queue_health}
              isLoading={dashboardLoading}
              error={dashboardError ? 'Failed to load queue health' : null}
            />

            {/* Worker Health Card */}
            <WorkerHealthCard
              data={operationsDashboard?.worker_health}
              isLoading={dashboardLoading}
              error={dashboardError ? 'Failed to load worker health' : null}
            />
          </div>

          {/* ── Recent Failures ── */}
          {(operationsDashboard?.recent_failures?.length ?? 0) > 0 && (
            <div>
              <h2 className="text-sm font-semibold text-slate-400 uppercase tracking-wider mb-3">
                Recent Failures
              </h2>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {operationsDashboard!.recent_failures.map((f: FailureEvent) => (
                  <div
                    key={f.id}
                    className="rounded-lg border border-red-500/20 bg-red-500/5 p-4 space-y-3"
                  >
                    <div className="flex items-start gap-2">
                      <span className="w-2 h-2 mt-1.5 rounded-full bg-red-400 flex-shrink-0" />
                      <p className="text-sm text-red-300 font-medium leading-snug">{f.reason || 'Unknown failure'}</p>
                    </div>
                    <div className="space-y-1 text-[12px] text-slate-500">
                      <p>Execution: <code className="font-mono text-slate-400">{f.execution_id.slice(0, 12)}…</code></p>
                      {f.worker_id && <p>Worker: <code className="font-mono text-slate-400">{f.worker_id.slice(0, 12)}</code></p>}
                      <p>{new Date(f.timestamp).toLocaleString()}</p>
                    </div>
                    <button
                      onClick={() => {
                        operationsApi.getExecution(f.execution_id)
                          .then(setSelectedExecution)
                          .catch(() => {})
                      }}
                      className="w-full text-xs text-center py-1.5 rounded border border-red-500/30 text-red-400 hover:bg-red-500/10 hover:text-red-300 transition-colors"
                    >
                      Investigate →
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* ── Execution Table - Full width ── */}
          <ExecutionTable
            executions={executionList?.executions ?? []}
            total={executionList?.count}
            isLoading={executionsLoading}
            error={executionsError ? 'Failed to load executions' : null}
            onSelectExecution={setSelectedExecution}
          />

          {/* ── Alerts & Recovery Events - 3 column layout ── */}
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
            {/* Alerts Panel */}
            {alertsLoading ? (
              <Skeleton className="h-64 rounded-lg" />
            ) : alertsError ? (
              <Card className="p-6 bg-red-500/10 border-red-500/20">
                <p className="text-red-400 text-sm">Failed to load alerts</p>
              </Card>
            ) : (
              <AlertsPanel alerts={alerts ?? []} />
            )}

            {/* Recovery Events Table */}
            <div className="lg:col-span-2">
              {recoveryLoading ? (
                <Skeleton className="h-64 rounded-lg" />
              ) : recoveryError ? (
                <Card className="p-6 bg-red-500/10 border-red-500/20">
                  <p className="text-red-400 text-sm">Failed to load recovery events</p>
                </Card>
              ) : (
                <RecoveryEventsTable events={recoveryEvents ?? []} />
              )}
            </div>
          </div>

          {/* ── Metrics History Chart - Full width ── */}
          {metricsLoading ? (
            <Skeleton className="h-80 rounded-lg" />
          ) : metricsError ? (
            <Card className="p-6 bg-red-500/10 border-red-500/20">
              <p className="text-red-400 text-sm">Failed to load metrics history</p>
            </Card>
          ) : (
            <MetricsHistoryChart data={metricsHistory} />
          )}
        </div>
      </div>

      {/* ── Execution Details Drawer Modal ── */}
      {selectedExecution && (
        <ExecutionDetailsDrawer
          execution={selectedExecution}
          isOpen={!!selectedExecution}
          onClose={() => setSelectedExecution(null)}
        />
      )}
    </ErrorBoundary>
  )
}

/**
 * Wrapped page component with ProtectedRoute
 */
export default function OperationsPage() {
  return (
    <ProtectedRoute>
      <OperationsContent />
    </ProtectedRoute>
  )
}

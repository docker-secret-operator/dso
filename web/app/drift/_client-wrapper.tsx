'use client'

import { useState } from 'react'
import type { ReactNode } from 'react'
import { useQueryClient, useQuery, useMutation } from '@tanstack/react-query'
import { AlertCircle, Play, CheckCircle2, TrendingUp, Clock } from 'lucide-react'
import Link from 'next/link'
import { listFindings, runScan, acknowledgeFinding, resolveFinding, getHistory, getMetrics } from '@/lib/api/drift'
import type { DriftFinding, DriftMetrics } from '@/lib/api/types'
import * as bulkApi from '@/lib/api/bulk'
import type { BulkIdResult } from '@/lib/api/bulk'
import { useSelection } from '@/components/common/useSelection'
import { BulkToolbar } from '@/components/common/BulkToolbar'

const FINDINGS_KEY = ['drift', 'findings'] as const
const HISTORY_KEY = ['drift', 'history'] as const
const METRICS_KEY = ['drift', 'metrics'] as const

const severityColor: Record<string, string> = {
  low: 'bg-green-50 text-green-800 border-green-200',
  medium: 'bg-yellow-50 text-yellow-800 border-yellow-200',
  high: 'bg-orange-50 text-orange-800 border-orange-200',
  critical: 'bg-red-50 text-red-800 border-red-200',
}

const statusColor: Record<string, string> = {
  detected: 'bg-blue-100 text-blue-800',
  acknowledged: 'bg-yellow-100 text-yellow-800',
  resolved: 'bg-green-100 text-green-800',
}

function shortHash(h?: string) {
  if (!h) return '—'
  return h.length > 12 ? h.slice(0, 12) + '…' : h
}

function formatMs(ms?: number) {
  if (!ms) return '—'
  return new Date(ms).toLocaleString()
}

export function DriftDashboardClient() {
  const qc = useQueryClient()

  const { data: findingsData, isLoading } = useQuery({
    queryKey: FINDINGS_KEY,
    queryFn: listFindings,
    refetchInterval: 30_000,
  })

  const { data: historyData } = useQuery({
    queryKey: HISTORY_KEY,
    queryFn: getHistory,
    refetchInterval: 60_000,
  })

  const { data: metrics } = useQuery<DriftMetrics>({
    queryKey: METRICS_KEY,
    queryFn: getMetrics,
    refetchInterval: 30_000,
  })

  const scanMutation = useMutation({
    mutationFn: runScan,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: FINDINGS_KEY })
      qc.invalidateQueries({ queryKey: HISTORY_KEY })
      qc.invalidateQueries({ queryKey: METRICS_KEY })
    },
  })

  const ackMutation = useMutation({
    mutationFn: acknowledgeFinding,
    onSuccess: () => qc.invalidateQueries({ queryKey: FINDINGS_KEY }),
  })

  const resolveMutation = useMutation({
    mutationFn: resolveFinding,
    onSuccess: () => qc.invalidateQueries({ queryKey: FINDINGS_KEY }),
  })

  const sel = useSelection()
  const [bulkStatus, setBulkStatus] = useState<BulkIdResult | null>(null)

  const bulkAckMutation = useMutation({
    mutationFn: (ids: string[]) => bulkApi.bulkDriftAck(ids),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: FINDINGS_KEY })
    },
  })

  const bulkResolveMutation = useMutation({
    mutationFn: (ids: string[]) => bulkApi.bulkDriftResolve(ids),
    onSuccess: (result) => {
      setBulkStatus(result)
      sel.clear()
      qc.invalidateQueries({ queryKey: FINDINGS_KEY })
    },
  })

  const findings = findingsData?.findings ?? []
  const lastScan = historyData?.scans?.[0]?.CreatedAt

  if (isLoading && !findingsData) {
    return <div className="p-8 text-gray-500">Loading…</div>
  }

  return (
    <div className="space-y-8 p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-gray-900">Drift Detection</h1>
          <p className="mt-1 text-sm text-gray-500 flex items-center gap-1">
            <Clock className="h-3.5 w-3.5" />
            {lastScan ? `Last scan: ${new Date(lastScan).toLocaleString()}` : 'No scans yet'}
          </p>
        </div>
        <button
          onClick={() => scanMutation.mutate()}
          disabled={scanMutation.isPending}
          className="flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:opacity-50"
        >
          <Play className="h-4 w-4" />
          {scanMutation.isPending ? 'Scanning…' : 'Run Scan'}
        </button>
      </div>

      {scanMutation.isError && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-red-800 flex items-center gap-2">
          <AlertCircle className="h-4 w-4" />
          Scan failed
        </div>
      )}

      {/* Metrics */}
      {metrics && (
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <MetricCard label="Total Findings" value={metrics.TotalFindings} icon={<TrendingUp className="h-5 w-5" />} />
          <MetricCard label="Critical" value={metrics.CriticalFindings} valueClass="text-red-600" />
          <MetricCard label="Open" value={metrics.OpenFindings} valueClass="text-orange-600" />
          <MetricCard label="Scans Run" value={metrics.Scans} valueClass="text-blue-600" />
        </div>
      )}

      {/* Bulk toolbar */}
      <BulkToolbar
        count={sel.size}
        onClear={() => { sel.clear(); setBulkStatus(null) }}
        status={
          (bulkAckMutation.isPending || bulkResolveMutation.isPending)
            ? `Processing ${sel.size} findings…`
            : bulkStatus
            ? bulkStatus.failed === 0
              ? `${bulkStatus.success} updated`
              : `${bulkStatus.success} succeeded · ${bulkStatus.failed} failed`
            : undefined
        }
        actions={[
          {
            label: 'Acknowledge',
            onClick: () => bulkAckMutation.mutate(Array.from(sel.selected)),
            disabled: bulkAckMutation.isPending || bulkResolveMutation.isPending,
          },
          {
            label: 'Resolve',
            onClick: () => bulkResolveMutation.mutate(Array.from(sel.selected)),
            disabled: bulkAckMutation.isPending || bulkResolveMutation.isPending,
          },
        ]}
      />

      {/* Findings table */}
      <div className="rounded-lg border border-gray-200 bg-white">
        <div className="border-b border-gray-200 px-6 py-4">
          <h2 className="font-semibold text-gray-900">Findings ({findings.length})</h2>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="border-b border-gray-200 bg-gray-50">
              <tr>
                <th className="pl-4 pr-2 py-3 w-8">
                  <input
                    type="checkbox"
                    aria-label="Select page"
                    checked={findings.length > 0 && findings.every(f => sel.isSelected(f.id))}
                    onChange={() => sel.togglePage(findings.map(f => f.id))}
                    className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
                  />
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Severity</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Secret</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Container</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Type</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Expected → Actual</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">Detected</th>
                <th className="px-4 py-3 text-right text-xs font-medium text-gray-500 uppercase">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {findings.length === 0 ? (
                <tr>
                  <td colSpan={9} className="px-6 py-12 text-center text-gray-400">
                    No drift findings — system is in sync
                  </td>
                </tr>
              ) : (
                findings.map(f => (
                  <FindingRow
                    key={f.id}
                    f={f}
                    onAck={(id) => ackMutation.mutate(id)}
                    onResolve={(id) => resolveMutation.mutate(id)}
                    isSelected={sel.isSelected(f.id)}
                    onSelect={sel.toggle}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function FindingRow({
  f,
  onAck,
  onResolve,
  isSelected,
  onSelect,
}: {
  f: DriftFinding
  onAck: (id: string) => void
  onResolve: (id: string) => void
  isSelected: boolean
  onSelect: (id: string) => void
}) {
  const secretName = f.secret_name || f.resource
  const container = f.container

  return (
    <tr className="hover:bg-gray-50">
      <td className="pl-4 pr-2 py-3">
        <input
          type="checkbox"
          aria-label={`Select finding ${f.id}`}
          checked={isSelected}
          onChange={() => onSelect(f.id)}
          className="rounded border-white/20 bg-transparent accent-indigo-500 cursor-pointer"
        />
      </td>
      <td className="px-4 py-3">
        <span className={`rounded border px-2 py-0.5 text-xs font-medium ${severityColor[f.severity] ?? 'bg-gray-50 text-gray-800 border-gray-200'}`}>
          {f.severity}
        </span>
      </td>
      <td className="px-4 py-3 text-sm">
        {secretName ? (
          <Link href={`/secrets?name=${encodeURIComponent(secretName)}`} className="text-blue-600 hover:underline font-mono">
            {secretName}
          </Link>
        ) : '—'}
      </td>
      <td className="px-4 py-3 text-sm">
        {container ? (
          <Link href={`/discovery?container=${encodeURIComponent(container)}`} className="text-blue-600 hover:underline font-mono">
            {container}
          </Link>
        ) : '—'}
      </td>
      <td className="px-4 py-3 text-xs text-gray-500 font-mono">{f.type}</td>
      <td className="px-4 py-3 text-xs font-mono text-gray-700">
        <span title={f.expected_version}>{shortHash(f.expected_version)}</span>
        {' → '}
        <span title={f.actual_version}>{shortHash(f.actual_version)}</span>
      </td>
      <td className="px-4 py-3">
        <span className={`rounded px-2 py-0.5 text-xs font-medium ${statusColor[f.status] ?? 'bg-gray-100 text-gray-800'}`}>
          {f.status}
        </span>
      </td>
      <td className="px-4 py-3 text-xs text-gray-500">{formatMs(f.detected_at)}</td>
      <td className="px-4 py-3">
        <div className="flex justify-end gap-2">
          {f.status === 'detected' && (
            <button
              onClick={() => onAck(f.id)}
              className="rounded p-1 hover:bg-gray-100"
              title="Acknowledge"
            >
              <CheckCircle2 className="h-4 w-4 text-yellow-600" />
            </button>
          )}
          {f.status !== 'resolved' && (
            <button
              onClick={() => onResolve(f.id)}
              className="rounded p-1 hover:bg-gray-100"
              title="Resolve"
            >
              <CheckCircle2 className="h-4 w-4 text-green-600" />
            </button>
          )}
        </div>
      </td>
    </tr>
  )
}

function MetricCard({
  label,
  value,
  icon,
  valueClass = 'text-gray-900',
}: {
  label: string
  value: string | number
  icon?: ReactNode
  valueClass?: string
}) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="flex items-center justify-between">
        <span className="text-sm text-gray-500">{label}</span>
        {icon && <div className="text-gray-400">{icon}</div>}
      </div>
      <div className={`mt-2 text-2xl font-bold ${valueClass}`}>{value}</div>
    </div>
  )
}

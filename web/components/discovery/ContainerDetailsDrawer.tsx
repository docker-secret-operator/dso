'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ContainerMetadata } from '@/lib/api/types'
import { Card, Badge } from '@/components/ui-modern'
import { X, Copy, ChevronDown } from 'lucide-react'
import * as auditApi from '@/lib/api/audit'

interface ContainerDetailsDrawerProps {
  container: ContainerMetadata | null
  onClose: () => void
}

export function ContainerDetailsDrawer({
  container,
  onClose,
}: ContainerDetailsDrawerProps) {
  const [showEnvVars, setShowEnvVars] = useState(false)
  const [copiedId, setCopiedId] = useState(false)

  const { data: auditData } = useQuery({
    queryKey: ['container-audit', container?.container_name],
    queryFn: () => auditApi.getAuditEvents({
      resource_id: container!.container_name,
      resource_type: 'container',
      limit: 3,
    }),
    enabled: !!container,
  })
  const recentAudit = auditData?.events ?? []

  if (!container) return null

  const handleCopyId = async () => {
    await navigator.clipboard.writeText(container.container_id)
    setCopiedId(true)
    setTimeout(() => setCopiedId(false), 2000)
  }

  const classificationColor: Record<string, string> = {
    managed: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    partial: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    unmanaged: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
  }

  const classification = container.dso_awareness?.status ?? 'unmanaged'

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-end md:items-center justify-end md:justify-center p-4">
      <Card className="w-full md:max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
        {/* Header */}
        <div className="px-6 py-4 border-b border-white/[0.06] flex items-center justify-between">
          <h2 className="text-lg font-semibold text-slate-200">Container Details</h2>
          <button
            onClick={onClose}
            className="text-slate-500 hover:text-slate-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto space-y-4 p-6">
          {/* General */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">General</h3>
            <div className="space-y-3">
              <div>
                <p className="text-xs text-slate-500 mb-1">Container ID</p>
                <div className="flex items-center gap-2">
                  <p className="text-sm font-mono text-slate-300 truncate">
                    {container.container_id.slice(0, 12)}
                  </p>
                  <button
                    onClick={handleCopyId}
                    className="text-slate-500 hover:text-slate-300 transition-colors"
                    title="Copy container ID"
                  >
                    <Copy className="w-4 h-4" />
                  </button>
                  {copiedId && <span className="text-xs text-emerald-400">Copied!</span>}
                </div>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Name</p>
                <p className="text-sm text-slate-200">{container.container_name}</p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Image</p>
                <p className="text-sm font-mono text-slate-300">{container.image}</p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Status</p>
                <Badge variant="outline" size="sm">
                  {container.status}
                </Badge>
              </div>
            </div>
          </div>

          {/* Networks */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">Networks</h3>
            <div className="space-y-2">
              {Object.entries(container.networks || {}).map(([name, info]) => (
                <div key={name} className="text-sm">
                  <p className="text-slate-300 font-medium">{name}</p>
                  <p className="text-xs text-slate-500">{(info as any)?.ip || 'N/A'}</p>
                </div>
              ))}
            </div>
          </div>

          {/* Restart Policy */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">Restart Policy</h3>
            <div className="text-sm">
              <p className="text-slate-300 font-medium">
                {(container.restart_policy as any)?.name || 'Unknown'}
              </p>
              {(container.restart_policy as any)?.maximum_retry_count && (
                <p className="text-xs text-slate-500">
                  Max retries: {(container.restart_policy as any).maximum_retry_count}
                </p>
              )}
            </div>
          </div>

          {/* Environment Variables */}
          <div>
            <button
              onClick={() => setShowEnvVars(!showEnvVars)}
              className="flex items-center gap-2 w-full mb-3"
            >
              <ChevronDown
                className={`w-4 h-4 text-slate-500 transition-transform ${
                  showEnvVars ? 'rotate-180' : ''
                }`}
              />
              <h3 className="text-sm font-semibold text-slate-300">
                Environment Variables ({Object.keys(container.env_vars || {}).length})
              </h3>
            </button>

            {showEnvVars && (
              <div className="bg-white/[0.01] border border-white/[0.06] rounded-lg p-3 max-h-64 overflow-y-auto">
                <div className="space-y-2">
                  {Object.entries(container.env_vars || {}).map(([key, value]) => (
                    <div key={key} className="text-xs">
                      <p className="font-mono text-slate-400">
                        {key}=<span className="text-slate-300">{value}</span>
                      </p>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* DSO Awareness */}
          <div>
            <h3 className="text-sm font-semibold text-slate-300 mb-3">DSO Awareness</h3>
            <div className="space-y-3">
              <div>
                <p className="text-xs text-slate-500 mb-1">Classification</p>
                <Badge
                  variant="outline"
                  size="sm"
                  className={classificationColor[classification]}
                >
                  {classification}
                </Badge>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Managed Secrets</p>
                {(container.dso_awareness?.managed_secrets?.length ?? 0) === 0 ? (
                  <p className="text-sm text-slate-200">0</p>
                ) : (
                  <div className="flex flex-wrap gap-1.5 mt-1">
                    {container.dso_awareness.managed_secrets.map(name => (
                      <a
                        key={name}
                        href={`/secrets?name=${encodeURIComponent(name)}`}
                        className="text-[12px] font-mono px-2 py-0.5 rounded bg-blue-500/10 border border-blue-500/20 text-blue-400 hover:text-blue-300 hover:bg-blue-500/15 transition-colors"
                      >
                        {name}
                      </a>
                    ))}
                  </div>
                )}
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Config References</p>
                <p className="text-sm text-slate-200">
                  {container.dso_awareness?.config_refs?.length ?? 0}
                </p>
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Missing Mappings</p>
                <p className="text-sm text-slate-200">
                  {container.dso_awareness?.missing_mappings?.length ?? 0}
                </p>
              </div>
            </div>
          </div>
          {/* Recent audit activity */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-semibold text-slate-300">Recent Activity</h3>
              <a
                href={`/audit?q=${encodeURIComponent(container.container_name)}`}
                className="text-[11px] text-blue-400/70 hover:text-blue-400 transition-colors"
              >
                View all →
              </a>
            </div>
            {recentAudit.length === 0 ? (
              <p className="text-xs text-slate-500">No recent audit events.</p>
            ) : (
              <div className="bg-white/[0.01] border border-white/[0.06] rounded-lg divide-y divide-white/[0.05]">
                {recentAudit.map(ev => (
                  <div key={ev.id} className="px-3 py-2 space-y-0.5">
                    <p className="text-xs text-slate-300 truncate">{ev.action}</p>
                    <p className="text-[11px] text-slate-600">
                      {ev.actor} · {new Date(ev.timestamp).toLocaleString()}
                    </p>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </Card>
    </div>
  )
}

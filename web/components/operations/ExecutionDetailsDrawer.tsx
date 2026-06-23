'use client'

import { useEffect, useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Badge, Skeleton } from '@/components/ui-modern'
import { X, ChevronDown, Copy, CheckCircle2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Execution, JourneyEvent } from '@/lib/api/types'
import * as operationsApi from '@/lib/api/operations'
import * as auditApi from '@/lib/api/audit'

interface ExecutionDetailsDrawerProps {
  execution: Execution | null
  isOpen: boolean
  onClose: () => void
}

/**
 * Drawer showing detailed execution information with 5 collapsible sections:
 * 1. General - basic info and timeline
 * 2. Plan - execution steps
 * 3. Validation - readiness and checks
 * 4. Trace - log events
 * 5. Journey - timeline of events
 */
export function ExecutionDetailsDrawer({ execution, isOpen, onClose }: ExecutionDetailsDrawerProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(new Set(['general']))
  const [copiedField, setCopiedField] = useState<string | null>(null)

  // Handle escape key
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose()
      }
    }

    if (isOpen) {
      window.addEventListener('keydown', handleEscape)
      return () => window.removeEventListener('keydown', handleEscape)
    }
  }, [isOpen, onClose])

  const { data: journey, isLoading: journeyLoading } = useQuery({
    queryKey: ['execution-journey', execution?.id],
    queryFn: () => operationsApi.getExecutionJourney(execution!.id),
    enabled: !!execution && isOpen && expandedSections.has('journey'),
  })

  const { data: chainData, isLoading: chainLoading } = useQuery({
    queryKey: ['correlation-chain', execution?.correlation_id],
    queryFn: () => auditApi.getCorrelationChain(execution!.correlation_id),
    enabled: !!execution?.correlation_id && isOpen &&
             (expandedSections.has('audit') || expandedSections.has('resources')),
  })

  const affectedSecrets = useMemo(() => {
    if (!chainData?.events) return []
    return [...new Set(
      chainData.events
        .filter((e: any) => e.resource_type === 'secret' && e.resource_id)
        .map((e: any) => e.resource_id as string)
    )]
  }, [chainData])

  const affectedContainers = useMemo(() => {
    if (!chainData?.events) return []
    return [...new Set(
      chainData.events
        .filter((e: any) => e.resource_type === 'container' && e.resource_id)
        .map((e: any) => e.resource_id as string)
    )]
  }, [chainData])

  const toggleSection = (section: string) => {
    const newSections = new Set(expandedSections)
    if (newSections.has(section)) {
      newSections.delete(section)
    } else {
      newSections.add(section)
    }
    setExpandedSections(newSections)
  }

  const copyToClipboard = (text: string, field: string) => {
    navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }

  if (!isOpen || !execution) return null

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
      case 'failed':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      case 'running':
        return 'bg-blue-500/10 text-blue-400 border-blue-500/20'
      case 'queued':
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
      case 'cancelled':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/20'
      case 'paused':
        return 'bg-amber-500/10 text-amber-400 border-amber-500/20'
      case 'timed_out':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      default:
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
    }
  }

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleString()
    } catch {
      return dateStr
    }
  }

  const formatDuration = (durationMs?: number) => {
    if (!durationMs) return '—'
    const seconds = Math.floor(durationMs / 1000)
    if (seconds < 60) return `${seconds}s`
    const minutes = Math.floor(seconds / 60)
    return `${minutes}m ${seconds % 60}s`
  }

  const relativeTime = (timestamp: string) => {
    try {
      const diff = Date.now() - new Date(timestamp).getTime()
      if (diff < 60000) return 'just now'
      if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
      if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
      return `${Math.floor(diff / 86400000)}d ago`
    } catch {
      return timestamp
    }
  }

  const sections = [
    { id: 'general',    title: 'General',    icon: 'ℹ️' },
    { id: 'journey',    title: 'Journey',    icon: '🗺️' },
    { id: 'audit',      title: 'Audit',      icon: '📋' },
    { id: 'resources',  title: 'Resources',  icon: '🔗' },
    { id: 'plan',       title: 'Plan',       icon: '📋' },
    { id: 'validation', title: 'Validation', icon: '✓' },
    { id: 'trace',      title: 'Trace',      icon: '📝' },
  ]

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/50 backdrop-blur-sm z-40"
        onClick={onClose}
      />

      {/* Drawer */}
      <div
        className={cn(
          'fixed right-0 top-0 h-full w-full max-w-2xl bg-[#111827] border-l border-white/10 shadow-xl z-50 transition-transform duration-300 flex flex-col',
          isOpen ? 'translate-x-0' : 'translate-x-full'
        )}
      >
        {/* Header */}
        <div className="sticky top-0 bg-[#111827] border-b border-white/10 p-6 flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-white">Execution Details</h2>
            <code className="text-xs text-slate-500 mt-1">{execution.id.substring(0, 16)}…</code>
          </div>
          <button
            onClick={onClose}
            className="p-1 hover:bg-white/10 rounded transition-colors"
            aria-label="Close"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto">
          <div className="p-6 space-y-3">
            {sections.map((section) => (
              <div key={section.id} className="rounded-lg border border-white/5 overflow-hidden">
                <button
                  onClick={() => toggleSection(section.id)}
                  className="w-full flex items-center gap-3 px-4 py-3 hover:bg-white/[0.02] transition-colors text-left"
                >
                  <ChevronDown
                    className={cn(
                      'w-4 h-4 text-slate-500 transition-transform flex-shrink-0',
                      expandedSections.has(section.id) && 'rotate-180'
                    )}
                  />
                  <span className="text-sm font-medium text-slate-300">{section.title}</span>
                  <span className="text-xs text-slate-600">{section.icon}</span>
                </button>

                {expandedSections.has(section.id) && (
                  <div className="px-4 py-3 border-t border-white/5 bg-white/[0.01] space-y-3">
                    {section.id === 'general' && (
                      <>
                        {/* ID */}
                        <div>
                          <p className="text-xs text-slate-500 mb-1">Execution ID</p>
                          <div className="flex items-center gap-2">
                            <code className="text-sm text-slate-300 bg-black/40 px-2 py-1 rounded flex-1 truncate">
                              {execution.id}
                            </code>
                            <button
                              onClick={() => copyToClipboard(execution.id, 'exec-id')}
                              className="p-1 hover:bg-white/10 rounded transition-colors flex-shrink-0"
                              title="Copy ID"
                            >
                              {copiedField === 'exec-id' ? (
                                <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                              ) : (
                                <Copy className="w-4 h-4 text-slate-500" />
                              )}
                            </button>
                          </div>
                        </div>

                        {/* Status */}
                        <div>
                          <p className="text-xs text-slate-500 mb-1">Status</p>
                          <Badge className={getStatusColor(execution.status)}>
                            {execution.status}
                          </Badge>
                        </div>

                        {/* Timeline */}
                        <div className="space-y-2">
                          <p className="text-xs text-slate-500">Created</p>
                          <p className="text-sm text-slate-300">{formatDate(execution.created_at)}</p>

                          {execution.started_at && (
                            <>
                              <p className="text-xs text-slate-500 mt-2">Started</p>
                              <p className="text-sm text-slate-300">{formatDate(execution.started_at)}</p>
                            </>
                          )}

                          {execution.completed_at && (
                            <>
                              <p className="text-xs text-slate-500 mt-2">Completed</p>
                              <p className="text-sm text-slate-300">{formatDate(execution.completed_at)}</p>
                            </>
                          )}

                          {execution.duration_ms !== undefined && (
                            <>
                              <p className="text-xs text-slate-500 mt-2">Duration</p>
                              <p className="text-sm text-slate-300">{formatDuration(execution.duration_ms)}</p>
                            </>
                          )}
                        </div>

                        {/* Readiness Score */}
                        <div>
                          <p className="text-xs text-slate-500 mb-1">Readiness Score</p>
                          <p className="text-sm text-slate-300">—</p>
                        </div>
                      </>
                    )}

                    {section.id === 'plan' && (
                      <div className="text-xs text-slate-500">
                        <p className="mb-2">No plan data available</p>
                        <p>Plan information will be loaded when available via API</p>
                      </div>
                    )}

                    {section.id === 'validation' && (
                      <div className="text-xs text-slate-500">
                        <p className="mb-2">No validation data available</p>
                        <p>Validation checks will be loaded when available via API</p>
                      </div>
                    )}

                    {section.id === 'trace' && (
                      <div className="text-xs text-slate-500">
                        <p className="mb-2">No trace events available</p>
                        <p>Trace logs will be loaded when available via API</p>
                      </div>
                    )}

                    {section.id === 'journey' && (
                      journeyLoading ? (
                        <div className="space-y-2">
                          {[1,2,3].map(i => (
                            <div key={i} className="h-10 bg-white/[0.04] rounded animate-pulse" />
                          ))}
                        </div>
                      ) : !journey?.events?.length ? (
                        <p className="text-xs text-slate-500">No journey events recorded.</p>
                      ) : (
                        <div className="space-y-2">
                          {journey.events.map((ev: JourneyEvent, i: number) => (
                            <div key={i} className="flex items-start gap-3 py-2 border-b border-white/[0.05] last:border-0">
                              <span className={cn(
                                'mt-0.5 w-2 h-2 rounded-full flex-shrink-0',
                                ev.status === 'success' ? 'bg-emerald-400' :
                                ev.status === 'failed'  ? 'bg-red-400' :
                                'bg-slate-500'
                              )} />
                              <div className="flex-1 min-w-0">
                                <p className="text-xs font-medium text-slate-300 truncate">{ev.action}</p>
                                <p className="text-[11px] text-slate-500">
                                  {ev.actor !== 'system' ? `${ev.actor} · ` : ''}{relativeTime(ev.timestamp)}
                                </p>
                                {ev.details && (
                                  <p className="text-[11px] text-slate-600 truncate mt-0.5">{ev.details}</p>
                                )}
                              </div>
                              <Badge className={cn(
                                'text-[10px] flex-shrink-0',
                                ev.status === 'success' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' :
                                ev.status === 'failed'  ? 'bg-red-500/10 text-red-400 border-red-500/20' :
                                'bg-slate-500/10 text-slate-400 border-slate-500/20'
                              )}>
                                {ev.status}
                              </Badge>
                            </div>
                          ))}
                          <p className="text-[11px] text-slate-600 pt-1">
                            {journey.total_steps} step{journey.total_steps === 1 ? '' : 's'} · {formatDuration(journey.duration_ms)}
                          </p>
                        </div>
                      )
                    )}

                    {section.id === 'audit' && (
                      chainLoading ? (
                        <div className="space-y-2">
                          {[1,2,3].map(i => <div key={i} className="h-8 bg-white/[0.04] rounded animate-pulse" />)}
                        </div>
                      ) : !chainData?.events?.length ? (
                        <p className="text-xs text-slate-500">No audit events for this correlation chain.</p>
                      ) : (
                        <div className="space-y-1">
                          {chainData.events.slice(0, 10).map((ev: any, i: number) => (
                            <div key={i} className="flex items-center gap-2 py-1.5 border-b border-white/[0.04] last:border-0">
                              <span className={cn(
                                'w-1.5 h-1.5 rounded-full flex-shrink-0',
                                ev.status === 'success' ? 'bg-emerald-400' :
                                (ev.status === 'failure' || ev.status === 'failed') ? 'bg-red-400' :
                                'bg-slate-500'
                              )} />
                              <span className="text-[12px] text-slate-300 flex-1 truncate">{ev.action}</span>
                              <span className="text-[11px] text-slate-600 flex-shrink-0">{relativeTime(ev.timestamp)}</span>
                            </div>
                          ))}
                          {chainData.events.length > 10 && (
                            <p className="text-[11px] text-slate-600 pt-1">+{chainData.events.length - 10} more events</p>
                          )}
                        </div>
                      )
                    )}

                    {section.id === 'resources' && (
                      chainLoading ? (
                        <div className="space-y-2">
                          {[1,2].map(i => <div key={i} className="h-8 bg-white/[0.04] rounded animate-pulse" />)}
                        </div>
                      ) : (affectedSecrets.length === 0 && affectedContainers.length === 0) ? (
                        <p className="text-xs text-slate-500">No specific resources found in correlation chain.</p>
                      ) : (
                        <div className="space-y-3">
                          {affectedSecrets.length > 0 && (
                            <div>
                              <p className="text-[11px] text-slate-500 uppercase tracking-wider mb-1.5">Secrets</p>
                              <div className="flex flex-wrap gap-1.5">
                                {affectedSecrets.map(name => (
                                  <a
                                    key={name}
                                    href={`/secrets?name=${encodeURIComponent(name)}`}
                                    className="text-[12px] font-mono px-2 py-1 rounded bg-blue-500/10 border border-blue-500/20 text-blue-400 hover:text-blue-300 hover:bg-blue-500/15 transition-colors"
                                  >
                                    {name}
                                  </a>
                                ))}
                              </div>
                            </div>
                          )}
                          {affectedContainers.length > 0 && (
                            <div>
                              <p className="text-[11px] text-slate-500 uppercase tracking-wider mb-1.5">Containers</p>
                              <div className="flex flex-wrap gap-1.5">
                                {affectedContainers.map(name => (
                                  <a
                                    key={name}
                                    href={`/discovery?container=${encodeURIComponent(name)}`}
                                    className="text-[12px] font-mono px-2 py-1 rounded bg-violet-500/10 border border-violet-500/20 text-violet-400 hover:text-violet-300 hover:bg-violet-500/15 transition-colors"
                                  >
                                    {name}
                                  </a>
                                ))}
                              </div>
                            </div>
                          )}
                        </div>
                      )
                    )}
                  </div>
                )}
              </div>
            ))}

            {/* Correlation ID */}
            <div className="rounded-lg border border-white/5 px-4 py-3 bg-white/[0.01]">
              <p className="text-xs text-slate-500 mb-2">Correlation ID</p>
              <div className="flex items-center gap-2">
                <code className="text-sm text-slate-300 bg-black/40 px-2 py-1 rounded flex-1 truncate">
                  {execution.correlation_id}
                </code>
                <button
                  onClick={() => copyToClipboard(execution.correlation_id, 'corr-id')}
                  className="p-1 hover:bg-white/10 rounded transition-colors flex-shrink-0"
                  title="Copy ID"
                >
                  {copiedField === 'corr-id' ? (
                    <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                  ) : (
                    <Copy className="w-4 h-4 text-slate-500" />
                  )}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

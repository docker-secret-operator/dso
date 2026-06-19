'use client'

import { useEffect, useState, useMemo } from 'react'
import { Badge, Skeleton } from '@/components/ui-modern'
import { X, ChevronDown, Copy, CheckCircle2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Execution } from '@/lib/api/types'

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
    { id: 'general', title: 'General', icon: 'ℹ️' },
    { id: 'plan', title: 'Plan', icon: '📋' },
    { id: 'validation', title: 'Validation', icon: '✓' },
    { id: 'trace', title: 'Trace', icon: '📝' },
    { id: 'journey', title: 'Journey', icon: '🗺️' },
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
          'fixed right-0 top-0 h-full w-full max-w-2xl bg-[#111318] border-l border-white/10 shadow-xl z-50 transition-transform duration-300 flex flex-col',
          isOpen ? 'translate-x-0' : 'translate-x-full'
        )}
      >
        {/* Header */}
        <div className="sticky top-0 bg-[#111318] border-b border-white/10 p-6 flex items-center justify-between">
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
                      <div className="text-xs text-slate-500">
                        <p className="mb-2">No journey events available</p>
                        <p>Journey timeline will be loaded when available via API</p>
                      </div>
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

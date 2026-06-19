'use client'

import { useEffect } from 'react'
import { Card, Badge } from '@/components/ui-modern'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Execution } from '@/lib/api/types'

interface ExecutionDetailsDrawerProps {
  execution: Execution | null
  isOpen: boolean
  onClose: () => void
}

/**
 * Drawer modal showing detailed information about a selected execution
 */
export function ExecutionDetailsDrawer({ execution, isOpen, onClose }: ExecutionDetailsDrawerProps) {
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
        return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20'
      case 'paused':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/20'
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
          'fixed right-0 top-0 h-full w-full max-w-md bg-slate-900 border-l border-white/10 shadow-xl z-50 transition-transform duration-300 overflow-y-auto',
          isOpen ? 'translate-x-0' : 'translate-x-full'
        )}
      >
        <div className="sticky top-0 bg-slate-900 border-b border-white/10 p-6 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-white">Execution Details</h2>
          <button
            onClick={onClose}
            className="p-1 hover:bg-white/10 rounded transition-colors"
            aria-label="Close"
          >
            <X className="w-5 h-5 text-slate-400" />
          </button>
        </div>

        <div className="p-6 space-y-6">
          {/* Status Section */}
          <div>
            <h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-3">Status</h3>
            <Badge className={getStatusColor(execution.status)}>
              {execution.status}
            </Badge>
          </div>

          {/* Execution ID Section */}
          <div>
            <h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">Execution ID</h3>
            <code className="block text-sm text-slate-300 bg-black/40 px-3 py-2 rounded border border-white/5 break-all">
              {execution.id}
            </code>
          </div>

          {/* Correlation ID Section */}
          <div>
            <h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">Correlation ID</h3>
            <code className="block text-sm text-slate-300 bg-black/40 px-3 py-2 rounded border border-white/5 break-all">
              {execution.correlation_id}
            </code>
          </div>

          {/* Timestamps Section */}
          <div className="space-y-3 p-4 rounded-lg bg-white/[0.02] border border-white/5">
            <h3 className="text-xs font-semibold text-slate-400 uppercase tracking-wider">Timeline</h3>

            <div>
              <p className="text-xs text-slate-500 mb-1">Created</p>
              <p className="text-sm text-slate-300">{formatDate(execution.created_at)}</p>
            </div>

            {execution.started_at && (
              <div>
                <p className="text-xs text-slate-500 mb-1">Started</p>
                <p className="text-sm text-slate-300">{formatDate(execution.started_at)}</p>
              </div>
            )}

            {execution.completed_at && (
              <div>
                <p className="text-xs text-slate-500 mb-1">Completed</p>
                <p className="text-sm text-slate-300">{formatDate(execution.completed_at)}</p>
              </div>
            )}

            {execution.duration_ms !== undefined && (
              <div>
                <p className="text-xs text-slate-500 mb-1">Duration</p>
                <p className="text-sm text-slate-300">{formatDuration(execution.duration_ms)}</p>
              </div>
            )}
          </div>

          {/* Additional Info */}
          <div className="p-4 rounded-lg bg-blue-500/5 border border-blue-500/10">
            <p className="text-xs text-slate-400">
              Use the execution ID to fetch detailed traces and journey information via the API.
            </p>
          </div>
        </div>
      </div>
    </>
  )
}

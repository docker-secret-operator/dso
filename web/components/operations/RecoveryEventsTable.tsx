'use client'

import { Card, Badge } from '@/components/ui-modern'
import { RefreshCw, Copy, Check } from 'lucide-react'
import { formatRelativeTime } from '@/lib/utils'
import { useState } from 'react'
import type { RecoveryEvent } from '@/lib/api/types'

interface RecoveryEventsTableProps {
  events: RecoveryEvent[]
  isLoading?: boolean
  error?: string
}

/**
 * Timeline table displaying recovery/failure events
 * Task 7B: Display events with type-based colors and relative timestamps
 */
export function RecoveryEventsTable({ events, isLoading, error }: RecoveryEventsTableProps) {
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const getEventTypeVariant = (type: string): 'danger' | 'success' | 'warning' | 'default' => {
    switch (type) {
      case 'worker_failure':
        return 'danger'
      case 'recovery':
        return 'success'
      case 'pause':
        return 'warning'
      case 'cancellation':
        return 'default'
      default:
        return 'default'
    }
  }

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text)
    setCopiedId(id)
    setTimeout(() => setCopiedId(null), 2000)
  }

  if (isLoading) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <RefreshCw className="w-5 h-5 text-blue-400" />
          <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        </div>
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-10 bg-white/[0.02] rounded-lg animate-pulse" />
          ))}
        </div>
      </Card>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <RefreshCw className="w-5 h-5 text-red-400" />
          <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        </div>
        <div className="text-red-400 text-xs">{error}</div>
      </Card>
    )
  }

  if (!events || events.length === 0) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <RefreshCw className="w-5 h-5 text-blue-400" />
          <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        </div>
        <div className="py-8 text-center">
          <p className="text-slate-500 text-sm">No recovery events</p>
        </div>
      </Card>
    )
  }

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-4">
        <RefreshCw className="w-5 h-5 text-blue-400" />
        <h3 className="text-sm font-semibold text-slate-300">Recovery Events</h3>
        <span className="ml-auto text-xs font-medium text-slate-500">
          {events.length} event{events.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div className="space-y-2 max-h-96 overflow-y-auto">
        {events.map((event) => (
          <div
            key={event.id}
            className="p-3 rounded-lg bg-white/[0.01] border border-white/5 hover:bg-white/[0.03] transition-colors"
          >
            <div className="flex items-start gap-3">
              <Badge variant={getEventTypeVariant(event.type)} size="sm">
                {event.type.replace(/_/g, ' ')}
              </Badge>
              <div className="flex-1 min-w-0">
                <div className="flex flex-wrap gap-3 items-center">
                  {event.execution_id && (
                    <button
                      onClick={() => copyToClipboard(event.execution_id, `exec-${event.id}`)}
                      className="group flex items-center gap-1.5 text-xs text-slate-400 hover:text-slate-300 transition-colors"
                      title="Click to copy"
                    >
                      <code className="font-mono">{event.execution_id.substring(0, 8)}</code>
                      {copiedId === `exec-${event.id}` ? (
                        <Check className="w-3 h-3 text-emerald-400" />
                      ) : (
                        <Copy className="w-3 h-3 opacity-0 group-hover:opacity-100 transition-opacity" />
                      )}
                    </button>
                  )}
                  {event.worker_id && (
                    <button
                      onClick={() => copyToClipboard(event.worker_id, `worker-${event.id}`)}
                      className="group flex items-center gap-1.5 text-xs text-slate-400 hover:text-slate-300 transition-colors"
                      title="Click to copy"
                    >
                      <code className="font-mono">{event.worker_id.substring(0, 8)}</code>
                      {copiedId === `worker-${event.id}` ? (
                        <Check className="w-3 h-3 text-emerald-400" />
                      ) : (
                        <Copy className="w-3 h-3 opacity-0 group-hover:opacity-100 transition-opacity" />
                      )}
                    </button>
                  )}
                </div>
                <p className="text-[10px] text-slate-600 mt-1.5">
                  {formatRelativeTime(event.timestamp)}
                </p>
              </div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  )
}

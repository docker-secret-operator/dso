'use client'

import { CorrelationChainResponse } from '@/lib/api/types'
import { Badge, Card, Skeleton } from '@/components/ui-modern'
import { Clock, X } from 'lucide-react'

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(ts))
}

interface CorrelationTimelineProps {
  data?: CorrelationChainResponse | null
  isLoading: boolean
  onClose: () => void
}

export function CorrelationTimeline({ data, isLoading, onClose }: CorrelationTimelineProps) {
  if (isLoading) {
    return (
      <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
        <Card className="w-full max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
          <div className="p-4 border-b border-white/[0.06] flex items-center justify-between">
            <h2 className="text-lg font-semibold">Correlation Chain</h2>
            <button onClick={onClose} className="text-slate-500 hover:text-slate-300">
              <X className="w-5 h-5" />
            </button>
          </div>
          <div className="p-4 space-y-3">
            <Skeleton className="h-12 w-full rounded" count={4} />
          </div>
        </Card>
      </div>
    )
  }

  if (!data) {
    return null
  }

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
        <div className="p-4 border-b border-white/[0.06] flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold">Correlation Chain</h2>
            <p className="text-xs text-slate-500">{data.count} events</p>
          </div>
          <button onClick={onClose} className="text-slate-500 hover:text-slate-300">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          <div className="space-y-1 p-4">
            {data.events.map((e, i) => (
              <div key={e.id} className="relative pl-6 pb-4">
                {/* Timeline line */}
                {i < data.events.length - 1 && (
                  <div className="absolute left-2 top-5 bottom-0 w-0.5 bg-white/[0.1]" />
                )}

                {/* Timeline dot */}
                <div className={`absolute left-0 top-1.5 w-4 h-4 rounded-full border-2 ${
                  e.status === 'success' ? 'bg-emerald-500 border-emerald-600' :
                  e.status === 'failed' ? 'bg-red-500 border-red-600' :
                  'bg-slate-500 border-slate-600'
                }`} />

                {/* Event content */}
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-slate-200">{e.action}</span>
                    <Badge variant="outline" size="sm">{e.resource_type}</Badge>
                  </div>

                  <div className="flex flex-wrap items-center gap-3 text-xs text-slate-500">
                    <span className="flex items-center gap-1">
                      <Clock className="w-3 h-3" />
                      {relTime(e.timestamp)}
                    </span>
                    <span>{e.actor}</span>
                    {e.details && <span className="text-slate-600">{e.details}</span>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="p-3 border-t border-white/[0.06] text-xs text-slate-600">
          Correlation ID: <span className="font-mono text-slate-400">{data.correlation_id}</span>
        </div>
      </Card>
    </div>
  )
}

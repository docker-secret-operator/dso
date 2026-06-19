'use client'

import { ActorTimelineResponse } from '@/lib/api/types'
import { Badge, Card, Skeleton, Button } from '@/components/ui-modern'
import { Clock, X } from 'lucide-react'

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(ts))
}

interface ActorTimelineProps {
  data?: ActorTimelineResponse | null
  isLoading: boolean
  period: '24h' | '7d' | '30d'
  onPeriodChange: (period: '24h' | '7d' | '30d') => void
  onClose: () => void
}

export function ActorTimeline({
  data,
  isLoading,
  period,
  onPeriodChange,
  onClose,
}: ActorTimelineProps) {
  if (isLoading) {
    return (
      <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
        <Card className="w-full max-w-2xl max-h-[80vh] overflow-hidden flex flex-col">
          <div className="p-4 border-b border-white/[0.06] flex items-center justify-between">
            <h2 className="text-lg font-semibold">Actor Timeline</h2>
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
        <div className="p-4 border-b border-white/[0.06]">
          <div className="flex items-center justify-between mb-3">
            <div>
              <h2 className="text-lg font-semibold">{data.actor_name}</h2>
              <p className="text-xs text-slate-500">{data.count} events in period</p>
            </div>
            <button onClick={onClose} className="text-slate-500 hover:text-slate-300">
              <X className="w-5 h-5" />
            </button>
          </div>

          {/* Period filter */}
          <div className="flex gap-2">
            {(['24h', '7d', '30d'] as const).map(p => (
              <Button
                key={p}
                variant={period === p ? 'primary' : 'ghost'}
                size="sm"
                onClick={() => onPeriodChange(p)}
              >
                {p}
              </Button>
            ))}
          </div>
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
                    {e.resource_type && <Badge variant="outline" size="sm">{e.resource_type}</Badge>}
                  </div>

                  <div className="flex flex-wrap items-center gap-3 text-xs text-slate-500">
                    <span className="flex items-center gap-1">
                      <Clock className="w-3 h-3" />
                      {relTime(e.timestamp)}
                    </span>
                    {e.resource && (
                      <span className="font-mono">{e.resource}/{e.resource_id?.slice(0, 8)}</span>
                    )}
                    {e.details && <span className="text-slate-600">{e.details}</span>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </Card>
    </div>
  )
}

'use client'

import { useState, useRef, useEffect, useCallback } from 'react'
import { useWebSocket } from '@/hooks/useWebSocket'
import { apiFetch } from '@/lib/api-fetch'
import { PageHeader, Card, Badge, StatusIndicator, EmptyState, Button } from '@/components/ui-modern'
import { AlertCircle, AlertTriangle, Info, Pause, Play, Trash2, ArrowDown } from 'lucide-react'

// ── Event row ─────────────────────────────────────────────────────────────────

interface WSEvent {
  timestamp: string
  severity: 'info' | 'warning' | 'error'
  message: string
  secret_name?: string
  provider?: string
  error?: string
  action?: string
}

function SeverityIcon({ s }: { s: string }) {
  if (s === 'error')   return <AlertCircle  className="w-3.5 h-3.5 flex-shrink-0" />
  if (s === 'warning') return <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0" />
  return <Info className="w-3.5 h-3.5 flex-shrink-0" />
}

const SEV_STYLES: Record<string, string> = {
  error:   'border-l-red-500    bg-red-500/[0.04]',
  warning: 'border-l-amber-500  bg-amber-500/[0.04]',
  info:    'border-l-blue-500   bg-transparent',
}

const SEV_TEXT: Record<string, string> = {
  error:   'text-red-400',
  warning: 'text-amber-400',
  info:    'text-blue-400',
}

function EventRow({ event }: { event: WSEvent }) {
  const sev = event.severity ?? 'info'
  return (
    <div className={`flex items-start gap-3 px-4 py-3 border-b border-white/[0.04] border-l-2 ${SEV_STYLES[sev] ?? SEV_STYLES.info}`}>
      <span className={`mt-0.5 ${SEV_TEXT[sev] ?? SEV_TEXT.info}`}>
        <SeverityIcon s={sev} />
      </span>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <Badge
            variant={sev === 'error' ? 'danger' : sev === 'warning' ? 'warning' : 'info'}
            size="sm"
          >
            {sev.toUpperCase()}
          </Badge>
          <span className="text-[11px] text-slate-600 font-mono">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        </div>
        <p className="text-sm text-slate-300 mt-1">{event.message}</p>
        {(event.secret_name || event.provider || event.error) && (
          <div className="flex flex-wrap gap-3 mt-1.5 text-[11px] text-slate-600 font-mono">
            {event.secret_name && <span>secret: <span className="text-slate-400">{event.secret_name}</span></span>}
            {event.provider    && <span>provider: <span className="text-slate-400 capitalize">{event.provider}</span></span>}
            {event.error       && <span className="text-red-500">{event.error}</span>}
          </div>
        )}
      </div>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────────────

type SevFilter = 'all' | 'info' | 'warning' | 'error'

export default function EventsPage() {
  const { events: rawEvents, isConnected } = useWebSocket('/api/events/ws')
  const [paused, setPaused]       = useState(false)
  const [sevFilter, setSevFilter] = useState<SevFilter>('all')
  const [autoScroll, setAutoScroll] = useState(true)
  const [buffered, setBuffered]   = useState<WSEvent[]>([])
  const [displayed, setDisplayed] = useState<WSEvent[]>([])
  const listRef = useRef<HTMLDivElement>(null)

  // Newest-first — accumulate events; buffer while paused
  useEffect(() => {
    if (rawEvents.length === 0) return
    const newest = rawEvents[rawEvents.length - 1] as WSEvent
    if (paused) {
      setBuffered(prev => [newest, ...prev])
    } else {
      setDisplayed(prev => [newest, ...prev].slice(0, 500))
    }
  }, [rawEvents]) // eslint-disable-line react-hooks/exhaustive-deps

  const resume = useCallback(() => {
    setPaused(false)
    setDisplayed(prev => [...buffered, ...prev].slice(0, 500))
    setBuffered([])
  }, [buffered])

  // HTTP fallback: seed initial events and keep polling while the WebSocket
  // isn't delivering (e.g. under `next dev`, which can't proxy WS upgrades).
  // In production the WebSocket connects and supersedes this.
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const res = await apiFetch('/api/events')
        if (!res.ok) return
        const data = await res.json()
        const list: WSEvent[] = data.events || []
        if (cancelled || list.length === 0) return
        setDisplayed(prev => {
          const seen = new Set(prev.map(e => `${e.timestamp}-${e.message}`))
          const merged = [...prev, ...list.filter(e => !seen.has(`${e.timestamp}-${e.message}`))]
          merged.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
          return merged.slice(0, 500)
        })
      } catch {
        /* ignore — WebSocket path is primary */
      }
    }
    load()
    const id = setInterval(() => {
      if (!isConnected && !paused) load()
    }, 10000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [isConnected, paused])

  // Auto-scroll to top (newest) when not paused
  useEffect(() => {
    if (autoScroll && !paused && listRef.current) {
      listRef.current.scrollTop = 0
    }
  }, [displayed, autoScroll, paused])

  const filtered = sevFilter === 'all'
    ? displayed
    : displayed.filter(e => e.severity === sevFilter)

  return (
    <div className="p-6 space-y-5 h-full flex flex-col">
      <PageHeader
        title="Events"
        description="Real-time event stream from the DSO agent."
        badge={
          <StatusIndicator
            status={isConnected ? 'healthy' : 'critical'}
            label={isConnected ? 'Connected' : 'Disconnected'}
            pulse={isConnected}
          />
        }
        actions={
          <div className="flex items-center gap-2">
            <button
              onClick={() => setDisplayed([])}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-white/10 text-slate-500 hover:text-slate-300 hover:bg-white/5 transition-colors"
              title="Clear events"
            >
              <Trash2 className="w-3.5 h-3.5" />
              Clear
            </button>
            {paused ? (
              <button
                onClick={resume}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg bg-indigo-600/80 text-white hover:bg-indigo-500 transition-colors"
              >
                <Play className="w-3.5 h-3.5" />
                Resume {buffered.length > 0 && `(${buffered.length} buffered)`}
              </button>
            ) : (
              <button
                onClick={() => setPaused(true)}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-colors"
              >
                <Pause className="w-3.5 h-3.5" />
                Pause
              </button>
            )}
          </div>
        }
      />

      {/* Filter + auto-scroll */}
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-1 bg-[#1a1d24] rounded-lg border border-white/[0.07] p-1">
          {(['all', 'info', 'warning', 'error'] as SevFilter[]).map(s => (
            <button
              key={s}
              onClick={() => setSevFilter(s)}
              className={`px-3 py-1 text-xs rounded-md transition-colors capitalize ${
                sevFilter === s
                  ? 'bg-white/10 text-slate-200 font-medium'
                  : 'text-slate-600 hover:text-slate-400'
              }`}
            >
              {s}
            </button>
          ))}
        </div>

        <button
          onClick={() => setAutoScroll(v => !v)}
          className={`inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border transition-colors ${
            autoScroll
              ? 'border-indigo-500/40 text-indigo-400 bg-indigo-500/10'
              : 'border-white/10 text-slate-600 hover:text-slate-400'
          }`}
        >
          <ArrowDown className="w-3 h-3" />
          Auto-scroll
        </button>

        {paused && (
          <span className="text-xs text-amber-400 animate-pulse">
            Paused — {buffered.length} event{buffered.length !== 1 ? 's' : ''} buffered
          </span>
        )}

        <span className="ml-auto text-xs text-slate-700 tabular-nums">
          {filtered.length} event{filtered.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Event list */}
      <Card className="flex-1 min-h-0 overflow-hidden flex flex-col">
        {!isConnected && (
          <div className="px-4 py-3 border-b border-white/[0.06] bg-amber-500/5 text-xs text-amber-400">
            Connecting to event stream…
          </div>
        )}

        <div ref={listRef} className="flex-1 overflow-y-auto">
          {filtered.length === 0 ? (
            <EmptyState
              icon={<AlertCircle className="w-5 h-5" />}
              title="No events"
              description={isConnected
                ? "Waiting for events from the DSO agent. Trigger a secret rotation to see activity."
                : "Connecting to the WebSocket event stream…"}
            />
          ) : (
            filtered.map((event, i) => (
              <EventRow key={`${event.timestamp}-${i}`} event={event} />
            ))
          )}
        </div>
      </Card>
    </div>
  )
}

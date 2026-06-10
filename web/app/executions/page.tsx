'use client'

import { useQuery } from '@tanstack/react-query'
import { useRouter, useSearchParams } from 'next/navigation'
import { Suspense } from 'react'
import { apiClient, JourneyStep } from '@/lib/api-client'
import {
  ArrowLeft, Clock, CheckCircle2, XCircle, AlertTriangle,
  Play, Pause, RotateCcw, Ban, User, Link2,
} from 'lucide-react'

const STEP_META: Record<string, { icon: React.ReactNode; color: string; label: string }> = {
  queued:    { icon: <Clock className="h-4 w-4" />,        color: 'border-blue-400 bg-blue-50 text-blue-700',    label: 'Queued' },
  started:   { icon: <Play className="h-4 w-4" />,         color: 'border-green-400 bg-green-50 text-green-700', label: 'Started' },
  paused:    { icon: <Pause className="h-4 w-4" />,        color: 'border-yellow-400 bg-yellow-50 text-yellow-700', label: 'Paused' },
  resumed:   { icon: <Play className="h-4 w-4" />,         color: 'border-teal-400 bg-teal-50 text-teal-700',    label: 'Resumed' },
  cancelled: { icon: <Ban className="h-4 w-4" />,          color: 'border-gray-400 bg-gray-50 text-gray-700',    label: 'Cancelled' },
  recovered: { icon: <RotateCcw className="h-4 w-4" />,    color: 'border-purple-400 bg-purple-50 text-purple-700', label: 'Recovered' },
  completed: { icon: <CheckCircle2 className="h-4 w-4" />, color: 'border-green-500 bg-green-50 text-green-800', label: 'Completed' },
  failed:    { icon: <XCircle className="h-4 w-4" />,      color: 'border-red-400 bg-red-50 text-red-700',       label: 'Failed' },
  timed_out: { icon: <AlertTriangle className="h-4 w-4" />,color: 'border-orange-400 bg-orange-50 text-orange-700', label: 'Timed Out' },
  dlq:       { icon: <AlertTriangle className="h-4 w-4" />,color: 'border-orange-500 bg-orange-50 text-orange-700', label: 'DLQ' },
}

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Date(ts).toLocaleString()
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`
}

function StepNode({ s, isLast }: { s: JourneyStep; isLast: boolean }) {
  const meta = STEP_META[s.step] ?? {
    icon: <Clock className="h-4 w-4" />,
    color: 'border-gray-300 bg-muted text-muted-foreground',
    label: s.step,
  }
  return (
    <div className="relative flex gap-4">
      {!isLast && <div className="absolute left-5 top-10 bottom-0 w-0.5 bg-border" />}
      <div className={`flex-shrink-0 flex items-center justify-center h-10 w-10 rounded-full border-2 z-10 ${meta.color}`}>
        {meta.icon}
      </div>
      <div className="flex-1 pb-6">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-semibold text-sm">{meta.label}</span>
          <span className={`text-xs rounded border px-1.5 py-0.5 ${s.status === 'success' ? 'border-green-200 bg-green-50 text-green-700' : 'border-red-200 bg-red-50 text-red-700'}`}>
            {s.status}
          </span>
          <span className="text-xs text-muted-foreground ml-auto">{relTime(s.timestamp)}</span>
        </div>
        <p className="text-xs text-muted-foreground font-mono mt-1">{s.action}</p>
        {s.details && <p className="text-xs text-muted-foreground mt-1 bg-muted/40 rounded px-2 py-1">{s.details}</p>}
        <div className="flex flex-wrap gap-3 mt-1.5 text-xs text-muted-foreground">
          {s.actor && s.actor !== 'system' && (
            <span className="flex items-center gap-1"><User className="h-3 w-3" />{s.actor}</span>
          )}
          {s.correlation_id && (
            <span className="flex items-center gap-1 font-mono"><Link2 className="h-3 w-3" />{s.correlation_id.slice(0, 24)}</span>
          )}
        </div>
      </div>
    </div>
  )
}

function ExecutionJourneyContent() {
  const searchParams = useSearchParams()
  const id = searchParams?.get('id') ?? ''
  const router = useRouter()

  const { data, isLoading } = useQuery({
    queryKey: ['execution', 'journey', id],
    queryFn: () => apiClient.getExecutionJourney(id),
    enabled: !!id,
  })

  const retries = (data?.steps ?? []).filter(s => s.step === 'recovered' || s.step === 'dlq').length
  const lastStep = data?.steps?.at(-1)?.step ?? ''
  const statusColor = lastStep === 'completed' ? 'text-green-600' : lastStep === 'failed' || lastStep === 'timed_out' ? 'text-red-600' : 'text-muted-foreground'

  if (!id) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        <p>No execution ID specified. Navigate here from an execution record.</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <button onClick={() => router.back()} className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Back
      </button>
      <div>
        <h1 className="text-2xl font-semibold">Execution Journey</h1>
        <p className="font-mono text-sm text-muted-foreground mt-0.5">{id}</p>
      </div>
      {data && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {[
            { label: 'Steps', value: String(data.total_steps) },
            { label: 'Duration', value: data.duration_ms > 0 ? formatDuration(data.duration_ms) : '—' },
            { label: 'Retries / DLQ', value: String(retries) },
            { label: 'Status', value: lastStep, extra: statusColor },
          ].map(({ label, value, extra }) => (
            <div key={label} className="rounded-lg border border-border bg-card p-3 text-center">
              <p className="text-xs text-muted-foreground">{label}</p>
              <p className={`text-base font-semibold mt-0.5 capitalize ${extra ?? ''}`}>{value || '—'}</p>
            </div>
          ))}
        </div>
      )}
      {data?.correlation_id && (
        <div className="text-xs text-muted-foreground font-mono bg-muted/30 rounded px-3 py-2">
          Correlation ID: {data.correlation_id}
        </div>
      )}
      <div className="rounded-lg border border-border bg-card p-5">
        <h2 className="text-sm font-semibold mb-5">Lifecycle Timeline</h2>
        {isLoading ? (
          <div className="flex justify-center py-12"><div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" /></div>
        ) : !data || data.steps.length === 0 ? (
          <div className="py-12 text-center text-muted-foreground">
            <p>No journey steps found for this execution.</p>
          </div>
        ) : (
          <div>{data.steps.map((s, i) => <StepNode key={`${s.action}-${s.timestamp}`} s={s} isLast={i === data.steps.length - 1} />)}</div>
        )}
      </div>
    </div>
  )
}

export default function ExecutionJourneyPage() {
  return (
    <Suspense fallback={<div className="p-6 flex justify-center"><div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" /></div>}>
      <ExecutionJourneyContent />
    </Suspense>
  )
}

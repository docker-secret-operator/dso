'use client'

import { useState, Suspense } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useRouter, useSearchParams } from 'next/navigation'
import { apiClient, AuditEvent } from '@/lib/api-client'
import {
  ArrowLeft, User, LogIn, ShieldCheck, CheckCircle2,
  Play, RotateCcw, Key, MonitorSmartphone, AlertCircle, Info,
} from 'lucide-react'

type Period = '24h' | '7d' | '30d'

function actionCategory(action: string): { icon: React.ReactNode; label: string; color: string } {
  if (action.includes('login') || action.includes('auth'))
    return { icon: <LogIn className="h-3.5 w-3.5" />, label: 'Login', color: 'text-blue-600' }
  if (action.includes('review'))
    return { icon: <CheckCircle2 className="h-3.5 w-3.5" />, label: 'Review', color: 'text-teal-600' }
  if (action.includes('approv'))
    return { icon: <ShieldCheck className="h-3.5 w-3.5" />, label: 'Approval', color: 'text-green-600' }
  if (action.includes('execution') || action.includes('exec'))
    return { icon: <Play className="h-3.5 w-3.5" />, label: 'Execution', color: 'text-indigo-600' }
  if (action.includes('dlq') || action.includes('retry'))
    return { icon: <RotateCcw className="h-3.5 w-3.5" />, label: 'DLQ Retry', color: 'text-orange-600' }
  if (action.includes('password') || action.includes('reset'))
    return { icon: <Key className="h-3.5 w-3.5" />, label: 'Password', color: 'text-yellow-600' }
  if (action.includes('session') || action.includes('revok'))
    return { icon: <MonitorSmartphone className="h-3.5 w-3.5" />, label: 'Session', color: 'text-purple-600' }
  if (action.includes('fail') || action.includes('error'))
    return { icon: <AlertCircle className="h-3.5 w-3.5" />, label: 'Failure', color: 'text-red-600' }
  return { icon: <Info className="h-3.5 w-3.5" />, label: action, color: 'text-muted-foreground' }
}

function relTime(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return new Date(ts).toLocaleString()
}

function categorize(events: AuditEvent[]) {
  const counts: Record<string, number> = {}
  for (const e of events) {
    const cat = actionCategory(e.action).label
    counts[cat] = (counts[cat] ?? 0) + 1
  }
  return counts
}

function ActivityRow({ e }: { e: AuditEvent }) {
  const cat = actionCategory(e.action)
  return (
    <div className="flex items-start gap-3 border-b border-border py-2.5 last:border-0 hover:bg-muted/20 px-2 rounded">
      <span className={`mt-0.5 flex-shrink-0 ${cat.color}`}>{cat.icon}</span>
      <div className="flex-1 min-w-0 space-y-0.5">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{e.action}</span>
          <span className={`text-xs rounded border px-1.5 py-0.5 ${e.status === 'success' ? 'border-green-200 bg-green-50 text-green-700' : 'border-red-200 bg-red-50 text-red-700'}`}>
            {e.status}
          </span>
        </div>
        {e.details && <p className="text-xs text-muted-foreground truncate">{e.details}</p>}
        <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
          <span>{relTime(e.timestamp)}</span>
          {e.resource && <span className="font-mono">{e.resource}/{e.resource_id?.slice(0, 12)}</span>}
          {e.ip_address && <span>{e.ip_address}</span>}
          {e.correlation_id && <span className="font-mono text-blue-600">{e.correlation_id.slice(0, 20)}</span>}
        </div>
      </div>
      <span className="text-xs text-muted-foreground font-medium shrink-0 capitalize">{cat.label}</span>
    </div>
  )
}

function ActorActivityContent() {
  const searchParams = useSearchParams()
  const id = searchParams?.get('id') ?? ''
  const router = useRouter()
  const [period, setPeriod] = useState<Period>('24h')

  const { data, isLoading } = useQuery({
    queryKey: ['actor', 'timeline', id, period],
    queryFn: () => apiClient.getActorTimeline(id, period),
    enabled: !!id,
  })

  const events = data?.events ?? []
  const cats = categorize(events)
  const failures = events.filter(e => e.status === 'failure').length

  if (!id) {
    return (
      <div className="p-6 text-center text-muted-foreground">
        <p>No actor ID specified.</p>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6 max-w-3xl mx-auto">
      <button onClick={() => router.back()} className="flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" /> Back
      </button>
      <div className="flex items-center gap-3">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10 text-primary">
          <User className="h-6 w-6" />
        </div>
        <div>
          <h1 className="text-2xl font-semibold">{data?.actor_name || id}</h1>
          <p className="font-mono text-xs text-muted-foreground">{id}</p>
        </div>
      </div>
      <div className="flex gap-1 rounded-md border border-border w-fit overflow-hidden">
        {(['24h', '7d', '30d'] as Period[]).map(p => (
          <button key={p} onClick={() => setPeriod(p)}
            className={`px-4 py-2 text-sm font-medium transition-colors ${period === p ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}>
            {p}
          </button>
        ))}
      </div>
      {events.length > 0 && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div className="rounded-lg border border-border bg-card p-3 text-center">
            <p className="text-xs text-muted-foreground">Total Actions</p>
            <p className="text-lg font-semibold mt-0.5">{events.length}</p>
          </div>
          <div className="rounded-lg border border-border bg-card p-3 text-center">
            <p className="text-xs text-muted-foreground">Failures</p>
            <p className={`text-lg font-semibold mt-0.5 ${failures > 0 ? 'text-red-600' : 'text-green-600'}`}>{failures}</p>
          </div>
          {Object.entries(cats).slice(0, 2).map(([label, count]) => (
            <div key={label} className="rounded-lg border border-border bg-card p-3 text-center">
              <p className="text-xs text-muted-foreground">{label}s</p>
              <p className="text-lg font-semibold mt-0.5">{count}</p>
            </div>
          ))}
        </div>
      )}
      {Object.keys(cats).length > 0 && (
        <div className="flex flex-wrap gap-2">
          {Object.entries(cats).map(([label, count]) => {
            const cat = actionCategory(label.toLowerCase())
            return (
              <span key={label} className={`inline-flex items-center gap-1.5 rounded-full border border-border bg-card px-3 py-1 text-xs font-medium ${cat.color}`}>
                {cat.icon}{label} ({count})
              </span>
            )
          })}
        </div>
      )}
      <div className="rounded-lg border border-border bg-card">
        <div className="border-b border-border px-4 py-2.5">
          <h2 className="text-sm font-semibold">Activity Timeline — last {period}</h2>
        </div>
        <div className="px-2 py-1">
          {isLoading ? (
            <div className="flex justify-center py-12"><div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" /></div>
          ) : events.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground">
              <User className="h-10 w-10 mx-auto mb-3 opacity-20" />
              <p>No activity in the last {period}.</p>
            </div>
          ) : (
            events.map(e => <ActivityRow key={e.id} e={e} />)
          )}
        </div>
      </div>
    </div>
  )
}

export default function ActorActivityPage() {
  return (
    <Suspense fallback={<div className="p-6 flex justify-center"><div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" /></div>}>
      <ActorActivityContent />
    </Suspense>
  )
}

'use client'

import { useQuery } from '@tanstack/react-query'
import Link from 'next/link'
import {
  AlertTriangle, Lock, Zap, Shield, Users,
  ChevronRight, TrendingUp, TrendingDown,
} from 'lucide-react'
import { PageHeader, Card, StatusIndicator, EmptyState, Skeleton } from '@/components/ui-modern'

interface SecurityOverview {
  active_sessions: number
  locked_accounts: number
  disabled_users: number
  failed_logins_24h: number
  successful_logins_24h: number
  password_resets_24h: number
  active_admins: number
  suspicious_activities: number
  trends: Record<string, string>
}

function useSecurity() {
  return useQuery<SecurityOverview>({
    queryKey: ['security-overview'],
    queryFn: async () => {
      const res = await fetch('/api/security/overview')
      if (!res.ok) throw new Error('Failed to load security overview')
      return res.json()
    },
    refetchInterval: 30000,
  })
}

function StatCard({
  title, value, icon, href, trend, urgent,
}: {
  title: string
  value: number
  icon: React.ReactNode
  href: string
  trend?: string
  urgent?: boolean
}) {
  return (
    <Link href={href} className="block group">
      <div className={`
        rounded-xl border transition-all duration-150 p-5
        ${urgent && value > 0
          ? 'bg-red-500/5 border-red-500/20 hover:border-red-500/30'
          : 'bg-[#111318] border-white/[0.07] hover:border-white/[0.14] hover:bg-[#1a1d24]'
        }
      `}>
        <div className="flex items-start justify-between mb-3">
          <span className={urgent && value > 0 ? 'text-red-400' : 'text-slate-600'}>{icon}</span>
          <div className="flex items-center gap-1.5">
            {trend === '↑' && <TrendingUp className="w-3.5 h-3.5 text-red-400" />}
            {trend === '↓' && <TrendingDown className="w-3.5 h-3.5 text-emerald-400" />}
            <ChevronRight className="w-3.5 h-3.5 text-slate-700 group-hover:text-slate-500 transition-colors" />
          </div>
        </div>
        <p className={`text-2xl font-semibold tabular-nums ${urgent && value > 0 ? 'text-red-400' : 'text-slate-100'}`}>
          {value}
        </p>
        <p className="text-xs text-slate-500 mt-0.5">{title}</p>
      </div>
    </Link>
  )
}

export default function SecurityPage() {
  const { data: overview, isLoading, error } = useSecurity()

  return (
    <div className="p-6 space-y-5">
      <PageHeader
        title="Security"
        description="Authentication events, session health, and suspicious activity."
        actions={
          <div className="flex items-center gap-2">
            <Link href="/security/events">
              <button className="inline-flex items-center gap-1.5 px-3 py-2 text-sm rounded-lg border border-white/10 text-slate-300 hover:bg-white/5 transition-colors">
                Events
              </button>
            </Link>
            <Link href="/security/sessions">
              <button className="inline-flex items-center gap-1.5 px-3 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-500 transition-colors">
                Sessions
              </button>
            </Link>
          </div>
        }
      />

      {error && (
        <div className="rounded-lg bg-red-500/10 border border-red-500/20 px-4 py-3 text-sm text-red-400">
          {(error as Error).message}
        </div>
      )}

      {isLoading ? (
        <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
          {Array.from({ length: 6 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-white/[0.07] bg-[#111318] p-5 space-y-3">
              <Skeleton className="h-4 w-20 rounded" />
              <Skeleton className="h-8 w-12 rounded" />
            </div>
          ))}
        </div>
      ) : overview ? (
        <>
          {/* Stats grid */}
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
            <StatCard
              title="Failed Logins (24h)"
              value={overview.failed_logins_24h}
              icon={<AlertTriangle className="w-5 h-5" />}
              href="/security/events?type=LOGIN_FAILURE"
              trend={overview.trends?.failed_logins}
              urgent
            />
            <StatCard
              title="Suspicious Activities"
              value={overview.suspicious_activities}
              icon={<AlertTriangle className="w-5 h-5" />}
              href="/security/suspicious"
              trend={overview.trends?.suspicious_activities}
              urgent
            />
            <StatCard
              title="Locked Accounts"
              value={overview.locked_accounts}
              icon={<Lock className="w-5 h-5" />}
              href="/users"
              urgent
            />
            <StatCard
              title="Active Sessions"
              value={overview.active_sessions}
              icon={<Zap className="w-5 h-5" />}
              href="/security/sessions"
            />
            <StatCard
              title="Password Resets (24h)"
              value={overview.password_resets_24h}
              icon={<Shield className="w-5 h-5" />}
              href="/security/events?type=PASSWORD_RESET"
            />
            <StatCard
              title="Disabled Users"
              value={overview.disabled_users}
              icon={<Users className="w-5 h-5" />}
              href="/users"
            />
          </div>

          {/* System status — from real data */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card className="p-5">
              <h2 className="text-sm font-semibold text-slate-300 mb-4">Login Activity (24h)</h2>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-slate-500">Successful</span>
                  <span className="text-sm font-medium text-emerald-400 tabular-nums">{overview.successful_logins_24h}</span>
                </div>
                <div className="h-1.5 rounded-full bg-white/[0.06] overflow-hidden">
                  <div
                    className="h-full bg-emerald-500 rounded-full"
                    style={{
                      width: `${Math.min(
                        overview.successful_logins_24h / Math.max(overview.successful_logins_24h + overview.failed_logins_24h, 1) * 100,
                        100
                      )}%`
                    }}
                  />
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-sm text-slate-500">Failed</span>
                  <span className="text-sm font-medium text-red-400 tabular-nums">{overview.failed_logins_24h}</span>
                </div>
                <div className="h-1.5 rounded-full bg-white/[0.06] overflow-hidden">
                  <div
                    className="h-full bg-red-500 rounded-full"
                    style={{
                      width: `${Math.min(
                        overview.failed_logins_24h / Math.max(overview.successful_logins_24h + overview.failed_logins_24h, 1) * 100,
                        100
                      )}%`
                    }}
                  />
                </div>
              </div>
            </Card>

            <Card className="p-5">
              <h2 className="text-sm font-semibold text-slate-300 mb-4">System Security Status</h2>
              <div className="space-y-3">
                {[
                  { label: 'Active Admins',       value: overview.active_admins,       ok: overview.active_admins > 0 },
                  { label: 'Sessions Active',      value: overview.active_sessions,     ok: true },
                  { label: 'Suspicious Activity',  value: overview.suspicious_activities, ok: overview.suspicious_activities === 0 },
                ].map(row => (
                  <div key={row.label} className="flex items-center justify-between">
                    <span className="text-sm text-slate-500">{row.label}</span>
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-slate-300 tabular-nums">{row.value}</span>
                      <StatusIndicator status={row.ok ? 'healthy' : 'warning'} pulse={false} />
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          </div>
        </>
      ) : null}
    </div>
  )
}

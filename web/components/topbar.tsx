'use client'

import { usePathname } from 'next/navigation'
import { useAuth } from '@/contexts/AuthContext'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { GlobalSearch } from '@/components/global-search'
import { Bell, ChevronRight, LogOut, User, Settings, RefreshCw } from 'lucide-react'
import { useState, useRef, useEffect } from 'react'
import Link from 'next/link'
import { useQueryClient } from '@tanstack/react-query'

// ── Breadcrumb derivation ─────────────────────────────────────────────────────

const ROUTE_LABELS: Record<string, string> = {
  dashboard:      'Dashboard',
  secrets:        'Secrets',
  alerts:         'Alerts',
  rules:          'Rules',
  incidents:      'Incidents',
  scheduler:      'Scheduler',
  drift:          'Drift Detection',
  recommendations:'Recommendations',
  forecasts:      'Forecasts',
  autonomy:       'Autonomy',
  graph:          'Dependency Graph',
  discovery:      'Discovery',
  events:         'Events',
  audit:          'Audit Logs',
  policies:       'Policies',
  configuration:  'Configuration',
  users:          'Users',
  activity:       'Activity',
  plugins:        'Plugins',
  integrations:   'Integrations',
  analytics:      'Analytics',
  timeline:       'Timeline',
  security:       'Security',
  sessions:       'Sessions',
  suspicious:     'Suspicious Activity',
  settings:       'Settings',
  password:       'Change Password',
  profile:        'Profile',
  backups:        'Backups',
  recovery:       'Recovery',
  admin:          'Admin',
  operations:     'Operations',
  dlq:            'Dead Letter Queue',
  trace:          'Trace',
  changesets:     'Changesets',
  workspace:      'Workspace',
  review:         'Review',
  remediation:    'Remediation',
  executions:     'Executions',
  graph_:         'Graph',
}

function useBreadcrumbs() {
  const pathname = usePathname() || ''
  const segments = pathname.split('/').filter(Boolean)

  if (segments.length === 0) return [{ label: 'Dashboard', href: '/dashboard' }]

  const crumbs = segments.map((seg, i) => ({
    label: ROUTE_LABELS[seg] ?? seg.charAt(0).toUpperCase() + seg.slice(1),
    href: '/' + segments.slice(0, i + 1).join('/'),
  }))

  return crumbs
}

// ── User dropdown ─────────────────────────────────────────────────────────────

function UserMenu() {
  const { user, logout } = useAuth()
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const initials = user?.display_name
    ? user.display_name.split(' ').map((n: string) => n[0]).join('').toUpperCase().slice(0, 2)
    : user?.username?.slice(0, 2).toUpperCase() ?? 'U'

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(v => !v)}
        className="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-white/5 transition-colors"
        aria-label="User menu"
      >
        <div className="w-7 h-7 rounded-full bg-indigo-600 flex items-center justify-center text-xs font-semibold text-white select-none">
          {initials}
        </div>
        <span className="hidden sm:block text-sm font-medium text-slate-300 max-w-[120px] truncate">
          {user?.display_name || user?.username}
        </span>
      </button>

      {open && (
        <div className="absolute right-0 top-full mt-1.5 w-56 bg-[#1a1d24] border border-white/10 rounded-xl shadow-xl z-50 overflow-hidden animate-fade-in">
          <div className="px-4 py-3 border-b border-white/07">
            <p className="text-sm font-semibold text-slate-200 truncate">{user?.display_name || user?.username}</p>
            <p className="text-xs text-slate-500 truncate mt-0.5 capitalize">{user?.role}</p>
          </div>
          <div className="p-1.5">
            <Link
              href="/profile"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm text-slate-300 hover:bg-white/5 hover:text-white transition-colors"
            >
              <User className="w-4 h-4 text-slate-500" />
              Profile
            </Link>
            <Link
              href="/settings"
              onClick={() => setOpen(false)}
              className="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm text-slate-300 hover:bg-white/5 hover:text-white transition-colors"
            >
              <Settings className="w-4 h-4 text-slate-500" />
              Settings
            </Link>
          </div>
          <div className="p-1.5 border-t border-white/07">
            <button
              onClick={() => { setOpen(false); logout() }}
              className="flex w-full items-center gap-2.5 px-3 py-2 rounded-lg text-sm text-red-400 hover:bg-red-500/10 hover:text-red-300 transition-colors"
            >
              <LogOut className="w-4 h-4" />
              Sign out
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Notification bell ─────────────────────────────────────────────────────────

function NotificationBell() {
  const { data: alerts } = useQuery({
    queryKey: ['alerts-count'],
    queryFn: () => apiClient.getAlerts({ limit: 50 }),
    refetchInterval: 15000,
  })

  const activeCount = (alerts as any)?.alerts?.filter((a: any) => a.state === 'active').length ?? 0

  return (
    <Link
      href="/alerts"
      className="relative flex items-center justify-center w-8 h-8 rounded-lg hover:bg-white/5 text-slate-400 hover:text-slate-200 transition-colors"
      aria-label={`Alerts${activeCount > 0 ? ` — ${activeCount} active` : ''}`}
    >
      <Bell className="w-4 h-4" />
      {activeCount > 0 && (
        <span className="absolute -top-0.5 -right-0.5 min-w-[16px] h-4 rounded-full bg-red-500 text-[10px] font-bold text-white flex items-center justify-center px-0.5 leading-none">
          {activeCount > 9 ? '9+' : activeCount}
        </span>
      )}
    </Link>
  )
}

// ── Health dot ────────────────────────────────────────────────────────────────

function HealthDot() {
  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 10000,
  })

  const isUp = health?.status === 'up'

  return (
    <div className="hidden md:flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-white/5 border border-white/07">
      <span
        className={`w-1.5 h-1.5 rounded-full ${isUp ? 'bg-emerald-400 status-dot-healthy' : 'bg-red-400 status-dot-critical'}`}
      />
      <span className="text-xs text-slate-400">{isUp ? 'Operational' : 'Degraded'}</span>
    </div>
  )
}

// ── Refresh button ────────────────────────────────────────────────────────────

function RefreshButton() {
  const qc = useQueryClient()
  const [spinning, setSpinning] = useState(false)

  const handleRefresh = async () => {
    setSpinning(true)
    await qc.invalidateQueries()
    setTimeout(() => setSpinning(false), 600)
  }

  return (
    <button
      onClick={handleRefresh}
      className="flex items-center justify-center w-8 h-8 rounded-lg hover:bg-white/5 text-slate-500 hover:text-slate-300 transition-colors"
      aria-label="Refresh all data"
      title="Refresh all data"
    >
      <RefreshCw className={`w-3.5 h-3.5 ${spinning ? 'animate-spin' : ''}`} />
    </button>
  )
}

// ── Topbar ────────────────────────────────────────────────────────────────────

export function Topbar() {
  const crumbs = useBreadcrumbs()

  return (
    <header className="flex-shrink-0 flex items-center justify-between h-12 px-4 border-b border-white/[0.07] bg-[#0a0b0f]/80 backdrop-blur-sm sticky top-0 z-30">
      {/* Left — breadcrumbs */}
      <nav className="flex items-center gap-1 min-w-0" aria-label="Breadcrumb">
        {crumbs.map((crumb, i) => (
          <span key={crumb.href} className="flex items-center gap-1 min-w-0">
            {i > 0 && <ChevronRight className="w-3.5 h-3.5 text-slate-600 flex-shrink-0" />}
            {i < crumbs.length - 1 ? (
              <Link
                href={crumb.href}
                className="text-sm text-slate-500 hover:text-slate-300 transition-colors truncate"
              >
                {crumb.label}
              </Link>
            ) : (
              <span className="text-sm font-medium text-slate-200 truncate">{crumb.label}</span>
            )}
          </span>
        ))}
      </nav>

      {/* Right — actions */}
      <div className="flex items-center gap-1.5 flex-shrink-0 ml-4">
        <HealthDot />
        <div className="w-px h-4 bg-white/10 mx-1" />
        <GlobalSearch />
        <RefreshButton />
        <NotificationBell />
        <div className="w-px h-4 bg-white/10 mx-1" />
        <UserMenu />
      </div>
    </header>
  )
}

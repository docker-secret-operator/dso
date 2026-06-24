'use client'

import { useState, useEffect } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useAuth } from '@/contexts/AuthContext'
import {
  LayoutDashboard,
  AlertCircle,
  AlertTriangle,
  Clock,
  ChevronDown,
  PanelLeftClose,
  PanelLeftOpen,
  BarChart3,
  Lightbulb,
  Zap,
  GitBranch,
  Bot,
  Shield,
  Lock,
  FileText,
  Settings,
  Users,
  Plug,
  Cable,
  ServerCog,
  Workflow,
  Activity,
  FolderSearch,
  ShieldAlert,
  LineChart,
  Gauge,
  ListChecks,
  Archive,
  GitPullRequest,
  ClipboardCheck,
  Layers,
  Wrench,
} from 'lucide-react'

// ── Types ─────────────────────────────────────────────────────────────────────

type NavItem = {
  label: string
  href: string
  icon: React.ReactNode
  accent?: string
  /** Marks a non-production-ready route (shows an "Experimental" badge). */
  experimental?: boolean
}

type NavGroup = {
  name: string
  items: NavItem[]
  defaultOpen?: boolean
}

// ── Navigation structure ──────────────────────────────────────────────────────

// Production = real, persistent, trusted (see HONESTY_AUDIT.md). Labs = not
// production-ready; clearly separated and badged so nothing misleads operators.
const navGroups: NavGroup[] = [
  {
    name: '',
    items: [
      { label: 'Dashboard', href: '/dashboard', icon: <LayoutDashboard className="w-4 h-4" />, accent: 'indigo' },
    ],
  },
  {
    name: 'Operations',
    defaultOpen: true,
    items: [
      { label: 'Secrets',     href: '/secrets',     icon: <Shield className="w-4 h-4" />,       accent: 'blue' },
      { label: 'Discovery',   href: '/discovery',   icon: <FolderSearch className="w-4 h-4" />, accent: 'sky' },
      { label: 'Operations',  href: '/operations',  icon: <Gauge className="w-4 h-4" />,        accent: 'indigo' },
      { label: 'Executions',  href: '/executions',  icon: <ListChecks className="w-4 h-4" />,   accent: 'violet' },
      { label: 'Events',      href: '/events',      icon: <Zap className="w-4 h-4" />,          accent: 'violet' },
      { label: 'Alerts',      href: '/alerts',      icon: <AlertCircle className="w-4 h-4" />,  accent: 'red' },
      { label: 'Scheduler',   href: '/scheduler',   icon: <Clock className="w-4 h-4" />,        accent: 'slate' },
    ],
  },
  {
    name: 'Governance',
    defaultOpen: false,
    items: [
      { label: 'Audit Logs',    href: '/audit',         icon: <FileText className="w-4 h-4" />,  accent: 'slate' },
      { label: 'Configuration', href: '/configuration', icon: <ServerCog className="w-4 h-4" />, accent: 'slate' },
      { label: 'Backups',       href: '/backups',       icon: <Archive className="w-4 h-4" />,    accent: 'slate' },
    ],
  },
  {
    name: 'Security',
    defaultOpen: false,
    items: [
      { label: 'Overview',   href: '/security',            icon: <ShieldAlert className="w-4 h-4" />,  accent: 'blue' },
      { label: 'Sessions',   href: '/security/sessions',   icon: <Activity className="w-4 h-4" />,     accent: 'slate' },
      { label: 'Suspicious', href: '/security/suspicious', icon: <AlertTriangle className="w-4 h-4" />, accent: 'red' },
    ],
  },
  {
    name: 'Admin',
    defaultOpen: false,
    items: [
      { label: 'Users',        href: '/users',        icon: <Users className="w-4 h-4" />,    accent: 'slate' },
      { label: 'Analytics',    href: '/analytics',    icon: <BarChart3 className="w-4 h-4" />, accent: 'slate' },
      { label: 'Plugins',      href: '/plugins',      icon: <Plug className="w-4 h-4" />,      accent: 'slate' },
      { label: 'Integrations', href: '/integrations', icon: <Cable className="w-4 h-4" />,     accent: 'slate' },
    ],
  },
  {
    name: 'Labs',
    defaultOpen: false,
    items: [
      { label: 'Incidents',       href: '/incidents',       icon: <AlertTriangle className="w-4 h-4" />, accent: 'slate', experimental: true },
      { label: 'Drift',           href: '/drift',           icon: <GitBranch className="w-4 h-4" />,     accent: 'slate', experimental: true },
      { label: 'Policies',        href: '/policies',        icon: <Lock className="w-4 h-4" />,          accent: 'slate', experimental: true },
      { label: 'Recommendations', href: '/recommendations', icon: <Lightbulb className="w-4 h-4" />,     accent: 'slate', experimental: true },
      { label: 'Forecasts',       href: '/forecasts',       icon: <LineChart className="w-4 h-4" />,     accent: 'slate', experimental: true },
      { label: 'Autonomy',        href: '/autonomy',        icon: <Bot className="w-4 h-4" />,           accent: 'slate', experimental: true },
      { label: 'Dep. Graph',      href: '/graph',           icon: <Workflow className="w-4 h-4" />,      accent: 'slate', experimental: true },
      { label: 'Changesets',      href: '/changesets',      icon: <GitPullRequest className="w-4 h-4" />, accent: 'slate', experimental: true },
      { label: 'Review',          href: '/review',          icon: <ClipboardCheck className="w-4 h-4" />, accent: 'slate', experimental: true },
      { label: 'Workspace',       href: '/workspace',       icon: <Layers className="w-4 h-4" />,         accent: 'slate', experimental: true },
      { label: 'Remediation',     href: '/remediation',     icon: <Wrench className="w-4 h-4" />,         accent: 'slate', experimental: true },
    ],
  },
]

// ── Accent color map ──────────────────────────────────────────────────────────

const accentBg: Record<string, string> = {
  indigo:  'bg-indigo-500/15 text-indigo-400',
  blue:    'bg-blue-500/15   text-blue-400',
  sky:     'bg-sky-500/15    text-sky-400',
  violet:  'bg-violet-500/15 text-violet-400',
  red:     'bg-red-500/15    text-red-400',
  orange:  'bg-orange-500/15 text-orange-400',
  amber:   'bg-amber-500/15  text-amber-400',
  purple:  'bg-purple-500/15 text-purple-400',
  cyan:    'bg-cyan-500/15   text-cyan-400',
  emerald: 'bg-emerald-500/15 text-emerald-400',
  slate:   'bg-slate-500/15  text-slate-400',
}

// ── CSS variable for sidebar width (so topbar and content can respond) ────────

const SIDEBAR_W_EXPANDED = 220
const SIDEBAR_W_COLLAPSED = 56

// ── Component ─────────────────────────────────────────────────────────────────

export function SidebarPremium() {
  const pathname = usePathname() || ''
  const { role } = useAuth()
  const [collapsed, setCollapsed] = useState(false)
  // Initialise every named group explicitly so that defaultOpen: false groups
  // actually start closed. Previously, groups absent from the map evaluated as
  // `undefined !== false` → true, making everything open at first render.
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(navGroups.filter(g => g.name).map(g => [g.name, g.defaultOpen ?? false]))
  )

  // Roles that may access the Admin nav group
  const isAdmin = role === 'admin'

  // Sync CSS variable so layout.tsx offset tracks correctly
  useEffect(() => {
    document.documentElement.style.setProperty(
      '--sidebar-width',
      `${collapsed ? SIDEBAR_W_COLLAPSED : SIDEBAR_W_EXPANDED}px`
    )
  }, [collapsed])

  const toggleGroup = (name: string) =>
    setOpenGroups(prev => ({ ...prev, [name]: !prev[name] }))

  const isActive = (href: string) =>
    pathname === href || (href !== '/' && pathname.startsWith(href + '/'))

  return (
    <aside
      className="fixed left-0 top-0 h-screen flex flex-col bg-[#111827] border-r border-white/[0.07] z-40 transition-all duration-200 ease-spring"
      style={{ width: collapsed ? SIDEBAR_W_COLLAPSED : SIDEBAR_W_EXPANDED }}
    >
      {/* ── Logo / collapse toggle ── */}
      <div className="flex items-center justify-between h-12 px-3 border-b border-white/[0.07] flex-shrink-0">
        {!collapsed && (
          <div className="flex items-center gap-2">
            <div className="w-6 h-6 rounded bg-indigo-600 flex items-center justify-center flex-shrink-0">
              <Shield className="w-3.5 h-3.5 text-white" />
            </div>
            <span className="text-sm font-semibold text-slate-100 tracking-tight">DSO</span>
          </div>
        )}
        {collapsed && (
          <div className="w-6 h-6 rounded bg-indigo-600 flex items-center justify-center mx-auto">
            <Shield className="w-3.5 h-3.5 text-white" />
          </div>
        )}
        {!collapsed && (
          <button
            onClick={() => setCollapsed(true)}
            className="p-1.5 rounded-md text-slate-600 hover:text-slate-300 hover:bg-white/5 transition-colors"
            aria-label="Collapse sidebar"
          >
            <PanelLeftClose className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* ── Expand button when collapsed ── */}
      {collapsed && (
        <button
          onClick={() => setCollapsed(false)}
          className="mx-auto mt-2 p-1.5 rounded-md text-slate-600 hover:text-slate-300 hover:bg-white/5 transition-colors"
          aria-label="Expand sidebar"
        >
          <PanelLeftOpen className="w-4 h-4" />
        </button>
      )}

      {/* ── Navigation ── */}
      <nav className="flex-1 overflow-y-auto px-2 py-3 space-y-0.5" aria-label="Main navigation">
        {navGroups.map((group, gi) => {
          // Gate Admin section to admin role only
          if (group.name === 'Admin' && !isAdmin) return null

          const groupOpen = !group.name || openGroups[group.name] === true
          const isCollapsible = !!group.name

          return (
            <div key={gi} className="mb-1">
              {/* Group header */}
              {group.name && !collapsed && (
                <button
                  onClick={() => isCollapsible && toggleGroup(group.name)}
                  className="flex items-center justify-between w-full px-2 py-1.5 mb-0.5 group"
                  title={group.name === 'Labs' ? 'Experimental features — not production-ready, state may not persist' : undefined}
                >
                  <span className={`text-[10px] font-semibold uppercase tracking-widest transition-colors ${
                    group.name === 'Labs'
                      ? 'text-amber-600/70 group-hover:text-amber-500/80'
                      : 'text-slate-600 group-hover:text-slate-500'
                  }`}>
                    {group.name === 'Labs' ? '⚗ Labs' : group.name}
                  </span>
                  {isCollapsible && (
                    <ChevronDown
                      className={`w-3 h-3 text-slate-600 transition-transform duration-150 ${groupOpen ? '' : '-rotate-90'}`}
                    />
                  )}
                </button>
              )}

              {/* Separator for collapsed groups */}
              {group.name && collapsed && (
                <div className="my-2 mx-3 h-px bg-white/[0.06]" />
              )}

              {/* Items */}
              {groupOpen && (
                <div className="space-y-0.5">
                  {group.items.map((item, ii) => {
                    const active = isActive(item.href)
                    const accent = item.accent ?? 'indigo'
                    const accentClass = accentBg[accent]

                    return (
                      <Link
                        key={ii}
                        href={item.href}
                        className={`
                          flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-sm transition-all duration-100 relative group
                          ${active
                            ? `${accentClass} font-medium`
                            : 'text-slate-500 hover:text-slate-200 hover:bg-white/5 font-normal'
                          }
                          ${collapsed ? 'justify-center px-0 mx-1.5' : ''}
                        `}
                        title={collapsed ? item.label : undefined}
                        aria-current={active ? 'page' : undefined}
                      >
                        {/* Active left bar */}
                        {active && !collapsed && (
                          <span className="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-5 rounded-full bg-current opacity-60" />
                        )}

                        <span className="flex-shrink-0">{item.icon}</span>

                        {!collapsed && (
                          <span className="truncate">{item.label}</span>
                        )}

                        {/* Experimental badge — flags non-production routes */}
                        {!collapsed && item.experimental && (
                          <span
                            className="ml-auto flex-shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wide text-amber-400/90"
                            title="Experimental — not production-ready"
                          >
                            Exp
                          </span>
                        )}

                        {/* Tooltip on collapsed */}
                        {collapsed && (
                          <span className="pointer-events-none absolute left-full ml-2 px-2 py-1 rounded-md bg-[#1a1f2e] border border-white/10 text-xs text-slate-200 whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity shadow-xl z-50">
                            {item.label}
                          </span>
                        )}
                      </Link>
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </nav>

      {/* ── Footer ── */}
      <div className="flex-shrink-0 border-t border-white/[0.07] p-2 space-y-0.5">
        <Link
          href="/settings"
          className={`
            relative group flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-sm transition-colors
            ${isActive('/settings') ? 'bg-white/8 text-slate-200' : 'text-slate-500 hover:text-slate-200 hover:bg-white/5'}
            ${collapsed ? 'justify-center px-0 mx-1.5' : ''}
          `}
          title={collapsed ? 'Settings' : undefined}
        >
          <Settings className="w-4 h-4 flex-shrink-0" />
          {!collapsed && <span>Settings</span>}
          {collapsed && (
            <span className="pointer-events-none absolute left-full ml-2 px-2 py-1 rounded-md bg-[#1a1f2e] border border-white/10 text-xs text-slate-200 whitespace-nowrap opacity-0 group-hover:opacity-100 transition-opacity shadow-xl z-50">
              Settings
            </span>
          )}
        </Link>
      </div>
    </aside>
  )
}

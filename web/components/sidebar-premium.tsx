'use client'

import { useState } from 'react'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useAuth } from '@/lib/auth-context'
import {
  LayoutDashboard,
  AlertCircle,
  AlertTriangle,
  Clock,
  LogOut,
  ChevronDown,
  Menu,
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
  Inbox,
  Eye,
  ServerCog,
  Workflow,
  CheckCircle2,
  Home,
  MoreVertical,
} from 'lucide-react'

type NavItem = {
  label: string
  href: string
  icon: React.ReactNode
  subsystem?: string
}

type NavGroup = {
  name: string
  items: NavItem[]
  collapsible?: boolean
  defaultOpen?: boolean
}

const navGroups: NavGroup[] = [
  {
    name: '',
    items: [
      { label: 'Dashboard', href: '/dashboard', icon: <LayoutDashboard className="w-5 h-5" />, subsystem: 'dashboard' },
    ],
  },
  {
    name: 'Operations',
    items: [
      { label: 'Alerts', href: '/alerts', icon: <AlertCircle className="w-5 h-5" />, subsystem: 'alerts' },
      { label: 'Incidents', href: '/incidents', icon: <AlertTriangle className="w-5 h-5" />, subsystem: 'incidents' },
      { label: 'Scheduler', href: '/scheduler', icon: <Clock className="w-5 h-5" />, subsystem: 'operations' },
    ],
    collapsible: true,
    defaultOpen: true,
  },
  {
    name: 'Intelligence',
    items: [
      { label: 'Drift Detection', href: '/drift', icon: <GitBranch className="w-5 h-5" />, subsystem: 'drift' },
      { label: 'Recommendations', href: '/recommendations', icon: <Lightbulb className="w-5 h-5" />, subsystem: 'recommendations' },
      { label: 'Forecasts', href: '/forecasts', icon: <BarChart3 className="w-5 h-5" />, subsystem: 'forecasts' },
      { label: 'Autonomy', href: '/autonomy', icon: <Bot className="w-5 h-5" />, subsystem: 'autonomy' },
      { label: 'Dependency Graph', href: '/graph', icon: <Workflow className="w-5 h-5" />, subsystem: 'operations' },
    ],
    collapsible: true,
    defaultOpen: true,
  },
  {
    name: 'Core',
    items: [
      { label: 'Secrets', href: '/secrets', icon: <Shield className="w-5 h-5" />, subsystem: 'security' },
      { label: 'Discovery', href: '/discovery', icon: <Eye className="w-5 h-5" />, subsystem: 'operations' },
      { label: 'Events', href: '/events', icon: <Zap className="w-5 h-5" />, subsystem: 'operations' },
      { label: 'Audit Logs', href: '/audit', icon: <FileText className="w-5 h-5" />, subsystem: 'security' },
    ],
    collapsible: true,
    defaultOpen: true,
  },
  {
    name: 'Governance',
    items: [
      { label: 'Policies', href: '/policies', icon: <Lock className="w-5 h-5" />, subsystem: 'policies' },
      { label: 'Configuration', href: '/configuration', icon: <ServerCog className="w-5 h-5" />, subsystem: 'operations' },
    ],
    collapsible: true,
    defaultOpen: true,
  },
  {
    name: 'Administration',
    items: [
      { label: 'Users', href: '/users', icon: <Users className="w-5 h-5" />, subsystem: 'security' },
      { label: 'Plugins', href: '/plugins', icon: <Plug className="w-5 h-5" />, subsystem: 'plugins' },
      { label: 'Integrations', href: '/integrations', icon: <Cable className="w-5 h-5" />, subsystem: 'operations' },
    ],
    collapsible: true,
    defaultOpen: false,
  },
  {
    name: 'Analytics',
    items: [
      { label: 'Analytics', href: '/analytics', icon: <BarChart3 className="w-5 h-5" />, subsystem: 'forecasts' },
      { label: 'Timeline', href: '/timeline', icon: <Inbox className="w-5 h-5" />, subsystem: 'operations' },
    ],
    collapsible: true,
    defaultOpen: false,
  },
]

export function SidebarPremium() {
  const pathname = usePathname() || ''
  const { user, logout } = useAuth()
  const [isOpen, setIsOpen] = useState(true)
  const [isIconOnly, setIsIconOnly] = useState(false)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({
    Operations: true,
    Intelligence: true,
    Core: true,
    Governance: true,
  })

  const toggleGroup = (groupName: string) => {
    setExpandedGroups(prev => ({
      ...prev,
      [groupName]: !prev[groupName],
    }))
  }

  const isActive = (href: string) => pathname === href || pathname.startsWith(href + '/')

  const subsystemGradients: Record<string, string> = {
    incidents: 'from-orange-500 to-orange-600',
    recommendations: 'from-purple-500 to-purple-600',
    forecasts: 'from-cyan-500 to-cyan-600',
    drift: 'from-amber-500 to-amber-600',
    autonomy: 'from-emerald-500 to-emerald-600',
    security: 'from-blue-500 to-blue-600',
    policies: 'from-indigo-500 to-indigo-600',
    plugins: 'from-slate-500 to-slate-600',
    alerts: 'from-red-500 to-red-600',
    operations: 'from-slate-600 to-slate-700',
    dashboard: 'from-indigo-500 to-indigo-600',
  }

  return (
    <div
      className={`fixed left-0 top-0 h-screen bg-slate-900 transition-all duration-300 z-50 flex flex-col border-r border-slate-800 ${
        isOpen ? 'w-56' : 'w-20'
      }`}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-slate-800">
        {isOpen && <h1 className="text-lg font-bold text-white">DSO</h1>}
        <button
          onClick={() => setIsIconOnly(!isIconOnly)}
          className="p-2 hover:bg-slate-800 rounded-lg text-slate-400 hover:text-white transition-colors"
          title={isIconOnly ? 'Expand' : 'Collapse'}
        >
          <Menu className="w-5 h-5" />
        </button>
      </div>

      {/* Navigation Groups */}
      <nav className="flex-1 overflow-y-auto px-3 py-4 space-y-1">
        {navGroups.map((group, groupIdx) => (
          <div key={groupIdx} className="mb-4">
            {/* Group Header */}
            {group.name && (
              <div
                className={`flex items-center justify-between ${isOpen ? 'px-2' : 'px-1'} py-2 cursor-pointer hover:bg-slate-800 rounded transition-colors`}
                onClick={() => group.collapsible && toggleGroup(group.name)}
              >
                {isOpen && (
                  <>
                    <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">{group.name}</span>
                    {group.collapsible && (
                      <ChevronDown
                        className={`w-4 h-4 text-slate-500 transition-transform ${
                          expandedGroups[group.name] ? 'rotate-0' : '-rotate-90'
                        }`}
                      />
                    )}
                  </>
                )}
              </div>
            )}

            {/* Group Items */}
            {(!group.collapsible || expandedGroups[group.name]) && (
              <div className="space-y-1">
                {group.items.map((item, itemIdx) => {
                  const active = isActive(item.href)
                  const gradient = subsystemGradients[item.subsystem || 'operations']

                  return (
                    <Link key={itemIdx} href={item.href}>
                      <button
                        className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-all relative overflow-hidden group ${
                          active
                            ? `bg-gradient-to-r ${gradient} text-white shadow-lg`
                            : 'text-slate-400 hover:text-white hover:bg-slate-800'
                        }`}
                        title={isIconOnly ? item.label : undefined}
                      >
                        {/* Active pill glow effect */}
                        {active && (
                          <div className="absolute inset-0 bg-gradient-to-r from-white/20 to-transparent opacity-0 group-hover:opacity-100 transition-opacity" />
                        )}

                        {/* Icon */}
                        <div className="flex-shrink-0 relative z-10">{item.icon}</div>

                        {/* Label */}
                        {isOpen && (
                          <span className="text-sm font-medium whitespace-nowrap flex-1 text-left z-10">
                            {item.label}
                          </span>
                        )}

                        {/* Active indicator dot */}
                        {active && !isOpen && (
                          <div className="absolute right-2 w-2 h-2 rounded-full bg-white" />
                        )}
                      </button>
                    </Link>
                  )
                })}
              </div>
            )}
          </div>
        ))}
      </nav>

      {/* Footer - User & Logout */}
      <div className="border-t border-slate-800 p-3 space-y-2">
        {isOpen && user && (
          <div className="px-2 py-2">
            <p className="text-xs font-medium text-slate-300">{user.display_name}</p>
            <p className="text-xs text-slate-500">{user.username}</p>
          </div>
        )}

        <Link href="/profile">
          <button className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-slate-400 hover:bg-slate-800 hover:text-white transition-colors text-sm">
            <Settings className="w-5 h-5 flex-shrink-0" />
            {isOpen && <span className="font-medium flex-1 text-left">Settings</span>}
          </button>
        </Link>

        <button
          onClick={logout}
          className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-red-400 hover:bg-red-900/20 hover:text-red-300 transition-colors text-sm"
        >
          <LogOut className="w-5 h-5 flex-shrink-0" />
          {isOpen && <span className="font-medium flex-1 text-left">Sign Out</span>}
        </button>
      </div>
    </div>
  )
}

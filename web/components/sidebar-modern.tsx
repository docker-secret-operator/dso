'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'
import {
  BarChart3,
  Lock,
  Bell,
  FileText,
  Settings,
  Database,
  Server,
  TrendingUp,
  Clock,
  AlertCircle,
  AlertTriangle,
  Wrench,
  GitBranch,
  Edit3,
  CheckCircle2,
  Users,
  MonitorSmartphone,
  ShieldCheck,
  Play,
  Lightbulb,
  Eye,
  Zap,
  LogOut,
  ChevronDown,
} from 'lucide-react'

interface NavItem {
  name: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  badge?: string
}

interface NavSection {
  title: string
  items: NavItem[]
}

const navSections: NavSection[] = [
  {
    title: 'Core',
    items: [
      { name: 'Dashboard', href: '/dashboard', icon: BarChart3 },
      { name: 'Secrets', href: '/secrets', icon: Lock },
      { name: 'Discovery', href: '/discovery', icon: Server },
      { name: 'Events', href: '/events', icon: Bell },
      { name: 'Audit Logs', href: '/audit', icon: FileText },
      { name: 'Configuration', href: '/configuration', icon: Settings },
    ],
  },
  {
    title: 'Operations',
    items: [
      { name: 'Alerts', href: '/operations/alerts', icon: Bell },
      { name: 'Incidents', href: '/incidents', icon: AlertTriangle },
      { name: 'Recommendations', href: '/recommendations', icon: Lightbulb },
      { name: 'Forecasts', href: '/forecasts', icon: Eye },
      { name: 'Autonomy', href: '/autonomy', icon: Zap },
      { name: 'Drift Detection', href: '/drift', icon: AlertCircle },
      { name: 'Remediation', href: '/remediation', icon: Wrench },
      { name: 'Change Sets', href: '/changesets', icon: GitBranch },
    ],
  },
  {
    title: 'Workflow',
    items: [
      { name: 'Workspace', href: '/workspace', icon: Edit3 },
      { name: 'Reviews', href: '/review', icon: CheckCircle2 },
      { name: 'Executions', href: '/executions', icon: Play },
    ],
  },
  {
    title: 'Insights',
    items: [
      { name: 'Analytics', href: '/analytics', icon: TrendingUp },
      { name: 'Timeline', href: '/timeline', icon: Clock },
    ],
  },
]

function NavLink({
  href,
  icon: Icon,
  name,
  isActive,
}: {
  href: string
  icon: React.ComponentType<{ className?: string }>
  name: string
  isActive: boolean
}) {
  return (
    <Link
      href={href}
      className={cn(
        'group relative flex items-center gap-3 px-3 py-2.5 rounded-lg font-medium text-sm transition-all duration-200',
        isActive
          ? 'bg-gradient-to-r from-coral-600/20 to-coral-500/10 text-coral-600'
          : 'text-slate-400 hover:text-slate-200 hover:bg-white/5'
      )}
    >
      {isActive && (
        <div className="absolute inset-y-1 left-0 w-1 bg-gradient-to-b from-coral-600 to-coral-500 rounded-full" />
      )}
      <Icon className={cn('w-5 h-5 transition-all', isActive && 'text-coral-600')} />
      <span>{name}</span>
    </Link>
  )
}

export function SidebarModern() {
  const pathname = usePathname()
  const { role, logout } = useAuth()

  const isAdmin = role === 'admin'

  return (
    <aside className="fixed left-4 top-4 bottom-4 w-64 bg-slate-900/50 backdrop-blur-xl border border-white/10 rounded-2xl flex flex-col overflow-hidden shadow-2xl">
      {/* Header */}
      <div className="p-6 border-b border-white/10">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-coral-600 to-coral-500 text-white font-bold shadow-lg">
            D
          </div>
          <div>
            <h1 className="text-lg font-bold text-white">DSO</h1>
            <p className="text-xs text-slate-400">Operations</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto px-4 py-6 space-y-8">
        {navSections.map(section => (
          <div key={section.title}>
            <h3 className="px-3 py-2 text-xs font-semibold uppercase text-slate-400 tracking-wider mb-3">
              {section.title}
            </h3>
            <div className="space-y-1">
              {section.items.map(item => (
                <NavLink
                  key={item.href}
                  href={item.href}
                  icon={item.icon}
                  name={item.name}
                  isActive={pathname?.startsWith(item.href) || false}
                />
              ))}
            </div>
          </div>
        ))}

        {/* Admin Section */}
        {isAdmin && (
          <div>
            <h3 className="px-3 py-2 text-xs font-semibold uppercase text-slate-400 tracking-wider mb-3">
              Administration
            </h3>
            <div className="space-y-1">
              <NavLink
                href="/scheduler"
                icon={Clock}
                name="Scheduler"
                isActive={pathname?.startsWith('/scheduler') || false}
              />
              <NavLink
                href="/policies"
                icon={AlertCircle}
                name="Policies"
                isActive={pathname?.startsWith('/policies') || false}
              />
              <NavLink
                href="/graph"
                icon={GitBranch}
                name="Dependency Graph"
                isActive={pathname?.startsWith('/graph') || false}
              />
              <NavLink
                href="/integrations"
                icon={Wrench}
                name="Integrations"
                isActive={pathname?.startsWith('/integrations') || false}
              />
              <NavLink
                href="/plugins"
                icon={Settings}
                name="Plugins"
                isActive={pathname?.startsWith('/plugins') || false}
              />
            </div>
          </div>
        )}
      </nav>

      {/* Account Section */}
      <div className="p-4 border-t border-white/10 space-y-2">
        <Link
          href="/profile"
          className={cn(
            'flex items-center gap-3 px-3 py-2.5 rounded-lg font-medium text-sm transition-all duration-200',
            pathname?.startsWith('/profile')
              ? 'bg-gradient-to-r from-coral-600/20 to-coral-500/10 text-coral-600'
              : 'text-slate-400 hover:text-slate-200 hover:bg-white/5'
          )}
        >
          <Users className="w-5 h-5" />
          <span>My Profile</span>
        </Link>

        <button
          onClick={logout}
          className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg font-medium text-sm text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-all duration-200"
        >
          <LogOut className="w-5 h-5" />
          <span>Sign Out</span>
        </button>
      </div>
    </aside>
  )
}

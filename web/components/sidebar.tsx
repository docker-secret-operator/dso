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
} from 'lucide-react'

const iconMap: Record<string, React.ComponentType<{ className?: string }>> = {
  BarChart3,
  Lock,
  Bell,
  FileText,
  Settings,
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
}

const navItems = [
  { name: 'Dashboard', href: '/dashboard', icon: 'BarChart3' as const },
  { name: 'Secrets', href: '/secrets', icon: 'Lock' as const },
  { name: 'Discovery', href: '/discovery', icon: 'Server' as const },
  { name: 'Events', href: '/events', icon: 'Bell' as const },
  { name: 'Audit Logs', href: '/audit', icon: 'FileText' as const },
  { name: 'Configuration', href: '/configuration', icon: 'Settings' as const },
]

const operationsItems = [
  { name: 'Alerts', href: '/operations/alerts', icon: 'Bell' as const },
  { name: 'Incidents', href: '/incidents', icon: 'AlertTriangle' as const },
  { name: 'Recommendations', href: '/recommendations', icon: 'Lightbulb' as const },
  { name: 'Forecasts', href: '/forecasts', icon: 'Eye' as const },
  { name: 'Autonomy', href: '/autonomy', icon: 'Zap' as const },
  { name: 'Drift Detection', href: '/drift', icon: 'AlertCircle' as const },
  { name: 'Remediation', href: '/remediation', icon: 'Wrench' as const },
  { name: 'Change Sets', href: '/changesets', icon: 'GitBranch' as const },
]

const workflowItems = [
  { name: 'Workspace', href: '/workspace', icon: 'Edit3' as const },
  { name: 'Reviews', href: '/review', icon: 'CheckCircle2' as const },
  { name: 'Executions', href: '/executions', icon: 'Play' as const },
]

const insightsItems = [
  { name: 'Analytics', href: '/analytics', icon: 'TrendingUp' as const },
  { name: 'Secret Insights', href: '/insights/secrets', icon: 'TrendingUp' as const },
  { name: 'Timeline', href: '/timeline', icon: 'Clock' as const },
]

function NavLink({ href, icon, name, pathname }: { href: string; icon: string; name: string; pathname: string | null }) {
  const Icon = iconMap[icon]
  const isActive = pathname && pathname.startsWith(href)
  return (
    <Link
      href={href}
      className={cn(
        'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
        isActive
          ? 'bg-primary text-primary-foreground'
          : 'text-foreground hover:bg-accent hover:text-accent-foreground'
      )}
    >
      <Icon className="h-4 w-4" />
      {name}
    </Link>
  )
}

export function Sidebar() {
  const pathname = usePathname()
  const { role } = useAuth()

  const isAdmin = role === 'admin'

  return (
    <aside className="w-64 border-r border-border bg-card">
      {/* Header */}
      <div className="border-b border-border p-6">
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Database className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-lg font-semibold">DSO</h1>
            <p className="text-xs text-muted-foreground">Dashboard</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex flex-col h-[calc(100vh-200px)] p-4 overflow-y-auto">
        {/* Main Items */}
        <div className="space-y-1">
          {navItems.map((item) => (
            <NavLink key={item.href} {...item} pathname={pathname} />
          ))}
        </div>

        {/* Operations Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Operations</p>
          <div className="space-y-1">
            {operationsItems.map((item) => (
              <NavLink key={item.href} {...item} pathname={pathname} />
            ))}
          </div>
        </div>

        {/* Workflow Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Workflow</p>
          <div className="space-y-1">
            {workflowItems.map((item) => (
              <NavLink key={item.href} {...item} pathname={pathname} />
            ))}
          </div>
        </div>

        {/* Insights Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Insights</p>
          <div className="space-y-1">
            {insightsItems.map((item) => (
              <NavLink key={item.href} {...item} pathname={pathname} />
            ))}
          </div>
        </div>

        {/* Administration Section (Admin Only) */}
        {isAdmin && (
          <div className="pt-4 mt-4 border-t">
            <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Administration</p>
            <div className="space-y-1">
              <NavLink href="/scheduler" icon="Clock" name="Scheduler" pathname={pathname} />
              <NavLink href="/policies" icon="AlertCircle" name="Policies" pathname={pathname} />
              <NavLink href="/drift" icon="AlertTriangle" name="Drift Detection" pathname={pathname} />
              <NavLink href="/graph" icon="GitBranch" name="Dependency Graph" pathname={pathname} />
              <NavLink href="/integrations" icon="Wrench" name="Integrations" pathname={pathname} />
              <NavLink href="/plugins" icon="Settings" name="Plugins" pathname={pathname} />
            </div>
          </div>
        )}

        {/* Account Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Account</p>
          <div className="space-y-1">
            <NavLink href="/profile" icon="Users" name="My Profile" pathname={pathname} />
            <NavLink href="/settings/sessions" icon="MonitorSmartphone" name="My Sessions" pathname={pathname} />
            <NavLink href="/settings/password" icon="ShieldCheck" name="Change Password" pathname={pathname} />
            {isAdmin && (
              <>
                <NavLink href="/users" icon="Users" name="Users" pathname={pathname} />
                <NavLink href="/admin/sessions" icon="MonitorSmartphone" name="All Sessions" pathname={pathname} />
              </>
            )}
          </div>
        </div>
      </nav>

      {/* Footer */}
      <div className="absolute bottom-0 left-0 w-64 border-t border-border p-4">
        <p className="text-xs text-muted-foreground">v3.6.0</p>
      </div>
    </aside>
  )
}

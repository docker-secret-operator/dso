'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { cn } from '@/lib/utils'
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
  Wrench,
  GitBranch,
  Edit3,
  CheckCircle2,
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
  Wrench,
  GitBranch,
  Edit3,
  CheckCircle2,
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
  { name: 'Drift Detection', href: '/drift', icon: 'AlertCircle' as const },
  { name: 'Remediation', href: '/remediation', icon: 'Wrench' as const },
  { name: 'Change Sets', href: '/changesets', icon: 'GitBranch' as const },
]

const workflowItems = [
  { name: 'Workspace', href: '/workspace', icon: 'Edit3' as const },
  { name: 'Reviews', href: '/review', icon: 'CheckCircle2' as const },
]

const insightsItems = [
  { name: 'Secret Insights', href: '/insights/secrets', icon: 'TrendingUp' as const },
  { name: 'Timeline', href: '/timeline', icon: 'Clock' as const },
]

export function Sidebar() {
  const pathname = usePathname()

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
          {navItems.map((item) => {
            const Icon = iconMap[item.icon]
            const isActive = pathname && pathname.startsWith(item.href)

            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-primary text-primary-foreground'
                    : 'text-foreground hover:bg-accent hover:text-accent-foreground'
                )}
              >
                <Icon className="h-4 w-4" />
                {item.name}
              </Link>
            )
          })}
        </div>

        {/* Operations Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Operations</p>
          <div className="space-y-1">
            {operationsItems.map((item) => {
              const Icon = iconMap[item.icon]
              const isActive = pathname && pathname.startsWith(item.href)

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-primary text-primary-foreground'
                      : 'text-foreground hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  <Icon className="h-4 w-4" />
                  {item.name}
                </Link>
              )
            })}
          </div>
        </div>

        {/* Workflow Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Workflow</p>
          <div className="space-y-1">
            {workflowItems.map((item) => {
              const Icon = iconMap[item.icon]
              const isActive = pathname && pathname.startsWith(item.href)

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-primary text-primary-foreground'
                      : 'text-foreground hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  <Icon className="h-4 w-4" />
                  {item.name}
                </Link>
              )
            })}
          </div>
        </div>

        {/* Insights Section */}
        <div className="pt-4 mt-4 border-t">
          <p className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase">Insights</p>
          <div className="space-y-1">
            {insightsItems.map((item) => {
              const Icon = iconMap[item.icon]
              const isActive = pathname && pathname.startsWith(item.href)

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                    isActive
                      ? 'bg-primary text-primary-foreground'
                      : 'text-foreground hover:bg-accent hover:text-accent-foreground'
                  )}
                >
                  <Icon className="h-4 w-4" />
                  {item.name}
                </Link>
              )
            })}
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

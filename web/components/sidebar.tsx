'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { BarChart3, Lock, Bell, Settings, LogOut, ScrollText, Boxes, FileClock, Plug, Users, GitCompare, LineChart, AlertTriangle } from 'lucide-react'

import { cn } from '@/lib/utils'
import { logout as apiLogout } from '@/lib/api/auth'
import { Logo } from '@/components/logo'

interface NavItem {
  name: string
  href: string
  icon: typeof BarChart3
}

interface NavSection {
  label: string
  items: NavItem[]
}

// Grouping mirrors the real route set wired in this file today -- no
// project/environment/policy/help entries, since no such routes exist.
const navSections: NavSection[] = [
  {
    label: 'Workspace',
    items: [
      { name: 'Dashboard', href: '/dashboard', icon: BarChart3 },
      { name: 'Secrets', href: '/secrets', icon: Lock },
      { name: 'Integrations', href: '/integrations', icon: Plug },
    ],
  },
  {
    label: 'Operations',
    items: [
      { name: 'Events', href: '/events', icon: Bell },
      { name: 'Audit', href: '/audit', icon: FileClock },
      { name: 'Analytics', href: '/analytics', icon: LineChart },
      { name: 'Alerts', href: '/alerts', icon: AlertTriangle },
      { name: 'Drift', href: '/drift', icon: GitCompare },
      { name: 'Logs', href: '/logs', icon: ScrollText },
    ],
  },
  {
    label: 'Access',
    items: [{ name: 'Users / Access', href: '/users', icon: Users }],
  },
  {
    label: 'System',
    items: [
      { name: 'Configuration', href: '/configuration', icon: Settings },
      { name: 'Containers', href: '/containers', icon: Boxes },
      { name: 'Settings', href: '/settings', icon: Settings },
    ],
  },
]

interface SidebarProps {
  /** Mobile drawer open state. Ignored at the lg breakpoint and up, where
   * the sidebar is always visible as a static column. */
  open?: boolean
  /** Called to close the mobile drawer -- on backdrop click, Escape, or
   * after a nav link is followed (so navigating on mobile doesn't leave the
   * drawer covering the new page). */
  onClose?: () => void
}

export function Sidebar({ open = false, onClose }: SidebarProps) {
  const pathname = usePathname()

  // Uses the auth API directly (rather than AuthContext.logout) so this
  // sidebar can render outside the per-page AuthProvider tree -- it's
  // mounted by the root layout, above where AuthGuard wraps individual
  // pages in AuthProvider. Behavior mirrors AuthContext.logout(): best-effort
  // POST /api/auth/logout, then a hard redirect to /login regardless of
  // outcome (the HttpOnly session cookie is what actually matters).
  const handleLogout = async () => {
    try {
      await apiLogout()
    } catch {
      // Always redirect even if the request itself failed.
    } finally {
      if (typeof window !== 'undefined') {
        window.location.href = '/login'
      }
    }
  }

  return (
    <>
      {/* Mobile backdrop -- below the lg breakpoint the sidebar becomes an
          off-canvas drawer; this dims/blocks the page and closes the drawer
          on click. Hidden entirely at lg+ where the sidebar is static. */}
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={onClose}
          aria-hidden="true"
          data-testid="sidebar-backdrop"
        />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex h-screen w-60 flex-shrink-0 -translate-x-full flex-col border-r border-border bg-card transition-transform duration-200 ease-out',
          'lg:static lg:z-auto lg:translate-x-0',
          open && 'translate-x-0'
        )}
      >
        <div className="flex items-center gap-3 border-b border-border p-6">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent-dim text-primary">
            <Logo className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-sm font-semibold text-foreground">DSO</h1>
            <p className="text-xs text-muted-foreground">Docker Secret Operator</p>
          </div>
        </div>

        <nav className="flex-1 space-y-5 overflow-y-auto p-4">
          {navSections.map((section) => (
            <div key={section.label} className="space-y-1">
              <p className="px-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/60">
                {section.label}
              </p>
              {section.items.map((item) => {
                const isActive = pathname?.startsWith(item.href)
                const Icon = item.icon
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    onClick={onClose}
                    aria-current={isActive ? 'page' : undefined}
                    className={cn(
                      'flex items-center gap-2.5 rounded-md border-l-2 py-1.5 pl-[11px] pr-3 text-sm font-medium transition-colors',
                      isActive
                        ? 'border-primary bg-accent-dim text-primary'
                        : 'border-transparent text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                    )}
                  >
                    <Icon className={cn('h-4 w-4', isActive ? 'text-primary' : 'text-muted-foreground/70')} />
                    {item.name}
                  </Link>
                )
              })}
            </div>
          ))}
        </nav>

        <div className="border-t border-border p-4">
          <button
            onClick={handleLogout}
            className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-red-400"
          >
            <LogOut className="h-4 w-4" />
            Sign out
          </button>
        </div>
      </aside>
    </>
  )
}

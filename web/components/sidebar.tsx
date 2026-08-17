'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { BarChart3, Lock, Bell, Settings, Database, LogOut, ScrollText, Boxes } from 'lucide-react'

import { cn } from '@/lib/utils'
import { logout as apiLogout } from '@/lib/api/auth'

const navItems = [
  { name: 'Dashboard', href: '/dashboard', icon: BarChart3 },
  { name: 'Secrets', href: '/secrets', icon: Lock },
  { name: 'Events', href: '/events', icon: Bell },
  { name: 'Logs', href: '/logs', icon: ScrollText },
  { name: 'Containers', href: '/containers', icon: Boxes },
  { name: 'Configuration', href: '/configuration', icon: Settings },
]

export function Sidebar() {
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
    <aside className="flex h-screen w-60 flex-shrink-0 flex-col border-r border-border bg-card">
      <div className="flex items-center gap-3 border-b border-border p-6">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
          <Database className="h-4.5 w-4.5" />
        </div>
        <div>
          <h1 className="text-sm font-semibold text-foreground">DSO</h1>
          <p className="text-xs text-muted-foreground">Docker Secret Operator</p>
        </div>
      </div>

      <nav className="flex-1 space-y-1 overflow-y-auto p-4">
        {navItems.map((item) => {
          const isActive = pathname?.startsWith(item.href)
          const Icon = item.icon
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
              )}
            >
              <Icon className="h-4 w-4" />
              {item.name}
            </Link>
          )
        })}
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
  )
}

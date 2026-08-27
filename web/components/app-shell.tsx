'use client'

import { usePathname } from 'next/navigation'

import { Sidebar } from '@/components/sidebar'
import { GlobalSearch } from '@/components/global-search'
import { NotificationBell } from '@/components/notification-bell'

// Pages that render their own full-screen layout (no shell chrome).
const SHELL_EXCLUDED = ['/login']

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() ?? ''
  const noShell = SHELL_EXCLUDED.some((p) => pathname === p || pathname.startsWith(p + '/'))

  if (noShell) {
    return <>{children}</>
  }

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex flex-shrink-0 items-center justify-end gap-3 border-b border-border px-6 py-3">
          <GlobalSearch />
          <NotificationBell />
        </header>
        <main className="flex-1 min-w-0 overflow-y-auto">{children}</main>
      </div>
    </div>
  )
}

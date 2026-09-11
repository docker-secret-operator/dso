'use client'

import { useEffect, useState } from 'react'
import { usePathname } from 'next/navigation'
import { Menu } from 'lucide-react'

import { Sidebar } from '@/components/sidebar'
import { GlobalSearch } from '@/components/global-search'
import { NotificationBell } from '@/components/notification-bell'

// Pages that render their own full-screen layout (no shell chrome).
const SHELL_EXCLUDED = ['/login']

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() ?? ''
  const noShell = SHELL_EXCLUDED.some((p) => pathname === p || pathname.startsWith(p + '/'))
  const [mobileNavOpen, setMobileNavOpen] = useState(false)

  // Close the mobile drawer on route change -- otherwise navigating from
  // within it (below lg, where clicking a link doesn't already call
  // Sidebar's onClose fast enough relative to the route transition) can
  // leave it open over the new page.
  useEffect(() => {
    setMobileNavOpen(false)
  }, [pathname])

  // Escape closes the drawer from anywhere, matching the command palette's
  // existing Escape-to-close convention (see global-search.tsx).
  useEffect(() => {
    if (!mobileNavOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setMobileNavOpen(false)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [mobileNavOpen])

  if (noShell) {
    return <>{children}</>
  }

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <Sidebar open={mobileNavOpen} onClose={() => setMobileNavOpen(false)} />
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-14 flex-shrink-0 items-center justify-between gap-3 border-b border-border px-4 sm:px-6">
          <button
            type="button"
            onClick={() => setMobileNavOpen(true)}
            aria-label="Open navigation menu"
            className="flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground lg:hidden"
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="flex flex-1 items-center justify-end gap-3">
            <GlobalSearch />
            <NotificationBell />
          </div>
        </header>
        <main className="flex-1 min-w-0 overflow-y-auto">{children}</main>
      </div>
    </div>
  )
}

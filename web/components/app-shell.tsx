'use client'

import { usePathname } from 'next/navigation'
import { SidebarPremium } from '@/components/sidebar-premium'
import { Topbar } from '@/components/topbar'

// Pages that render their own full-screen layout (no shell chrome)
const SHELL_EXCLUDED = ['/login']

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() ?? ''
  const noShell  = SHELL_EXCLUDED.some(p => pathname === p || pathname.startsWith(p + '/'))

  if (noShell) {
    return <>{children}</>
  }

  return (
    <div className="flex h-screen overflow-hidden bg-[#0B1020]">
      <SidebarPremium />

      {/* Content area — left offset tracks sidebar CSS variable set by the sidebar */}
      <div
        className="flex flex-col flex-1 min-w-0 transition-[margin] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)]"
        style={{ marginLeft: 'var(--sidebar-width, 220px)' }}
      >
        <Topbar />
        <main className="flex-1 overflow-y-auto">
          {children}
        </main>
      </div>
    </div>
  )
}

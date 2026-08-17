'use client'

import { usePathname } from 'next/navigation'

import { Sidebar } from '@/components/sidebar'

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
      <main className="flex-1 min-w-0 overflow-y-auto">{children}</main>
    </div>
  )
}

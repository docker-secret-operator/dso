'use client'

import { usePathname } from 'next/navigation'
import { SidebarPremium } from '@/components/sidebar-premium'
import { Topbar } from '@/components/topbar'
import { ExperimentalBanner } from '@/components/experimental-banner'

// Pages that render their own full-screen layout (no shell chrome)
const SHELL_EXCLUDED = ['/login']

// Routes whose backend is not production-ready. The banner makes this
// unmistakable so an operator can't confuse them with trusted pages.
// (Source of truth: HONESTY_AUDIT.md.)
const EXPERIMENTAL_ROUTES = [
  '/incidents', '/recommendations', '/autonomy',
  '/graph', '/changesets', '/review', '/workspace', '/remediation',
]
const BETA_ROUTES = ['/forecasts']

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname() ?? ''
  const noShell  = SHELL_EXCLUDED.some(p => pathname === p || pathname.startsWith(p + '/'))

  const matches = (routes: string[]) =>
    routes.some(p => pathname === p || pathname.startsWith(p + '/'))
  const isExperimental = matches(EXPERIMENTAL_ROUTES)
  const isBeta = matches(BETA_ROUTES)

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
          {(isExperimental || isBeta) && (
            <div className="px-6 pt-6">
              {isBeta ? (
                <ExperimentalBanner variant="beta">
                  Forecasts are estimates derived from historical trends and should not be treated as authoritative operational data.
                </ExperimentalBanner>
              ) : (
                <ExperimentalBanner />
              )}
            </div>
          )}
          {children}
        </main>
      </div>
    </div>
  )
}

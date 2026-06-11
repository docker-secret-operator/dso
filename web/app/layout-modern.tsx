import type { Metadata } from 'next'
import { Providers } from '@/components/providers'
import { AuthGuard } from '@/components/auth-guard'
import { SidebarModern } from '@/components/sidebar-modern'
import { Header } from '@/components/header-modern'
import '@/styles/globals.css'

export const metadata: Metadata = {
  title: 'DSO Dashboard - Docker Secret Operator',
  description: 'Enterprise-grade intelligent operations platform',
  icons: {
    icon: '/favicon.ico',
  },
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
      </head>
      <body className="bg-gradient-to-b from-slate-50 to-white text-slate-900 antialiased">
        <Providers>
          <AuthGuard>
            <div className="flex h-screen overflow-hidden">
              {/* Modern Sidebar */}
              <SidebarModern />

              {/* Main Content */}
              <div className="flex-1 flex flex-col overflow-hidden ml-4">
                {/* Header */}
                <Header />

                {/* Page Content */}
                <main className="flex-1 overflow-y-auto">
                  {children}
                </main>
              </div>
            </div>
          </AuthGuard>
        </Providers>
      </body>
    </html>
  )
}

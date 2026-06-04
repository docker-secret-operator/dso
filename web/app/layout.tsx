import type { Metadata } from 'next'
import { Sidebar } from '@/components/sidebar'
import { Header } from '@/components/header'
import { Providers } from '@/components/providers'
import '@/styles/globals.css'

export const metadata: Metadata = {
  title: 'DSO Dashboard - Docker Secret Operator',
  description: 'Monitor and manage secret rotations for Docker Compose',
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
      <body className="bg-background text-foreground antialiased">
        <Providers>
          <div className="flex h-screen overflow-hidden">
            {/* Sidebar */}
            <Sidebar />

            {/* Main Content */}
            <div className="flex flex-1 flex-col overflow-hidden">
              {/* Header */}
              <Header />

              {/* Page Content */}
              <main className="flex-1 overflow-y-auto">
                <div className="h-full bg-background">
                  {children}
                </div>
              </main>
            </div>
          </div>
        </Providers>
      </body>
    </html>
  )
}

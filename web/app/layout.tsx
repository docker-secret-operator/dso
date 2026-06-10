import type { Metadata } from 'next'
import { Providers } from '@/components/providers'
import { AuthGuard } from '@/components/auth-guard'
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
          <AuthGuard>
            {children}
          </AuthGuard>
        </Providers>
      </body>
    </html>
  )
}

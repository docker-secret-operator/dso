import type { Metadata } from 'next'
import { Providers } from '@/components/providers'
import { AuthGuard } from '@/components/auth-guard'
import { AppShell } from '@/components/app-shell'
import '@/styles/globals.css'

export const metadata: Metadata = {
  title: 'DSO — Docker Secret Operator',
  description: 'Enterprise operations platform for Docker secrets, drift detection, and autonomous remediation',
  icons: { icon: '/favicon.ico' },
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="_csrf_token" content="" />
        {/* Google Fonts — set NEXT_PUBLIC_DISABLE_EXTERNAL_FONTS=true for air-gapped deployments */}
        {process.env.NEXT_PUBLIC_DISABLE_EXTERNAL_FONTS !== 'true' && (
          <>
            <link rel="preconnect" href="https://fonts.googleapis.com" />
            <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
            <link
              href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap"
              rel="stylesheet"
            />
          </>
        )}
      </head>
      <body className="bg-[#0B1020] text-slate-200 antialiased font-sans">
        <Providers>
          <AuthGuard>
            <AppShell>
              {children}
            </AppShell>
          </AuthGuard>
        </Providers>
      </body>
    </html>
  )
}

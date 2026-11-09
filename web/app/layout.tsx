import type { Metadata } from 'next'
import { JetBrains_Mono, Plus_Jakarta_Sans } from 'next/font/google'
import './globals.css'
import { AppShell } from '@/components/app-shell'
import { ToastSystemProvider } from '@/components/toast-provider'
import { cn } from '@/lib/utils'

const fontSans = Plus_Jakarta_Sans({
  subsets: ['latin'],
  variable: '--font-sans',
  display: 'swap',
})

const fontMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-mono',
  display: 'swap',
})

export const metadata: Metadata = {
  title: 'DSO',
  description: 'Docker Secret Operator Web Dashboard',
}

// PASS 2 SCOPE: this root layout intentionally does not wrap children in
// AuthGuard/AuthProvider -- doing so at the root would apply auth checks to
// every route uniformly, including /login itself, before there's a real
// route tree to reason about. Each route segment porting in later passes
// decides for itself whether it needs AuthGuard (see components/auth-guard.tsx).
//
// AppShell is purely presentational: it renders the sidebar nav for every
// route except /login (which renders its own full-screen layout) and does
// not touch auth state itself.
export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en">
      <body className={cn(fontSans.variable, fontMono.variable, 'font-sans')}>
        <ToastSystemProvider>
          <AppShell>{children}</AppShell>
        </ToastSystemProvider>
      </body>
    </html>
  )
}

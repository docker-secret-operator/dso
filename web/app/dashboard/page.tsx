'use client'

import { AuthGuard } from '@/components/auth-guard'

/**
 * PLACEHOLDER PAGE -- NOT PRODUCT CONTENT.
 *
 * This exists only so `/dashboard` is a real route for `next build`'s
 * static export (the login flow redirects here) and so AuthGuard has
 * somewhere real to protect. The actual dashboard UI (metrics, secrets
 * table, event feed, etc.) is ported in a later pass -- see AuthGuard's
 * own TODO about WebSocketProvider/SessionTimeoutWarning being deferred
 * for the same reason.
 */
function DashboardPlaceholder() {
  return (
    <div className="min-h-screen bg-[#0B1020] flex items-center justify-center px-4">
      <div className="text-center">
        <h1 className="text-xl font-semibold text-slate-100">DSO Dashboard</h1>
        <p className="text-sm text-slate-500 mt-2">
          Under construction -- the real dashboard UI has not been ported yet.
        </p>
      </div>
    </div>
  )
}

export default function DashboardPage() {
  return (
    <AuthGuard>
      <DashboardPlaceholder />
    </AuthGuard>
  )
}

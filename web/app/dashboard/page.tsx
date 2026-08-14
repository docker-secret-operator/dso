'use client'

import { AuthGuard } from '@/components/auth-guard'
import { DashboardContent } from '@/components/dashboard-content'

export default function DashboardPage() {
  return (
    <AuthGuard>
      <DashboardContent />
    </AuthGuard>
  )
}

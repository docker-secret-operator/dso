'use client'

import { AuthGuard } from '@/components/auth-guard'
import { AnalyticsContent } from '@/components/analytics-content'

export default function AnalyticsPage() {
  return (
    <AuthGuard>
      <AnalyticsContent />
    </AuthGuard>
  )
}

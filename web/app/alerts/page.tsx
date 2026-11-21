'use client'

import { AuthGuard } from '@/components/auth-guard'
import { AlertsContent } from '@/components/alerts-content'

export default function AlertsPage() {
  return (
    <AuthGuard>
      <AlertsContent />
    </AuthGuard>
  )
}

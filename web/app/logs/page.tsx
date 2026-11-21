'use client'

import { AuthGuard } from '@/components/auth-guard'
import { LogsContent } from '@/components/logs-content'

export default function LogsPage() {
  return (
    <AuthGuard>
      <LogsContent />
    </AuthGuard>
  )
}

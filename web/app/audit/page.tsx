'use client'

import { AuthGuard } from '@/components/auth-guard'
import { AuditContent } from '@/components/audit-content'

export default function AuditPage() {
  return (
    <AuthGuard>
      <AuditContent />
    </AuthGuard>
  )
}

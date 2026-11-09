'use client'

import { AuthGuard } from '@/components/auth-guard'
import { DriftContent } from '@/components/drift-content'

export default function DriftPage() {
  return (
    <AuthGuard>
      <DriftContent />
    </AuthGuard>
  )
}

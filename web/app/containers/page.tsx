'use client'

import { AuthGuard } from '@/components/auth-guard'
import { ContainersContent } from '@/components/containers-content'

export default function ContainersPage() {
  return (
    <AuthGuard>
      <ContainersContent />
    </AuthGuard>
  )
}

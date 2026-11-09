'use client'

import { AuthGuard } from '@/components/auth-guard'
import { SecretsContent } from '@/components/secrets-content'

export default function SecretsPage() {
  return (
    <AuthGuard>
      <SecretsContent />
    </AuthGuard>
  )
}

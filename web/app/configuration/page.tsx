'use client'

import { AuthGuard } from '@/components/auth-guard'
import { ConfigurationContent } from '@/components/configuration-content'

export default function ConfigurationPage() {
  return (
    <AuthGuard>
      <ConfigurationContent />
    </AuthGuard>
  )
}

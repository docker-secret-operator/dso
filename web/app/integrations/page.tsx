'use client'

import { AuthGuard } from '@/components/auth-guard'
import { IntegrationsContent } from '@/components/integrations-content'

export default function IntegrationsPage() {
  return (
    <AuthGuard>
      <IntegrationsContent />
    </AuthGuard>
  )
}

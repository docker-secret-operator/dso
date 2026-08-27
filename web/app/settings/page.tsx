'use client'

import { AuthGuard } from '@/components/auth-guard'
import { SettingsContent } from '@/components/settings-content'

export default function SettingsPage() {
  return (
    <AuthGuard>
      <SettingsContent />
    </AuthGuard>
  )
}

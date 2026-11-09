'use client'

import { AuthGuard } from '@/components/auth-guard'
import { EventsContent } from '@/components/events-content'

export default function EventsPage() {
  return (
    <AuthGuard>
      <EventsContent />
    </AuthGuard>
  )
}

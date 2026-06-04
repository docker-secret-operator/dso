import { Suspense } from 'react'
import { ChangeSetsDashboardClient } from './_client-wrapper'

export default function ChangeSetsPage() {
  return (
    <Suspense fallback={<div className="p-8 text-center">Loading change sets...</div>}>
      <ChangeSetsDashboardClient />
    </Suspense>
  )
}

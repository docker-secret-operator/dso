import { Suspense } from 'react'
import { WorkspacePageClient } from './_client-wrapper'

export default function WorkspacePage() {
  return (
    <Suspense fallback={<div className="p-8">Loading workspace...</div>}>
      <WorkspacePageClient />
    </Suspense>
  )
}

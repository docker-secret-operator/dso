import { Suspense } from 'react'
import { DriftDashboardClient } from './_client-wrapper'

export default function DriftPage() {
  return (
    <Suspense
      fallback={
        <div className="p-8">
          <div className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border border-gray-300 border-t-blue-600"></div>
          </div>
        </div>
      }
    >
      <DriftDashboardClient />
    </Suspense>
  )
}

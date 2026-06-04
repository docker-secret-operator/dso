import { Suspense } from 'react'
import { DiscoveryPageClient } from './_client-wrapper'

export default function DiscoveryPage() {
  return (
    <Suspense
      fallback={
        <div className="p-6">
          <div className="flex items-center justify-center h-64">
            <div className="animate-spin rounded-full h-8 w-8 border border-gray-300 border-t-blue-600"></div>
          </div>
        </div>
      }
    >
      <DiscoveryPageClient />
    </Suspense>
  )
}

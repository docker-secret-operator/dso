'use client'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Server } from 'lucide-react'

export interface DiscoverySummary {
  totalContainers: number
  managedContainers: number
  partialContainers: number
  unmanagedContainers: number
}

export interface DiscoverySummaryCardProps {
  title?: string
  summary?: DiscoverySummary
  loading?: boolean
  onViewDetails?: () => void
}

export function DiscoverySummaryCard({
  title = 'Container Discovery',
  summary,
  loading = false,
  onViewDetails,
}: DiscoverySummaryCardProps) {
  return (
    <Card className="cursor-pointer hover:shadow-md transition-shadow" onClick={onViewDetails}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Server className="w-5 h-5" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-3">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-4 bg-gray-200 rounded animate-pulse" />
            ))}
          </div>
        ) : summary ? (
          <div className="space-y-4">
            {/* Total Count */}
            <div className="text-center pb-4 border-b">
              <p className="text-3xl font-bold text-gray-900">{summary.totalContainers}</p>
              <p className="text-xs text-gray-600">Total Containers</p>
            </div>

            {/* Status Breakdown */}
            <div className="grid grid-cols-3 gap-2">
              <div className="bg-green-50 p-3 rounded text-center">
                <p className="text-lg font-bold text-green-700">{summary.managedContainers}</p>
                <p className="text-xs text-green-600 mt-1">Managed</p>
              </div>
              <div className="bg-yellow-50 p-3 rounded text-center">
                <p className="text-lg font-bold text-yellow-700">{summary.partialContainers}</p>
                <p className="text-xs text-yellow-600 mt-1">Partial</p>
              </div>
              <div className="bg-gray-50 p-3 rounded text-center">
                <p className="text-lg font-bold text-gray-700">{summary.unmanagedContainers}</p>
                <p className="text-xs text-gray-600 mt-1">Unmanaged</p>
              </div>
            </div>

            {/* Coverage */}
            {summary.totalContainers > 0 && (
              <div className="pt-2 border-t">
                <p className="text-xs text-gray-600 mb-2">DSO Coverage</p>
                <div className="w-full bg-gray-200 rounded-full h-2">
                  <div
                    className="bg-green-600 h-2 rounded-full"
                    style={{
                      width: `${Math.round((summary.managedContainers / summary.totalContainers) * 100)}%`,
                    }}
                  />
                </div>
                <p className="text-xs text-gray-500 mt-1">
                  {Math.round((summary.managedContainers / summary.totalContainers) * 100)}% Managed
                </p>
              </div>
            )}

            {onViewDetails && (
              <p className="text-xs text-blue-600 text-center pt-2">Click to view details →</p>
            )}
          </div>
        ) : (
          <p className="text-center text-sm text-gray-500 py-4">No discovery data available</p>
        )}
      </CardContent>
    </Card>
  )
}

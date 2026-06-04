'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export interface ProviderStats {
  name: string
  count: number
  status: 'active' | 'inactive'
  color: string
  textColor: string
}

interface ProviderSummaryCardProps {
  providers: ProviderStats[]
}

export function ProviderSummaryCard({ providers }: ProviderSummaryCardProps) {
  const activeProviders = providers.filter((p) => p.status === 'active')
  const totalSecrets = providers.reduce((sum, p) => sum + p.count, 0)

  return (
    <Card>
      <CardHeader>
        <CardTitle>Provider Distribution</CardTitle>
        <CardDescription>Secrets stored across different providers</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {/* Summary Stats */}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <p className="text-xs text-gray-600 uppercase font-semibold">Active Providers</p>
              <p className="text-2xl font-bold text-gray-900">{activeProviders.length}</p>
            </div>
            <div>
              <p className="text-xs text-gray-600 uppercase font-semibold">Total Secrets</p>
              <p className="text-2xl font-bold text-gray-900">{totalSecrets}</p>
            </div>
          </div>

          {/* Provider Breakdown */}
          <div className="pt-4 border-t space-y-3">
            {providers.map((provider) => (
              <div key={provider.name} className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className={`w-3 h-3 rounded-full ${provider.color}`} />
                  <div>
                    <p className="text-sm font-medium text-gray-900">{provider.name}</p>
                    <p className="text-xs text-gray-600">{provider.count} secrets</p>
                  </div>
                </div>
                <Badge
                  variant={provider.status === 'active' ? 'default' : 'secondary'}
                  className="text-xs"
                >
                  {provider.status === 'active' ? 'Active' : 'Inactive'}
                </Badge>
              </div>
            ))}
          </div>

          {/* Visual bars */}
          <div className="pt-4 space-y-2">
            {providers.map((provider) => {
              const percentage = totalSecrets > 0 ? (provider.count / totalSecrets) * 100 : 0
              return (
                <div key={`${provider.name}-bar`}>
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-xs text-gray-600">{provider.name}</span>
                    <span className="text-xs font-semibold text-gray-900">{percentage.toFixed(0)}%</span>
                  </div>
                  <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
                    <div
                      className={`h-full ${provider.color} transition-all`}
                      style={{ width: `${percentage}%` }}
                    />
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

'use client'

import { useQuery } from '@tanstack/react-query'
import { apiClient, Health } from '@/lib/api-client'
import { GlobalSearch } from './global-search'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, CheckCircle, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'

export function Header() {
  const { data: health, isLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 5000, // Refresh every 5 seconds
  })

  const isHealthy = health?.status === 'up'

  return (
    <header className="border-b border-border bg-card">
      <div className="flex items-center justify-between px-6 py-4">
        <div>
          <h2 className="text-2xl font-semibold text-foreground">
            DSO Dashboard
          </h2>
          <p className="text-sm text-muted-foreground">
            Docker Secret Operator
          </p>
        </div>

        <div className="flex items-center gap-6">
          {/* Global Search */}
          <GlobalSearch />

          {/* Agent Status */}
          <div className="flex items-center gap-3">
            <div className="text-right">
              <p className="text-sm font-medium text-foreground">Agent Status</p>
              <p className="text-xs text-muted-foreground">
                {health?.version || 'v3.6.0'}
              </p>
            </div>

            {isLoading ? (
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            ) : isHealthy ? (
              <CheckCircle className="h-5 w-5 text-green-600" />
            ) : (
              <AlertCircle className="h-5 w-5 text-red-600" />
            )}

            <Badge variant={isHealthy ? 'default' : 'destructive'}>
              {isHealthy ? 'UP' : 'DOWN'}
            </Badge>
          </div>

          {/* Info Icon - placeholder for future features */}
          <div className="h-8 w-8 rounded-full bg-muted" />
        </div>
      </div>
    </header>
  )
}

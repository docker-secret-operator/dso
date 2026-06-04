'use client'

import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AlertCircle, AlertTriangle, ArrowRight } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'
import { findOperationalHotspots } from '@/lib/correlation'

export function OperationalHotspotsWidget() {
  // Fetch secrets
  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: () => apiClient.getSecrets(),
    refetchInterval: 30000,
  })

  // Fetch containers
  const { data: containers = [] } = useQuery({
    queryKey: ['containers'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/discovery/docker')
        if (!response.ok) return []
        const data = await response.json()
        return data.containers || []
      } catch {
        return []
      }
    },
    refetchInterval: 60000,
  })

  // Fetch events
  const { data: events = [] } = useQuery({
    queryKey: ['events'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/events')
        if (!response.ok) return []
        const data = await response.json()
        return data.events || []
      } catch {
        return []
      }
    },
    refetchInterval: 10000,
  })

  // Calculate hotspots
  const hotspots = useMemo(
    () => findOperationalHotspots(containers, secrets, events, 5),
    [containers, secrets, events]
  )

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Operational Hotspots</CardTitle>
          <CardDescription>Top issues ranked by impact</CardDescription>
        </div>
        <Link href="/insights/secrets">
          <Button variant="outline" size="sm" className="gap-2">
            View all
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent>
        {hotspots.length === 0 ? (
          <div className="text-center py-8 text-gray-600 text-sm">
            <p>No operational issues detected</p>
            <p className="text-xs mt-2">System is operating normally</p>
          </div>
        ) : (
          <div className="space-y-3">
            {hotspots.map((hotspot, idx) => {
              const severityConfig = {
                high: {
                  bg: 'bg-red-50',
                  border: 'border-red-200',
                  badge: 'bg-red-100 text-red-800',
                  icon: AlertCircle,
                },
                medium: {
                  bg: 'bg-yellow-50',
                  border: 'border-yellow-200',
                  badge: 'bg-yellow-100 text-yellow-800',
                  icon: AlertTriangle,
                },
                low: {
                  bg: 'bg-gray-50',
                  border: 'border-gray-200',
                  badge: 'bg-gray-100 text-gray-800',
                  icon: AlertTriangle,
                },
              }

              const config = severityConfig[hotspot.severity]
              const Icon = config.icon

              const targetRoute =
                hotspot.type === 'secret'
                  ? `/secrets?name=${encodeURIComponent(hotspot.resource)}`
                  : `/discovery?container=${encodeURIComponent(hotspot.resource)}`

              return (
                <Link key={idx} href={targetRoute}>
                  <div
                    className={`p-3 rounded-lg border ${config.bg} ${config.border} cursor-pointer hover:shadow-sm transition-shadow`}
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex items-start gap-2 flex-1 min-w-0">
                        <Icon className="w-4 h-4 mt-0.5 flex-shrink-0" />
                        <div className="flex-1 min-w-0">
                          <p className="text-sm font-semibold text-gray-900 truncate">
                            {hotspot.resource}
                          </p>
                          <p className="text-xs text-gray-600 mt-1">
                            Affects {hotspot.affectedCount}{' '}
                            {hotspot.type === 'secret' ? 'containers' : 'secrets'} • {hotspot.recentIssues}{' '}
                            recent{hotspot.recentIssues === 1 ? ' error' : ' errors'}
                          </p>
                        </div>
                      </div>
                      <Badge className={`${config.badge} text-xs flex-shrink-0`}>
                        {hotspot.severity === 'high' ? 'Critical' : 'Warning'}
                      </Badge>
                    </div>
                  </div>
                </Link>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

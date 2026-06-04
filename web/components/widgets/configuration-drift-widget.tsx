'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { AlertCircle, AlertTriangle, ArrowRight } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'
import { detectDriftIssues, generateValidationSummary } from '@/lib/drift-detection'

export function ConfigurationDriftWidget() {
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

  // Fetch secrets
  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/secrets')
        if (!response.ok) return []
        const data = await response.json()
        return data.secrets || []
      } catch {
        return []
      }
    },
    refetchInterval: 30000,
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

  // Build mappings from containers' DSO awareness
  const mappings = useMemo(() => {
    const result: Array<{ container: string; secret: string }> = []
    containers.forEach((container: any) => {
      const secrets = container.dso_awareness?.managed_secrets || []
      secrets.forEach((secret: string) => {
        result.push({ container: container.name, secret })
      })
    })
    return result
  }, [containers])

  // Detect drift issues
  const driftIssues = useMemo(() => {
    return detectDriftIssues(containers, secrets, mappings, events)
  }, [containers, secrets, mappings, events])

  // Generate summary
  const summary = useMemo(() => {
    return generateValidationSummary(driftIssues)
  }, [driftIssues])

  const criticalIssues = driftIssues.filter((i) => i.severity === 'critical')
  const warningIssues = driftIssues.filter((i) => i.severity === 'warning')

  // Determine trend
  const trend = summary.critical > 0 ? 'increasing' : 'stable'
  const trendColor = trend === 'increasing' ? 'text-red-600' : 'text-green-600'
  const trendIcon = trend === 'increasing' ? '📈' : '✓'

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Configuration Drift</CardTitle>
          <CardDescription>Issues detected in your configuration</CardDescription>
        </div>
        <Link href="/drift">
          <Button variant="outline" size="sm" className="gap-2">
            View all
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Status Overview */}
        <div className="grid grid-cols-3 gap-4">
          <div className="text-center p-3 bg-red-50 rounded-lg border border-red-200">
            <p className="text-2xl font-bold text-red-600">{summary.critical}</p>
            <p className="text-xs text-red-700 font-semibold mt-1">Critical Issues</p>
          </div>
          <div className="text-center p-3 bg-yellow-50 rounded-lg border border-yellow-200">
            <p className="text-2xl font-bold text-yellow-600">{summary.warning}</p>
            <p className="text-xs text-yellow-700 font-semibold mt-1">Warnings</p>
          </div>
          <div className={`text-center p-3 rounded-lg border ${
            trend === 'stable'
              ? 'bg-green-50 border-green-200'
              : 'bg-red-50 border-red-200'
          }`}>
            <p className={`text-xl font-bold ${trendColor}`}>{trendIcon}</p>
            <p className={`text-xs font-semibold mt-1 ${trendColor}`}>
              {trend === 'stable' ? 'Stable' : 'Attention Needed'}
            </p>
          </div>
        </div>

        {/* Critical Issues List */}
        {summary.critical > 0 && (
          <div className="space-y-2 pt-2 border-t">
            <p className="text-xs font-semibold text-red-900 uppercase">Critical Issues</p>
            <div className="space-y-1 max-h-32 overflow-y-auto">
              {criticalIssues.slice(0, 3).map((issue) => (
                <div key={issue.id} className="flex items-start gap-2 text-xs p-2 bg-red-50 rounded">
                  <AlertCircle className="w-3 h-3 text-red-600 flex-shrink-0 mt-0.5" />
                  <div className="flex-1 min-w-0">
                    <p className="text-red-900 font-medium">{issue.resource}</p>
                    <p className="text-red-700 text-xs">{issue.type}</p>
                  </div>
                </div>
              ))}
              {summary.critical > 3 && (
                <p className="text-xs text-red-700 italic px-2">
                  +{summary.critical - 3} more critical issues
                </p>
              )}
            </div>
          </div>
        )}

        {/* Warning Issues List */}
        {summary.warning > 0 && summary.critical === 0 && (
          <div className="space-y-2 pt-2 border-t">
            <p className="text-xs font-semibold text-yellow-900 uppercase">Warnings</p>
            <div className="space-y-1 max-h-32 overflow-y-auto">
              {warningIssues.slice(0, 3).map((issue) => (
                <div key={issue.id} className="flex items-start gap-2 text-xs p-2 bg-yellow-50 rounded">
                  <AlertTriangle className="w-3 h-3 text-yellow-600 flex-shrink-0 mt-0.5" />
                  <div className="flex-1 min-w-0">
                    <p className="text-yellow-900 font-medium">{issue.resource}</p>
                    <p className="text-yellow-700 text-xs">{issue.type}</p>
                  </div>
                </div>
              ))}
              {summary.warning > 3 && (
                <p className="text-xs text-yellow-700 italic px-2">
                  +{summary.warning - 3} more warnings
                </p>
              )}
            </div>
          </div>
        )}

        {/* No Issues State */}
        {summary.total === 0 && (
          <div className="text-center py-6 text-green-600">
            <p className="text-sm font-semibold">✓ No drift detected</p>
            <p className="text-xs text-green-700 mt-1">Your configuration is consistent</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ArrowRight, Zap } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'
import {
  generateRemediationPlans,
  generateRemediationSummary,
} from '@/lib/remediation-planner'
import { detectDriftIssues } from '@/lib/drift-detection'

export function RecommendedActionsWidget() {
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

  // Build mappings
  const mappings = useMemo(() => {
    const result: Array<{ container: string; secret: string }> = []
    containers.forEach((container: any) => {
      const containerSecrets = container.dso_awareness?.managed_secrets || []
      containerSecrets.forEach((secret: string) => {
        result.push({ container: container.name, secret })
      })
    })
    return result
  }, [containers])

  // Detect drift issues
  const driftIssues = useMemo(() => {
    return detectDriftIssues(containers, secrets, mappings, events)
  }, [containers, secrets, mappings, events])

  // Generate remediation plans
  const remediationPlans = useMemo(() => {
    return generateRemediationPlans(driftIssues, containers, secrets, mappings)
  }, [driftIssues, containers, secrets, mappings])

  // Generate summary
  const summary = useMemo(() => {
    return generateRemediationSummary(remediationPlans)
  }, [remediationPlans])

  const topActions = summary.topOpportunities.slice(0, 3)

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Recommended Actions</CardTitle>
          <CardDescription>Top remediation opportunities</CardDescription>
        </div>
        <Link href="/remediation">
          <Button variant="outline" size="sm" className="gap-2">
            View all
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent className="space-y-4">
        {topActions.length > 0 ? (
          <div className="space-y-3">
            {topActions.map((action) => {
              const priorityColor =
                action.priorityScore >= 70
                  ? 'bg-red-50 border-red-200'
                  : action.priorityScore >= 50
                    ? 'bg-yellow-50 border-yellow-200'
                    : 'bg-blue-50 border-blue-200'

              const priorityTextColor =
                action.priorityScore >= 70
                  ? 'text-red-900'
                  : action.priorityScore >= 50
                    ? 'text-yellow-900'
                    : 'text-blue-900'

              return (
                <div
                  key={action.id}
                  className={`p-3 rounded-lg border ${priorityColor} space-y-2`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex-1 min-w-0">
                      <p className={`text-sm font-semibold ${priorityTextColor} truncate`}>
                        {action.suggestedFix}
                      </p>
                      <p className="text-xs text-gray-600 mt-1">{action.resource}</p>
                    </div>
                    <Badge
                      className={`text-xs flex-shrink-0 ${
                        action.riskLevel === 'high'
                          ? 'bg-red-100 text-red-800'
                          : action.riskLevel === 'medium'
                            ? 'bg-yellow-100 text-yellow-800'
                            : 'bg-green-100 text-green-800'
                      }`}
                    >
                      {action.riskLevel}
                    </Badge>
                  </div>
                  <div className="flex items-center gap-2">
                    <div className="flex-1 h-1.5 bg-gray-200 rounded-full overflow-hidden">
                      <div
                        className="h-full bg-blue-600"
                        style={{ width: `${action.priorityScore}%` }}
                      />
                    </div>
                    <span className="text-xs font-semibold text-gray-600">
                      {action.priorityScore}
                    </span>
                  </div>
                  <p className="text-xs text-gray-700">
                    <span className="font-semibold">Impact:</span> {action.affectedContainers.length}{' '}
                    containers, {action.affectedSecrets.length} secrets
                  </p>
                </div>
              )
            })}
          </div>
        ) : (
          <div className="text-center py-6 text-green-600">
            <Zap className="w-8 h-8 mx-auto mb-2" />
            <p className="text-sm font-semibold">All systems optimal</p>
            <p className="text-xs text-green-700 mt-1">No remediation needed</p>
          </div>
        )}

        {summary.total > topActions.length && (
          <div className="pt-2 border-t">
            <p className="text-xs text-gray-600 italic">
              +{summary.total - topActions.length} more remediation opportunities
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

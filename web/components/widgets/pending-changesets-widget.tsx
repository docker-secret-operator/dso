'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ArrowRight } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'
import { generateRemediationPlans, generateRemediationSummary } from '@/lib/remediation-planner'
import { generateChangeSets, filterChangeSets } from '@/lib/change-set'
import { detectDriftIssues } from '@/lib/drift-detection'

export function PendingChangeSetsWidget() {
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

  const driftIssues = useMemo(() => {
    return detectDriftIssues(containers, secrets, mappings, events)
  }, [containers, secrets, mappings, events])

  const remediationPlans = useMemo(() => {
    return generateRemediationPlans(driftIssues, containers, secrets, mappings)
  }, [driftIssues, containers, secrets, mappings])

  const changeSets = useMemo(() => {
    return generateChangeSets(remediationPlans, containers, secrets, mappings)
  }, [remediationPlans, containers, secrets, mappings])

  const pendingChangeSets = useMemo(() => {
    return filterChangeSets(changeSets, { approvalStatus: ['pending'] }).slice(0, 3)
  }, [changeSets])

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Pending Change Sets</CardTitle>
          <CardDescription>High-priority previews</CardDescription>
        </div>
        <Link href="/changesets">
          <Button variant="outline" size="sm" className="gap-2">
            View all
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent className="space-y-3">
        {pendingChangeSets.length > 0 ? (
          pendingChangeSets.map((cs) => (
            <div key={cs.id} className="p-3 bg-gray-50 rounded border text-sm space-y-1">
              <p className="font-semibold text-gray-900">{cs.title}</p>
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="text-xs">
                  #{cs.executionOrder}
                </Badge>
                <Badge
                  variant={
                    cs.riskLevel === 'high'
                      ? 'destructive'
                      : cs.riskLevel === 'medium'
                        ? 'secondary'
                        : 'default'
                  }
                  className="text-xs"
                >
                  {cs.riskLevel}
                </Badge>
                {cs.blockers.length > 0 && (
                  <Badge variant="destructive" className="text-xs">
                    {cs.blockers.length} blocker(s)
                  </Badge>
                )}
              </div>
              <p className="text-xs text-gray-600">
                +{cs.diffSummary.additions} -{cs.diffSummary.removals}
              </p>
            </div>
          ))
        ) : (
          <div className="text-center py-6 text-gray-600">
            <p className="text-sm">No pending change sets</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

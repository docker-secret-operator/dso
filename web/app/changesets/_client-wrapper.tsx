'use client'

import { useRouter } from 'next/navigation'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorBoundary } from '@/components/error-boundary'
import { RefreshCw, AlertCircle, CheckCircle2, XCircle } from 'lucide-react'
import { useMemo, useState } from 'react'
import {
  generateRemediationPlans,
  generateRemediationSummary,
} from '@/lib/remediation-planner'
import {
  generateChangeSets,
  generateChangeSetSummary,
  filterChangeSets,
  sortChangeSets,
  visualizeImpactGraph,
  type ApprovalStatus,
  type ChangeSet,
} from '@/lib/change-set'
import { detectDriftIssues } from '@/lib/drift-detection'
import { createDraftFromChangeSet } from '@/lib/workspace-validation'

export function ChangeSetsDashboardClient() {
  const router = useRouter()
  const [selectedApprovalStatus, setSelectedApprovalStatus] = useState<ApprovalStatus[]>([
    'pending',
    'approved',
    'rejected',
  ])
  const [sortBy, setSortBy] = useState<'order' | 'priority' | 'risk'>('order')
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)

  const { data: containers = [], refetch: refetchContainers } = useQuery({
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

  const { data: secrets = [], refetch: refetchSecrets } = useQuery({
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

  const { data: events = [], refetch: refetchEvents } = useQuery({
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

  const summary = useMemo(() => {
    return generateChangeSetSummary(changeSets)
  }, [changeSets])

  const filteredChangeSets = useMemo(() => {
    const filtered = filterChangeSets(changeSets, { approvalStatus: selectedApprovalStatus })
    return sortChangeSets(filtered, sortBy)
  }, [changeSets, selectedApprovalStatus, sortBy])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await Promise.all([refetchContainers(), refetchSecrets(), refetchEvents()])
    } finally {
      setIsRefreshing(false)
    }
  }

  const getApprovalIcon = (status: ApprovalStatus) => {
    switch (status) {
      case 'approved':
        return <CheckCircle2 className="w-4 h-4 text-green-600" />
      case 'rejected':
        return <XCircle className="w-4 h-4 text-red-600" />
      default:
        return <AlertCircle className="w-4 h-4 text-yellow-600" />
    }
  }

  const handleLoadIntoWorkspace = (changeSet: ChangeSet) => {
    const draftChanges = createDraftFromChangeSet(changeSet)
    sessionStorage.setItem(
      'workspace-draft-from-changeset',
      JSON.stringify({
        changeSet,
        changes: draftChanges,
        timestamp: new Date().toISOString(),
      })
    )
    router.push('/workspace')
  }

  return (
    <ErrorBoundary>
      <div className="p-8 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold">Change Sets</h1>
            <p className="text-gray-600 mt-1">Explicit configuration change previews</p>
          </div>
          <Button
            onClick={handleRefresh}
            variant="outline"
            size="sm"
            disabled={isRefreshing}
            className="gap-2"
          >
            <RefreshCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        {/* Summary */}
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold">{summary.total}</p>
              <p className="text-xs text-gray-600 mt-1">Total Change Sets</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-yellow-600">{summary.pending}</p>
              <p className="text-xs text-gray-600 mt-1">Pending</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-green-600">{summary.approved}</p>
              <p className="text-xs text-gray-600 mt-1">Approved</p>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <p className="text-3xl font-bold text-red-600">{summary.blockerCount}</p>
              <p className="text-xs text-gray-600 mt-1">Blockers</p>
            </CardContent>
          </Card>
        </div>

        {/* Filters */}
        <Card>
          <CardHeader>
            <CardTitle>Filters</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <p className="text-sm font-semibold mb-2">Approval Status</p>
              <div className="flex gap-2">
                {(['pending', 'approved', 'rejected'] as ApprovalStatus[]).map((status) => (
                  <button
                    key={status}
                    onClick={() => {
                      if (selectedApprovalStatus.includes(status)) {
                        setSelectedApprovalStatus(
                          selectedApprovalStatus.filter((s) => s !== status)
                        )
                      } else {
                        setSelectedApprovalStatus([...selectedApprovalStatus, status])
                      }
                    }}
                    className={`px-3 py-1 text-sm rounded border transition-colors ${
                      selectedApprovalStatus.includes(status)
                        ? 'bg-blue-100 border-blue-300'
                        : 'bg-white border-gray-200 hover:bg-gray-50'
                    }`}
                  >
                    {status.charAt(0).toUpperCase() + status.slice(1)}
                  </button>
                ))}
              </div>
            </div>

            <div>
              <p className="text-sm font-semibold mb-2">Sort</p>
              <div className="flex gap-2">
                {(['order', 'priority', 'risk'] as const).map((option) => (
                  <button
                    key={option}
                    onClick={() => setSortBy(option)}
                    className={`px-3 py-1 text-sm rounded border transition-colors ${
                      sortBy === option
                        ? 'bg-blue-100 border-blue-300'
                        : 'bg-white border-gray-200 hover:bg-gray-50'
                    }`}
                  >
                    By {option.charAt(0).toUpperCase() + option.slice(1)}
                  </button>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Change Sets List */}
        <div className="space-y-4">
          {filteredChangeSets.map((cs) => (
            <Card key={cs.id} className="hover:shadow-sm transition-shadow">
              <CardHeader
                className="cursor-pointer"
                onClick={() => setExpandedId(expandedId === cs.id ? null : cs.id)}
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      {getApprovalIcon(cs.approvalStatus)}
                      <CardTitle className="text-lg">{cs.title}</CardTitle>
                      <Badge variant="outline">{cs.executionOrder}</Badge>
                    </div>
                    <CardDescription className="mt-2">{cs.description}</CardDescription>
                  </div>
                  <div className="text-right">
                    <Badge
                      variant={
                        cs.riskLevel === 'high'
                          ? 'destructive'
                          : cs.riskLevel === 'medium'
                            ? 'secondary'
                            : 'default'
                      }
                    >
                      {cs.riskLevel}
                    </Badge>
                  </div>
                </div>
              </CardHeader>

              {expandedId === cs.id && (
                <CardContent className="space-y-4 border-t pt-4">
                  {/* Diff */}
                  <div>
                    <p className="text-sm font-semibold mb-2">Changes</p>
                    <div className="bg-gray-50 p-3 rounded text-sm font-mono space-y-1 max-h-48 overflow-y-auto">
                      <p className="text-gray-600">
                        +{cs.diffSummary.additions} -{cs.diffSummary.removals}{' '}
                        ~{cs.diffSummary.modifications}
                      </p>
                      {cs.diff.slice(0, 5).map((line, i) => (
                        <div
                          key={i}
                          className={
                            line.type === 'add'
                              ? 'text-green-700'
                              : line.type === 'remove'
                                ? 'text-red-700'
                                : 'text-gray-700'
                          }
                        >
                          {line.proposed || line.current || line.description}
                        </div>
                      ))}
                      {cs.diff.length > 5 && (
                        <p className="text-gray-500">+{cs.diff.length - 5} more...</p>
                      )}
                    </div>
                  </div>

                  {/* Validation */}
                  {cs.blockers.length > 0 && (
                    <div className="p-3 bg-red-50 border border-red-200 rounded">
                      <p className="text-sm font-semibold text-red-900 mb-2">
                        ⚠ {cs.blockers.length} Blocker(s)
                      </p>
                      {cs.blockers.map((r, i) => (
                        <p key={i} className="text-sm text-red-800">
                          • {r.message}
                        </p>
                      ))}
                    </div>
                  )}

                  {/* Impact */}
                  <div>
                    <p className="text-sm font-semibold mb-2">Impact</p>
                    <p className="text-sm text-gray-700">
                      {cs.affectedResources.containers.length} containers,{' '}
                      {cs.affectedResources.secrets.length} secrets
                    </p>
                  </div>

                  {/* Action */}
                  <Button
                    onClick={() => handleLoadIntoWorkspace(cs)}
                    variant="outline"
                    className="w-full text-blue-600 hover:bg-blue-50"
                  >
                    Load into Workspace
                  </Button>
                </CardContent>
              )}
            </Card>
          ))}
        </div>

        {/* Info */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Preview Mode:</strong> Review change sets before execution capabilities are added.
              No changes will be applied.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}

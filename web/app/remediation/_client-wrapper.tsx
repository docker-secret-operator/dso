'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorBoundary } from '@/components/error-boundary'
import { EmptyState } from '@/components/empty-state'
import { RefreshCw, Search, AlertCircle, AlertTriangle, CheckCircle2 } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useMemo, useState, useCallback } from 'react'
import {
  generateRemediationPlans,
  generateRemediationSummary,
  filterRemediationPlans,
  sortRemediationPlans,
  estimateCumulativeImpact,
  type RemediationPlan,
  type RiskLevel,
} from '@/lib/remediation-planner'
import { detectDriftIssues } from '@/lib/drift-detection'
import { InsightsTable, type InsightsTableColumn } from '@/components/insights-table'
import { createDraftFromRemediationPlan } from '@/lib/workspace-validation'

export function RemediationDashboardClient() {
  const router = useRouter()
  const [selectedRiskLevel, setSelectedRiskLevel] = useState<RiskLevel[]>([
    'high',
    'medium',
    'low',
  ])
  const [searchQuery, setSearchQuery] = useState('')
  const [sortBy, setSortBy] = useState<'priority' | 'resource' | 'type'>('priority')
  const [isRefreshing, setIsRefreshing] = useState(false)

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

  // Filter and sort plans
  const filteredPlans = useMemo(() => {
    const filtered = filterRemediationPlans(remediationPlans, {
      riskLevel: selectedRiskLevel,
      searchQuery,
    })
    return sortRemediationPlans(filtered, sortBy)
  }, [remediationPlans, selectedRiskLevel, searchQuery, sortBy])

  // Calculate cumulative impact
  const cumulativeImpact = useMemo(() => {
    return estimateCumulativeImpact(filteredPlans)
  }, [filteredPlans])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await Promise.all([refetchContainers(), refetchSecrets(), refetchEvents()])
    } finally {
      setIsRefreshing(false)
    }
  }

  const handleSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value)
  }, [])

  const handlePreviewInWorkspace = useCallback((plan: RemediationPlan) => {
    const draftChanges = createDraftFromRemediationPlan(plan)
    sessionStorage.setItem(
      'workspace-draft-from-plan',
      JSON.stringify({
        plan,
        changes: draftChanges,
        timestamp: new Date().toISOString(),
      })
    )
    router.push('/workspace')
  }, [router])

  const getRiskIcon = (risk: RiskLevel) => {
    switch (risk) {
      case 'high':
        return <AlertCircle className="w-4 h-4" />
      case 'medium':
        return <AlertTriangle className="w-4 h-4" />
      default:
        return <CheckCircle2 className="w-4 h-4" />
    }
  }

  const getRiskColor = (risk: RiskLevel) => {
    switch (risk) {
      case 'high':
        return 'bg-red-100 text-red-800'
      case 'medium':
        return 'bg-yellow-100 text-yellow-800'
      default:
        return 'bg-green-100 text-green-800'
    }
  }

  interface PlanRow extends RemediationPlan {}

  const columns: InsightsTableColumn<PlanRow>[] = [
    {
      key: 'priorityScore',
      label: 'Priority',
      render: (value: unknown) => {
        const numValue = value as number
        return (
          <div className="flex items-center gap-2">
            <div className="w-12 h-2 bg-gray-200 rounded-full overflow-hidden">
              <div
                className="h-full bg-blue-600"
                style={{ width: `${numValue}%` }}
              />
            </div>
            <span className="text-sm font-semibold">{numValue}</span>
          </div>
        )
      },
    },
    {
      key: 'riskLevel',
      label: 'Risk',
      render: (value: unknown) => {
        const risk = value as RiskLevel
        return (
          <Badge className={`${getRiskColor(risk)} text-xs gap-1`}>
            {getRiskIcon(risk)}
            {risk.charAt(0).toUpperCase() + risk.slice(1)}
          </Badge>
        )
      },
    },
    { key: 'resource', label: 'Resource', sortable: true },
    { key: 'type', label: 'Remediation Type', sortable: true },
    { key: 'suggestedFix', label: 'Suggested Fix' },
    {
      key: 'actions' as unknown as keyof PlanRow,
      label: 'Actions',
      render: (_, row) => {
        const plan = row as RemediationPlan
        return (
          <Button
            variant="outline"
            size="sm"
            onClick={() => handlePreviewInWorkspace(plan)}
            className="text-blue-600 hover:bg-blue-50"
          >
            Preview in Workspace
          </Button>
        )
      },
    },
  ]

  return (
    <ErrorBoundary>
      <div className="p-8 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-3xl font-bold">Remediation Planning</h1>
            <p className="text-gray-600 mt-1">Plan configuration changes and understand impact</p>
          </div>
          <Button
            onClick={handleRefresh}
            variant="outline"
            size="sm"
            disabled={isRefreshing}
            className="gap-2"
          >
            <RefreshCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
            {isRefreshing ? 'Refreshing...' : 'Refresh'}
          </Button>
        </div>

        {/* Summary Cards */}
        <div className="grid grid-cols-5 gap-4">
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-900">{summary.total}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">
                  Total Plans
                </p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-red-600">{summary.critical}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">
                  Critical
                </p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-orange-600">{summary.high}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">
                  High Risk
                </p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-yellow-600">{summary.medium}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">
                  Medium Risk
                </p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-green-600">{summary.low}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">
                  Low Risk
                </p>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Impact Overview */}
        <Card className="bg-blue-50 border-blue-200">
          <CardHeader>
            <CardTitle>Cumulative Impact Estimate</CardTitle>
            <CardDescription>If all visible plans are executed</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-5 gap-4">
              <div>
                <p className="text-sm text-blue-700 font-semibold mb-1">Containers Affected</p>
                <p className="text-2xl font-bold text-blue-900">{cumulativeImpact.totalContainers}</p>
              </div>
              <div>
                <p className="text-sm text-blue-700 font-semibold mb-1">Secrets Affected</p>
                <p className="text-2xl font-bold text-blue-900">{cumulativeImpact.totalSecrets}</p>
              </div>
              <div>
                <p className="text-sm text-blue-700 font-semibold mb-1">Mappings Affected</p>
                <p className="text-2xl font-bold text-blue-900">{cumulativeImpact.totalMappings}</p>
              </div>
              <div>
                <p className="text-sm text-blue-700 font-semibold mb-1">Timeframe</p>
                <p className="text-xl font-bold text-blue-900">{cumulativeImpact.estimatedTimeframe}</p>
              </div>
              <div>
                <p className="text-sm text-blue-700 font-semibold mb-1">Reversibility</p>
                <p className="text-2xl font-bold text-blue-900">
                  {cumulativeImpact.reversibilityScore}%
                </p>
              </div>
            </div>
            {cumulativeImpact.containsHighRisk && (
              <div className="mt-4 p-3 bg-red-100 border border-red-300 rounded text-sm text-red-800">
                ⚠️ <strong>High-risk changes included:</strong> Carefully review and test before
                applying
              </div>
            )}
          </CardContent>
        </Card>

        {/* Filters */}
        <Card>
          <CardHeader>
            <CardTitle>Filters</CardTitle>
            <CardDescription>Filter remediation plans</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Risk Level Filter */}
            <div>
              <p className="text-sm font-semibold text-gray-900 mb-2">Risk Level</p>
              <div className="flex gap-2 flex-wrap">
                {['high', 'medium', 'low'].map((risk) => (
                  <button
                    key={risk}
                    onClick={() => {
                      if (selectedRiskLevel.includes(risk as RiskLevel)) {
                        setSelectedRiskLevel(
                          selectedRiskLevel.filter((r) => r !== risk)
                        )
                      } else {
                        setSelectedRiskLevel([
                          ...selectedRiskLevel,
                          risk as RiskLevel,
                        ])
                      }
                    }}
                    className={`px-3 py-1 text-sm rounded-lg border transition-colors ${
                      selectedRiskLevel.includes(risk as RiskLevel)
                        ? 'bg-blue-100 border-blue-300 text-blue-900'
                        : 'bg-white border-gray-200 text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    {risk.charAt(0).toUpperCase() + risk.slice(1)}
                  </button>
                ))}
              </div>
            </div>

            {/* Search */}
            <div>
              <p className="text-sm font-semibold text-gray-900 mb-2">Search</p>
              <div className="relative">
                <Search className="absolute left-3 top-3 w-4 h-4 text-gray-400" />
                <input
                  type="text"
                  placeholder="Search by resource, fix, or type..."
                  value={searchQuery}
                  onChange={handleSearchChange}
                  className="w-full pl-9 pr-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            </div>

            {/* Sort */}
            <div>
              <p className="text-sm font-semibold text-gray-900 mb-2">Sort</p>
              <div className="flex gap-2 flex-wrap">
                {['priority', 'resource', 'type'].map((option) => (
                  <button
                    key={option}
                    onClick={() => setSortBy(option as 'priority' | 'resource' | 'type')}
                    className={`px-3 py-1 text-sm rounded-lg border transition-colors ${
                      sortBy === option
                        ? 'bg-blue-100 border-blue-300 text-blue-900'
                        : 'bg-white border-gray-200 text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    By {option.charAt(0).toUpperCase() + option.slice(1)}
                  </button>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Plans Table */}
        {filteredPlans.length > 0 ? (
          <InsightsTable<PlanRow>
            title="Remediation Plans"
            description={`Showing ${filteredPlans.length} of ${remediationPlans.length} plans`}
            columns={columns}
            data={filteredPlans}
            searchableFields={['resource', 'suggestedFix']}
          />
        ) : (
          <EmptyState
            type={remediationPlans.length > 0 ? 'no-results' : 'empty'}
            title={
              remediationPlans.length > 0
                ? 'No plans match your filters'
                : 'No remediation needed'
            }
            description={
              remediationPlans.length > 0
                ? 'Try adjusting your filters'
                : 'Your configuration is optimal'
            }
          />
        )}

        {/* Info Banner */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Preview Only:</strong> This page shows remediation plans with impact estimates.
              No changes are made. Review plans carefully before implementation.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}

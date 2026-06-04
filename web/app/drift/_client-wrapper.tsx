'use client'

import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ErrorBoundary } from '@/components/error-boundary'
import { EmptyState } from '@/components/empty-state'
import { AlertCircle, AlertTriangle, Info, RefreshCw, Search } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useMemo, useState, useCallback } from 'react'
import {
  detectDriftIssues,
  generateValidationSummary,
  filterDriftIssues,
  sortDriftIssues,
  type DriftIssue,
  type DriftSeverity,
  type DriftCategory,
} from '@/lib/drift-detection'
import { InsightsTable, type InsightsTableColumn } from '@/components/insights-table'
import { createDraftFromDriftIssue } from '@/lib/workspace-validation'

export function DriftDashboardClient() {
  const router = useRouter()
  const [selectedSeverity, setSelectedSeverity] = useState<DriftSeverity[]>([
    'critical',
    'warning',
    'informational',
  ])
  const [selectedCategories, setSelectedCategories] = useState<DriftCategory[]>([
    'container',
    'secret',
    'mapping',
    'configuration',
  ])
  const [searchQuery, setSearchQuery] = useState('')
  const [sortBy, setSortBy] = useState<'severity' | 'resource' | 'type'>('severity')
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

  // Filter and sort issues
  const filteredIssues = useMemo(() => {
    const filtered = filterDriftIssues(driftIssues, {
      severity: selectedSeverity,
      category: selectedCategories,
      searchQuery,
    })
    return sortDriftIssues(filtered, sortBy)
  }, [driftIssues, selectedSeverity, selectedCategories, searchQuery, sortBy])

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

  const handleOpenInWorkspace = useCallback((issue: DriftIssue) => {
    // Store issue in sessionStorage for workspace to pick up
    const draftChanges = createDraftFromDriftIssue(issue, containers, secrets)
    sessionStorage.setItem(
      'workspace-draft-from-issue',
      JSON.stringify({
        issue,
        changes: draftChanges,
        timestamp: new Date().toISOString(),
      })
    )
    router.push('/workspace')
  }, [containers, secrets, router])

  const getSeverityIcon = (severity: DriftSeverity) => {
    switch (severity) {
      case 'critical':
        return <AlertCircle className="w-4 h-4" />
      case 'warning':
        return <AlertTriangle className="w-4 h-4" />
      default:
        return <Info className="w-4 h-4" />
    }
  }

  const getSeverityColor = (severity: DriftSeverity) => {
    switch (severity) {
      case 'critical':
        return 'bg-red-100 text-red-800'
      case 'warning':
        return 'bg-yellow-100 text-yellow-800'
      default:
        return 'bg-blue-100 text-blue-800'
    }
  }

  interface IssueRow extends DriftIssue {}

  const columns: InsightsTableColumn<IssueRow>[] = [
    {
      key: 'severity',
      label: 'Severity',
      render: (value: unknown) => {
        const severity = value as DriftSeverity
        return (
          <Badge className={`${getSeverityColor(severity)} text-xs gap-1`}>
            {getSeverityIcon(severity)}
            {severity.charAt(0).toUpperCase() + severity.slice(1)}
          </Badge>
        )
      },
    },
    { key: 'type', label: 'Type', sortable: true },
    { key: 'resource', label: 'Resource', sortable: true },
    { key: 'description', label: 'Description' },
    { key: 'recommendedAction', label: 'Recommended Action' },
    {
      key: 'actions' as unknown as keyof IssueRow,
      label: 'Actions',
      render: (_, row) => {
        const issue = row as DriftIssue
        return (
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleOpenInWorkspace(issue)}
            className="text-blue-600 hover:bg-blue-50"
          >
            Open in Workspace
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
            <h1 className="text-3xl font-bold">Configuration Drift Detection</h1>
            <p className="text-gray-600 mt-1">Identify configuration mismatches and inconsistencies</p>
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
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-900">{summary.total}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Total Issues</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-red-600">{summary.critical}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Critical</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-yellow-600">{summary.warning}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Warning</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-blue-600">{summary.informational}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Informational</p>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Category Breakdown */}
        <div className="grid grid-cols-4 gap-4">
          {Object.entries(summary.byCategory).map(([category, count]) => (
            <Card key={category}>
              <CardContent className="pt-6">
                <div>
                  <p className="text-sm font-semibold text-gray-900 mb-2">
                    {category.charAt(0).toUpperCase() + category.slice(1)}
                  </p>
                  <div className="flex items-baseline gap-2">
                    <p className="text-2xl font-bold text-gray-900">{count}</p>
                    <p className="text-xs text-gray-600">
                      {((count / summary.total) * 100).toFixed(0)}%
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>

        {/* Filters */}
        <Card>
          <CardHeader>
            <CardTitle>Filters</CardTitle>
            <CardDescription>Filter issues by severity and category</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Severity Filter */}
            <div>
              <p className="text-sm font-semibold text-gray-900 mb-2">Severity</p>
              <div className="flex gap-2 flex-wrap">
                {['critical', 'warning', 'informational'].map((severity) => (
                  <button
                    key={severity}
                    onClick={() => {
                      if (selectedSeverity.includes(severity as DriftSeverity)) {
                        setSelectedSeverity(
                          selectedSeverity.filter((s) => s !== severity)
                        )
                      } else {
                        setSelectedSeverity([
                          ...selectedSeverity,
                          severity as DriftSeverity,
                        ])
                      }
                    }}
                    className={`px-3 py-1 text-sm rounded-lg border transition-colors ${
                      selectedSeverity.includes(severity as DriftSeverity)
                        ? 'bg-blue-100 border-blue-300 text-blue-900'
                        : 'bg-white border-gray-200 text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    {severity.charAt(0).toUpperCase() + severity.slice(1)}
                  </button>
                ))}
              </div>
            </div>

            {/* Category Filter */}
            <div>
              <p className="text-sm font-semibold text-gray-900 mb-2">Category</p>
              <div className="flex gap-2 flex-wrap">
                {['container', 'secret', 'mapping', 'configuration'].map((category) => (
                  <button
                    key={category}
                    onClick={() => {
                      if (selectedCategories.includes(category as DriftCategory)) {
                        setSelectedCategories(
                          selectedCategories.filter((c) => c !== category)
                        )
                      } else {
                        setSelectedCategories([
                          ...selectedCategories,
                          category as DriftCategory,
                        ])
                      }
                    }}
                    className={`px-3 py-1 text-sm rounded-lg border transition-colors ${
                      selectedCategories.includes(category as DriftCategory)
                        ? 'bg-blue-100 border-blue-300 text-blue-900'
                        : 'bg-white border-gray-200 text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    {category.charAt(0).toUpperCase() + category.slice(1)}
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
                  placeholder="Search issues..."
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
                {['severity', 'resource', 'type'].map((option) => (
                  <button
                    key={option}
                    onClick={() => setSortBy(option as 'severity' | 'resource' | 'type')}
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

        {/* Issues List */}
        {filteredIssues.length > 0 ? (
          <InsightsTable<IssueRow>
            title="Configuration Drift Issues"
            description={`Showing ${filteredIssues.length} of ${driftIssues.length} issues`}
            columns={columns}
            data={filteredIssues}
            searchableFields={['resource', 'description']}
          />
        ) : (
          <EmptyState
            type={driftIssues.length > 0 ? 'no-results' : 'empty'}
            title={driftIssues.length > 0 ? 'No issues match your filters' : 'No drift detected'}
            description={
              driftIssues.length > 0
                ? 'Try adjusting your filters'
                : 'Your configuration is consistent with runtime state'
            }
          />
        )}

        {/* Info Banner */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Read-Only View:</strong> This dashboard detects configuration mismatches and inconsistencies.
              It helps you identify problems before making configuration changes.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}

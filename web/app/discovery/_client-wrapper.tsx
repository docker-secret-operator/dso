'use client'

import { useDiscovery, useSecretMappings, useDiscoveryMetrics } from '@/hooks/useDiscovery'
import { apiFetch } from "@/lib/api-fetch"
import { useToast } from '@/hooks/useToast'
import { DiscoveryFilters, FilterType } from '@/components/discovery-filters'
import { EmptyState } from '@/components/empty-state'
import { ErrorBoundary } from '@/components/error-boundary'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { AlertCircle, CheckCircle2, RefreshCw, Server, Container, AlertTriangle, Lightbulb, Zap, Search, ChevronLeft, Loader2 } from 'lucide-react'
import { useState, useMemo, useCallback } from 'react'
import { useSearchParams } from 'next/navigation'
import { useQuery } from '@tanstack/react-query'
import { getSecretsForContainer, getEventsForContainer } from '@/lib/correlation'
import { InsightsTable, type InsightsTableColumn } from '@/components/insights-table'

type SortOption = 'name' | 'status' | 'created' | 'managed'

export function DiscoveryPageClient() {
  const searchParams = useSearchParams()
  const containerName = searchParams?.get('container')

  const { containers, discovery, loading, error, refresh } = useDiscovery()
  const { suggestions, loading: suggestionsLoading, error: suggestionsError, refresh: refreshMappings } = useSecretMappings()
  const { metrics } = useDiscoveryMetrics()
  const { toast } = useToast()
  const [expandedContainer, setExpandedContainer] = useState<string | null>(null)
  const [showMappings, setShowMappings] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [selectedFilters, setSelectedFilters] = useState<FilterType[]>([])
  const [searchQuery, setSearchQuery] = useState('')
  const [sortBy, setSortBy] = useState<SortOption>('name')

  // Fetch secrets and events for correlation
  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: async () => {
      try {
        const response = await apiFetch('/api/secrets')
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
        const response = await apiFetch('/api/events')
        if (!response.ok) return []
        const data = await response.json()
        return data.events || []
      } catch {
        return []
      }
    },
    refetchInterval: 10000,
  })

  // Calculate container counts for filter UI
  const containerCounts = useMemo(
    () => ({
      managed: containers.filter((c) => c.dso_awareness?.status === 'managed').length,
      partial: containers.filter((c) => c.dso_awareness?.status === 'partial').length,
      unmanaged: containers.filter((c) => c.dso_awareness?.status === 'unmanaged').length,
    }),
    [containers]
  )

  // Filter containers by status
  const filteredByStatus = useMemo(() => {
    if (selectedFilters.length === 0) return containers
    return containers.filter((c) => selectedFilters.includes(c.dso_awareness?.status as FilterType))
  }, [containers, selectedFilters])

  // Filter and search containers
  const filteredContainers = useMemo(() => {
    if (!searchQuery.trim()) return filteredByStatus

    const query = searchQuery.toLowerCase()
    return filteredByStatus.filter(
      (c) =>
        c.name?.toLowerCase().includes(query) ||
        c.image?.toLowerCase().includes(query) ||
        Object.values(c.labels ?? {}).some((label) => label.toLowerCase().includes(query))
    )
  }, [filteredByStatus, searchQuery])

  // Sort containers
  const sortedContainers = useMemo(() => {
    const sorted = [...filteredContainers]
    switch (sortBy) {
      case 'name':
        return sorted.sort((a, b) => a.name.localeCompare(b.name))
      case 'status':
        return sorted.sort((a, b) => a.status.localeCompare(b.status))
      case 'created':
        return sorted.sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime())
      case 'managed':
        return sorted.sort((a, b) => {
          const statusOrder = { managed: 0, partial: 1, unmanaged: 2 }
          return (
            (statusOrder[a.dso_awareness?.status as FilterType] ?? 3) -
            (statusOrder[b.dso_awareness?.status as FilterType] ?? 3)
          )
        })
      default:
        return sorted
    }
  }, [filteredContainers, sortBy])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await refresh()
      await refreshMappings()
      toast.success('Discovery refreshed', 'Container discovery and mapping suggestions updated')
    } catch (err) {
      toast.error('Refresh failed', err instanceof Error ? err.message : 'Failed to refresh discovery')
    } finally {
      setIsRefreshing(false)
    }
  }

  const handleSearchChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    setSearchQuery(e.target.value)
  }, [])

  const getDSOBadgeColor = (status: string) => {
    switch (status) {
      case 'managed':
        return 'bg-green-100 text-green-800'
      case 'partial':
        return 'bg-yellow-100 text-yellow-800'
      default:
        return 'bg-gray-100 text-gray-800'
    }
  }

  const getDSOIcon = (status: string) => {
    switch (status) {
      case 'managed':
        return <CheckCircle2 className="w-3 h-3" />
      case 'partial':
        return <AlertTriangle className="w-3 h-3" />
      default:
        return <AlertCircle className="w-3 h-3" />
    }
  }

  const container = useMemo(
    () => containers.find((c) => c.name === containerName),
    [containers, containerName]
  )

  const relatedSecrets = useMemo(
    () => getSecretsForContainer(containerName || '', containers, secrets),
    [containerName, containers, secrets]
  )

  const relatedEvents = useMemo(() => getEventsForContainer(containerName || '', events), [containerName, events])

  if (loading) {
    return (
      <div className="p-6">
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <RefreshCw className="w-8 h-8 animate-spin mx-auto mb-2" />
            <p>Discovering containers...</p>
          </div>
        </div>
      </div>
    )
  }

  // Detail view
  if (containerName) {
    if (!container) {
      return (
        <ErrorBoundary>
          <div className="p-8">
            <Button
              onClick={() => window.history.back()}
              variant="outline"
              size="sm"
              className="gap-2 mb-6"
            >
              <ChevronLeft className="w-4 h-4" />
              Back
            </Button>
            <EmptyState
              type="empty"
              title="Container not found"
              description={`The container "${containerName}" could not be found`}
            />
          </div>
        </ErrorBoundary>
      )
    }

    interface SecretRelationRow {
      id: string
      name: string
      provider: string
      status: string
    }

    interface EventRow {
      id: string
      timestamp: string
      severity: string
      message: string
    }

    const secretData: SecretRelationRow[] = relatedSecrets.map((s) => ({
      id: s.name,
      name: s.name,
      provider: s.provider,
      status: s.status,
    }))

    const eventData: EventRow[] = relatedEvents.map((e) => ({
      id: e.id,
      timestamp: e.timestamp,
      severity: e.severity,
      message: e.message,
    }))

    const secretColumns: InsightsTableColumn<SecretRelationRow>[] = [
      { key: 'name', label: 'Secret Name', sortable: true },
      {
        key: 'provider',
        label: 'Provider',
        render: (value: unknown) => (
          <Badge variant="outline" className="text-xs capitalize">
            {String(value)}
          </Badge>
        ),
      },
      {
        key: 'status',
        label: 'Status',
        render: (value: unknown) => (
          <Badge variant={value === 'ok' ? 'default' : 'destructive'} className="text-xs">
            {String(value)}
          </Badge>
        ),
      },
    ]

    const eventColumns: InsightsTableColumn<EventRow>[] = [
      {
        key: 'timestamp',
        label: 'Timestamp',
        render: (value: unknown) => new Date(value as string).toLocaleString(),
      },
      {
        key: 'severity',
        label: 'Severity',
        render: (value: unknown) => {
          const severity = String(value)
          return (
            <Badge
              variant={
                severity === 'error' ? 'destructive' : severity === 'warning' ? 'secondary' : 'default'
              }
              className="text-xs"
            >
              {severity.charAt(0).toUpperCase() + severity.slice(1)}
            </Badge>
          )
        },
      },
      { key: 'message', label: 'Message', sortable: true },
    ]

    return (
      <ErrorBoundary>
        <div className="p-8 space-y-6">
          <div className="flex items-center gap-4">
            <Button
              onClick={() => window.history.back()}
              variant="outline"
              size="sm"
              className="gap-2"
            >
              <ChevronLeft className="w-4 h-4" />
              Back
            </Button>
            <div>
              <h1 className="text-3xl font-bold flex items-center gap-2">
                <Container className="w-8 h-8" />
                {containerName}
              </h1>
              <p className="text-gray-600 mt-1">Container details and relationships</p>
            </div>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Container Summary</CardTitle>
              <CardDescription>Core container information</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div>
                  <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Status</p>
                  <p className="text-sm text-gray-900">
                    <Badge className="bg-blue-100 text-blue-800">{container.status}</Badge>
                  </p>
                </div>
                <div>
                  <p className="text-xs text-gray-600 uppercase font-semibold mb-2">DSO Status</p>
                  <p className="text-sm text-gray-900">
                    <Badge className={getDSOBadgeColor(container.dso_awareness.status)}>
                      {container.dso_awareness.status}
                    </Badge>
                  </p>
                </div>
                <div>
                  <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Created</p>
                  <p className="text-sm text-gray-900">
                    {new Date(container.created).toLocaleDateString()}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Image</p>
                  <p className="text-sm font-mono text-gray-900 truncate">{container.image}</p>
                </div>
              </div>
            </CardContent>
          </Card>

          {secretData.length > 0 && (
            <InsightsTable<SecretRelationRow>
              title="Managed Secrets"
              description={`${secretData.length} secrets configured for this container`}
              columns={secretColumns}
              data={secretData}
              searchableFields={['name']}
              sortByDefault="name"
            />
          )}

          {eventData.length > 0 && (
            <InsightsTable<EventRow>
              title="Related Events"
              description="Recent events mentioning this container (newest first)"
              columns={eventColumns}
              data={eventData}
              searchableFields={['message']}
            />
          )}

          <Card className="bg-blue-50 border-blue-200">
            <CardContent className="pt-6">
              <p className="text-sm text-blue-900">
                <strong>Read-Only View:</strong> This page shows correlations between containers, secrets, and
                events.
              </p>
            </CardContent>
          </Card>
        </div>
      </ErrorBoundary>
    )
  }

  return (
    <ErrorBoundary>
      <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Runtime Discovery</h1>
          <p className="text-gray-600 mt-1">Discover Docker containers and DSO configuration status</p>
        </div>
        <Button onClick={handleRefresh} variant="outline" size="sm" disabled={isRefreshing || loading}>
          <RefreshCw className={`w-4 h-4 mr-2 ${isRefreshing ? 'animate-spin' : ''}`} />
          {isRefreshing ? 'Refreshing...' : 'Refresh'}
        </Button>
      </div>

      {/* Cache Status */}
      {metrics && (
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <Zap className={`w-5 h-5 ${metrics.is_fresh ? 'text-green-600' : 'text-yellow-600'}`} />
                <div>
                  <p className="text-sm font-semibold text-gray-900">
                    Cache Status: {metrics.is_fresh ? '✨ Fresh' : '⏱️ Stale (Auto-refreshing)'}
                  </p>
                  <p className="text-xs text-gray-600 mt-1">
                    Last refresh: {Math.round(metrics.cache_age_ms / 1000)}s ago • {metrics.cache_hits + metrics.cache_misses} requests • {metrics.refresh_latency_ms}ms latency
                  </p>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Error Alert */}
      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4 flex items-start gap-3">
          <AlertCircle className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
          <div className="text-sm text-red-800">{error}</div>
        </div>
      )}

      {/* Summary Cards */}
      {discovery && (
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-900">{discovery.total_count}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Total Containers</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-green-600">{discovery.managed_count}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Managed by DSO</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-yellow-600">{discovery.partial_count}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Partial Match</p>
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="text-center">
                <p className="text-3xl font-bold text-gray-600">{discovery.unmanaged_count}</p>
                <p className="text-xs text-gray-600 uppercase font-semibold mt-1">Unmanaged</p>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Containers List */}
      {containers.length > 0 ? (
        <>
          {/* Filters and Search Controls */}
          <div className="grid grid-cols-4 gap-6">
            {/* Filters Panel */}
            <div className="col-span-1">
              <DiscoveryFilters
                selectedFilters={selectedFilters}
                onFilterChange={setSelectedFilters}
                containerCount={containerCounts}
              />
            </div>

            {/* Search and Sort */}
            <div className="col-span-3 space-y-4">
              {/* Search Box */}
              <div className="relative">
                <Search className="absolute left-3 top-3 w-4 h-4 text-gray-400" />
                <input
                  type="text"
                  placeholder="Search containers by name, image, or labels..."
                  value={searchQuery}
                  onChange={handleSearchChange}
                  className="w-full pl-9 pr-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>

              {/* Sort Controls */}
              <div className="flex gap-2 flex-wrap">
                {['name', 'status', 'created', 'managed'].map((option) => (
                  <button
                    key={option}
                    onClick={() => setSortBy(option as SortOption)}
                    className={`px-3 py-1 text-sm rounded-lg border transition-colors ${
                      sortBy === option
                        ? 'bg-blue-100 border-blue-300 text-blue-900'
                        : 'bg-white border-gray-200 text-gray-700 hover:bg-gray-50'
                    }`}
                  >
                    Sort by {option === 'created' ? 'Created Date' : option.charAt(0).toUpperCase() + option.slice(1)}
                  </button>
                ))}
              </div>

              {/* Results count */}
              <p className="text-sm text-gray-600">
                Showing {sortedContainers.length} of {containers.length} containers
                {selectedFilters.length > 0 && ` (${selectedFilters.length} filters applied)`}
                {searchQuery && ` (matching "${searchQuery}")`}
              </p>
            </div>
          </div>

          {/* Containers List Card */}
          {sortedContainers.length > 0 ? (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Container className="w-5 h-5" />
                  Discovered Containers ({sortedContainers.length})
                </CardTitle>
                <CardDescription>Docker containers with DSO configuration status</CardDescription>
              </CardHeader>
              <CardContent>
            <div className="space-y-3">
              {sortedContainers.map((container) => (
                <div
                  key={container.id}
                  className="border rounded-lg overflow-hidden hover:shadow-sm transition-shadow"
                >
                  {/* Container Header */}
                  <a href={`/discovery?container=${encodeURIComponent(container.name)}`}>
                  <div
                    className="p-4 bg-gray-50 cursor-pointer hover:bg-gray-100 transition-colors flex items-center justify-between"
                  >
                    <div className="flex items-center gap-4 flex-1 min-w-0">
                      <div className="flex-1">
                        <p className="font-medium text-sm text-gray-900 truncate">{container.name}</p>
                        <p className="text-xs text-gray-600 font-mono truncate">{container.id.substring(0, 12)}</p>
                      </div>

                      <div className="flex items-center gap-2 ml-auto">
                        {/* Status Badge */}
                        <Badge className="bg-blue-100 text-blue-800 whitespace-nowrap">{container.status}</Badge>

                        {/* DSO Status Badge */}
                        <Badge
                          className={`${getDSOBadgeColor(container.dso_awareness?.status ?? '')} flex items-center gap-1 whitespace-nowrap`}
                        >
                          {getDSOIcon(container.dso_awareness?.status ?? '')}
                          {(container.dso_awareness?.status ?? '').charAt(0).toUpperCase() +
                            (container.dso_awareness?.status ?? '').slice(1)}
                        </Badge>
                      </div>
                    </div>

                    {/* Expand Icon */}
                    <div className="ml-2">
                      <ChevronIcon expanded={expandedContainer === container.id} />
                    </div>
                  </div>
                  </a>

                  {/* Container Details (Expandable) */}
                  {expandedContainer === container.id && (
                    <div className="border-t bg-white p-4 space-y-4">
                      {/* Image and Status */}
                      <div className="grid grid-cols-2 gap-4">
                        <div>
                          <p className="text-xs text-gray-600 uppercase font-semibold">Image</p>
                          <p className="text-sm font-mono text-gray-900 mt-1 break-all">{container.image}</p>
                        </div>
                        <div>
                          <p className="text-xs text-gray-600 uppercase font-semibold">Created</p>
                          <p className="text-sm text-gray-900 mt-1">
                            {new Date(container.created).toLocaleString()}
                          </p>
                        </div>
                      </div>

                      {/* Network Info */}
                      <div className="pt-2 border-t">
                        <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Network</p>
                        <div className="grid grid-cols-2 gap-4">
                          {container.network.ip && (
                            <div>
                              <p className="text-xs text-gray-600">IP Address</p>
                              <p className="text-sm font-mono text-gray-900">{container.network.ip}</p>
                            </div>
                          )}
                          {(container.network?.networks?.length ?? 0) > 0 && (
                            <div>
                              <p className="text-xs text-gray-600">Networks</p>
                              <p className="text-sm text-gray-900">{container.network.networks.join(', ')}</p>
                            </div>
                          )}
                        </div>
                      </div>

                      {/* Restart Policy */}
                      {container.restart_policy && (
                        <div className="pt-2 border-t">
                          <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Restart Policy</p>
                          <Badge className="bg-purple-100 text-purple-800">
                            {container.restart_policy.name}
                            {(container.restart_policy.max_retry_count ?? 0) > 0 &&
                              ` (max ${container.restart_policy.max_retry_count})`}
                          </Badge>
                        </div>
                      )}

                      {/* DSO Status Detail */}
                      <div className="pt-2 border-t">
                        <p className="text-xs text-gray-600 uppercase font-semibold mb-2">DSO Configuration</p>
                        <div className="space-y-2">
                          {(container.dso_awareness?.managed_secrets?.length ?? 0) > 0 && (
                            <div>
                              <p className="text-xs text-gray-600">Managed Secrets</p>
                              <div className="flex flex-wrap gap-2 mt-1">
                                {container.dso_awareness.managed_secrets.map((secret) => (
                                  <Badge key={secret} className="bg-green-100 text-green-800">
                                    {secret}
                                  </Badge>
                                ))}
                              </div>
                            </div>
                          )}
                          {(container.dso_awareness?.config_refs?.length ?? 0) > 0 && (
                            <div>
                              <p className="text-xs text-gray-600">Configuration References</p>
                              <div className="space-y-1 mt-1">
                                {container.dso_awareness.config_refs.map((ref, i) => (
                                  <p key={i} className="text-xs text-gray-700 font-mono">
                                    • {ref}
                                  </p>
                                ))}
                              </div>
                            </div>
                          )}
                        </div>
                      </div>

                      {/* Environment Variables Summary */}
                      {container.environment_variable_names && container.environment_variable_names.length > 0 && (
                        <div className="pt-2 border-t">
                          <p className="text-xs text-gray-600 uppercase font-semibold mb-2">
                            Environment Variables ({container.environment_variable_names.length})
                            {container.sensitive_variable_count > 0 && (
                              <span className="ml-2 text-red-600">
                                • {container.sensitive_variable_count} sensitive
                              </span>
                            )}
                          </p>
                          <div className="max-h-48 overflow-y-auto">
                            <div className="space-y-1">
                              {container.environment_variable_names.slice(0, 15).map((name) => (
                                <div key={name} className="text-xs font-mono text-gray-700">
                                  <span className="text-gray-900 font-semibold">{name}</span>
                                </div>
                              ))}
                              {container.environment_variable_names.length > 15 && (
                                <p className="text-xs text-gray-600 italic mt-2">
                                  +{container.environment_variable_names.length - 15} more
                                </p>
                              )}
                            </div>
                          </div>
                        </div>
                      )}

                      {/* Labels */}
                      {Object.keys(container.labels ?? {}).length > 0 && (
                        <div className="pt-2 border-t">
                          <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Labels</p>
                          <div className="space-y-1">
                            {Object.entries(container.labels ?? {})
                              .slice(0, 5)
                              .map(([key, value]) => (
                                <p key={key} className="text-xs text-gray-700">
                                  <span className="font-semibold">{key}</span>: {value}
                                </p>
                              ))}
                            {Object.keys(container.labels ?? {}).length > 5 && (
                              <p className="text-xs text-gray-600 italic">
                                +{Object.keys(container.labels ?? {}).length - 5} more labels
                              </p>
                            )}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
              </CardContent>
            </Card>
          ) : (
            <EmptyState
              type="no-results"
              title="No containers match your filters"
              description={
                searchQuery
                  ? `No containers match "${searchQuery}"`
                  : 'Try adjusting your filters or search query'
              }
              action={
                selectedFilters.length > 0 || searchQuery
                  ? {
                      label: 'Clear filters and search',
                      onClick: () => {
                        setSelectedFilters([])
                        setSearchQuery('')
                      },
                    }
                  : undefined
              }
            />
          )}
        </>
      ) : (
        <EmptyState
          type="empty"
          title="No containers discovered"
          description="Run discovery to find Docker containers in your environment"
          action={{
            label: 'Refresh Discovery',
            onClick: handleRefresh,
          }}
        />
      )}

      {/* Secret Mappings Section */}
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-bold flex items-center gap-2">
          <Lightbulb className="w-5 h-5" />
          Secret Mapping Suggestions
        </h2>
        <Button
          onClick={() => setShowMappings(!showMappings)}
          variant={showMappings ? 'default' : 'outline'}
          size="sm"
        >
          {showMappings ? 'Hide' : 'Show'} Suggestions
        </Button>
      </div>

      {showMappings && (
        <>
          {suggestionsError && (
            <div className="bg-red-50 border border-red-200 rounded-lg p-4 flex items-start gap-3">
              <AlertCircle className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
              <div className="text-sm text-red-800">{suggestionsError}</div>
            </div>
          )}

          {suggestionsLoading ? (
            <Card>
              <CardContent className="pt-6">
                <div className="flex items-center justify-center">
                  <RefreshCw className="w-5 h-5 animate-spin mr-2" />
                  <p>Analyzing environment variables...</p>
                </div>
              </CardContent>
            </Card>
          ) : suggestions.length > 0 ? (
            <Card>
              <CardHeader>
                <CardTitle>Environment Variables Needing Secrets</CardTitle>
                <CardDescription>
                  Variables that look like secrets and might need DSO configuration
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {suggestions.map((suggestion, i) => (
                    <div key={i} className="border rounded-lg p-3 bg-gray-50">
                      <div className="flex items-start justify-between mb-2">
                        <div>
                          <p className="font-medium text-sm text-gray-900">{suggestion.container_name}</p>
                          <p className="text-xs text-gray-600 font-mono">{suggestion.env_var_name}</p>
                        </div>
                        <Badge
                          className={
                            suggestion.confidence === 'high'
                              ? 'bg-red-100 text-red-800'
                              : suggestion.confidence === 'medium'
                                ? 'bg-yellow-100 text-yellow-800'
                                : 'bg-blue-100 text-blue-800'
                          }
                        >
                          {suggestion.confidence.charAt(0).toUpperCase() + suggestion.confidence.slice(1)} Confidence
                        </Badge>
                      </div>
                      <p className="text-xs text-gray-600 mb-2">{suggestion.reason}</p>
                      <div className="flex items-center gap-2 text-xs">
                        <span className="text-gray-700">
                          <span className="font-semibold">Suggested:</span> {suggestion.suggested_secret_name}
                        </span>
                        {suggestion.configured_secret && (
                          <Badge className="bg-green-100 text-green-800">
                            Already configured: {suggestion.configured_secret.secret_name}
                          </Badge>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="pt-6">
                <p className="text-center text-gray-600">No high-confidence secret suggestions found</p>
              </CardContent>
            </Card>
          )}
        </>
      )}

      {/* Info Banner */}
      <div className="bg-blue-50 border border-blue-200 rounded-lg p-4">
        <p className="text-sm text-blue-900">
          <strong>Read-Only View:</strong> This page shows runtime container discovery and suggests environment
          variables that might need secret management. To manage DSO configuration, use the Configuration page or the
          CLI.
        </p>
      </div>
    </div>
    </ErrorBoundary>
  )
}

function ChevronIcon({ expanded }: { expanded: boolean }) {
  return (
    <svg
      className={`w-4 h-4 text-gray-600 transition-transform ${expanded ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      viewBox="0 0 24 24"
    >
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
    </svg>
  )
}

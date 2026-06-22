'use client'

import { useState, useMemo, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'
import { PageHeader, Card } from '@/components/ui-modern'
import { Search, X } from 'lucide-react'
import * as discoveryApi from '@/lib/api/discovery'
import { ContainerMetadata } from '@/lib/api/types'
import { mockContainers, mockMappings, mockMetrics } from '@/lib/data/discovery-mock'
import { exportContainersToCSV, exportContainersToJSON, downloadExport } from '@/lib/utils/discovery-export'

// Import components
import { CoverageMetrics } from '@/components/discovery/CoverageMetrics'
import { ContainerTable } from '@/components/discovery/ContainerTable'
import { ContainerDetailsDrawer } from '@/components/discovery/ContainerDetailsDrawer'
import { DiscoveryFilters } from '@/components/discovery/DiscoveryFilters'
import { SecretMappingsTable } from '@/components/discovery/SecretMappingsTable'
import { DiscoveryMetricsSection } from '@/components/discovery/DiscoveryMetricsSection'
import { RefreshButton } from '@/components/discovery/RefreshButton'
import { EmptyState } from '@/components/discovery/EmptyState'
import { QuickStats } from '@/components/discovery/QuickStats'

type FilterType = 'managed' | 'partial' | 'unmanaged' | 'running' | 'stopped'

function DiscoveryContent() {
  const queryClient = useQueryClient()

  // State
  const [searchTerm, setSearchTerm] = useState('')
  const [filters, setFilters] = useState<{ classification: FilterType[]; status: FilterType[] }>({
    classification: [],
    status: [],
  })
  const [selectedContainer, setSelectedContainer] = useState<ContainerMetadata | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [demoMode, setDemoMode] = useState(false)
  const [selectedContainerIds, setSelectedContainerIds] = useState<Set<string>>(new Set())

  // Queries
  const { data: discoveryData, isLoading: containersLoading, error: containersError } = useQuery({
    queryKey: ['discovery', 'containers', demoMode],
    queryFn: async () => {
      if (demoMode) {
        return {
          containers: mockContainers,
          total_count: mockContainers.length,
          managed_count: mockContainers.filter(c => c.dso_awareness?.status === 'managed').length,
          partial_count: mockContainers.filter(c => c.dso_awareness?.status === 'partial').length,
          unmanaged_count: mockContainers.filter(c => c.dso_awareness?.status === 'unmanaged').length,
          timestamp: new Date().toISOString(),
        }
      }
      return discoveryApi.getContainers()
    },
    refetchInterval: demoMode ? false : 30000,
    staleTime: demoMode ? Infinity : 25000,
    retry: demoMode ? false : 2,
    refetchOnWindowFocus: false,
  })

  const { data: mappingsData, isLoading: mappingsLoading, error: mappingsError } = useQuery({
    queryKey: ['discovery', 'mappings', demoMode],
    queryFn: async () => {
      if (demoMode) {
        return {
          suggestions: mockMappings,
          count: mockMappings.length,
          timestamp: new Date().toISOString(),
        }
      }
      return discoveryApi.getMappings()
    },
    refetchInterval: demoMode ? false : 30000,
    staleTime: demoMode ? Infinity : 25000,
    retry: demoMode ? false : 2,
    refetchOnWindowFocus: false,
  })

  const { data: metricsData, isLoading: metricsLoading, error: metricsError } = useQuery({
    queryKey: ['discovery', 'metrics', demoMode],
    queryFn: async () => {
      if (demoMode) {
        return mockMetrics
      }
      return discoveryApi.getDiscoveryMetrics()
    },
    refetchInterval: demoMode ? false : 30000,
    staleTime: demoMode ? Infinity : 25000,
    retry: demoMode ? false : 2,
    refetchOnWindowFocus: false,
  })

  // Normalization and filtering
  const normalizedSearch = searchTerm.trim().toLowerCase()

  const filteredContainers = useMemo(() => {
    const containers = discoveryData?.containers || []

    return containers
      .filter(c => {
        if (filters.classification.length === 0) return true
        const classification = c.dso_awareness?.status ?? 'unmanaged'
        return filters.classification.includes(classification as FilterType)
      })
      .filter(c => {
        if (filters.status.length === 0) return true
        return filters.status.includes(c.status as FilterType)
      })
      .filter(c => {
        if (normalizedSearch === '') return true
        return (
          c.container_name.toLowerCase().includes(normalizedSearch) ||
          c.image.toLowerCase().includes(normalizedSearch) ||
          c.status.toLowerCase().includes(normalizedSearch)
        )
      })
  }, [discoveryData?.containers, filters, normalizedSearch])

  const handleToggleSelect = (containerId: string) => {
    const newSelected = new Set(selectedContainerIds)
    if (newSelected.has(containerId)) {
      newSelected.delete(containerId)
    } else {
      newSelected.add(containerId)
    }
    setSelectedContainerIds(newSelected)
  }

  const selectedContainers = filteredContainers.filter(c =>
    selectedContainerIds.has(c.container_id)
  )

  // Manual refresh
  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true)
    try {
      await discoveryApi.refreshDiscovery()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['discovery', 'containers'] }),
        queryClient.invalidateQueries({ queryKey: ['discovery', 'mappings'] }),
        queryClient.invalidateQueries({ queryKey: ['discovery', 'metrics'] }),
      ])
    } finally {
      setIsRefreshing(false)
    }
  }, [queryClient])

  // Container counts for filter display
  const containerCounts = {
    managed: discoveryData?.managed_count ?? 0,
    partial: discoveryData?.partial_count ?? 0,
    unmanaged: discoveryData?.unmanaged_count ?? 0,
  }

  return (
    <div className="p-6 space-y-5">
      {/* Header */}
      <PageHeader
        title="Discovery"
        description="Container discovery and secret mapping suggestions"
        actions={
          <div className="flex items-center gap-2">
            {demoMode && (
              <span className="text-xs bg-amber-500/20 text-amber-300 px-2 py-1 rounded border border-amber-500/30">
                Demo Mode
              </span>
            )}
            {selectedContainerIds.size > 0 && (
              <button
                onClick={() => {
                  const csv = exportContainersToCSV(selectedContainers)
                  downloadExport(csv, `discovery-selected-${selectedContainerIds.size}.csv`, 'text/csv')
                }}
                className="text-xs px-3 py-1.5 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white transition-colors"
              >
                Export {selectedContainerIds.size}
              </button>
            )}
            <button
              onClick={() => {
                const csv = exportContainersToCSV(filteredContainers)
                downloadExport(csv, 'discovery-containers.csv', 'text/csv')
              }}
              className="text-xs px-3 py-1.5 rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 transition-colors"
            >
              CSV
            </button>
            <button
              onClick={() => {
                const json = exportContainersToJSON(filteredContainers)
                downloadExport(json, 'discovery-containers.json', 'application/json')
              }}
              className="text-xs px-3 py-1.5 rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 transition-colors"
            >
              JSON
            </button>
            <button
              onClick={() => setDemoMode(!demoMode)}
              title="Toggle mock data mode"
              className="text-xs px-3 py-1.5 rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 transition-colors"
            >
              {demoMode ? '🎯 Mock' : '🔴 Live'}
            </button>
            <RefreshButton isRefreshing={isRefreshing} onRefresh={handleRefresh} />
          </div>
        }
      />

      {/* Search & Filters */}
      <div className="space-y-3">
        {/* Search Bar */}
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-500" />
          <input
            className="w-full pl-10 pr-10 py-2.5 text-sm rounded-lg border border-white/[0.09] bg-white/[0.03] text-slate-200 placeholder:text-slate-500 focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20 transition-colors"
            placeholder="Search by container name, image, or status…"
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
            aria-label="Search containers"
          />
          {searchTerm && (
            <button
              className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-400 transition-colors"
              onClick={() => setSearchTerm('')}
              aria-label="Clear search"
            >
              <X className="w-4 h-4" />
            </button>
          )}
        </div>

        {/* Filters Panel */}
        <Card className="p-4 border-white/[0.06]">
          <DiscoveryFilters
            filters={filters}
            onFilterChange={setFilters}
            containerCount={containerCounts}
            totalCount={discoveryData?.containers?.length || 0}
          />
        </Card>

        {/* Filter Results Summary */}
        {(filters.classification.length > 0 || filters.status.length > 0 || searchTerm) && (
          <div className="flex items-center justify-between px-3 py-2 bg-indigo-500/10 border border-indigo-500/20 rounded-lg">
            <span className="text-[13px] font-normal text-[#F3F4F6]">
              <span className="font-semibold text-indigo-400">{filteredContainers.length}</span>
              {' '}of{' '}
              <span className="font-semibold">{discoveryData?.containers?.length || 0}</span>
              {' '}containers
            </span>
            <button
              onClick={() => {
                setFilters({ classification: [], status: [] })
                setSearchTerm('')
              }}
              className="text-[11px] font-normal text-indigo-400 hover:text-indigo-300 transition-colors"
            >
              Reset all
            </button>
          </div>
        )}
      </div>

      {/* Coverage Metrics */}
      <CoverageMetrics containers={discoveryData?.containers} isLoading={containersLoading} />

      {/* Quick Stats */}
      <QuickStats containers={discoveryData?.containers} lastRefreshTime={new Date()} />

      {/* Container Error */}
      {containersError && (
        <Card className="p-4 border-red-500/30 bg-red-500/10">
          <div className="flex items-center justify-between">
            <p className="text-[13px] font-normal text-red-400">Unable to load discovered containers</p>
            <button
              onClick={() =>
                queryClient.invalidateQueries({ queryKey: ['discovery', 'containers'] })
              }
              className="text-[13px] font-normal text-red-400 hover:text-red-300 underline"
            >
              Retry
            </button>
          </div>
        </Card>
      )}

      {/* Container Table */}
      {!containersError && (
        <ContainerTable
          containers={filteredContainers}
          isLoading={containersLoading}
          onSelectContainer={setSelectedContainer}
          selectedIds={selectedContainerIds}
          onToggleSelect={handleToggleSelect}
        />
      )}

      {/* Secret Mappings */}
      <div>
        <h2 className="text-[18px] font-semibold text-[#F3F4F6] mb-3">Secret Mapping Suggestions</h2>
        {mappingsError ? (
          <Card className="p-4 border-amber-500/30 bg-amber-500/10">
            <p className="text-[13px] font-normal text-amber-400">Unable to load secret suggestions</p>
          </Card>
        ) : (
          <SecretMappingsTable
            mappings={mappingsData?.suggestions}
            searchTerm={searchTerm}
            isLoading={mappingsLoading}
          />
        )}
      </div>

      {/* Discovery Metrics */}
      <div>
        <h2 className="text-[18px] font-semibold text-[#F3F4F6] mb-3">Cache Health</h2>
        <DiscoveryMetricsSection metrics={metricsData} isLoading={metricsLoading} />
      </div>

      {/* Container Details Drawer */}
      <ContainerDetailsDrawer
        container={selectedContainer}
        onClose={() => setSelectedContainer(null)}
      />
    </div>
  )
}

export default function DiscoveryPage() {
  return (
    <ProtectedRoute>
      <ErrorBoundary>
        <DiscoveryContent />
      </ErrorBoundary>
    </ProtectedRoute>
  )
}

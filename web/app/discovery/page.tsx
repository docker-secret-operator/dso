'use client'

import { useState, useMemo, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
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
          total: mockContainers.length,
          managed: mockContainers.filter(c => c.dso_awareness?.status === 'managed').length,
          partial: mockContainers.filter(c => c.dso_awareness?.status === 'partial').length,
          unmanaged: mockContainers.filter(c => c.dso_awareness?.status === 'unmanaged').length,
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
    managed: discoveryData?.managed ?? 0,
    partial: discoveryData?.partial ?? 0,
    unmanaged: discoveryData?.unmanaged ?? 0,
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
      <div className="flex flex-col md:flex-row gap-3">
        <div className="relative flex-1 max-w-lg">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-600" />
          <input
            className="w-full pl-9 pr-4 py-2 text-sm rounded-lg border border-white/[0.09] bg-[#1a1d24] text-slate-300 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/50 focus:ring-1 focus:ring-indigo-500/20"
            placeholder="Search by container name, image, or status…"
            value={searchTerm}
            onChange={e => setSearchTerm(e.target.value)}
          />
          {searchTerm && (
            <button
              className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-600 hover:text-slate-400"
              onClick={() => setSearchTerm('')}
            >
              <X className="w-3.5 h-3.5" />
            </button>
          )}
        </div>

        <Card className="p-3">
          <DiscoveryFilters
            filters={filters}
            onFilterChange={setFilters}
            containerCount={containerCounts}
          />
        </Card>
      </div>

      {/* Coverage Metrics */}
      <CoverageMetrics containers={discoveryData?.containers} isLoading={containersLoading} />

      {/* Quick Stats */}
      <QuickStats containers={discoveryData?.containers} lastRefreshTime={new Date()} />

      {/* Container Error */}
      {containersError && (
        <Card className="p-4 border-red-500/30 bg-red-500/10">
          <div className="flex items-center justify-between">
            <p className="text-sm text-red-400">Unable to load discovered containers</p>
            <button
              onClick={() =>
                queryClient.invalidateQueries({ queryKey: ['discovery', 'containers'] })
              }
              className="text-sm text-red-400 hover:text-red-300 underline"
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
        <h2 className="text-lg font-semibold text-slate-200 mb-3">Secret Mapping Suggestions</h2>
        {mappingsError ? (
          <Card className="p-4 border-amber-500/30 bg-amber-500/10">
            <p className="text-sm text-amber-400">Unable to load secret suggestions</p>
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
        <h2 className="text-lg font-semibold text-slate-200 mb-3">Cache Health</h2>
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
      <DiscoveryContent />
    </ProtectedRoute>
  )
}

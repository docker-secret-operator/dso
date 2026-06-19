'use client'

import { useState, useMemo, useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { PageHeader, Card } from '@/components/ui-modern'
import { Search, X } from 'lucide-react'
import * as discoveryApi from '@/lib/api/discovery'
import { ContainerMetadata } from '@/lib/api/types'

// Import components
import { CoverageMetrics } from '@/components/discovery/CoverageMetrics'
import { ContainerTable } from '@/components/discovery/ContainerTable'
import { ContainerDetailsDrawer } from '@/components/discovery/ContainerDetailsDrawer'
import { DiscoveryFilters } from '@/components/discovery/DiscoveryFilters'
import { SecretMappingsTable } from '@/components/discovery/SecretMappingsTable'
import { DiscoveryMetricsSection } from '@/components/discovery/DiscoveryMetricsSection'
import { RefreshButton } from '@/components/discovery/RefreshButton'
import { EmptyState } from '@/components/discovery/EmptyState'

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

  // Queries
  const { data: discoveryData, isLoading: containersLoading, error: containersError } = useQuery({
    queryKey: ['discovery', 'containers'],
    queryFn: discoveryApi.getContainers,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
    refetchOnWindowFocus: false,
  })

  const { data: mappingsData, isLoading: mappingsLoading, error: mappingsError } = useQuery({
    queryKey: ['discovery', 'mappings'],
    queryFn: discoveryApi.getMappings,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
    refetchOnWindowFocus: false,
  })

  const { data: metricsData, isLoading: metricsLoading, error: metricsError } = useQuery({
    queryKey: ['discovery', 'metrics'],
    queryFn: discoveryApi.getDiscoveryMetrics,
    refetchInterval: 30000,
    staleTime: 25000,
    retry: 2,
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
        actions={<RefreshButton isRefreshing={isRefreshing} onRefresh={handleRefresh} />}
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

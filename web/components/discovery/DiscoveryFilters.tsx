'use client'

import React from 'react'
import { Badge, Button } from '@/components/ui-modern'
import { X, Check, Filter } from 'lucide-react'

export type FilterType = 'managed' | 'partial' | 'unmanaged' | 'running' | 'stopped'

export interface DiscoveryFiltersProps {
  filters: { classification: FilterType[]; status: FilterType[] }
  onFilterChange: (filters: { classification: FilterType[]; status: FilterType[] }) => void
  containerCount?: {
    managed?: number
    partial?: number
    unmanaged?: number
    running?: number
    stopped?: number
  }
  totalCount?: number
}

export function DiscoveryFilters({
  filters,
  onFilterChange,
  containerCount = {},
  totalCount = 0,
}: DiscoveryFiltersProps) {
  const toggleFilter = (filter: FilterType, type: 'classification' | 'status') => {
    const current = filters[type]
    const updated = current.includes(filter)
      ? current.filter(f => f !== filter)
      : [...current, filter]
    onFilterChange({
      ...filters,
      [type]: updated,
    })
  }

  const clearAllFilters = () => {
    onFilterChange({ classification: [], status: [] })
  }

  const hasActiveFilters = filters.classification.length > 0 || filters.status.length > 0

  // Get color based on filter type
  const getFilterColor = (filter: FilterType) => {
    switch (filter) {
      case 'managed':
        return 'emerald'
      case 'partial':
        return 'amber'
      case 'unmanaged':
        return 'slate'
      case 'running':
        return 'emerald'
      case 'stopped':
        return 'slate'
      default:
        return 'slate'
    }
  }

  // Get count for filter option
  const getFilterCount = (filter: FilterType): number => {
    return (containerCount[filter as keyof typeof containerCount] as number) || 0
  }

  const classificationFilters = ['managed', 'partial', 'unmanaged'] as const
  const statusFilters = ['running', 'stopped'] as const

  return (
    <div className="space-y-4">
      {/* Header with Clear All */}
      <div className="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <div className="flex items-center gap-2">
          <Filter className="w-4 h-4 text-slate-400" />
          <h3 className="text-sm font-semibold text-slate-200">Filters</h3>
          {hasActiveFilters && (
            <span className="text-xs px-2 py-1 rounded-full bg-blue-500/20 text-blue-400">
              {filters.classification.length + filters.status.length} active
            </span>
          )}
        </div>
        {hasActiveFilters && (
          <Button
            onClick={clearAllFilters}
            variant="ghost"
            size="sm"
            className="text-xs text-slate-400 hover:text-slate-200 hover:bg-white/[0.05]"
          >
            <X className="w-3 h-3 mr-1" />
            Clear
          </Button>
        )}
      </div>

      {/* Classification Filters */}
      <div className="space-y-2">
        <p className="text-xs text-slate-400 font-medium uppercase tracking-wider">Classification</p>
        <div className="space-y-1.5">
          {classificationFilters.map(type => {
            const isActive = filters.classification.includes(type)
            const color = getFilterColor(type)
            const count = getFilterCount(type as FilterType)

            return (
              <button
                key={type}
                onClick={() => toggleFilter(type as FilterType, 'classification')}
                className={`w-full flex items-center gap-2 px-3 py-2.5 rounded-lg border transition-all ${
                  isActive
                    ? `border-${color}-500/50 bg-${color}-500/10 text-${color}-300`
                    : 'border-white/[0.09] bg-white/[0.02] text-slate-400 hover:bg-white/[0.06] hover:border-white/[0.15]'
                }`}
              >
                {/* Checkbox-style indicator */}
                <div className={`w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 transition-colors ${
                  isActive
                    ? `border-${color}-500/70 bg-${color}-500/30`
                    : 'border-slate-500/30 bg-transparent'
                }`}>
                  {isActive && <Check className={`w-3 h-3 text-${color}-300`} />}
                </div>

                {/* Label and count */}
                <div className="flex-1 text-left">
                  <span className="text-sm font-medium capitalize">{type}</span>
                </div>

                {/* Count badge */}
                {count > 0 && (
                  <span className="text-xs px-2 py-1 rounded-full bg-white/[0.08] text-slate-400 font-medium">
                    {count}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      </div>

      {/* Status Filters */}
      <div className="space-y-2">
        <p className="text-xs text-slate-400 font-medium uppercase tracking-wider">Status</p>
        <div className="space-y-1.5">
          {statusFilters.map(status => {
            const isActive = filters.status.includes(status)
            const color = getFilterColor(status)
            const count = getFilterCount(status as FilterType)

            return (
              <button
                key={status}
                onClick={() => toggleFilter(status as FilterType, 'status')}
                className={`w-full flex items-center gap-2 px-3 py-2.5 rounded-lg border transition-all ${
                  isActive
                    ? `border-${color}-500/50 bg-${color}-500/10 text-${color}-300`
                    : 'border-white/[0.09] bg-white/[0.02] text-slate-400 hover:bg-white/[0.06] hover:border-white/[0.15]'
                }`}
              >
                {/* Checkbox-style indicator */}
                <div className={`w-4 h-4 rounded border flex items-center justify-center flex-shrink-0 transition-colors ${
                  isActive
                    ? `border-${color}-500/70 bg-${color}-500/30`
                    : 'border-slate-500/30 bg-transparent'
                }`}>
                  {isActive && <Check className={`w-3 h-3 text-${color}-300`} />}
                </div>

                {/* Label and count */}
                <div className="flex-1 text-left">
                  <span className="text-sm font-medium capitalize">{status}</span>
                </div>

                {/* Count badge */}
                {count > 0 && (
                  <span className="text-xs px-2 py-1 rounded-full bg-white/[0.08] text-slate-400 font-medium">
                    {count}
                  </span>
                )}
              </button>
            )
          })}
        </div>
      </div>

      {/* Active Filters Summary */}
      {hasActiveFilters && (
        <div className="pt-3 border-t border-white/[0.06] space-y-2">
          <p className="text-xs text-slate-500">Active filters:</p>
          <div className="flex flex-wrap gap-2">
            {[...filters.classification, ...filters.status].map(filter => {
              const color = getFilterColor(filter)
              return (
                <Badge
                  key={filter}
                  className={`gap-1.5 text-xs border border-${color}-500/40 bg-${color}-500/15 text-${color}-300`}
                >
                  <span className="capitalize">{filter}</span>
                  <button
                    onClick={() => {
                      if (filters.classification.includes(filter as FilterType)) {
                        toggleFilter(filter as FilterType, 'classification')
                      } else {
                        toggleFilter(filter as FilterType, 'status')
                      }
                    }}
                    className="ml-0.5 hover:opacity-70 transition-opacity"
                    aria-label={`Remove ${filter} filter`}
                  >
                    <X className="w-3 h-3" />
                  </button>
                </Badge>
              )
            })}
          </div>
        </div>
      )}

      {/* Results Summary */}
      {totalCount > 0 && (
        <div className="pt-3 border-t border-white/[0.06] bg-slate-500/5 rounded-lg p-3">
          <p className="text-xs text-slate-400">
            {hasActiveFilters
              ? `Showing filtered containers`
              : `Total: ${totalCount} container${totalCount !== 1 ? 's' : ''}`
            }
          </p>
        </div>
      )}
    </div>
  )
}

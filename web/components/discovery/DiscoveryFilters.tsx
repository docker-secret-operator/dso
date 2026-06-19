'use client'

import React from 'react'
import { Badge, Button } from '@/components/ui-modern'
import { X } from 'lucide-react'

export type FilterType = 'managed' | 'partial' | 'unmanaged' | 'running' | 'stopped'

export interface DiscoveryFiltersProps {
  filters: { classification: FilterType[]; status: FilterType[] }
  onFilterChange: (filters: { classification: FilterType[]; status: FilterType[] }) => void
  containerCount: {
    managed: number
    partial: number
    unmanaged: number
  }
}

export function DiscoveryFilters({
  filters,
  onFilterChange,
  containerCount,
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

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-slate-300">Filters</h3>
        {hasActiveFilters && (
          <Button
            onClick={clearAllFilters}
            variant="ghost"
            size="sm"
            className="text-xs text-slate-500 hover:text-slate-300"
          >
            Clear all
          </Button>
        )}
      </div>

      {/* Classification Filters */}
      <div>
        <p className="text-xs text-slate-500 font-medium mb-2">Classification</p>
        <div className="space-y-2">
          {['managed', 'partial', 'unmanaged'].map(type => (
            <button
              key={type}
              onClick={() => toggleFilter(type as FilterType, 'classification')}
              className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
                filters.classification.includes(type as FilterType)
                  ? type === 'managed'
                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                    : type === 'partial'
                      ? 'border-amber-500/40 bg-amber-500/10 text-amber-400'
                      : 'border-slate-500/40 bg-slate-500/10 text-slate-400'
                  : 'border-white/[0.09] bg-transparent text-slate-400 hover:bg-white/[0.05]'
              }`}
            >
              <span className="text-sm font-medium capitalize">{type}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Status Filters */}
      <div>
        <p className="text-xs text-slate-500 font-medium mb-2">Status</p>
        <div className="space-y-2">
          {['running', 'stopped'].map(status => (
            <button
              key={status}
              onClick={() => toggleFilter(status as FilterType, 'status')}
              className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
                filters.status.includes(status as FilterType)
                  ? status === 'running'
                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
                    : 'border-slate-500/40 bg-slate-500/10 text-slate-400'
                  : 'border-white/[0.09] bg-transparent text-slate-400 hover:bg-white/[0.05]'
              }`}
            >
              <span className="text-sm font-medium capitalize">{status}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Active Chips */}
      {hasActiveFilters && (
        <div className="pt-2 border-t border-white/[0.06]">
          <div className="flex flex-wrap gap-2">
            {[...filters.classification, ...filters.status].map(filter => (
              <Badge
                key={filter}
                variant="default"
                className="gap-1 text-xs"
              >
                {filter.charAt(0).toUpperCase() + filter.slice(1)}
                <button
                  onClick={() => {
                    if (filters.classification.includes(filter as FilterType)) {
                      toggleFilter(filter as FilterType, 'classification')
                    } else {
                      toggleFilter(filter as FilterType, 'status')
                    }
                  }}
                  className="ml-1 hover:opacity-75"
                >
                  <X className="w-3 h-3" />
                </button>
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

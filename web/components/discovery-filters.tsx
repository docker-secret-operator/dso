'use client'

import React, { useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { X } from 'lucide-react'

export type FilterType = 'managed' | 'partial' | 'unmanaged'

export interface DiscoveryFiltersProps {
  selectedFilters: FilterType[]
  onFilterChange: (filters: FilterType[]) => void
  containerCount: {
    managed: number
    partial: number
    unmanaged: number
  }
}

export function DiscoveryFilters({
  selectedFilters,
  onFilterChange,
  containerCount,
}: DiscoveryFiltersProps) {
  const toggleFilter = (filter: FilterType) => {
    if (selectedFilters.includes(filter)) {
      onFilterChange(selectedFilters.filter((f) => f !== filter))
    } else {
      onFilterChange([...selectedFilters, filter])
    }
  }

  const clearAllFilters = () => {
    onFilterChange([])
  }

  const hasActiveFilters = selectedFilters.length > 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-900">Filter by Status</h3>
        {hasActiveFilters && (
          <Button
            onClick={clearAllFilters}
            variant="ghost"
            size="sm"
            className="text-xs text-gray-600 hover:text-gray-900"
          >
            Clear all
          </Button>
        )}
      </div>

      <div className="space-y-2">
        <button
          onClick={() => toggleFilter('managed')}
          className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
            selectedFilters.includes('managed')
              ? 'border-green-300 bg-green-50'
              : 'border-gray-200 bg-white hover:bg-gray-50'
          }`}
        >
          <div className="flex items-center gap-2">
            <div
              className={`w-2 h-2 rounded-full ${
                selectedFilters.includes('managed') ? 'bg-green-600' : 'bg-green-400'
              }`}
            />
            <span className="text-sm font-medium text-gray-900">Managed</span>
          </div>
          <span className="text-xs font-semibold text-gray-600">{containerCount.managed}</span>
        </button>

        <button
          onClick={() => toggleFilter('partial')}
          className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
            selectedFilters.includes('partial')
              ? 'border-yellow-300 bg-yellow-50'
              : 'border-gray-200 bg-white hover:bg-gray-50'
          }`}
        >
          <div className="flex items-center gap-2">
            <div
              className={`w-2 h-2 rounded-full ${
                selectedFilters.includes('partial') ? 'bg-yellow-600' : 'bg-yellow-400'
              }`}
            />
            <span className="text-sm font-medium text-gray-900">Partial</span>
          </div>
          <span className="text-xs font-semibold text-gray-600">{containerCount.partial}</span>
        </button>

        <button
          onClick={() => toggleFilter('unmanaged')}
          className={`w-full flex items-center justify-between px-3 py-2 rounded-lg border transition-colors ${
            selectedFilters.includes('unmanaged')
              ? 'border-gray-300 bg-gray-100'
              : 'border-gray-200 bg-white hover:bg-gray-50'
          }`}
        >
          <div className="flex items-center gap-2">
            <div
              className={`w-2 h-2 rounded-full ${
                selectedFilters.includes('unmanaged') ? 'bg-gray-600' : 'bg-gray-400'
              }`}
            />
            <span className="text-sm font-medium text-gray-900">Unmanaged</span>
          </div>
          <span className="text-xs font-semibold text-gray-600">{containerCount.unmanaged}</span>
        </button>
      </div>

      {hasActiveFilters && (
        <div className="pt-2 border-t">
          <div className="flex flex-wrap gap-2">
            {selectedFilters.map((filter) => (
              <Badge key={filter} variant="secondary" className="gap-1">
                {filter.charAt(0).toUpperCase() + filter.slice(1)}
                <button
                  onClick={() => toggleFilter(filter)}
                  className="ml-1 hover:bg-white rounded-full"
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

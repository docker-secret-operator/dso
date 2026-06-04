'use client'

import { X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

export type TimelineFilterType = 'info' | 'warning' | 'error'

interface TimelineFilterProps {
  selectedSeverities: TimelineFilterType[]
  onSeverityChange: (severities: TimelineFilterType[]) => void
  searchQuery: string
  onSearchChange: (query: string) => void
  onClearAll?: () => void
}

const severityOptions = [
  { value: 'info' as const, label: 'Info', color: 'bg-blue-100 text-blue-800' },
  { value: 'warning' as const, label: 'Warning', color: 'bg-yellow-100 text-yellow-800' },
  { value: 'error' as const, label: 'Error', color: 'bg-red-100 text-red-800' },
]

export function TimelineFilter({
  selectedSeverities,
  onSeverityChange,
  searchQuery,
  onSearchChange,
  onClearAll,
}: TimelineFilterProps) {
  const toggleSeverity = (severity: TimelineFilterType) => {
    if (selectedSeverities.includes(severity)) {
      onSeverityChange(selectedSeverities.filter((s) => s !== severity))
    } else {
      onSeverityChange([...selectedSeverities, severity])
    }
  }

  const hasActiveFilters = selectedSeverities.length > 0 || searchQuery.trim().length > 0

  return (
    <div className="space-y-4">
      {/* Search */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-2">Search</label>
        <input
          type="text"
          placeholder="Search by message, container, or secret..."
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
          className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>

      {/* Severity Filters */}
      <div>
        <label className="block text-sm font-medium text-gray-700 mb-2">Severity</label>
        <div className="space-y-2">
          {severityOptions.map((option) => (
            <button
              key={option.value}
              onClick={() => toggleSeverity(option.value)}
              className={`w-full px-3 py-2 rounded-lg border text-sm font-medium transition-colors ${
                selectedSeverities.includes(option.value)
                  ? `${option.color} border-current`
                  : 'border-gray-200 text-gray-700 hover:bg-gray-50'
              }`}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>

      {/* Active Filters */}
      {hasActiveFilters && (
        <div className="pt-4 border-t space-y-3">
          {selectedSeverities.length > 0 && (
            <div className="flex flex-wrap gap-2">
              {selectedSeverities.map((severity) => (
                <Badge
                  key={severity}
                  variant="secondary"
                  className="gap-1 cursor-pointer"
                  onClick={() => toggleSeverity(severity)}
                >
                  {severity.charAt(0).toUpperCase() + severity.slice(1)}
                  <X className="w-3 h-3" />
                </Badge>
              ))}
            </div>
          )}

          {searchQuery && (
            <div className="flex items-center gap-2 text-xs">
              <span className="text-gray-600">Searching: "{searchQuery}"</span>
            </div>
          )}

          <Button
            onClick={onClearAll}
            variant="ghost"
            size="sm"
            className="w-full text-xs text-gray-600 hover:text-gray-900"
          >
            Clear all filters
          </Button>
        </div>
      )}
    </div>
  )
}

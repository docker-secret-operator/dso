'use client'

import { AuditFilters as AuditFiltersType } from '@/lib/api/types'
import { Card, Button } from '@/components/ui-modern'
import { Filter, X } from 'lucide-react'

const FILTER_FIELDS = [
  { key: 'actor', label: 'Actor name' },
  { key: 'actor_id', label: 'Actor ID' },
  { key: 'action', label: 'Action' },
  { key: 'resource', label: 'Resource type' },
  { key: 'correlation_id', label: 'Correlation ID' },
  { key: 'execution_id', label: 'Execution ID' },
  { key: 'start_time', label: 'From (ISO date)' },
  { key: 'end_time', label: 'To (ISO date)' },
]

interface AuditFiltersProps {
  filters: AuditFiltersType
  showFilters: boolean
  onToggleFilters: () => void
  onFilterChange: (key: keyof AuditFiltersType, value: string) => void
  onClearFilters: () => void
}

export function AuditFilters({
  filters,
  showFilters,
  onToggleFilters,
  onFilterChange,
  onClearFilters,
}: AuditFiltersProps) {
  const hasActive = Object.keys(filters).some(k => k !== 'limit' && k !== 'offset' && (filters as any)[k])

  return (
    <>
      {/* Filter toggle button */}
      <button
        onClick={onToggleFilters}
        className={`inline-flex items-center gap-1.5 px-3 py-2 text-sm rounded-lg border transition-colors ${
          hasActive
            ? 'border-indigo-500/40 text-indigo-400 bg-indigo-500/10'
            : 'border-white/10 text-slate-500 hover:text-slate-300 hover:bg-white/5'
        }`}
      >
        <Filter className="w-3.5 h-3.5" />
        Filters
        {hasActive && (
          <span className="w-4 h-4 rounded-full bg-indigo-600 text-white text-[10px] font-bold flex items-center justify-center">
            {Object.keys(filters).filter(k => k !== 'limit' && k !== 'offset' && (filters as any)[k]).length}
          </span>
        )}
      </button>

      {/* Filter panel — animated */}
      {showFilters && (
        <Card className="p-4 animate-fade-in">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
            {FILTER_FIELDS.map(({ key, label }) => (
              <div key={key} className="space-y-1">
                <label className="text-[11px] text-slate-500 font-medium">{label}</label>
                <input
                  className="w-full rounded-md border border-white/[0.09] bg-[#1a1f2e] px-2.5 py-1.5 text-sm text-slate-300 placeholder:text-slate-700 focus:outline-none focus:border-indigo-500/50"
                  value={(filters as any)[key] ?? ''}
                  onChange={e => onFilterChange(key as keyof AuditFiltersType, e.target.value)}
                  placeholder={key.includes('time') ? '2025-01-01T00:00:00Z' : ''}
                  aria-label={label}
                />
              </div>
            ))}
          </div>
          {hasActive && (
            <div className="flex justify-end mt-3 pt-3 border-t border-white/[0.06]">
              <Button variant="ghost" size="sm" onClick={onClearFilters}>
                <X className="w-3 h-3" />
                Clear all
              </Button>
            </div>
          )}
        </Card>
      )}

      {/* Active filter chips */}
      {hasActive && (
        <div className="flex flex-wrap gap-2">
          {Object.entries(filters)
            .filter(([k, v]) => k !== 'limit' && k !== 'offset' && v)
            .map(([k, v]) => (
              <span
                key={k}
                className="inline-flex items-center gap-1.5 rounded-full bg-indigo-500/10 border border-indigo-500/25 text-indigo-400 text-xs px-2.5 py-1"
              >
                {k}: {String(v)}
                <button
                  onClick={() => onFilterChange(k as keyof AuditFiltersType, '')}
                  className="hover:text-indigo-200 transition-colors"
                  aria-label={`Remove ${k} filter`}
                >
                  <X className="w-3 h-3" />
                </button>
              </span>
            ))}
        </div>
      )}
    </>
  )
}

'use client'

import { SecretMappingSuggestion } from '@/lib/api/types'
import { Card, Badge, Skeleton } from '@/components/ui-modern'
import { CheckCircle2, AlertCircle } from 'lucide-react'
import { EmptyState } from './EmptyState'

interface SecretMappingsTableProps {
  mappings?: SecretMappingSuggestion[]
  searchTerm: string
  isLoading: boolean
}

export function SecretMappingsTable({
  mappings,
  searchTerm,
  isLoading,
}: SecretMappingsTableProps) {
  if (isLoading) {
    return (
      <Card className="overflow-hidden">
        <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
          <div className="grid grid-cols-5 gap-3 text-xs font-semibold text-slate-500">
            <span>Environment Variable</span>
            <span>Suggested Secret</span>
            <span>Confidence</span>
            <span>Reason</span>
            <span>Status</span>
          </div>
        </div>
        <div>
          {[...Array(4)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-none border-b border-white/[0.06]" />
          ))}
        </div>
      </Card>
    )
  }

  if (!mappings || mappings.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="no-mappings" />
      </Card>
    )
  }

  const normalizedSearch = searchTerm.trim().toLowerCase()

  const filtered = normalizedSearch === ''
    ? mappings
    : mappings.filter(
        m =>
          m.env_var_name.toLowerCase().includes(normalizedSearch) ||
          m.suggested_secret_name.toLowerCase().includes(normalizedSearch)
      )

  if (filtered.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="filter-mismatch" />
      </Card>
    )
  }

  const confidenceColors: Record<string, string> = {
    high: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    medium: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    low: 'bg-red-500/20 text-red-300 border-red-500/30',
  }

  return (
    <Card className="overflow-hidden">
      <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
        <div className="grid grid-cols-5 gap-3 text-xs font-semibold text-slate-500">
          <span>Environment Variable</span>
          <span>Suggested Secret</span>
          <span>Confidence</span>
          <span>Reason</span>
          <span>Status</span>
        </div>
      </div>
      <div>
        {filtered.map(mapping => {
          const isHighlighted =
            mapping.env_var_name.toLowerCase().includes(normalizedSearch) ||
            mapping.suggested_secret_name.toLowerCase().includes(normalizedSearch)

          return (
            <div
              key={mapping.env_var_name}
              className={`px-4 py-3 border-b border-white/[0.06] ${
                isHighlighted ? 'bg-indigo-500/10' : 'hover:bg-white/[0.02]'
              } transition-colors`}
            >
              <div className="grid grid-cols-5 gap-3 items-center">
                <p className="text-sm font-mono text-slate-300">{mapping.env_var_name}</p>
                <p className="text-sm font-mono text-slate-400">{mapping.suggested_secret_name}</p>
                <Badge
                  variant="outline"
                  size="sm"
                  className={confidenceColors[mapping.confidence]}
                >
                  {mapping.confidence}
                </Badge>
                <p className="text-xs text-slate-500" title={mapping.reason}>
                  {mapping.reason}
                </p>
                <div className="flex items-center justify-center">
                  {mapping.is_configured ? (
                    <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                  ) : (
                    <AlertCircle className="w-4 h-4 text-amber-400" />
                  )}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </Card>
  )
}

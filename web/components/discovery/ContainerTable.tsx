'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Card, Skeleton } from '@/components/ui-modern'
import { ContainerRow } from './ContainerRow'
import { EmptyState } from './EmptyState'

interface ContainerTableProps {
  containers: ContainerMetadata[]
  isLoading: boolean
  onSelectContainer: (container: ContainerMetadata) => void
  selectedIds?: Set<string>
  onToggleSelect?: (containerId: string) => void
}

export function ContainerTable({
  containers,
  isLoading,
  onSelectContainer,
  selectedIds,
  onToggleSelect,
}: ContainerTableProps) {
  const hasSelection = !!onToggleSelect

  if (isLoading) {
    return (
      <Card className="overflow-hidden">
        <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
          <div className={`grid ${hasSelection ? 'grid-cols-7' : 'grid-cols-6'} gap-3 text-xs font-semibold text-slate-500`}>
            {hasSelection && <span>Select</span>}
            <span>Name</span>
            <span>Image</span>
            <span>Status</span>
            <span>Classification</span>
            <span>Secrets</span>
            <span>Missing</span>
          </div>
        </div>
        <div>
          {[...Array(5)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-none border-b border-white/[0.06]" />
          ))}
        </div>
      </Card>
    )
  }

  if (containers.length === 0) {
    return (
      <Card className="p-8">
        <EmptyState type="filter-mismatch" />
      </Card>
    )
  }

  return (
    <Card className="overflow-hidden">
      <div className="px-4 py-2.5 border-b border-white/[0.06] bg-white/[0.01]">
        <div className={`grid ${hasSelection ? 'grid-cols-7' : 'grid-cols-6'} gap-3 text-xs font-semibold text-slate-500`}>
          {hasSelection && <span>Select</span>}
          <span>Name</span>
          <span>Image</span>
          <span>Status</span>
          <span>Classification</span>
          <span>Secrets</span>
          <span>Missing</span>
        </div>
      </div>
      <div>
        {containers.map(container => (
          <ContainerRow
            key={container.container_id}
            container={container}
            onSelect={onSelectContainer}
            isSelected={selectedIds?.has(container.container_id)}
            onToggleSelect={onToggleSelect}
          />
        ))}
      </div>
    </Card>
  )
}

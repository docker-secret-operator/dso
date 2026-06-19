'use client'

import { ContainerMetadata } from '@/lib/api/types'
import { Badge } from '@/components/ui-modern'
import { ChevronRight } from 'lucide-react'

interface ContainerRowProps {
  container: ContainerMetadata
  onSelect: (container: ContainerMetadata) => void
  onToggleSelect?: (containerId: string) => void
  isSelected?: boolean
}

export function ContainerRow({ container, onSelect, onToggleSelect, isSelected }: ContainerRowProps) {
  const classificationColor: Record<string, string> = {
    managed: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    partial: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
    unmanaged: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
  }

  const statusColor: Record<string, string> = {
    running: 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
    stopped: 'bg-slate-500/20 text-slate-300 border-slate-500/30',
    paused: 'bg-amber-500/20 text-amber-300 border-amber-500/30',
  }

  const classification = container.dso_awareness?.status ?? 'unmanaged'
  const status = container.status ?? 'unknown'

  return (
    <button
      onClick={() => onSelect(container)}
      className="w-full px-4 py-3 border-b border-white/[0.06] hover:bg-white/[0.02] transition-colors text-left"
    >
      <div className={`grid ${onToggleSelect ? 'grid-cols-7' : 'grid-cols-6'} gap-3 items-center`}>
        {onToggleSelect && (
          <div className="col-span-1">
            <input
              type="checkbox"
              checked={isSelected ?? false}
              onChange={(e) => {
                e.stopPropagation()
                onToggleSelect(container.container_id)
              }}
              onClick={(e) => e.stopPropagation()}
              className="w-4 h-4 rounded border-white/20 text-indigo-500 focus:ring-indigo-500/20"
            />
          </div>
        )}
        <div className="col-span-1 truncate">
          <p className="text-sm font-medium text-slate-200 truncate">{container.container_name}</p>
        </div>
        <div className="col-span-1 truncate">
          <p className="text-xs text-slate-400 truncate">{container.image}</p>
        </div>
        <div className="col-span-1">
          <Badge variant="outline" size="sm" className={statusColor[status]}>
            {status}
          </Badge>
        </div>
        <div className="col-span-1">
          <Badge variant="outline" size="sm" className={classificationColor[classification]}>
            {classification}
          </Badge>
        </div>
        <div className="col-span-1">
          <p className="text-sm text-slate-400 text-center">
            {container.dso_awareness?.managed_secrets?.length ?? 0}
          </p>
        </div>
        <div className="col-span-1 flex items-center justify-between">
          <p className="text-sm text-slate-400 text-center">
            {container.dso_awareness?.missing_mappings?.length ?? 0}
          </p>
          <ChevronRight className="w-4 h-4 text-slate-600" />
        </div>
      </div>
    </button>
  )
}

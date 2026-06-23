'use client'

import type { ReactNode } from 'react'
import { X } from 'lucide-react'

export interface BulkAction {
  label: string
  onClick: () => void
  variant?: 'default' | 'danger'
  disabled?: boolean
}

interface BulkToolbarProps {
  count: number
  actions: BulkAction[]
  onClear: () => void
  status?: ReactNode
}

export function BulkToolbar({ count, actions, onClear, status }: BulkToolbarProps) {
  if (count === 0) return null
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 bg-indigo-500/10 border border-indigo-500/20 rounded-lg">
      <span className="text-sm font-semibold text-indigo-300 tabular-nums whitespace-nowrap">
        {count} selected
      </span>
      {status && (
        <span className="text-xs text-slate-400 truncate">{status}</span>
      )}
      <div className="flex items-center gap-2">
        {actions.map(action => (
          <button
            key={action.label}
            onClick={action.onClick}
            disabled={action.disabled}
            className={[
              'px-3 py-1.5 text-xs rounded-md border transition-all disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap',
              action.variant === 'danger'
                ? 'border-red-500/30 text-red-300 hover:bg-red-500/10 hover:border-red-500/40'
                : 'border-white/[0.09] text-slate-300 hover:bg-white/5 hover:border-white/20',
            ].join(' ')}
          >
            {action.label}
          </button>
        ))}
      </div>
      <button
        onClick={onClear}
        className="ml-auto p-1 text-slate-600 hover:text-slate-300 transition-colors flex-shrink-0"
        title="Clear selection"
        aria-label="Clear selection"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  )
}

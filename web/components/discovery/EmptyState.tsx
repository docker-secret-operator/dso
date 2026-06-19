'use client'

import { AlertCircle, Search, Database } from 'lucide-react'

export type EmptyStateType = 'no-containers' | 'no-mappings' | 'filter-mismatch'

interface EmptyStateProps {
  type: EmptyStateType
  onRetry?: () => void
}

export function EmptyState({ type, onRetry }: EmptyStateProps) {
  const config = {
    'no-containers': {
      icon: Database,
      title: 'No containers discovered',
      description: 'Try refreshing or check your environment.',
    },
    'no-mappings': {
      icon: AlertCircle,
      title: 'No secret mappings detected',
      description: 'This is perfectly valid — your containers may already be configured.',
    },
    'filter-mismatch': {
      icon: Search,
      title: 'No containers match current filters',
      description: 'Try adjusting your search term or filters.',
    },
  }

  const { icon: Icon, title, description } = config[type]

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <Icon className="w-12 h-12 text-slate-500 mb-3" />
      <h3 className="text-lg font-semibold text-slate-200 mb-1">{title}</h3>
      <p className="text-sm text-slate-400 mb-4 text-center max-w-md">{description}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="text-sm text-indigo-400 hover:text-indigo-300 underline"
        >
          Try again
        </button>
      )}
    </div>
  )
}

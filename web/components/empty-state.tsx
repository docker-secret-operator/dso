'use client'

import React from 'react'
import { AlertTriangle, Package, Zap } from 'lucide-react'
import { Button } from '@/components/ui-modern'

export interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
  }
  type?: 'no-executions' | 'no-alerts' | 'no-events' | 'empty' | 'no-results' | 'error'
}

/**
 * Reusable empty state component for operations console
 * Task 9: Renders different states based on type with appropriate icons and messages
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  type = 'empty',
}: EmptyStateProps) {
  const iconMap: Record<string, React.ReactNode> = {
    'no-executions': <Package className="w-12 h-12 text-slate-400" />,
    'no-alerts': <Zap className="w-12 h-12 text-slate-400" />,
    'no-events': <AlertTriangle className="w-12 h-12 text-slate-400" />,
    empty: <Package className="w-12 h-12 text-slate-400" />,
    'no-results': <Package className="w-12 h-12 text-slate-400" />,
    error: <AlertTriangle className="w-12 h-12 text-red-400" />,
  }

  const messageMap: Record<string, { title: string; description: string }> = {
    'no-executions': {
      title: 'No executions found',
      description: 'Try searching or creating a new execution',
    },
    'no-alerts': {
      title: 'No alerts',
      description: 'The system is operating normally',
    },
    'no-events': {
      title: 'No recovery events',
      description: 'Check back later for system events',
    },
  }

  const displayIcon = icon ?? iconMap[type]
  const finalTitle = title || messageMap[type]?.title || title
  const finalDescription = description || messageMap[type]?.description || description

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <div className="mb-4">{displayIcon}</div>
      <h3 className="text-base font-semibold text-slate-300 mb-2">{finalTitle}</h3>
      {finalDescription && (
        <p className="text-sm text-slate-500 mb-6 text-center max-w-sm">{finalDescription}</p>
      )}
      {action && (
        <Button onClick={action.onClick} variant="secondary" size="sm">
          {action.label}
        </Button>
      )}
    </div>
  )
}

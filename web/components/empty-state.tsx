'use client'

import React from 'react'
import { AlertCircle, Package, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'

export interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
  }
  type?: 'empty' | 'no-results' | 'error'
}

const defaultIcons = {
  empty: <Package className="w-12 h-12 text-gray-400" />,
  'no-results': <Search className="w-12 h-12 text-gray-400" />,
  error: <AlertCircle className="w-12 h-12 text-red-400" />,
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  type = 'empty',
}: EmptyStateProps) {
  const displayIcon = icon ?? defaultIcons[type]

  return (
    <div className="flex flex-col items-center justify-center py-12 px-4">
      <div className="mb-4">{displayIcon}</div>
      <h3 className="text-lg font-semibold text-gray-900 mb-2">{title}</h3>
      {description && <p className="text-sm text-gray-600 mb-6 text-center max-w-sm">{description}</p>}
      {action && (
        <Button onClick={action.onClick} variant="outline" size="sm">
          {action.label}
        </Button>
      )}
    </div>
  )
}

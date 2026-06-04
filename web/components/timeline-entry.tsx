'use client'

import { AlertCircle, AlertTriangle, Info, ChevronDown } from 'lucide-react'
import { useState } from 'react'
import { formatTime } from '@/lib/utils'
import Link from 'next/link'

export type TimelineSeverity = 'info' | 'warning' | 'error'

export interface TimelineEvent {
  id: string
  timestamp: string
  severity: TimelineSeverity
  source: 'event' | 'rotation' | 'discovery' | 'config' | 'error'
  title: string
  message: string
  metadata?: Record<string, unknown>
  container?: string
  secret?: string
}

interface TimelineEntryProps {
  event: TimelineEvent
  expanded?: boolean
  onExpandChange?: (expanded: boolean) => void
}

const severityConfig = {
  info: {
    icon: Info,
    bg: 'bg-blue-50',
    border: 'border-blue-200',
    dot: 'bg-blue-600',
    text: 'text-blue-900',
  },
  warning: {
    icon: AlertTriangle,
    bg: 'bg-yellow-50',
    border: 'border-yellow-200',
    dot: 'bg-yellow-600',
    text: 'text-yellow-900',
  },
  error: {
    icon: AlertCircle,
    bg: 'bg-red-50',
    border: 'border-red-200',
    dot: 'bg-red-600',
    text: 'text-red-900',
  },
}

const sourceConfig = {
  event: 'System Event',
  rotation: 'Secret Rotation',
  discovery: 'Discovery',
  config: 'Configuration',
  error: 'Error',
}

export function TimelineEntry({ event, expanded = false, onExpandChange }: TimelineEntryProps) {
  const [isExpanded, setIsExpanded] = useState(expanded)
  const config = severityConfig[event.severity]
  const Icon = config.icon

  const handleExpand = () => {
    setIsExpanded(!isExpanded)
    onExpandChange?.(!isExpanded)
  }

  const hasDetails = event.metadata || event.container || event.secret

  return (
    <div className={`border rounded-lg transition-colors ${isExpanded ? config.bg : ''} ${config.border}`}>
      {/* Header */}
      <div
        className={`p-4 ${isExpanded ? '' : `${config.bg}`} cursor-pointer hover:opacity-90 transition-opacity`}
        onClick={handleExpand}
      >
        <div className="flex items-start gap-4">
          {/* Timeline dot */}
          <div className="flex flex-col items-center gap-2 pt-1">
            <div className={`w-3 h-3 rounded-full ${config.dot}`} />
          </div>

          {/* Content */}
          <div className="flex-1 min-w-0">
            <div className="flex items-start justify-between gap-4">
              <div className="flex-1">
                <div className="flex items-center gap-2 mb-1">
                  <Icon className={`w-4 h-4 ${config.text}`} />
                  <h3 className={`font-semibold text-sm ${config.text}`}>{event.title}</h3>
                  <span className="text-xs bg-white px-2 py-1 rounded border">{sourceConfig[event.source]}</span>
                </div>
                <p className="text-sm text-gray-700 mb-2">{event.message}</p>
                <p className="text-xs text-gray-500">{formatTime(event.timestamp)}</p>
              </div>

              {/* Expand indicator */}
              {hasDetails && (
                <ChevronDown
                  className={`w-4 h-4 text-gray-400 transition-transform flex-shrink-0 ${
                    isExpanded ? 'rotate-180' : ''
                  }`}
                />
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Details */}
      {isExpanded && hasDetails && (
        <div className="border-t p-4 bg-white space-y-3">
          {event.container && (
            <div>
              <p className="text-xs text-gray-600 uppercase font-semibold mb-1">Container</p>
              <Link
                href={`/discovery?container=${encodeURIComponent(event.container)}`}
                className="text-sm font-mono text-blue-600 hover:text-blue-800 hover:underline cursor-pointer"
              >
                {event.container} →
              </Link>
            </div>
          )}
          {event.secret && (
            <div>
              <p className="text-xs text-gray-600 uppercase font-semibold mb-1">Secret</p>
              <Link
                href={`/secrets?name=${encodeURIComponent(event.secret)}`}
                className="text-sm font-mono text-blue-600 hover:text-blue-800 hover:underline cursor-pointer"
              >
                {event.secret} →
              </Link>
            </div>
          )}
          {event.metadata && (
            <div>
              <p className="text-xs text-gray-600 uppercase font-semibold mb-2">Details</p>
              <pre className="bg-gray-900 text-gray-100 p-3 rounded text-xs overflow-auto max-h-48">
                {JSON.stringify(event.metadata, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

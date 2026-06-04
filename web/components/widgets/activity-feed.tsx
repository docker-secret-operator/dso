'use client'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, CheckCircle2, InfoIcon, AlertTriangle } from 'lucide-react'
import { format } from 'date-fns'

export interface Activity {
  id: string
  title: string
  description?: string
  severity: 'info' | 'warning' | 'error'
  timestamp: Date | string
  icon?: React.ReactNode
}

export interface ActivityFeedProps {
  title: string
  activities: Activity[]
  maxItems?: number
  loading?: boolean
}

export function ActivityFeed({
  title,
  activities,
  maxItems = 5,
  loading = false,
}: ActivityFeedProps) {
  const getIcon = (severity: Activity['severity']) => {
    switch (severity) {
      case 'info':
        return <InfoIcon className="w-4 h-4 text-blue-600" />
      case 'warning':
        return <AlertTriangle className="w-4 h-4 text-yellow-600" />
      case 'error':
        return <AlertCircle className="w-4 h-4 text-red-600" />
    }
  }

  const getSeverityColor = (severity: Activity['severity']) => {
    switch (severity) {
      case 'info':
        return 'bg-blue-100 text-blue-800'
      case 'warning':
        return 'bg-yellow-100 text-yellow-800'
      case 'error':
        return 'bg-red-100 text-red-800'
    }
  }

  const formatTime = (timestamp: Date | string) => {
    const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp
    return format(date, 'MMM dd, HH:mm')
  }

  const displayActivities = activities.slice(0, maxItems)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {loading ? (
            <div className="space-y-2">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="h-12 bg-gray-200 rounded animate-pulse" />
              ))}
            </div>
          ) : displayActivities.length > 0 ? (
            displayActivities.map((activity) => (
              <div
                key={activity.id}
                className="flex gap-3 p-2 bg-gray-50 rounded border-l-2 border-gray-200"
              >
                <div className="flex-shrink-0 mt-1">
                  {activity.icon || getIcon(activity.severity)}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium text-gray-900">{activity.title}</p>
                      {activity.description && (
                        <p className="text-xs text-gray-500 mt-0.5">{activity.description}</p>
                      )}
                    </div>
                    <Badge className={getSeverityColor(activity.severity)}>
                      {activity.severity}
                    </Badge>
                  </div>
                  <p className="text-xs text-gray-400 mt-2">{formatTime(activity.timestamp)}</p>
                </div>
              </div>
            ))
          ) : (
            <p className="text-center text-sm text-gray-500 py-4">No recent activity</p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

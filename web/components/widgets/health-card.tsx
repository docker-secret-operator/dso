'use client'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Activity, AlertCircle, CheckCircle2 } from 'lucide-react'

export interface HealthStatus {
  name: string
  status: 'healthy' | 'warning' | 'error'
  message?: string
  lastChecked?: string
}

export interface HealthCardProps {
  title: string
  statuses: HealthStatus[]
  loading?: boolean
}

export function HealthCard({ title, statuses, loading = false }: HealthCardProps) {
  const getIcon = (status: HealthStatus['status']) => {
    switch (status) {
      case 'healthy':
        return <CheckCircle2 className="w-5 h-5 text-green-600" />
      case 'warning':
        return <AlertCircle className="w-5 h-5 text-yellow-600" />
      case 'error':
        return <AlertCircle className="w-5 h-5 text-red-600" />
    }
  }

  const getBadgeColor = (status: HealthStatus['status']) => {
    switch (status) {
      case 'healthy':
        return 'bg-green-100 text-green-800'
      case 'warning':
        return 'bg-yellow-100 text-yellow-800'
      case 'error':
        return 'bg-red-100 text-red-800'
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Activity className="w-5 h-5" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {loading ? (
            <div className="space-y-2">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="h-4 bg-gray-200 rounded animate-pulse" />
              ))}
            </div>
          ) : (
            statuses.map((status, i) => (
              <div key={i} className="flex items-center justify-between p-2 bg-gray-50 rounded">
                <div className="flex items-center gap-2 flex-1">
                  {getIcon(status.status)}
                  <div>
                    <p className="text-sm font-medium text-gray-900">{status.name}</p>
                    {status.message && <p className="text-xs text-gray-500">{status.message}</p>}
                  </div>
                </div>
                <Badge className={getBadgeColor(status.status)}>
                  {status.status.charAt(0).toUpperCase() + status.status.slice(1)}
                </Badge>
              </div>
            ))
          )}
        </div>
      </CardContent>
    </Card>
  )
}

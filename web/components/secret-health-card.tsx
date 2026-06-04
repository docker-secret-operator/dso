'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, AlertTriangle, CheckCircle } from 'lucide-react'

export interface HealthStats {
  healthy: number
  warning: number
  error: number
}

interface SecretHealthCardProps {
  stats: HealthStats
  title?: string
  description?: string
}

export function SecretHealthCard({
  stats,
  title = 'Secret Health',
  description = 'Based on recent events and rotations',
}: SecretHealthCardProps) {
  const total = stats.healthy + stats.warning + stats.error
  const healthyPercent = total > 0 ? (stats.healthy / total) * 100 : 0
  const warningPercent = total > 0 ? (stats.warning / total) * 100 : 0
  const errorPercent = total > 0 ? (stats.error / total) * 100 : 0

  const getOverallStatus = () => {
    if (stats.error > 0) return 'error'
    if (stats.warning > 0) return 'warning'
    return 'healthy'
  }

  const status = getOverallStatus()

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Overall Status */}
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-gray-600 mb-2">Overall Status</p>
            <Badge
              variant={status === 'error' ? 'destructive' : status === 'warning' ? 'secondary' : 'default'}
              className="text-sm"
            >
              {status === 'error' ? 'Error' : status === 'warning' ? 'Warning' : 'Healthy'}
            </Badge>
          </div>
          {status === 'healthy' ? (
            <CheckCircle className="w-8 h-8 text-green-600" />
          ) : status === 'warning' ? (
            <AlertTriangle className="w-8 h-8 text-yellow-600" />
          ) : (
            <AlertCircle className="w-8 h-8 text-red-600" />
          )}
        </div>

        {/* Status Breakdown */}
        <div className="space-y-3 pt-4 border-t">
          {/* Healthy */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <div className="flex items-center gap-2">
                <CheckCircle className="w-4 h-4 text-green-600" />
                <span className="text-sm text-gray-700">Healthy</span>
              </div>
              <span className="text-sm font-semibold text-gray-900">{stats.healthy}</span>
            </div>
            <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
              <div className="bg-green-600 h-full" style={{ width: `${healthyPercent}%` }} />
            </div>
          </div>

          {/* Warning */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <div className="flex items-center gap-2">
                <AlertTriangle className="w-4 h-4 text-yellow-600" />
                <span className="text-sm text-gray-700">Warning</span>
              </div>
              <span className="text-sm font-semibold text-gray-900">{stats.warning}</span>
            </div>
            <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
              <div className="bg-yellow-600 h-full" style={{ width: `${warningPercent}%` }} />
            </div>
          </div>

          {/* Error */}
          <div>
            <div className="flex items-center justify-between mb-1">
              <div className="flex items-center gap-2">
                <AlertCircle className="w-4 h-4 text-red-600" />
                <span className="text-sm text-gray-700">Error</span>
              </div>
              <span className="text-sm font-semibold text-gray-900">{stats.error}</span>
            </div>
            <div className="w-full bg-gray-200 rounded-full h-2 overflow-hidden">
              <div className="bg-red-600 h-full" style={{ width: `${errorPercent}%` }} />
            </div>
          </div>
        </div>

        {/* Total */}
        <div className="pt-2 border-t text-center">
          <p className="text-xs text-gray-600 uppercase font-semibold">Total Secrets</p>
          <p className="text-2xl font-bold text-gray-900">{total}</p>
        </div>
      </CardContent>
    </Card>
  )
}

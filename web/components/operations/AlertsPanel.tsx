'use client'

import { Card, Badge } from '@/components/ui-modern'
import { AlertCircle } from 'lucide-react'
import { formatRelativeTime } from '@/lib/utils'
import type { Alert } from '@/lib/api/types'

interface AlertsPanelProps {
  alerts: Alert[]
  isLoading?: boolean
  error?: string
}

/**
 * Panel displaying active alerts with severity levels
 * Task 7A: Display alerts with severity colors (info=slate, warning=amber, critical=red)
 */
export function AlertsPanel({ alerts, isLoading, error }: AlertsPanelProps) {
  const getSeverityBadgeVariant = (severity: string): 'info' | 'warning' | 'danger' | 'default' => {
    switch (severity) {
      case 'critical':
      case 'error':
        return 'danger'
      case 'warning':
        return 'warning'
      case 'info':
        return 'info'
      default:
        return 'default'
    }
  }

  if (isLoading) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <AlertCircle className="w-5 h-5 text-amber-400" />
          <h3 className="text-sm font-semibold text-slate-300">Alerts</h3>
        </div>
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="h-12 bg-white/[0.02] rounded-lg animate-pulse" />
          ))}
        </div>
      </Card>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <AlertCircle className="w-5 h-5 text-red-400" />
          <h3 className="text-sm font-semibold text-slate-300">Alerts</h3>
        </div>
        <div className="text-red-400 text-xs">{error}</div>
      </Card>
    )
  }

  if (!alerts || alerts.length === 0) {
    return (
      <Card className="p-6">
        <div className="flex items-center gap-3 mb-4">
          <AlertCircle className="w-5 h-5 text-amber-400" />
          <h3 className="text-sm font-semibold text-slate-300">Alerts</h3>
        </div>
        <div className="py-8 text-center">
          <p className="text-slate-500 text-sm">No alerts</p>
        </div>
      </Card>
    )
  }

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-4">
        <AlertCircle className="w-5 h-5 text-amber-400" />
        <h3 className="text-sm font-semibold text-slate-300">Alerts</h3>
        <span className="ml-auto text-xs font-medium text-slate-500">
          {alerts.length} alert{alerts.length !== 1 ? 's' : ''}
        </span>
      </div>

      <div className="space-y-3 max-h-96 overflow-y-auto">
        {alerts.map((alert) => (
          <div key={alert.id} className="p-3 rounded-lg bg-white/[0.01] border border-white/5 hover:bg-white/[0.03] transition-colors">
            <div className="flex items-start gap-3">
              <Badge variant={getSeverityBadgeVariant(alert.severity)} size="sm" dot>
                {alert.severity}
              </Badge>
              <div className="flex-1 min-w-0">
                <p className="text-xs font-medium text-slate-300">{alert.message}</p>
                {(alert.value !== undefined || alert.threshold !== undefined) && (
                  <p className="text-[11px] text-slate-500 mt-1">
                    Value: {alert.value?.toFixed(2) ?? '-'} / Threshold: {alert.threshold?.toFixed(2) ?? '-'}
                  </p>
                )}
                <p className="text-[10px] text-slate-600 mt-1.5">
                  {formatRelativeTime(alert.timestamp)}
                </p>
              </div>
            </div>
          </div>
        ))}
      </div>
    </Card>
  )
}

import { Card } from '@/components/ui-modern'
import { AlertCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Alert } from '@/lib/api/types'

interface AlertsPanelProps {
  alerts: Alert[]
}

/**
 * Panel displaying active alerts with severity levels
 */
export function AlertsPanel({ alerts }: AlertsPanelProps) {
  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'bg-red-500/10 border-red-500/20'
      case 'error':
        return 'bg-red-500/10 border-red-500/20'
      case 'warning':
        return 'bg-amber-500/10 border-amber-500/20'
      case 'info':
        return 'bg-blue-500/10 border-blue-500/20'
      default:
        return 'bg-slate-500/10 border-slate-500/20'
    }
  }

  const getSeverityBadgeColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'bg-red-500'
      case 'error':
        return 'bg-red-500'
      case 'warning':
        return 'bg-amber-500'
      case 'info':
        return 'bg-blue-500'
      default:
        return 'bg-slate-500'
    }
  }

  const getSeverityTextColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'text-red-400'
      case 'error':
        return 'text-red-400'
      case 'warning':
        return 'text-amber-400'
      case 'info':
        return 'text-blue-400'
      default:
        return 'text-slate-400'
    }
  }

  return (
    <Card className="p-6">
      <div className="flex items-center gap-3 mb-4">
        <AlertCircle className="w-5 h-5 text-amber-400" />
        <h3 className="text-sm font-semibold text-slate-300">Active Alerts</h3>
        {alerts.length > 0 && (
          <span className="ml-auto text-xs font-medium text-slate-500">
            {alerts.length} alert{alerts.length !== 1 ? 's' : ''}
          </span>
        )}
      </div>

      <div className="space-y-2">
        {alerts.length === 0 ? (
          <div className="text-slate-500 text-xs py-4">No active alerts</div>
        ) : (
          alerts.slice(0, 10).map((alert) => (
            <div
              key={alert.id}
              className={cn(
                'p-3 rounded-lg border transition-colors hover:bg-white/[0.02]',
                getSeverityColor(alert.severity)
              )}
            >
              <div className="flex items-start gap-2">
                <div className={cn('w-1.5 h-1.5 rounded-full mt-1.5 flex-shrink-0', getSeverityBadgeColor(alert.severity))} />
                <div className="flex-1 min-w-0">
                  <p className={cn('text-xs font-medium truncate', getSeverityTextColor(alert.severity))}>
                    {alert.type}
                  </p>
                  <p className="text-xs text-slate-400 mt-0.5 line-clamp-2">{alert.message}</p>
                  <div className="flex items-center justify-between mt-1">
                    <span className="text-[11px] text-slate-600">
                      Value: {alert.value.toFixed(2)} (threshold: {alert.threshold.toFixed(2)})
                    </span>
                    <span className="text-[10px] text-slate-700">
                      {new Date(alert.timestamp).toLocaleTimeString([], {
                        hour: '2-digit',
                        minute: '2-digit',
                      })}
                    </span>
                  </div>
                </div>
              </div>
            </div>
          ))
        )}
      </div>
    </Card>
  )
}

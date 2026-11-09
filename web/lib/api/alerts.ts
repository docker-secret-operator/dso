import { apiFetch } from '../api-fetch'

/**
 * Wire shape for GET /api/alerts (see internal/server/rest.go
 * handleAlerts / internal/alert.Alert). Alerts are Phase 4.1's smallest
 * useful record: enough to know what broke, where, and whether it's still
 * broken. Message is always a fixed, templated string -- never raw
 * provider error text or a secret value.
 */
export type AlertType = 'rotation_failed' | 'injection_failed' | 'service_degraded'
export type AlertStatus = 'firing' | 'resolved'

export interface Alert {
  id: string
  type: AlertType | string
  severity: string
  status: AlertStatus | string
  resource: string
  message: string
  dedup_key: string
  first_triggered_at: string
  last_triggered_at: string
  resolved_at?: string
}

export interface AlertsResponse {
  alerts: Alert[]
  total_count: number
}

export async function fetchAlerts(): Promise<AlertsResponse> {
  const res = await apiFetch('/api/alerts')
  if (!res.ok) {
    throw new Error(`Failed to load alerts (${res.status})`)
  }
  return res.json()
}

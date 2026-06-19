import { apiClient } from '../api-client'
import { MetricsHistoryResponse } from './types'

/**
 * Metrics History & Analytics API service
 * Provides historical metrics with trends, forecasting, and anomaly detection
 */

const API_BASE = '/api/metrics'

/**
 * Get metrics history with trends & forecast
 * GET /api/metrics/history
 */
export async function getHistory(params?: {
  period?: '1h' | '24h' | '7d' | '30d'
  granularity?: '1m' | '5m' | '1h'
}): Promise<MetricsHistoryResponse> {
  const response = await apiClient.client.get<MetricsHistoryResponse>(
    `${API_BASE}/history`,
    { params }
  )
  return response.data
}

/**
 * Get export URL for metrics as CSV or JSON
 * GET /api/metrics/export
 */
export function getExportURL(
  period: string = '24h',
  format: 'json' | 'csv' = 'csv'
): string {
  const params = new URLSearchParams()
  params.append('period', period)
  params.append('format', format)
  return `${API_BASE}/export?${params.toString()}`
}

/**
 * Download metrics as a file
 */
export async function exportMetrics(
  period: string = '24h',
  format: 'json' | 'csv' = 'csv'
): Promise<void> {
  const url = getExportURL(period, format)
  const link = document.createElement('a')
  link.href = url
  link.download = `metrics-export.${format}`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

/**
 * Get current system metrics (convenience helper)
 */
export async function getCurrentMetrics(): Promise<MetricsHistoryResponse> {
  return getHistory({ period: '1h', granularity: '1m' })
}

/**
 * Check if system is experiencing anomalies
 */
export async function hasAnomalies(): Promise<boolean> {
  try {
    const metrics = await getCurrentMetrics()
    return metrics.anomalies && metrics.anomalies.length > 0
  } catch {
    return false
  }
}

/**
 * Get anomaly details
 */
export async function getAnomalies() {
  const metrics = await getCurrentMetrics()
  return metrics.anomalies || []
}

/**
 * Get forecast warnings
 */
export async function getForecastWarnings() {
  const metrics = await getCurrentMetrics()
  const warnings = []

  if (metrics.forecast.queue_status === 'critical') {
    warnings.push('Queue is approaching saturation')
  }
  if (metrics.forecast.worker_status === 'critical') {
    warnings.push('Workers are approaching exhaustion')
  }

  return warnings
}

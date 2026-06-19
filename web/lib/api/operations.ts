import { apiClient } from '../api-client'
import {
  OperationsDashboardResponse,
  ListAlertsResponse,
  ListRecoveryEventsResponse,
  MetricsHistoryResponse,
  PaginationParams,
} from './types'

/**
 * Operations Dashboard API service
 * Monitors KPIs, queue/worker health, alerts, recovery events, and metrics history
 */

const API_BASE = '/api/operations'

/**
 * Get operations dashboard (KPIs, queue health, worker health, execution status, recovery stats, DLQ stats, recent failures)
 * GET /api/operations/dashboard
 */
export async function getDashboard(): Promise<OperationsDashboardResponse> {
  const response = await apiClient.client.get<OperationsDashboardResponse>(
    `${API_BASE}/dashboard`
  )
  return response.data
}

/**
 * Get active alerts (queue depth, worker health, failure rate, DLQ growth, timeout rate)
 * GET /api/operations/alerts
 */
export async function getAlerts(params?: PaginationParams): Promise<ListAlertsResponse> {
  const response = await apiClient.client.get<ListAlertsResponse>(`${API_BASE}/alerts`, { params })
  return response.data
}

/**
 * Get recovery events (worker failures, queue recovery, execution cancellations, pauses)
 * GET /api/operations/recovery-events
 */
export async function getRecoveryEvents(params?: PaginationParams): Promise<ListRecoveryEventsResponse> {
  const response = await apiClient.client.get<ListRecoveryEventsResponse>(
    `${API_BASE}/recovery-events`,
    { params }
  )
  return response.data
}

/**
 * Get historical metrics snapshots
 * GET /api/operations/metrics-history
 */
export async function getMetricsHistory(params?: PaginationParams): Promise<MetricsHistoryResponse> {
  const response = await apiClient.client.get<MetricsHistoryResponse>(
    `${API_BASE}/metrics-history`,
    { params }
  )
  return response.data
}

/**
 * Check if operations are healthy (all systems functioning normally)
 */
export async function isOperational(): Promise<boolean> {
  try {
    const dashboard = await getDashboard()
    return dashboard.system_health.status === 'healthy'
  } catch {
    return false
  }
}

/**
 * Get system health status from operations dashboard
 */
export async function getSystemHealth() {
  const dashboard = await getDashboard()
  return dashboard.system_health
}

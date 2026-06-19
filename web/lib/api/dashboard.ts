import { apiClient } from '../api-client'
import {
  DashboardOverviewResponse,
  DashboardMetricsResponse,
} from './types'

/**
 * Dashboard API service
 * Provides dashboard overview and metrics data
 */

const API_BASE = '/api/dashboard'

/**
 * Get dashboard overview with workflow and system status
 * GET /api/dashboard/overview
 */
export async function getOverview(): Promise<DashboardOverviewResponse> {
  const response = await apiClient.client.get<DashboardOverviewResponse>(
    `${API_BASE}/overview`
  )
  return response.data
}

/**
 * Get dashboard metrics
 * GET /api/dashboard/metrics
 */
export async function getMetrics(): Promise<DashboardMetricsResponse> {
  const response = await apiClient.client.get<DashboardMetricsResponse>(
    `${API_BASE}/metrics`
  )
  return response.data
}

/**
 * Get workflow chain for a specific draft
 * GET /api/dashboard/workflow/{draftId}
 */
export async function getWorkflowChain(draftId: string): Promise<any> {
  const response = await apiClient.client.get<any>(
    `${API_BASE}/workflow/${encodeURIComponent(draftId)}`
  )
  return response.data
}

/**
 * Get audit summary for dashboard
 * GET /api/dashboard/audit
 */
export async function getAuditSummary(limit?: number): Promise<any> {
  const params = limit ? { limit } : {}
  const response = await apiClient.client.get<any>(`${API_BASE}/audit`, { params })
  return response.data
}

/**
 * Get complete dashboard data (overview + metrics)
 */
export async function getDashboardData() {
  const [overview, metrics] = await Promise.all([getOverview(), getMetrics()])
  return { overview, metrics }
}

import { apiClient } from '../api-client'
import {
  AuditExplorerResponse,
  CorrelationChainResponse,
  ActorTimelineResponse,
  AuditFilters,
  NotFoundError,
} from './types'

/**
 * Audit Explorer API service
 * Handles audit event queries, correlation chains, and timeline views
 */

const API_BASE = '/api/audit'

/**
 * Get audit events with optional filtering, searching, and pagination
 * GET /api/audit
 */
export async function getAuditEvents(filters?: AuditFilters): Promise<AuditExplorerResponse> {
  const response = await apiClient.client.get<AuditExplorerResponse>(API_BASE, { params: filters })
  return response.data
}

/**
 * Get complete execution story - all audit events for a correlation ID
 * Includes execution events, review activities, approvals
 * GET /api/audit/correlation/{id}
 */
export async function getCorrelationChain(id: string): Promise<CorrelationChainResponse> {
  try {
    const response = await apiClient.client.get<CorrelationChainResponse>(
      `${API_BASE}/correlation/${encodeURIComponent(id)}`
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Correlation chain not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get actor's activity timeline
 * Supports periods: 24h, 7d, 30d
 * GET /api/audit/actors/{id}
 */
export async function getActorTimeline(
  actorId: string,
  period?: '24h' | '7d' | '30d'
): Promise<ActorTimelineResponse> {
  try {
    const params = period ? { period } : {}
    const response = await apiClient.client.get<ActorTimelineResponse>(
      `${API_BASE}/actors/${encodeURIComponent(actorId)}`,
      { params }
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Actor timeline not found: ${actorId}`)
    }
    throw error
  }
}

/**
 * Export audit events as CSV or JSON
 * GET /api/audit/export
 */
export function getAuditExportURL(filters?: AuditFilters, format: 'json' | 'csv' = 'csv'): string {
  const params = new URLSearchParams()
  params.append('format', format)

  if (filters) {
    Object.entries(filters).forEach(([key, value]) => {
      if (value !== undefined) {
        params.append(key, String(value))
      }
    })
  }

  return `${API_BASE}/export?${params.toString()}`
}

/**
 * Download audit events as a file
 */
export async function exportAudit(
  filters?: AuditFilters,
  format: 'json' | 'csv' = 'csv'
): Promise<void> {
  const url = getAuditExportURL(filters, format)
  const link = document.createElement('a')
  link.href = url
  link.download = `audit-export.${format}`
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

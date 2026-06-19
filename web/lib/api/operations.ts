import { apiClient } from '../api-client'
import {
  OperationsDashboard,
  Alert,
  RecoveryEvent,
  MetricsHistory,
  ExecutionList,
  Execution,
  ExecutionPlan,
  ExecutionValidation,
  ExecutionTrace,
  ExecutionJourney,
  NotFoundError,
  PaginationParams,
} from './types'

/**
 * Operations Dashboard API service
 * Monitors KPIs, queue/worker health, alerts, recovery events, metrics history, and executions
 */

const API_BASE = '/api/operations'

/**
 * Get operations dashboard (KPIs, queue health, worker health, execution status, recovery stats, DLQ stats, recent failures)
 * GET /api/operations/dashboard
 */
export async function getOperationsDashboard(): Promise<OperationsDashboard> {
  const response = await apiClient.client.get<OperationsDashboard>(`${API_BASE}/dashboard`)
  return response.data
}

/**
 * Get active alerts (queue depth, worker health, failure rate, DLQ growth, timeout rate)
 * GET /api/operations/alerts
 */
export async function getAlerts(params?: PaginationParams): Promise<Alert[]> {
  const response = await apiClient.client.get<{ alerts: Alert[] }>(`${API_BASE}/alerts`, { params })
  return response.data.alerts
}

/**
 * Get recovery events (worker failures, queue recovery, execution cancellations, pauses)
 * GET /api/operations/recovery-events
 */
export async function getRecoveryEvents(params?: PaginationParams): Promise<RecoveryEvent[]> {
  const response = await apiClient.client.get<{ events: RecoveryEvent[] }>(
    `${API_BASE}/recovery-events`,
    { params }
  )
  return response.data.events
}

/**
 * Get historical metrics snapshots
 * GET /api/operations/metrics-history
 */
export async function getMetricsHistory(params?: PaginationParams): Promise<MetricsHistory> {
  const response = await apiClient.client.get<MetricsHistory>(`${API_BASE}/metrics-history`, {
    params,
  })
  return response.data
}

/**
 * List executions with pagination and optional filtering
 * GET /api/operations/executions
 */
export async function getExecutions(params?: PaginationParams & { status?: string }): Promise<ExecutionList> {
  const response = await apiClient.client.get<ExecutionList>(`${API_BASE}/executions`, { params })
  return response.data
}

/**
 * Get single execution by ID
 * GET /api/operations/executions/{id}
 */
export async function getExecution(id: string): Promise<Execution> {
  try {
    const response = await apiClient.client.get<Execution>(
      `${API_BASE}/executions/${encodeURIComponent(id)}`
    )
    return response.data
  } catch (error: unknown) {
    const err = error as { response?: { status?: number } }
    if (err.response?.status === 404) {
      throw new NotFoundError(`Execution not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get execution plan with steps, risk score, affected resources
 * GET /api/operations/executions/{id}/plan
 */
export async function getExecutionPlan(id: string): Promise<ExecutionPlan> {
  try {
    const response = await apiClient.client.get<ExecutionPlan>(
      `${API_BASE}/executions/${encodeURIComponent(id)}/plan`
    )
    return response.data
  } catch (error: unknown) {
    const err = error as { response?: { status?: number } }
    if (err.response?.status === 404) {
      throw new NotFoundError(`Execution plan not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get execution validation result
 * GET /api/operations/executions/{id}/validation
 */
export async function getExecutionValidation(id: string): Promise<ExecutionValidation> {
  try {
    const response = await apiClient.client.get<ExecutionValidation>(
      `${API_BASE}/executions/${encodeURIComponent(id)}/validation`
    )
    return response.data
  } catch (error: unknown) {
    const err = error as { response?: { status?: number } }
    if (err.response?.status === 404) {
      throw new NotFoundError(`Execution validation not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get execution trace (execution + plan + events)
 * GET /api/operations/executions/{id}/trace
 */
export async function getExecutionTrace(id: string): Promise<ExecutionTrace> {
  try {
    const response = await apiClient.client.get<ExecutionTrace>(
      `${API_BASE}/executions/${encodeURIComponent(id)}/trace`
    )
    return response.data
  } catch (error: unknown) {
    const err = error as { response?: { status?: number } }
    if (err.response?.status === 404) {
      throw new NotFoundError(`Execution trace not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get execution journey (lifecycle steps from audit)
 * GET /api/operations/executions/{id}/journey
 */
export async function getExecutionJourney(id: string): Promise<ExecutionJourney> {
  try {
    const response = await apiClient.client.get<ExecutionJourney>(
      `${API_BASE}/executions/${encodeURIComponent(id)}/journey`
    )
    return response.data
  } catch (error: unknown) {
    const err = error as { response?: { status?: number } }
    if (err.response?.status === 404) {
      throw new NotFoundError(`Execution journey not found: ${id}`)
    }
    throw error
  }
}

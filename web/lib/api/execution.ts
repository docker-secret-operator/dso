import { apiClient } from '../api-client'
import {
  ExecutionResponse,
  ExecutionPlanResponse,
  ExecutionJourneyResponse,
  ValidationResponse,
  ListExecutionsResponse,
  CreateExecutionRequest,
  NotFoundError,
  PaginationParams,
} from './types'

/**
 * Execution Orchestration API service
 * Handles execution creation, querying, planning, validation, and tracing
 */

const API_BASE = '/api/executions'

/**
 * Create execution request
 * POST /api/executions
 */
export async function createExecution(request: CreateExecutionRequest): Promise<ExecutionResponse> {
  const response = await apiClient.client.post<ExecutionResponse>(API_BASE, request)
  return response.data
}

/**
 * List execution requests with pagination and optional status filter
 * GET /api/executions
 */
export async function listExecutions(params?: PaginationParams & { status?: string }): Promise<ListExecutionsResponse> {
  const response = await apiClient.client.get<ListExecutionsResponse>(API_BASE, { params })
  return response.data
}

/**
 * Get single execution request by ID
 * GET /api/executions/{id}
 */
export async function getExecution(id: string): Promise<ExecutionResponse> {
  try {
    const response = await apiClient.client.get<ExecutionResponse>(
      `${API_BASE}/${encodeURIComponent(id)}`
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Execution not found: ${id}`)
    }
    throw error
  }
}

/**
 * Get execution plan with steps, risk score, affected resources, rollback info
 * GET /api/executions/{id}/plan
 */
export async function getPlan(executionId: string): Promise<ExecutionPlanResponse> {
  try {
    const response = await apiClient.client.get<ExecutionPlanResponse>(
      `${API_BASE}/${encodeURIComponent(executionId)}/plan`
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Execution plan not found: ${executionId}`)
    }
    throw error
  }
}

/**
 * Get validation result for an execution
 * GET /api/executions/{id}/validation
 */
export async function getValidation(
  executionId: string,
  draftId?: string,
  approvalId?: string
): Promise<ValidationResponse> {
  const params: Record<string, string> = {}
  if (draftId) params.draft_id = draftId
  if (approvalId) params.approval_id = approvalId

  const response = await apiClient.client.get<ValidationResponse>(
    `${API_BASE}/${encodeURIComponent(executionId)}/validation`,
    { params }
  )
  return response.data
}

/**
 * Get execution trace (execution + plan + correlation)
 * GET /api/executions/{id}/trace
 */
export async function getTrace(executionId: string): Promise<any> {
  try {
    const response = await apiClient.client.get<any>(
      `${API_BASE}/${encodeURIComponent(executionId)}/trace`
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Execution trace not found: ${executionId}`)
    }
    throw error
  }
}

/**
 * Get execution lifecycle journey (all timestamped steps from audit)
 * GET /api/executions/{id}/journey
 */
export async function getJourney(executionId: string): Promise<ExecutionJourneyResponse> {
  try {
    const response = await apiClient.client.get<ExecutionJourneyResponse>(
      `${API_BASE}/${encodeURIComponent(executionId)}/journey`
    )
    return response.data
  } catch (error: any) {
    if (error.response?.status === 404) {
      throw new NotFoundError(`Execution journey not found: ${executionId}`)
    }
    throw error
  }
}

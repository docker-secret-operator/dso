import { apiClient } from '../api-client'
import {
  HealthResponse,
  ReadyResponse,
  StorageResponse,
} from './types'

/**
 * System health and status API service
 */

const API_BASE = '/api/system'

/**
 * Get full health status including all checks
 * GET /api/system/health
 */
export async function getHealth(): Promise<HealthResponse> {
  const response = await apiClient.client.get<HealthResponse>(`${API_BASE}/health`)
  return response.data
}

/**
 * Get simple readiness check (quick database ping)
 * GET /api/system/health/ready
 */
export async function getReady(): Promise<ReadyResponse> {
  const response = await apiClient.client.get<ReadyResponse>(`${API_BASE}/health/ready`)
  return response.data
}

/**
 * Get storage/persistence status
 * GET /api/system/storage
 */
export async function getStorage(): Promise<StorageResponse> {
  const response = await apiClient.client.get<StorageResponse>(`${API_BASE}/storage`)
  return response.data
}

/**
 * Check if system is healthy
 */
export async function isHealthy(): Promise<boolean> {
  try {
    const health = await getHealth()
    return health.status === 'up'
  } catch {
    return false
  }
}

/**
 * Check if system is ready to serve requests
 */
export async function isReady(): Promise<boolean> {
  try {
    const ready = await getReady()
    return ready.ready
  } catch {
    return false
  }
}

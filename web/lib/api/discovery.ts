import { apiClient } from '../api-client'
import {
  DiscoveryResponse,
  MappingResponse,
  RefreshResponse,
  DiscoveryMetrics,
} from './types'

/**
 * Container & Secret Discovery API service
 * Discovers containers, suggests secret mappings, provides cache metrics
 */

const API_BASE = '/api/discovery'

/**
 * List all containers with DSO metadata (cached, 30s TTL)
 * GET /api/discovery/containers
 */
export async function getContainers(): Promise<DiscoveryResponse> {
  const response = await apiClient.client.get<DiscoveryResponse>(`${API_BASE}/containers`)
  return response.data
}

/**
 * Suggest secret mappings based on container env analysis (cached)
 * GET /api/discovery/mappings
 */
export async function getMappings(): Promise<MappingResponse> {
  const response = await apiClient.client.get<MappingResponse>(`${API_BASE}/mappings`)
  return response.data
}

/**
 * Invalidate and refresh discovery cache asynchronously
 * GET /api/discovery/refresh
 */
export async function refreshDiscovery(): Promise<RefreshResponse> {
  const response = await apiClient.client.get<RefreshResponse>(`${API_BASE}/refresh`)
  return response.data
}

/**
 * Get cache performance metrics (hits, misses, refresh count, latency, cache age)
 * GET /api/discovery/metrics
 */
export async function getDiscoveryMetrics(): Promise<DiscoveryMetrics> {
  const response = await apiClient.client.get<DiscoveryMetrics>(`${API_BASE}/metrics`)
  return response.data
}

/**
 * Get summary of discovered containers
 */
export async function getDiscoverySummary(): Promise<{
  total: number
  managed: number
  unmanaged: number
  partial: number
}> {
  const data = await getContainers()
  return {
    total: data.total,
    managed: data.managed,
    unmanaged: data.unmanaged,
    partial: data.partial,
  }
}

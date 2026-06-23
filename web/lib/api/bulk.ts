import { apiClient } from '../api-client'

export interface BulkRotateResult {
  success: number
  failed: number
  failures: Array<{ name: string; error: string }>
}

export interface BulkIdResult {
  success: number
  failed: number
  failures: Array<{ id: string; error: string }>
}

export async function bulkRotate(names: string[]): Promise<BulkRotateResult> {
  const res = await apiClient.client.post<BulkRotateResult>('/api/secrets/bulk-rotate', { names })
  return res.data
}

export async function bulkDriftAck(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/drift/bulk-ack', { ids })
  return res.data
}

export async function bulkDriftResolve(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/drift/bulk-resolve', { ids })
  return res.data
}

export async function bulkPolicyEnable(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-enable', { ids })
  return res.data
}

export async function bulkPolicyDisable(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-disable', { ids })
  return res.data
}

export async function bulkPolicyDelete(ids: string[]): Promise<BulkIdResult> {
  const res = await apiClient.client.post<BulkIdResult>('/api/policies/bulk-delete', { ids })
  return res.data
}

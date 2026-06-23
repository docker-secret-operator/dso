import { apiClient } from '../api-client'
import {
  DriftFinding,
  DriftListResponse,
  DriftMetrics,
  DriftHistoryResponse,
} from './types'

const API_BASE = '/api/drift'

export async function listFindings(): Promise<DriftListResponse> {
  const res = await apiClient.client.get<DriftListResponse>(API_BASE)
  return res.data
}

export async function getFinding(id: string): Promise<DriftFinding> {
  const res = await apiClient.client.get<DriftFinding>(`${API_BASE}/${id}`)
  return res.data
}

export async function runScan(): Promise<{ status: string; findings: number }> {
  const res = await apiClient.client.post<{ status: string; findings: number }>(`${API_BASE}/scan`)
  return res.data
}

export async function acknowledgeFinding(id: string): Promise<void> {
  await apiClient.client.post(`${API_BASE}/${id}/acknowledge`)
}

export async function resolveFinding(id: string): Promise<void> {
  await apiClient.client.post(`${API_BASE}/${id}/resolve`)
}

export async function getMetrics(): Promise<DriftMetrics> {
  const res = await apiClient.client.get<DriftMetrics>(`${API_BASE}/metrics`)
  return res.data
}

export async function getHistory(): Promise<DriftHistoryResponse> {
  const res = await apiClient.client.get<DriftHistoryResponse>(`${API_BASE}/history`)
  return res.data
}

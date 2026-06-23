import { apiClient } from '../api-client'

export interface SecretVersionEntry {
  version: number
  createdAt: string
  rotatedBy: string
  rotationSource: string
  provider: string
  executionId?: string
}

export interface SecretHistoryResponse {
  currentVersion: number
  versions: SecretVersionEntry[]
}

export interface SecretTimelineEvent {
  type: 'rotation' | 'audit' | 'drift' | 'execution'
  timestamp: string
  description: string
  executionId?: string
  driftId?: string
  auditId?: string
  version?: number
  actor?: string
  source?: string
}

export interface SecretDiffResponse {
  v1: number
  v2: number
  providerChanged: boolean
  rotationSourceChanged: boolean
  executionChanged: boolean
  hashChanged: boolean
  containersAffected: number
  v1RotatedBy: string
  v2RotatedBy: string
  v1CreatedAt: string
  v2CreatedAt: string
}

export async function getSecretHistory(name: string): Promise<SecretHistoryResponse> {
  const res = await apiClient.client.get<SecretHistoryResponse>(`/api/secrets/${name}/history`)
  return res.data
}

export async function getSecretTimeline(name: string): Promise<SecretTimelineEvent[]> {
  const res = await apiClient.client.get<SecretTimelineEvent[]>(`/api/secrets/${name}/timeline`)
  return res.data
}

export async function getSecretDiff(name: string, v1: number, v2: number): Promise<SecretDiffResponse> {
  const res = await apiClient.client.get<SecretDiffResponse>(`/api/secrets/${name}/diff?v1=${v1}&v2=${v2}`)
  return res.data
}

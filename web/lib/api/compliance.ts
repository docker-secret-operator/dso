import { apiClient } from '../api-client'

export type RotationStatus = 'compliant' | 'overdue' | 'never_rotated' | 'unknown'
export type OverallStatus = 'compliant' | 'warning' | 'non_compliant'

export interface ComplianceSummary {
  totalSecrets: number
  compliant: number
  warning: number
  nonCompliant: number
}

export interface SecretComplianceDetail {
  rotationStatus: RotationStatus
  openDrift: number
  versionCount: number
  auditCount: number
  lastRotation: string | null
  overallStatus: OverallStatus
}

export interface SecretComplianceRecord {
  secretName: string
  provider: string
  rotationStatus: RotationStatus
  driftFree: boolean
  lastRotatedAt: string | null
  versionCount: number
  openDriftFindings: number
  auditEventCount: number
  overallStatus: OverallStatus
}

export interface SecretCompliancePage {
  total: number
  page: number
  pageSize: number
  items: SecretComplianceRecord[]
}

export interface ComplianceListParams {
  status?: string
  provider?: string
  search?: string
  page?: number
  pageSize?: number
}

export async function getSummary(): Promise<ComplianceSummary> {
  const res = await apiClient.client.get<ComplianceSummary>('/api/compliance/summary')
  return res.data
}

export async function getSecretsList(params?: ComplianceListParams): Promise<SecretCompliancePage> {
  const res = await apiClient.client.get<SecretCompliancePage>('/api/compliance/secrets', { params })
  return res.data
}

export async function getSecretCompliance(name: string): Promise<SecretComplianceDetail> {
  const res = await apiClient.client.get<SecretComplianceDetail>(`/api/compliance/secrets/${name}`)
  return res.data
}

export function complianceExportUrl(format: 'json' | 'csv'): string {
  return `/api/compliance/export?format=${format}`
}

export function reportUrl(kind: 'rotation' | 'drift' | 'policy' | 'activity', format: 'json' | 'csv'): string {
  return `/api/compliance/reports/${kind}?format=${format}`
}

import { apiFetch } from '../api-fetch'

/**
 * Wire shape for GET /api/drift (see internal/server/rest.go handleDrift
 * and internal/watcher/drift.go ComputeDrift). Computed live on every
 * request from the loaded config and a fresh Docker container list -- no
 * persistence, no history. Fields are strictly safe metadata (secret,
 * target, container identifiers, label presence) -- never a secret value.
 */
export interface DriftFinding {
  secret: string
  target: string
  container?: string
  drift_type: 'MISSING_CONTAINER' | 'MISSING_MAPPING' | 'MAPPING_MISMATCH'
  expected: string
  actual: string
}

export interface DriftSummary {
  total: number
  in_sync: number
  drifted: number
}

export interface DriftResponse {
  summary: DriftSummary
  findings: DriftFinding[]
}

export async function fetchDrift(): Promise<DriftResponse> {
  const res = await apiFetch('/api/drift')
  if (!res.ok) {
    throw new Error(`Failed to load drift status (${res.status})`)
  }
  return res.json()
}

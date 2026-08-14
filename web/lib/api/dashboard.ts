import { apiFetch } from '../api-fetch'

/** Matches internal/server/rest.go handleHealth: `{"status":"up"}`. */
export interface HealthResponse {
  status: string
}

/** One entry in handleListSecrets' `active_secrets` array. Only fields the
 * backend actually populates are declared here -- optional fields are
 * genuinely omitted (omitempty) rather than fabricated. */
export interface SecretSummary {
  name: string
  provider: string
  status: string
  last_synced_at?: string
  last_updated_at?: string
  last_error?: string
  injection_type: string
  mount_path?: string
  version?: string
  rotation_enabled: boolean
  auto_sync_enabled: boolean
}

/** Matches internal/server/rest.go handleListSecrets response body. */
export interface SecretsResponse {
  active_secrets: SecretSummary[]
  total_count: number
}

/** Matches internal/server/rest.go handleDiscovery response body. */
export interface DiscoveryResponse {
  webui_enabled: boolean
  webhook_enabled: boolean
  secret_count: number
}

export async function fetchHealth(): Promise<HealthResponse> {
  const res = await apiFetch('/health')
  if (!res.ok) throw new Error(`Health check failed (${res.status})`)
  return res.json()
}

export async function fetchSecrets(): Promise<SecretsResponse> {
  const res = await apiFetch('/api/secrets')
  if (!res.ok) throw new Error(`Failed to load secrets (${res.status})`)
  return res.json()
}

export async function fetchDiscovery(): Promise<DiscoveryResponse> {
  const res = await apiFetch('/api/discovery')
  if (!res.ok) throw new Error(`Failed to load discovery info (${res.status})`)
  return res.json()
}

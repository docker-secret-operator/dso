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

/** Matches internal/server/rest.go handleSecretRotate's success response
 * body. Never carries a secret value -- only an accept/reject signal for
 * the POST itself. The actual rotation outcome arrives later via the
 * existing rotation_succeeded/rotation_failed WebSocket events. */
export interface RotateSecretResponse {
  status: string
  secret: string
}

/**
 * POST /api/secrets/{name}/rotate -- triggers the exact same production
 * rotation machinery as `dso sync --secret NAME`
 * (watcher.ReloaderController.TriggerReload). This call only reports
 * whether the request was accepted; it never blocks on the rotation
 * actually finishing. Callers must watch the WebSocket event stream (see
 * hooks/useWebSocket) for the real success/failure outcome.
 */
export async function rotateSecret(name: string): Promise<RotateSecretResponse> {
  const res = await apiFetch(`/api/secrets/${encodeURIComponent(name)}/rotate`, {
    method: 'POST',
  })
  if (!res.ok) {
    const message = await res.text().catch(() => '')
    throw new Error(message || `Failed to trigger rotation (${res.status})`)
  }
  return res.json()
}

export async function fetchDiscovery(): Promise<DiscoveryResponse> {
  const res = await apiFetch('/api/discovery')
  if (!res.ok) throw new Error(`Failed to load discovery info (${res.status})`)
  return res.json()
}

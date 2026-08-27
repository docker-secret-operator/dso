import { apiFetch } from '../api-fetch'

/**
 * Wire shape for one entry in GET /api/integrations (see
 * internal/server/rest.go handleIntegrations / integrationView). Read-only
 * -- there are no mutation endpoints to call, by design: DSO's current
 * branch has no plugin/integration registry, only the config-derived facts
 * this endpoint already knows (the inbound webhook and configured
 * providers).
 */
export interface IntegrationInfo {
  name: string
  type: string
  enabled: boolean
  auth_configured?: boolean
  provider_count?: number
}

export interface IntegrationsResponse {
  integrations: IntegrationInfo[]
  total_count: number
}

export async function fetchIntegrations(): Promise<IntegrationsResponse> {
  const res = await apiFetch('/api/integrations')
  if (!res.ok) {
    throw new Error(`Failed to load integrations (${res.status})`)
  }
  return res.json()
}

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

/** One destination's outcome from POST /api/integrations/notifications/test
 * (see internal/notify.TestResult). `error`, when present, is a delivery
 * error string (e.g. connection refused) -- never a credential or secret. */
export interface NotificationTestResult {
  destination: string
  success: boolean
  error?: string
}

export interface NotificationTestResponse {
  status: string
  results: NotificationTestResult[] | null
  total_count: number
  failed: number
}

/**
 * POST /api/integrations/notifications/test -- delivers ONE synthetic test
 * event to every configured notification destination, reusing the exact
 * same logic as `dso notifications test` (internal/notify.RunTest). Sends
 * no payload; the browser cannot influence what gets delivered.
 */
export async function testNotifications(): Promise<NotificationTestResponse> {
  const res = await apiFetch('/api/integrations/notifications/test', { method: 'POST' })
  if (!res.ok) {
    throw new Error(`Notification test failed (${res.status})`)
  }
  return res.json()
}

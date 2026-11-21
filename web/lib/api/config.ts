import { apiFetch } from '../api-fetch'

/** One entry in handleConfigRaw's `secrets` array (name/provider only --
 * the backend never includes credentials or secret values here). Matches
 * internal/server/rest.go handleConfigRaw's secretView type. */
export interface ConfigSecretSummary {
  name: string
  provider: string
}

/** Matches internal/server/rest.go handleConfigRaw response body: a
 * read-only, secret-redacted view of the loaded configuration. */
export interface ConfigRawResponse {
  secrets: ConfigSecretSummary[]
  providers: string[]
}

/** Thrown by fetchConfigRaw so callers can distinguish "not authorized" from
 * a generic failure and render an honest not-authorized state rather than
 * silently showing empty data. */
export class ConfigFetchError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.name = 'ConfigFetchError'
    this.status = status
  }
}

export async function fetchConfigRaw(): Promise<ConfigRawResponse> {
  const res = await apiFetch('/api/config/raw')
  if (!res.ok) {
    throw new ConfigFetchError(res.status, `Failed to load configuration (${res.status})`)
  }
  return res.json()
}

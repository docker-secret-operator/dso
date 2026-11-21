import { AuthenticationError } from './errors'

/**
 * apiFetch — a thin wrapper around fetch() for same-origin DSO `/api/*`
 * calls.
 *
 * The backend authenticates webui requests via an HttpOnly
 * `dso_webui_session` cookie (see internal/webuiauth), not via a
 * client-readable Bearer token. Browsers attach cookies automatically for
 * same-origin requests as long as `credentials: 'include'` is set, so this
 * wrapper's only job is to make sure that's always on -- there is no token
 * to read from localStorage and no Authorization header to construct.
 *
 * A 401 response means the session cookie is missing or expired. That's
 * distinct from any other failure (network error, 4xx/5xx) -- callers
 * should not retry it, they should send the operator back to /login. We
 * surface that distinction here by throwing the same `AuthenticationError`
 * the axios-based client (lib/handle-api-error.ts) already throws for 401s,
 * rather than letting it fall through as a generic Error the caller has to
 * re-detect from a status code.
 */
export async function apiFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers as HeadersInit | undefined)
  const res = await fetch(input, { ...init, credentials: 'include', headers })
  if (res.status === 401) {
    throw new AuthenticationError('Your session has expired. Please log in again.')
  }
  return res
}

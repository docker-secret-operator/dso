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
 */
export async function apiFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers as HeadersInit | undefined)
  return fetch(input, { ...init, credentials: 'include', headers })
}

/**
 * apiFetch — a thin wrapper around fetch() that attaches the DSO auth token.
 *
 * The backend authenticates requests ONLY via the `Authorization: Bearer`
 * header (it does not read cookies). The auth token lives in localStorage
 * (set at login). Pages that call bare `fetch('/api/...')` are therefore
 * unauthenticated and get 401/403. Use apiFetch for any same-origin `/api/*`
 * request so the Bearer header is always attached, mirroring the axios
 * api-client interceptor.
 */

function getToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem('dso_api_token')
}

/**
 * Drop-in replacement for fetch() for DSO API calls. Merges the Authorization
 * header into any caller-supplied headers without clobbering them.
 */
export async function apiFetch(input: string, init: RequestInit = {}): Promise<Response> {
  const token = getToken()

  const headers = new Headers(init.headers as HeadersInit | undefined)
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  return fetch(input, { ...init, headers })
}

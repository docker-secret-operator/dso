import axios, { AxiosInstance } from 'axios'
import { getCsrfHeaders } from './csrf'
import { handleApiError } from './handle-api-error'

/**
 * PASS 1 SCOPE: this is a minimal axios instance carrying only what the auth
 * flow needs. feature/web-ui's api-client.ts is a much larger file that also
 * defines dashboard/secrets/audit/metrics methods and types -- those belong
 * to a later porting pass and are deliberately NOT ported here.
 *
 * Auth model: the DSO backend authenticates webui requests via an HttpOnly
 * `dso_webui_session` cookie (see internal/webuiauth), never via a
 * client-readable bearer token. `withCredentials: true` makes the browser
 * attach that cookie automatically on same-origin requests; there is no
 * localStorage token to read or attach here.
 */

const getApiBaseUrl = (): string => {
  if (typeof window !== 'undefined') {
    // Client-side: use same origin for proxied API calls
    return window.location.origin
  }

  // Build-time / static-export prerender: this module is evaluated in
  // Node while `next build` renders the static HTML shell for each route
  // (there is no real per-request Node server in production -- see
  // next.config.js `output: 'export'`). There is no request origin to
  // resolve here, so fall back to a relative base; axios resolves relative
  // URLs against the page's own origin once this code actually runs in the
  // browser, which is the only place these requests are ever issued.
  return ''
}

const API_BASE_URL = getApiBaseUrl()

class APIClient {
  public client: AxiosInstance

  constructor(baseURL: string = API_BASE_URL) {
    this.client = axios.create({
      baseURL,
      timeout: 10000,
      withCredentials: true,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // Attach CSRF headers for state-changing requests. No Authorization
    // header is set here -- the session cookie is sent automatically via
    // withCredentials.
    this.client.interceptors.request.use((config) => {
      if (typeof window !== 'undefined') {
        if (config.method && ['post', 'put', 'delete', 'patch'].includes(config.method.toLowerCase())) {
          const csrfHeaders = getCsrfHeaders()
          Object.assign(config.headers, csrfHeaders)
        }
      }
      return config
    })

    this.client.interceptors.response.use(
      (response) => response,
      (error: unknown) => {
        try {
          handleApiError(error)
        } catch (typedError) {
          return Promise.reject(typedError)
        }
      }
    )
  }
}

export const apiClient = new APIClient(API_BASE_URL)
export default apiClient

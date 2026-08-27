import { apiClient } from '../api-client'
import {
  LoginRequest,
  LoginResponse,
  LogoutResponse,
  SessionCheckResponse,
  UnauthorizedError,
} from './types'

/**
 * Authentication API service.
 *
 * The DSO backend (internal/server/rest.go) never returns a session token in
 * a response body -- login sets an HttpOnly `dso_webui_session` cookie via
 * Set-Cookie and the browser handles it transparently from then on
 * (apiClient is configured with withCredentials: true). There is nothing
 * for this module to store client-side.
 */

const API_BASE = '/api/auth'

export async function login(credentials: LoginRequest): Promise<LoginResponse> {
  try {
    const response = await apiClient.client.post<LoginResponse>(`${API_BASE}/login`, credentials)
    return response.data
  } catch (error: any) {
    if (error.response?.status === 401) {
      throw new UnauthorizedError('Invalid username or password')
    }
    throw error
  }
}

export async function logout(): Promise<LogoutResponse> {
  const response = await apiClient.client.post<LogoutResponse>(`${API_BASE}/logout`, {})
  return response.data
}

/**
 * Asks the server whether the current session cookie is valid. This is the
 * only source of truth for auth state on the client -- there is no token to
 * inspect locally.
 */
export async function checkSession(): Promise<boolean> {
  try {
    const response = await apiClient.client.get<SessionCheckResponse>(`${API_BASE}/session`)
    return response.data.authenticated === true
  } catch {
    return false
  }
}

/**
 * Returns the authenticated operator's identity, or null if the session is
 * invalid. Backs the Users/Access page -- DSO's single-operator model means
 * this is "who am I", not a user lookup.
 */
export async function fetchOperatorIdentity(): Promise<{ username: string } | null> {
  try {
    const response = await apiClient.client.get<SessionCheckResponse>(`${API_BASE}/session`)
    if (response.data.authenticated !== true || !response.data.username) {
      return null
    }
    return { username: response.data.username }
  } catch {
    return null
  }
}

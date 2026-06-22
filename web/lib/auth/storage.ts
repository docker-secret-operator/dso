/**
 * Secure token storage management
 * Handles persistence of auth tokens and user data
 *
 * SECURITY NOTE: tokens are stored in localStorage to stay consistent with the
 * rest of the app (the login flow, route guards, and ~20 pages all read
 * `dso_api_token` from localStorage). sessionStorage is NOT a meaningful XSS
 * mitigation — script running in the page can read either store. The genuine
 * hardening is httpOnly + Secure + SameSite cookies issued by the backend
 * (so JS never touches the token); that is tracked as backend work. Do not
 * switch only the client to sessionStorage — it splits the token store and
 * breaks auth (see incident: login wrote localStorage while api-client read
 * sessionStorage → every request was unauthenticated).
 */

const TOKEN_KEY = 'dso_api_token'
const REFRESH_TOKEN_KEY = 'dso_refresh_token'
const USER_KEY = 'dso_user'
const SESSION_KEY = 'dso_session'

export interface StoredUser {
  id: string
  username: string
  display_name: string
  role: string
  must_change_password: boolean
  password_expires_at?: string
}

export interface StoredSession {
  id: string
  created_at: string
  expires_at: string
  ip_address: string
}

/**
 * Get access token from storage
 */
export function getAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem(TOKEN_KEY)
}

/**
 * Set access token in storage
 */
export function setAccessToken(token: string): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(TOKEN_KEY, token)
}

/**
 * Get refresh token from storage
 */
export function getRefreshToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem(REFRESH_TOKEN_KEY)
}

/**
 * Set refresh token in storage
 */
export function setRefreshToken(token: string): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(REFRESH_TOKEN_KEY, token)
}

/**
 * Get stored user data
 */
export function getStoredUser(): StoredUser | null {
  if (typeof window === 'undefined') return null
  const data = localStorage.getItem(USER_KEY)
  if (!data) return null
  try {
    return JSON.parse(data)
  } catch {
    return null
  }
}

/**
 * Store user data
 */
export function setStoredUser(user: StoredUser): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(USER_KEY, JSON.stringify(user))
}

/**
 * Get stored session
 */
export function getStoredSession(): StoredSession | null {
  if (typeof window === 'undefined') return null
  const data = localStorage.getItem(SESSION_KEY)
  if (!data) return null
  try {
    return JSON.parse(data)
  } catch {
    return null
  }
}

/**
 * Store session
 */
export function setStoredSession(session: StoredSession): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(SESSION_KEY, JSON.stringify(session))
}

/**
 * Clear all auth data
 */
export function clearAllAuthData(): void {
  if (typeof window === 'undefined') return
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
  localStorage.removeItem(SESSION_KEY)
  // Transient UI flag — always lived in sessionStorage.
  sessionStorage.removeItem('session_expired')
}

/**
 * Check if session is expired
 */
export function isSessionExpired(): boolean {
  if (typeof window === 'undefined') return false
  const session = getStoredSession()
  if (!session) return true
  const expiresAt = new Date(session.expires_at)
  return new Date() > expiresAt
}

/**
 * Check if user needs password change
 */
export function mustChangePassword(): boolean {
  const user = getStoredUser()
  return user?.must_change_password ?? false
}

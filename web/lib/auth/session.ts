/**
 * Session management
 * Handles session initialization, refresh, and validation
 */

import * as storage from './storage'
import { currentUser, refreshToken, logout } from '../api/auth'

/**
 * Initialize session from stored data
 * Called on app startup
 */
export async function initializeSession(): Promise<storage.StoredUser | null> {
  // Check if we have a stored token
  const token = storage.getAccessToken()
  if (!token) {
    storage.clearAllAuthData()
    return null
  }

  // Check if session is expired
  if (storage.isSessionExpired()) {
    storage.clearAllAuthData()
    return null
  }

  // Try to use stored user data first
  const storedUser = storage.getStoredUser()
  if (storedUser) {
    return storedUser
  }

  // If no stored user, fetch current user from API
  try {
    const user = await currentUser()
    storage.setStoredUser(user)
    return user
  } catch (error) {
    // Token is invalid or expired
    storage.clearAllAuthData()
    return null
  }
}

/**
 * Refresh access token
 * Called when receiving 401 response
 */
export async function refreshAccessToken(): Promise<boolean> {
  try {
    const refreshTokenValue = storage.getRefreshToken()
    if (!refreshTokenValue) {
      // No refresh token available
      storage.clearAllAuthData()
      return false
    }

    // Call refresh endpoint
    const response = await refreshToken()

    // Store new expiry
    if (response.expires_at) {
      const session = storage.getStoredSession()
      if (session) {
        storage.setStoredSession({
          ...session,
          expires_at: response.expires_at,
        })
      }
    }

    return true
  } catch (error) {
    // Refresh failed, clear session
    storage.clearAllAuthData()
    return false
  }
}

/**
 * Validate current session
 * Returns true if session is valid and not expired
 */
export function isSessionValid(): boolean {
  const token = storage.getAccessToken()
  const user = storage.getStoredUser()

  if (!token || !user) {
    return false
  }

  if (storage.isSessionExpired()) {
    return false
  }

  return true
}

/**
 * Get session time remaining in seconds
 */
export function getSessionTimeRemaining(): number {
  const session = storage.getStoredSession()
  if (!session) return 0

  const expiresAt = new Date(session.expires_at).getTime()
  const now = new Date().getTime()
  const remaining = (expiresAt - now) / 1000

  return Math.max(0, remaining)
}

/**
 * Check if session is about to expire (within 5 minutes)
 */
export function isSessionExpiringSoon(): boolean {
  const remaining = getSessionTimeRemaining()
  return remaining > 0 && remaining < 300 // 5 minutes
}

/**
 * Logout and clear session
 */
export async function clearSession(): Promise<void> {
  try {
    // Try to logout via API
    await logout()
  } catch (error) {
    // Ignore errors during logout
  } finally {
    // Always clear local data
    storage.clearAllAuthData()
  }
}

/**
 * Get session info
 */
export function getSessionInfo() {
  return {
    user: storage.getStoredUser(),
    session: storage.getStoredSession(),
    isValid: isSessionValid(),
    timeRemaining: getSessionTimeRemaining(),
    expiringSoon: isSessionExpiringSoon(),
  }
}

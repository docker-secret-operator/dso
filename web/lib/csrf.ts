/**
 * CSRF (Cross-Site Request Forgery) Protection
 * Generates and manages CSRF tokens for state-changing requests
 */

const CSRF_TOKEN_KEY = '_csrf_token'
const CSRF_HEADER_NAME = 'X-CSRF-Token'

/**
 * Generate a random CSRF token
 */
export function generateCsrfToken(): string {
  if (typeof window === 'undefined') {
    return ''
  }

  // Check if token already exists in meta tag
  const metaToken = document.querySelector(`meta[name="${CSRF_TOKEN_KEY}"]`)?.getAttribute('content')
  if (metaToken) {
    return metaToken
  }

  // Generate new random token
  const array = new Uint8Array(32)
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(array)
  }
  const token = Array.from(array, byte => byte.toString(16).padStart(2, '0')).join('')

  // Store in sessionStorage as backup
  if (typeof sessionStorage !== 'undefined') {
    sessionStorage.setItem(CSRF_TOKEN_KEY, token)
  }

  return token
}

/**
 * Get current CSRF token
 */
export function getCsrfToken(): string {
  if (typeof window === 'undefined') {
    return ''
  }

  // Try to get from meta tag first
  const metaToken = document.querySelector(`meta[name="${CSRF_TOKEN_KEY}"]`)?.getAttribute('content')
  if (metaToken) {
    return metaToken
  }

  // Fall back to sessionStorage
  if (typeof sessionStorage !== 'undefined') {
    const storedToken = sessionStorage.getItem(CSRF_TOKEN_KEY)
    if (storedToken) {
      return storedToken
    }
  }

  // Generate new if not found
  return generateCsrfToken()
}

/**
 * Get CSRF header name for HTTP requests
 */
export function getCsrfHeaderName(): string {
  return CSRF_HEADER_NAME
}

/**
 * Get CSRF headers object for API requests
 */
export function getCsrfHeaders(): Record<string, string> {
  const token = getCsrfToken()
  if (!token) {
    return {}
  }
  return {
    [CSRF_HEADER_NAME]: token,
  }
}

/**
 * Validate CSRF token (server-side)
 * @param token - The token from request header
 * @param sessionToken - The expected token from session
 */
export function validateCsrfToken(token: string, sessionToken: string): boolean {
  if (!token || !sessionToken) {
    return false
  }
  // Use timing-safe comparison in production
  return token === sessionToken
}

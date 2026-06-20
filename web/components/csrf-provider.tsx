'use client'

import { useEffect } from 'react'
import { generateCsrfToken } from '@/lib/csrf'

/**
 * Client component that generates and initializes CSRF token
 * Must be rendered early in the app to ensure token is available
 */
export function CsrfProvider() {
  useEffect(() => {
    // Generate CSRF token on client mount
    generateCsrfToken()
  }, [])

  return null
}

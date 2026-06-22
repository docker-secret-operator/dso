/**
 * API error handler with proper type narrowing
 * Converts Axios errors to typed error classes
 */

import axios, { AxiosError } from 'axios'
import {
  ApiError,
  AuthenticationError,
  ForbiddenError,
  NotFoundError,
  ConflictError,
  ValidationError,
  TimeoutError,
  NetworkError,
  UnknownError,
} from './errors'

/**
 * Handles API errors with proper type narrowing and returns appropriate typed error
 * @param error - The error to handle (typically from axios or other promise rejection)
 * @returns Never - Always throws a typed error
 */
export function handleApiError(error: unknown): never {
  // Handle Axios errors with response
  if (axios.isAxiosError(error)) {
    const status = error.response?.status
    const data = error.response?.data as any

    switch (status) {
      case 401:
        // Clear auth tokens and redirect to login
        clearAuthTokens()
        throw new AuthenticationError(
          data?.message || 'Your session has expired. Please log in again.'
        )

      case 403:
        throw new ForbiddenError(
          data?.message || 'You do not have permission to access this resource.'
        )

      case 404:
        throw new NotFoundError(
          data?.message || 'The requested resource was not found.'
        )

      case 409:
        throw new ConflictError(
          data?.message || 'The request conflicted with existing data.'
        )

      case 400:
        throw new ValidationError(
          data?.message || 'The request contains invalid data.',
          data?.details || {}
        )

      case 500:
      case 502:
      case 503:
      case 504:
        throw new ApiError(
          status,
          data?.message || 'An error occurred on the server. Please try again later.'
        )

      default:
        if (status && status >= 400 && status < 500) {
          throw new ApiError(
            status,
            data?.message || `Request failed with status ${status}`
          )
        }
        if (status && status >= 500) {
          throw new ApiError(
            status,
            data?.message || 'Server error occurred. Please try again later.'
          )
        }
    }

    // Handle network errors (no response)
    if (error.code === 'ECONNABORTED') {
      throw new TimeoutError(
        'The request timed out. Please check your connection and try again.'
      )
    }

    if (error.code === 'ECONNREFUSED' || error.message === 'Network Error') {
      throw new NetworkError(
        'Unable to reach the server. Please check your internet connection.'
      )
    }

    if (!error.response) {
      throw new NetworkError(
        'Network error: ' + (error.message || 'No response from server')
      )
    }

    // Fallback for other axios errors
    throw new ApiError(
      error.response?.status || 0,
      error.message || 'An unexpected error occurred'
    )
  }

  // Handle standard Error objects
  if (error instanceof Error) {
    throw new UnknownError(error.message)
  }

  // Handle string errors
  if (typeof error === 'string') {
    throw new UnknownError(error)
  }

  // Fallback for unknown error types
  throw new UnknownError('An unexpected error occurred')
}

/**
 * Clears auth tokens when authentication fails
 * This function should clear tokens from sessionStorage and redirect to login
 */
function clearAuthTokens() {
  if (typeof window !== 'undefined') {
    sessionStorage.removeItem('dso_api_token')
    sessionStorage.removeItem('dso_refresh_token')
    sessionStorage.removeItem('dso_user')
    sessionStorage.removeItem('dso_session')
    // Redirect to login
    window.location.href = '/login'
  }
}

/**
 * Wraps an async function to handle errors with proper typing
 * Usage: const result = await withErrorHandling(() => apiClient.get('/data'))
 */
export async function withErrorHandling<T>(
  fn: () => Promise<T>
): Promise<T> {
  try {
    return await fn()
  } catch (error) {
    handleApiError(error)
  }
}

/**
 * Creates an error handler for React Query/TanStack Query
 * Returns false if error should not retry, true if it should
 */
export function shouldRetryRequest(error: unknown, attemptIndex: number): boolean {
  // Only retry on network errors or 5xx server errors
  if (axios.isAxiosError(error)) {
    // Don't retry on 4xx errors (client errors)
    if (error.response?.status && error.response.status >= 400 && error.response.status < 500) {
      return false
    }

    // Don't retry on 401 (auth error)
    if (error.response?.status === 401) {
      return false
    }

    // Retry on 5xx or network errors, but limit attempts
    return attemptIndex < 3
  }

  // Retry other errors (network timeouts, etc.)
  return attemptIndex < 3
}

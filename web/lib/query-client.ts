import { QueryClient } from '@tanstack/react-query'
import axios from 'axios'

// Determines if an error should be retried
// Only retries on network errors and 5xx server errors
// Does not retry on 4xx client errors (invalid request, not found, etc.)
function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  // Don't retry if we've already tried 3 times
  if (failureCount >= 3) {
    return false
  }

  // Retry on Axios errors
  if (axios.isAxiosError(error)) {
    const status = error.response?.status

    // Don't retry 4xx client errors (400, 401, 403, 404, 409, etc.)
    if (status && status >= 400 && status < 500) {
      return false
    }

    // Do retry on 5xx server errors
    if (status && status >= 500) {
      return true
    }

    // Retry on network errors (ECONNREFUSED, ECONNABORTED, etc.)
    if (error.code === 'ECONNREFUSED' || error.code === 'ECONNABORTED' || error.code === 'ENETUNREACH') {
      return true
    }

    // Retry if no response (network error)
    if (!error.response) {
      return true
    }
  }

  // Retry other errors up to the limit
  return true
}

function shouldRetryMutation(failureCount: number, error: unknown): boolean {
  // Only retry on network errors, not on server responses
  if (axios.isAxiosError(error)) {
    // Don't retry if server responded (any status code)
    if (error.response) {
      return false
    }

    // Retry on network errors
    return failureCount < 2
  }

  return failureCount < 2
}

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 60, // 1 minute
        gcTime: 1000 * 60 * 5, // 5 minutes (formerly cacheTime)
        retry: shouldRetryQuery, // Smart retry predicate
        retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 10000), // Cap at 10s
        throwOnError: false, // Don't throw - let components handle gracefully
      },
      mutations: {
        retry: shouldRetryMutation, // Smart retry for mutations
        retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 10000),
        throwOnError: false,
      },
    },
  })
}

let clientSingleton: QueryClient | undefined

export function getQueryClient() {
  if (typeof window === 'undefined') {
    return createQueryClient()
  }

  if (!clientSingleton) {
    clientSingleton = createQueryClient()
  }

  return clientSingleton
}

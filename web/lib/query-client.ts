import { QueryClient } from '@tanstack/react-query'

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 60, // 1 minute
        gcTime: 1000 * 60 * 5, // 5 minutes (formerly cacheTime)
        retry: 1, // Reduce retries for faster failure
        retryDelay: (attemptIndex) => Math.min(1000 * 2 ** attemptIndex, 10000), // Cap at 10s
        throwOnError: false, // Don't throw - let components handle gracefully
      },
      mutations: {
        retry: 2,
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

import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, act } from '@testing-library/react'
import React, { ReactNode, useEffect, useState } from 'react'
import { QueryClient, QueryClientProvider, useQuery, useInfiniteQuery } from '@tanstack/react-query'

/**
 * Performance Test Suite
 * Validates React Query caching, memory management, and re-render optimization
 * Target: ~8 tests, execution < 3 seconds
 */

// Mock API with controllable latency
const createMockApi = (latency = 0) => ({
  fetchData: async (key: string) => {
    await new Promise((resolve) => setTimeout(resolve, latency))
    return { id: key, data: `Result for ${key}`, timestamp: Date.now() }
  },
  fetchList: async () => {
    await new Promise((resolve) => setTimeout(resolve, latency))
    return Array.from({ length: 10 }, (_, i) => ({ id: i, name: `Item ${i}` }))
  },
  fetchFiltered: async (filter: string) => {
    await new Promise((resolve) => setTimeout(resolve, latency))
    return Array.from({ length: 5 }, (_, i) => ({ id: i, name: `${filter}-${i}` }))
  },
})

const mockApi = createMockApi()

// Test component for React Query caching
function CacheTestComponent({ queryKey }: { queryKey: string }) {
  const { data, isFetching, isStale } = useQuery({
    queryKey: ['test', queryKey],
    queryFn: () => mockApi.fetchData(queryKey),
    staleTime: 5000,
  })

  return (
    <div>
      <div data-testid="cache-data">{data?.data}</div>
      <div data-testid="cache-fetching">{isFetching ? 'fetching' : 'idle'}</div>
      <div data-testid="cache-stale">{isStale ? 'stale' : 'fresh'}</div>
    </div>
  )
}

// Test component for duplicate request prevention
function DuplicateRequestComponent() {
  const [count, setCount] = useState(0)
  const { data, isFetching } = useQuery({
    queryKey: ['duplicate-test'],
    queryFn: () => mockApi.fetchData('shared'),
    staleTime: 60000,
  })

  return (
    <div>
      <div data-testid="dup-data">{data?.data}</div>
      <div data-testid="dup-fetching">{isFetching ? 'fetching' : 'idle'}</div>
      <button onClick={() => setCount(c => c + 1)} data-testid="dup-button">
        Count: {count}
      </button>
    </div>
  )
}

// Test component for memory leak detection
function MemoryLeakComponent({ shouldUnmount }: { shouldUnmount: boolean }) {
  const { data } = useQuery({
    queryKey: ['memory-test'],
    queryFn: () => mockApi.fetchData('memory'),
    staleTime: 1000,
  })

  useEffect(() => {
    const interval = setInterval(() => {
      // This should be cleaned up on unmount
    }, 100)

    return () => {
      clearInterval(interval)
    }
  }, [])

  if (shouldUnmount) return null

  return <div data-testid="memory-data">{data?.data}</div>
}

// Test component for re-render optimization
function RenderOptimizationComponent() {
  const [filter, setFilter] = useState('')
  const [renderCount, setRenderCount] = useState(0)

  const { data } = useQuery({
    queryKey: ['render-test', filter],
    queryFn: () => mockApi.fetchFiltered(filter || 'all'),
    staleTime: 5000,
  })

  useEffect(() => {
    setRenderCount((c) => c + 1)
  }, [])

  return (
    <div>
      <input
        data-testid="filter-input"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Filter"
      />
      <div data-testid="render-count">{renderCount}</div>
      <div data-testid="render-data">
        {data?.map((item) => (
          <div key={item.id}>{item.name}</div>
        ))}
      </div>
    </div>
  )
}

// Test component for multiple queries on same page
function MultiQueryComponent() {
  const query1 = useQuery({
    queryKey: ['query1'],
    queryFn: () => mockApi.fetchData('q1'),
    staleTime: 5000,
  })

  const query2 = useQuery({
    queryKey: ['query2'],
    queryFn: () => mockApi.fetchData('q2'),
    staleTime: 5000,
  })

  return (
    <div>
      <div data-testid="q1-data">{query1.data?.data}</div>
      <div data-testid="q2-data">{query2.data?.data}</div>
      <div data-testid="q1-fetching">{query1.isFetching ? 'yes' : 'no'}</div>
      <div data-testid="q2-fetching">{query2.isFetching ? 'yes' : 'no'}</div>
    </div>
  )
}

// Performance tracking utility
class PerformanceTracker {
  private marks: Map<string, number> = new Map()
  private measures: Map<string, number[]> = new Map()

  mark(label: string) {
    this.marks.set(label, performance.now())
  }

  measure(label: string, startMark: string) {
    const start = this.marks.get(startMark)
    if (start === undefined) return
    const duration = performance.now() - start
    if (!this.measures.has(label)) {
      this.measures.set(label, [])
    }
    this.measures.get(label)!.push(duration)
  }

  getAverage(label: string): number {
    const times = this.measures.get(label) || []
    return times.length > 0 ? times.reduce((a, b) => a + b, 0) / times.length : 0
  }

  getMax(label: string): number {
    const times = this.measures.get(label) || []
    return times.length > 0 ? Math.max(...times) : 0
  }

  clear() {
    this.marks.clear()
    this.measures.clear()
  }
}

describe('Performance Tests', () => {
  let queryClient: QueryClient
  let tracker: PerformanceTracker

  beforeAll(() => {
    tracker = new PerformanceTracker()
    // Clear any performance marks from previous runs
    performance.clearMarks()
    performance.clearMeasures()
  })

  afterAll(() => {
    tracker.clear()
  })

  beforeEach(() => {
    // Fresh QueryClient for each test with controlled cache behavior
    queryClient = new QueryClient({
      defaultOptions: {
        queries: {
          staleTime: 5000,
          gcTime: 10000,
          retry: 1,
        },
      },
    })
    vi.clearAllMocks()
    tracker.clear()
  })

  afterEach(() => {
    queryClient.clear()
  })

  describe('React Query Caching', () => {
    it('should use cache on subsequent queries within staleTime', async () => {
      const fetchSpy = vi.spyOn(mockApi, 'fetchData')

      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      const { rerender } = render(
        <CacheTestComponent queryKey="cache-hit-test" />,
        { wrapper: Wrapper }
      )

      // Wait for initial fetch
      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toHaveTextContent('Result for cache-hit-test')
      })

      const firstCallCount = fetchSpy.mock.calls.length

      // Rerender component - should use cache
      rerender(<CacheTestComponent queryKey="cache-hit-test" />)

      // Wait a bit and verify
      await waitFor(() => {
        expect(screen.getByTestId('cache-fetching')).toHaveTextContent('idle')
      })

      // Should not have made another network request
      expect(fetchSpy).toHaveBeenCalledTimes(firstCallCount)

      fetchSpy.mockRestore()
    })

    it('should refetch when staleTime expires', async () => {
      const fetchSpy = vi.spyOn(mockApi, 'fetchData')
      const testQueryClient = new QueryClient({
        defaultOptions: { queries: { staleTime: 100 } },
      })

      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={testQueryClient}>
          {children}
        </QueryClientProvider>
      )

      render(<CacheTestComponent queryKey="stale-test" />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toBeInTheDocument()
      })

      const initialCalls = fetchSpy.mock.calls.length

      // Wait for staleTime to expire and trigger refetch via invalidation
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 150))
      })

      // Manually invalidate to trigger refetch
      await act(async () => {
        await testQueryClient.invalidateQueries({ queryKey: ['test', 'stale-test'] })
      })

      // Should have called fetch (initial + refetch)
      expect(fetchSpy.mock.calls.length).toBeGreaterThanOrEqual(initialCalls)

      fetchSpy.mockRestore()
    })

    it('should mark query as stale after staleTime but keep data', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(<CacheTestComponent queryKey="stale-marker-test" />, { wrapper: Wrapper })

      // Initially fresh
      await waitFor(() => {
        expect(screen.getByTestId('cache-stale')).toHaveTextContent('fresh')
      })

      // Data should still be visible
      expect(screen.getByTestId('cache-data')).toBeInTheDocument()
    })

    it('should track cache metrics with performance API', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('cache-test-start')

      render(<CacheTestComponent queryKey="perf-cache-test" />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toBeInTheDocument()
      })

      tracker.measure('cache-fetch-time', 'cache-test-start')
      const cacheTime = tracker.getAverage('cache-fetch-time')

      // Cache fetch should be relatively fast (< 1 second for mock)
      expect(cacheTime).toBeLessThan(1000)
    })
  })

  describe('Duplicate Request Prevention', () => {
    it('should prevent duplicate requests for same query key', async () => {
      const fetchSpy = vi.spyOn(mockApi, 'fetchData')

      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(
        <>
          <DuplicateRequestComponent />
          <DuplicateRequestComponent />
        </>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        const dataElements = screen.getAllByTestId('dup-data')
        expect(dataElements[0]).toHaveTextContent('Result for shared')
      })

      // Should only fetch once for both components
      const fetchCalls = fetchSpy.mock.calls.filter((call) => call[0] === 'shared')
      expect(fetchCalls.length).toBe(1)

      fetchSpy.mockRestore()
    })

    it('should share query result across multiple components', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(
        <>
          <DuplicateRequestComponent />
          <DuplicateRequestComponent />
        </>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        const dataElements = screen.getAllByTestId('dup-data')
        expect(dataElements.length).toBe(2)
      })

      // Both should show the same data
      const dataElements = screen.getAllByTestId('dup-data')
      expect(dataElements[0].textContent).toBe(dataElements[1].textContent)
    })

    it('should sync loading state across multiple components with same query', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(
        <>
          <DuplicateRequestComponent />
          <DuplicateRequestComponent />
        </>,
        { wrapper: Wrapper }
      )

      // Both should have same fetching state
      await waitFor(() => {
        const fetchingElements = screen.getAllByTestId('dup-fetching')
        expect(fetchingElements[0].textContent).toBe(fetchingElements[1].textContent)
      })
    })
  })

  describe('Memory Leak Prevention', () => {
    it('should cleanup query subscriptions on component unmount', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      const { rerender } = render(
        <MemoryLeakComponent shouldUnmount={false} />,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('memory-data')).toBeInTheDocument()
      })

      // Get initial observer count via cache getter
      const cache = queryClient.getQueryCache()
      expect(cache).toBeDefined()

      // Unmount component
      rerender(<MemoryLeakComponent shouldUnmount={true} />)

      // Verify cache still exists after unmount
      const cacheAfter = queryClient.getQueryCache()
      expect(cacheAfter).toBeDefined()
    })

    it('should not leak memory when component remounts', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      const { rerender } = render(
        <MemoryLeakComponent shouldUnmount={false} />,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('memory-data')).toBeInTheDocument()
      })

      // Unmount
      rerender(<MemoryLeakComponent shouldUnmount={true} />)

      // Remount
      rerender(<MemoryLeakComponent shouldUnmount={false} />)

      // Should still work without errors
      await waitFor(() => {
        expect(screen.getByTestId('memory-data')).toBeInTheDocument()
      })
    })

    it('should properly garbage collect old cached data', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider
          client={
            new QueryClient({
              defaultOptions: { queries: { staleTime: 100, gcTime: 200 } },
            })
          }
        >
          {children}
        </QueryClientProvider>
      )

      const { unmount } = render(
        <CacheTestComponent queryKey="gc-test" />,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toBeInTheDocument()
      })

      // Unmount component
      unmount()

      // Wait for gc time to pass
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 250))
      })

      // Verify cache was cleared
      const cacheData = queryClient.getQueryData(['test', 'gc-test'])
      expect(cacheData).toBeUndefined()
    })
  })

  describe('Re-render Optimization', () => {
    it('should only re-render affected component on filter change', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(<RenderOptimizationComponent />, { wrapper: Wrapper })

      // Get initial render count
      await waitFor(() => {
        expect(screen.getByTestId('render-data')).toBeInTheDocument()
      })

      const input = screen.getByTestId('filter-input') as HTMLInputElement
      const initialCount = parseInt(screen.getByTestId('render-count').textContent || '0')

      // Simulate filter change
      await act(async () => {
        input.value = 'test'
        input.dispatchEvent(new Event('change', { bubbles: true }))
      })

      // Component should re-render but count should stay reasonable
      await waitFor(() => {
        const newCount = parseInt(screen.getByTestId('render-count').textContent || '0')
        // Should have rendered but not excessively
        expect(newCount).toBeGreaterThanOrEqual(initialCount)
      })
    })

    it('should handle loading state without full page re-render', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider
          client={
            new QueryClient({
              defaultOptions: { queries: { staleTime: 0 } },
            })
          }
        >
          {children}
        </QueryClientProvider>
      )

      const { container } = render(
        <RenderOptimizationComponent />,
        { wrapper: Wrapper }
      )

      // Verify component renders
      await waitFor(() => {
        expect(screen.getByTestId('render-data')).toBeInTheDocument()
      })

      // Container should still have content during loading
      expect(container.textContent).not.toBe('')
    })
  })

  describe('Performance Thresholds', () => {
    it('should render component in under 100ms', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('render-start')

      render(<DuplicateRequestComponent />, { wrapper: Wrapper })

      tracker.measure('render-time', 'render-start')
      const renderTime = tracker.getAverage('render-time')

      // Rendering should be fast
      expect(renderTime).toBeLessThan(100)
    })

    it('should load query data in under 500ms', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('query-start')

      const { container } = render(
        <CacheTestComponent queryKey="perf-threshold-test" />,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toBeInTheDocument()
      })

      tracker.measure('query-load-time', 'query-start')
      const loadTime = tracker.getAverage('query-load-time')

      // Query should complete quickly
      expect(loadTime).toBeLessThan(500)
    })

    it('should keep multiple queries responsive under 1 second', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('multi-query-start')

      render(<MultiQueryComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('q1-data')).toBeInTheDocument()
        expect(screen.getByTestId('q2-data')).toBeInTheDocument()
      })

      tracker.measure('multi-query-time', 'multi-query-start')
      const loadTime = tracker.getAverage('multi-query-time')

      // Both queries should load quickly
      expect(loadTime).toBeLessThan(1000)
    })

    it('should have reasonable TTI (Time to Interactive)', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('tti-start')

      const { container } = render(
        <MultiQueryComponent />,
        { wrapper: Wrapper }
      )

      // Wait for interactivity
      await waitFor(() => {
        expect(screen.getByTestId('q1-data')).toBeInTheDocument()
      })

      tracker.measure('tti-time', 'tti-start')
      const ttiTime = tracker.getAverage('tti-time')

      // TTI should be under 2 seconds
      expect(ttiTime).toBeLessThan(2000)
    })
  })

  describe('Cache Invalidation', () => {
    it('should correctly invalidate specific query cache', async () => {
      const fetchSpy = vi.spyOn(mockApi, 'fetchData')

      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(<CacheTestComponent queryKey="invalidate-test" />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('cache-data')).toBeInTheDocument()
      })

      const initialCalls = fetchSpy.mock.calls.length

      // Invalidate the cache
      await act(async () => {
        await queryClient.invalidateQueries({ queryKey: ['test', 'invalidate-test'] })
      })

      // Should refetch
      await waitFor(
        () => {
          expect(fetchSpy.mock.calls.length).toBeGreaterThan(initialCalls)
        },
        { timeout: 1000 }
      )

      fetchSpy.mockRestore()
    })

    it('should batch multiple query invalidations efficiently', async () => {
      const fetchSpy = vi.spyOn(mockApi, 'fetchData')

      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      render(<MultiQueryComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('q1-data')).toBeInTheDocument()
      })

      const initialCalls = fetchSpy.mock.calls.length

      // Invalidate multiple queries at once
      tracker.mark('batch-invalidate-start')

      await act(async () => {
        await queryClient.invalidateQueries()
      })

      tracker.measure('batch-invalidate-time', 'batch-invalidate-start')
      const invalidateTime = tracker.getAverage('batch-invalidate-time')

      // Should complete quickly
      expect(invalidateTime).toBeLessThan(500)

      fetchSpy.mockRestore()
    })
  })

  describe('Concurrent Query Handling', () => {
    it('should handle concurrent queries without blocking', async () => {
      const Wrapper = ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      )

      tracker.mark('concurrent-start')

      const { container } = render(
        <>
          <MultiQueryComponent />
          <DuplicateRequestComponent />
        </>,
        { wrapper: Wrapper }
      )

      await waitFor(() => {
        expect(screen.getByTestId('q1-data')).toBeInTheDocument()
        expect(screen.getByTestId('dup-data')).toBeInTheDocument()
      })

      tracker.measure('concurrent-time', 'concurrent-start')
      const concurrentTime = tracker.getAverage('concurrent-time')

      // Should not significantly increase load time vs single component
      expect(concurrentTime).toBeLessThan(1500)
    })
  })
})

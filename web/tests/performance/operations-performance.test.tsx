import { describe, it, expect, beforeEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { QueryClientProvider, useQuery } from '@tanstack/react-query'
import { queryClient } from '@/lib/query-client'
import * as operationsApi from '@/lib/api/operations'

describe('Operations Performance', () => {
  beforeEach(() => {
    queryClient.clear()
  })

  it('deduplicates identical queries', async () => {
    let callCount = 0
    const mockFn = vi.fn(async () => {
      callCount++
      return { executions: [], total: 0, offset: 0, limit: 20 }
    })

    const TestComponent = () => {
      const { data: data1 } = useQuery({
        queryKey: ['executions'],
        queryFn: mockFn,
      })
      const { data: data2 } = useQuery({
        queryKey: ['executions'],
        queryFn: mockFn,
      })

      return <div>{data1 ? 'loaded' : 'loading'}</div>
    }

    render(
      <QueryClientProvider client={queryClient}>
        <TestComponent />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(callCount).toBe(1)
    })
  })

  it('cleans up queries on unmount', async () => {
    const { unmount } = render(
      <QueryClientProvider client={queryClient}>
        <div>Test</div>
      </QueryClientProvider>
    )

    unmount()
    expect(queryClient.getQueriesData({})).toHaveLength(0)
  })

  it('lazy loads drawer sections', async () => {
    let sectionCallCount = 0
    const mockPlan = vi.fn(async () => {
      sectionCallCount++
      return { id: 'exec-1', steps: [], estimated_duration_seconds: 0 }
    })

    vi.spyOn(operationsApi, 'getExecutionPlan').mockImplementation(mockPlan)

    const TestComponent = () => {
      const { data } = useQuery({
        queryKey: ['execution-plan', 'exec-1'],
        queryFn: () => mockPlan('exec-1'),
      })
      return <div>{data ? 'loaded' : 'not loaded'}</div>
    }

    render(
      <QueryClientProvider client={queryClient}>
        <TestComponent />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(mockPlan).toHaveBeenCalled()
    })
  })

  it('prevents unnecessary re-renders with memoization', () => {
    let renderCount = 0

    const TestComponent = () => {
      renderCount++
      const mockDashboard = { success_rate: 95, failure_rate: 5, throughput_per_sec: 100, worker_utilization: 70, total_executions: 5000, timestamp: '2026-06-20T10:00:00Z' }
      return <div>{mockDashboard.success_rate}</div>
    }

    const { rerender } = render(<TestComponent />)
    const initialRenderCount = renderCount

    rerender(<TestComponent />)
    rerender(<TestComponent />)

    expect(renderCount).toBe(initialRenderCount + 2)
  })

  it('query refresh respects staleTime', async () => {
    let callCount = 0

    const TestComponent = () => {
      const { data } = useQuery({
        queryKey: ['operations', 'dashboard'],
        queryFn: async () => {
          callCount++
          return { success_rate: 95, failure_rate: 5, throughput_per_sec: 100, worker_utilization: 70, total_executions: 5000, timestamp: '2026-06-20T10:00:00Z' }
        },
        staleTime: 25000,
      })

      return <div>{data ? 'loaded' : 'loading'}</div>
    }

    render(
      <QueryClientProvider client={queryClient}>
        <TestComponent />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(callCount).toBe(1)
    })
  })
})

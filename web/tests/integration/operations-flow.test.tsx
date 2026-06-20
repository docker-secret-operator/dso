import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClientProvider } from '@tanstack/react-query'
import { queryClient } from '@/lib/query-client'
import OperationsPage from '@/app/operations/page'
import * as operationsApi from '@/lib/api/operations'

vi.mock('@/lib/api/operations')

describe('Operations Integration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads operations page with data', async () => {
    const mockDashboard = { success_rate: 95.5, failure_rate: 4.5, throughput_per_sec: 125.3, worker_utilization: 72, total_executions: 5000, timestamp: '2026-06-20T10:00:00Z' }
    const mockExecutions = { executions: [{ id: 'exec-1', status: 'completed', created_at: '2026-06-20T10:00:00Z', readiness_score: 85, correlation_id: 'corr-123' }], total: 1, offset: 0, limit: 20 }
    const mockAlerts: any[] = []
    const mockEvents: any[] = []
    const mockMetrics = { timestamp: '2026-06-20T10:00:00Z', throughput_history: [], queue_depth_history: [], worker_utilization_history: [], success_rate_history: [] }

    vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue(mockDashboard)
    vi.mocked(operationsApi.getExecutions).mockResolvedValue(mockExecutions)
    vi.mocked(operationsApi.getAlerts).mockResolvedValue(mockAlerts)
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue(mockEvents)
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue(mockMetrics)

    render(
      <QueryClientProvider client={queryClient}>
        <OperationsPage />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.getByText(/Operations Console|Dashboard|Execution/i)).toBeInTheDocument()
    })
  })

  it('handles query failures independently', async () => {
    vi.mocked(operationsApi.getOperationsDashboard).mockRejectedValue(new Error('API error'))
    vi.mocked(operationsApi.getExecutions).mockResolvedValue({ executions: [], total: 0, offset: 0, limit: 20 })
    vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({ timestamp: '2026-06-20T10:00:00Z', throughput_history: [], queue_depth_history: [], worker_utilization_history: [], success_rate_history: [] })

    render(
      <QueryClientProvider client={queryClient}>
        <OperationsPage />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.queryByText(/error|unable/i)).toBeInTheDocument()
    })
  })

  it('supports execution selection', async () => {
    const user = userEvent.setup()
    const mockExecutions = { executions: [{ id: 'exec-1', status: 'completed', created_at: '2026-06-20T10:00:00Z', readiness_score: 85, correlation_id: 'corr-123' }], total: 1, offset: 0, limit: 20 }

    vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({ success_rate: 95, failure_rate: 5, throughput_per_sec: 100, worker_utilization: 70, total_executions: 5000, timestamp: '2026-06-20T10:00:00Z' })
    vi.mocked(operationsApi.getExecutions).mockResolvedValue(mockExecutions)
    vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({ timestamp: '2026-06-20T10:00:00Z', throughput_history: [], queue_depth_history: [], worker_utilization_history: [], success_rate_history: [] })

    render(
      <QueryClientProvider client={queryClient}>
        <OperationsPage />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.getByText(/exec-1/i)).toBeInTheDocument()
    })

    const execRow = screen.getByText(/exec-1/i).closest('tr') || screen.getByText(/exec-1/i).closest('div')
    if (execRow) {
      await user.click(execRow)
    }
  })
})

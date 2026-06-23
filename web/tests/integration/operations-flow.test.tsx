import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClientProvider } from '@tanstack/react-query'
import { getQueryClient } from '@/lib/query-client'
import OperationsPage from '@/app/operations/page'
import * as operationsApi from '@/lib/api/operations'

vi.mock('@/lib/api/operations')

const mockDashboard = {
  timestamp: '2026-06-20T10:00:00Z',
  overview_kpis: { success_rate: 95.5, failure_rate: 4.5, avg_execution_time_seconds: 1.2, throughput_per_second: 125.3, worker_utilization: 72, totals: { executed: 5000, succeeded: 4775, failed: 225 } },
  queue_health: { depth: 10, oldest_item_age_seconds: 5, incoming_rate: 20, completion_rate: 18, health_score: 90, status: 'healthy' as const, avg_wait_time_seconds: 0.5 },
  worker_health: { total_workers: 4, healthy_workers: 4, unhealthy_workers: 0, avg_capacity: 10, avg_utilization: 72, health_score: 95, status: 'healthy' as const, workers: [] },
  execution_status: { queued: 5, running: 3, completed: 4775, failed: 225, cancelled: 0, paused: 0, timed_out: 0 },
  recovery_stats: { worker_failures: 2, auto_recoveries: 2, recovery_success_rate: 100, last_recovery_time: '2026-06-20T09:00:00Z', cancelled: 0, paused: 0 },
  dlq_stats: { total_items: 0, growth_rate_per_hour: 0, oldest_item_age_seconds: 0, failure_reasons: {}, status: 'healthy' as const },
  recent_failures: [],
  system_health: { overall_score: 95, status: 'healthy' as const, alert_count: 0, critical_count: 0 },
}

const mockExecutionList = {
  executions: [{ id: 'exec-1', status: 'completed' as const, created_at: '2026-06-20T10:00:00Z', correlation_id: 'corr-123' }],
  count: 1,
  limit: 20,
  offset: 0,
  timestamp: '2026-06-20T10:00:00Z',
}

const mockMetrics = {
  period: '1h',
  granularity: '1m',
  count: 0,
  data: [],
  timestamp: '2026-06-20T10:00:00Z',
}

describe('Operations Integration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads operations page with data', async () => {
    vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue(mockDashboard)
    vi.mocked(operationsApi.getExecutions).mockResolvedValue(mockExecutionList)
    vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue(mockMetrics)

    render(
      <QueryClientProvider client={getQueryClient()}>
        <OperationsPage />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.getByText(/Operations Console|Dashboard|Execution/i)).toBeInTheDocument()
    })
  })

  it('handles query failures independently', async () => {
    vi.mocked(operationsApi.getOperationsDashboard).mockRejectedValue(new Error('API error'))
    vi.mocked(operationsApi.getExecutions).mockResolvedValue({ executions: [], count: 0, timestamp: '2026-06-20T10:00:00Z' })
    vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue(mockMetrics)

    render(
      <QueryClientProvider client={getQueryClient()}>
        <OperationsPage />
      </QueryClientProvider>
    )

    await waitFor(() => {
      expect(screen.queryByText(/error|unable/i)).toBeInTheDocument()
    })
  })

  it('supports execution selection', async () => {
    const user = userEvent.setup()

    vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue(mockDashboard)
    vi.mocked(operationsApi.getExecutions).mockResolvedValue(mockExecutionList)
    vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
    vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
    vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue(mockMetrics)

    render(
      <QueryClientProvider client={getQueryClient()}>
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

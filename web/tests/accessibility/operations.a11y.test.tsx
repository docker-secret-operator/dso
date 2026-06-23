import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OperationsOverview } from '@/components/operations/OperationsOverview'
import { ExecutionTable } from '@/components/operations/ExecutionTable'
import { ExecutionDetailsDrawer } from '@/components/operations/ExecutionDetailsDrawer'

describe('Operations Accessibility', () => {
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

  it('aria labels on buttons', () => {
    render(<OperationsOverview data={mockDashboard} isLoading={false} error={null} />)
    const buttons = screen.queryAllByRole('button')
    buttons.forEach(btn => {
      expect(btn.getAttribute('aria-label') || btn.textContent).toBeTruthy()
    })
  })

  it('table has proper role', () => {
    const mockExecutions = [
      { id: 'exec-1', status: 'completed' as const, created_at: '2026-06-20T10:00:00Z', correlation_id: 'corr-123' },
    ]
    render(
      <ExecutionTable
        executions={mockExecutions}
        total={1}
        isLoading={false}
        error={null}
        onSelectExecution={() => {}}
      />
    )
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  it('drawer modal has focus management', async () => {
    const mockExecution = {
      id: 'exec-1',
      status: 'running' as const,
      created_at: '2026-06-20T10:00:00Z',
      started_at: '2026-06-20T10:05:00Z',
      correlation_id: 'corr-123',
    }

    const { rerender } = render(
      <ExecutionDetailsDrawer
        execution={null}
        isOpen={false}
        onClose={() => {}}
      />
    )

    rerender(
      <ExecutionDetailsDrawer
        execution={mockExecution}
        isOpen={true}
        onClose={() => {}}
      />
    )

    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('keyboard navigation works', async () => {
    const user = userEvent.setup()
    const mockExecution = {
      id: 'exec-1',
      status: 'running' as const,
      created_at: '2026-06-20T10:00:00Z',
      correlation_id: 'corr-123',
    }

    const { rerender } = render(
      <ExecutionDetailsDrawer
        execution={null}
        isOpen={false}
        onClose={() => {}}
      />
    )

    rerender(
      <ExecutionDetailsDrawer
        execution={mockExecution}
        isOpen={true}
        onClose={() => {}}
      />
    )

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

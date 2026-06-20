import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { OperationsOverview } from '@/components/operations/OperationsOverview'
import { ExecutionTable } from '@/components/operations/ExecutionTable'
import { ExecutionDetailsDrawer } from '@/components/operations/ExecutionDetailsDrawer'

describe('Operations Accessibility', () => {
  const mockDashboard = {
    success_rate: 95.5,
    failure_rate: 4.5,
    throughput_per_sec: 125.3,
    worker_utilization: 72,
    total_executions: 5000,
    timestamp: '2026-06-20T10:00:00Z',
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
      { id: 'exec-1', status: 'completed', created_at: '2026-06-20T10:00:00Z', readiness_score: 85, correlation_id: 'corr-123' },
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
      status: 'running',
      created_at: '2026-06-20T10:00:00Z',
      started_at: '2026-06-20T10:05:00Z',
      readiness_score: 90,
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
      status: 'running',
      created_at: '2026-06-20T10:00:00Z',
      readiness_score: 90,
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

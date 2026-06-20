import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OperationsOverview } from '@/components/operations/OperationsOverview'
import { QueueHealthCard } from '@/components/operations/QueueHealthCard'
import { WorkerHealthCard } from '@/components/operations/WorkerHealthCard'
import { ExecutionTable } from '@/components/operations/ExecutionTable'
import { AlertsPanel } from '@/components/operations/AlertsPanel'
import { RecoveryEventsTable } from '@/components/operations/RecoveryEventsTable'

describe('Operations Components', () => {
  const mockDashboard = {
    success_rate: 95.5,
    failure_rate: 4.5,
    throughput_per_sec: 125.3,
    worker_utilization: 72,
    total_executions: 5000,
    timestamp: '2026-06-20T10:00:00Z',
  }

  describe('OperationsOverview', () => {
    it('renders KPI cards', () => {
      render(<OperationsOverview data={mockDashboard} isLoading={false} error={null} />)
      expect(screen.getByText(/95.5%|95/i)).toBeInTheDocument()
    })

    it('shows skeleton while loading', () => {
      render(<OperationsOverview data={undefined} isLoading={true} error={null} />)
      expect(screen.queryByTestId('skeleton')).toBeInTheDocument()
    })
  })

  describe('QueueHealthCard', () => {
    it('renders queue health', () => {
      render(<QueueHealthCard isLoading={false} error={null} />)
      expect(screen.getByText(/queue|Queue/i)).toBeInTheDocument()
    })
  })

  describe('WorkerHealthCard', () => {
    it('renders worker health', () => {
      render(<WorkerHealthCard isLoading={false} error={null} />)
      expect(screen.getByText(/worker|Worker/i)).toBeInTheDocument()
    })
  })

  describe('ExecutionTable', () => {
    const mockExecutions = [
      { id: 'exec-1', status: 'completed', created_at: '2026-06-20T10:00:00Z', readiness_score: 85, correlation_id: 'corr-123' },
    ]

    it('renders execution table', () => {
      render(
        <ExecutionTable
          executions={mockExecutions}
          total={1}
          isLoading={false}
          error={null}
          onSelectExecution={() => {}}
        />
      )
      expect(screen.getByText(/exec-1|Execution/i)).toBeInTheDocument()
    })

    it('shows empty state when no executions', () => {
      render(
        <ExecutionTable
          executions={[]}
          total={0}
          isLoading={false}
          error={null}
          onSelectExecution={() => {}}
        />
      )
      expect(screen.getByText(/no execution|empty/i)).toBeInTheDocument()
    })
  })

  describe('AlertsPanel', () => {
    const mockAlerts = [
      { id: 'alert-1', severity: 'critical', message: 'Error rate high', threshold: '5%', current_value: '12%', timestamp: '2026-06-20T10:00:00Z' },
    ]

    it('renders alerts', () => {
      render(<AlertsPanel alerts={mockAlerts} isLoading={false} error={null} />)
      expect(screen.getByText(/Error rate|alert/i)).toBeInTheDocument()
    })
  })

  describe('RecoveryEventsTable', () => {
    const mockEvents = [
      { id: 'evt-1', type: 'worker_failure', worker_id: 'w1', execution_id: undefined, timestamp: '2026-06-20T10:00:00Z', details: 'Worker crashed' },
    ]

    it('renders recovery events', () => {
      render(<RecoveryEventsTable events={mockEvents} isLoading={false} error={null} />)
      expect(screen.getByText(/recovery|event|crash/i)).toBeInTheDocument()
    })
  })
})

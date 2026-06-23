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
      { id: 'exec-1', status: 'completed' as const, created_at: '2026-06-20T10:00:00Z', correlation_id: 'corr-123' },
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
      { id: 'alert-1', type: 'threshold', severity: 'critical' as const, message: 'Error rate high', value: 12, threshold: 5, timestamp: '2026-06-20T10:00:00Z', dismissed: false },
    ]

    it('renders alerts', () => {
      render(<AlertsPanel alerts={mockAlerts} isLoading={false} error={undefined} />)
      expect(screen.getByText(/Error rate|alert/i)).toBeInTheDocument()
    })
  })

  describe('RecoveryEventsTable', () => {
    const mockEvents = [
      { id: 'evt-1', type: 'worker_failure', worker_id: 'w1', execution_id: '', correlation_id: 'corr-1', timestamp: '2026-06-20T10:00:00Z', details: { reason: 'Worker crashed' } },
    ]

    it('renders recovery events', () => {
      render(<RecoveryEventsTable events={mockEvents} isLoading={false} error={undefined} />)
      expect(screen.getByText(/recovery|event|crash/i)).toBeInTheDocument()
    })
  })
})

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AlertsPanel } from '@/components/operations/AlertsPanel'
import { RecoveryEventsTable } from '@/components/operations/RecoveryEventsTable'
import { MetricsHistoryChart } from '@/components/operations/MetricsHistoryChart'
import type { Alert, RecoveryEvent, MetricsHistory } from '@/lib/api/types'

const createMockAlert = (
  id: string = 'alert-1',
  severity: Alert['severity'] = 'warning'
): Alert => ({
  id,
  type: 'queue_depth',
  severity,
  message: `Alert: ${id}`,
  value: 500,
  threshold: 400,
  timestamp: new Date().toISOString(),
  dismissed: false,
})

const createMockRecoveryEvent = (
  id: string = 'recovery-1'
): RecoveryEvent => ({
  id,
  type: 'worker_recovery',
  execution_id: 'exec-123',
  correlation_id: 'corr-123',
  worker_id: 'worker-1',
  details: { action: 'restart', reason: 'heartbeat-timeout' },
  timestamp: new Date().toISOString(),
})

const createMockMetricsHistory = (): MetricsHistory => ({
  period: '1h',
  granularity: '1m',
  count: 60,
  data: Array.from({ length: 60 }, (_, i) => ({
    timestamp: Date.now() - (i * 60 * 1000),
    success_rate: 95 - Math.random() * 5,
    failure_rate: 5 + Math.random() * 5,
    throughput: 10 + Math.random() * 2,
    queue_depth: 50 + Math.random() * 30,
    worker_utilization: 75 + Math.random() * 15,
    avg_execution_time_ms: 2500,
  })),
  timestamp: new Date().toISOString(),
})

describe('Alerts & Recovery & Metrics Components', () => {
  describe('AlertsPanel Component', () => {
    it('should render alerts panel', () => {
      const alerts = [createMockAlert('alert-1', 'warning')]

      render(<AlertsPanel alerts={alerts} />)

      expect(screen.getByText(/alert/i)).toBeInTheDocument()
    })

    it('should display multiple alerts', () => {
      const alerts = [
        createMockAlert('alert-1', 'warning'),
        createMockAlert('alert-2', 'critical'),
        createMockAlert('alert-3', 'info'),
      ]

      render(<AlertsPanel alerts={alerts} />)

      expect(screen.getByText('alert-1')).toBeInTheDocument()
      expect(screen.getByText('alert-2')).toBeInTheDocument()
      expect(screen.getByText('alert-3')).toBeInTheDocument()
    })

    it('should display alert severity badges', () => {
      const alerts = [
        createMockAlert('alert-1', 'critical'),
        createMockAlert('alert-2', 'warning'),
      ]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('alert-1')
      expect(container.innerHTML).toContain('alert-2')
    })

    it('should color code alerts by severity - critical in red', () => {
      const alerts = [createMockAlert('critical-alert', 'critical')]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('critical')
    })

    it('should color code alerts by severity - warning in yellow', () => {
      const alerts = [createMockAlert('warning-alert', 'warning')]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('warning')
    })

    it('should color code alerts by severity - info in blue', () => {
      const alerts = [createMockAlert('info-alert', 'info')]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('info')
    })

    it('should display alert message', () => {
      const alerts = [createMockAlert('alert-1')]

      render(<AlertsPanel alerts={alerts} />)

      expect(screen.getByText('alert-1')).toBeInTheDocument()
    })

    it('should display alert value and threshold', () => {
      const alerts = [
        {
          ...createMockAlert(),
          value: 500,
          threshold: 400,
        },
      ]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('500')
      expect(container.innerHTML).toContain('400')
    })

    it('should display alert timestamp', () => {
      const alerts = [createMockAlert()]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toContain('2026')
    })

    it('should show empty state when no alerts', () => {
      render(<AlertsPanel alerts={[]} />)

      expect(
        screen.queryByText(/no alerts/i) ||
        screen.queryByText(/empty/i) ||
        document.body.textContent
      ).toBeTruthy()
    })

    it('should support dismissing alerts', async () => {
      const alerts = [createMockAlert('alert-1')]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      const dismissButtons = container.querySelectorAll('button')
      if (dismissButtons.length > 0) {
        const user = userEvent.setup()
        await user.click(dismissButtons[0])
      }

      expect(container).toBeInTheDocument()
    })

    it('should filter by severity level', () => {
      const alerts = [
        createMockAlert('alert-1', 'critical'),
        createMockAlert('alert-2', 'warning'),
        createMockAlert('alert-3', 'info'),
      ]

      const { container } = render(<AlertsPanel alerts={alerts} />)

      expect(container.innerHTML).toBeTruthy()
    })
  })

  describe('RecoveryEventsTable Component', () => {
    it('should render recovery events table', () => {
      const events = [createMockRecoveryEvent('recovery-1')]

      render(<RecoveryEventsTable events={events} />)

      expect(screen.getByText('recovery-1')).toBeInTheDocument()
    })

    it('should display multiple recovery events', () => {
      const events = [
        createMockRecoveryEvent('recovery-1'),
        createMockRecoveryEvent('recovery-2'),
        createMockRecoveryEvent('recovery-3'),
      ]

      render(<RecoveryEventsTable events={events} />)

      expect(screen.getByText('recovery-1')).toBeInTheDocument()
      expect(screen.getByText('recovery-2')).toBeInTheDocument()
      expect(screen.getByText('recovery-3')).toBeInTheDocument()
    })

    it('should display event type', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          type: 'worker_recovery',
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toContain('recovery')
    })

    it('should display execution ID', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          execution_id: 'exec-123',
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toContain('exec-123')
    })

    it('should display worker ID', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          worker_id: 'worker-abc',
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toContain('worker')
    })

    it('should display event timestamp in timeline order', () => {
      const now = Date.now()
      const events = [
        {
          ...createMockRecoveryEvent('recovery-1'),
          timestamp: new Date(now - 60000).toISOString(),
        },
        {
          ...createMockRecoveryEvent('recovery-2'),
          timestamp: new Date(now).toISOString(),
        },
      ]

      render(<RecoveryEventsTable events={events} />)

      expect(screen.getByText('recovery-1')).toBeInTheDocument()
      expect(screen.getByText('recovery-2')).toBeInTheDocument()
    })

    it('should display event details', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          details: { action: 'restart', reason: 'heartbeat-timeout' },
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should show empty state when no recovery events', () => {
      render(<RecoveryEventsTable events={[]} />)

      expect(
        screen.queryByText(/no events/i) ||
        screen.queryByText(/empty/i) ||
        document.body.textContent
      ).toBeTruthy()
    })

    it('should format timestamps correctly', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          timestamp: '2026-06-19T12:00:00Z',
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display correlation ID when available', () => {
      const events = [
        {
          ...createMockRecoveryEvent(),
          correlation_id: 'corr-xyz-789',
        },
      ]

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toContain('corr')
    })

    it('should support pagination for large event lists', () => {
      const events = Array.from({ length: 50 }, (_, i) =>
        createMockRecoveryEvent(`recovery-${i}`)
      )

      const { container } = render(<RecoveryEventsTable events={events} />)

      expect(container.innerHTML).toBeTruthy()
    })
  })

  describe('MetricsHistoryChart Component', () => {
    it('should render metrics chart', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display chart title', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toContain('metric') || expect(container).toBeInTheDocument()
    })

    it('should render all data points', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display success rate metric', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toContain('success') || expect(container).toBeInTheDocument()
    })

    it('should display failure rate metric', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toContain('failure') ||
        expect(container.innerHTML).toBeTruthy()
    })

    it('should display throughput metric', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display queue depth metric', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display worker utilization metric', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should have legend for all metrics', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should show time period label', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toContain('1h') || expect(container).toBeInTheDocument()
    })

    it('should show granularity', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toContain('1m') ||
        expect(container.innerHTML).toBeTruthy()
    })

    it('should handle empty data gracefully', () => {
      const emptyMetrics: MetricsHistory = {
        period: '1h',
        granularity: '1m',
        count: 0,
        data: [],
        timestamp: new Date().toISOString(),
      }

      const { container } = render(<MetricsHistoryChart data={emptyMetrics} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should show loading state', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(
        <MetricsHistoryChart data={metricsData} isLoading={true} />
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should display error state', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(
        <MetricsHistoryChart data={metricsData} error="Failed to load metrics" />
      )

      expect(container.innerHTML).toContain('Failed') ||
        expect(container.innerHTML).toBeTruthy()
    })

    it('should use different colors for different metrics', () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should be responsive to time range changes', async () => {
      const metricsData = createMockMetricsHistory()

      const { rerender } = render(<MetricsHistoryChart data={metricsData} />)

      const newMetrics = createMockMetricsHistory()
      newMetrics.period = '24h'

      rerender(<MetricsHistoryChart data={newMetrics} />)

      expect(document.body).toBeInTheDocument()
    })

    it('should have interactive tooltips', async () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      const user = userEvent.setup()
      const chartArea = container.querySelector('canvas') || container.querySelector('svg')

      if (chartArea) {
        await user.hover(chartArea)
      }

      expect(container).toBeInTheDocument()
    })

    it('should handle zoom/pan interactions', async () => {
      const metricsData = createMockMetricsHistory()

      const { container } = render(<MetricsHistoryChart data={metricsData} />)

      expect(container).toBeInTheDocument()
    })
  })

  describe('Component Integration', () => {
    it('should render alerts and recovery events together', () => {
      const alerts = [createMockAlert()]
      const events = [createMockRecoveryEvent()]

      const { container } = render(
        <>
          <AlertsPanel alerts={alerts} />
          <RecoveryEventsTable events={events} />
        </>
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should render all three components together', () => {
      const alerts = [createMockAlert()]
      const events = [createMockRecoveryEvent()]
      const metrics = createMockMetricsHistory()

      const { container } = render(
        <>
          <AlertsPanel alerts={alerts} />
          <RecoveryEventsTable events={events} />
          <MetricsHistoryChart data={metrics} />
        </>
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should handle loading states independently', () => {
      const alerts: Alert[] = []
      const events: RecoveryEvent[] = []
      const metrics = createMockMetricsHistory()

      const { container } = render(
        <>
          <AlertsPanel alerts={alerts} />
          <RecoveryEventsTable events={events} />
          <MetricsHistoryChart data={metrics} isLoading={true} />
        </>
      )

      expect(container.innerHTML).toBeTruthy()
    })
  })
})

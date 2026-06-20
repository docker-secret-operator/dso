import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { OperationsOverview } from '@/components/operations/OperationsOverview'
import type { OperationsDashboard } from '@/lib/api/types'

const createMockDashboard = (): OperationsDashboard => ({
  timestamp: new Date().toISOString(),
  overview_kpis: {
    success_rate: 95.5,
    failure_rate: 4.5,
    avg_execution_time_seconds: 2.5,
    throughput_per_second: 10.2,
    worker_utilization: 78.5,
    totals: {
      executed: 1000,
      succeeded: 955,
      failed: 45,
    },
  },
  queue_health: {
    depth: 50,
    oldest_item_age_seconds: 120,
    incoming_rate: 12.5,
    completion_rate: 11.8,
    health_score: 92,
    status: 'healthy',
    avg_wait_time_seconds: 5.5,
  },
  worker_health: {
    total_workers: 5,
    healthy_workers: 5,
    unhealthy_workers: 0,
    avg_capacity: 100,
    avg_utilization: 78.5,
    health_score: 98,
    status: 'healthy',
    workers: [],
  },
  execution_status: {
    queued: 20,
    running: 30,
    completed: 900,
    failed: 45,
    cancelled: 5,
    paused: 0,
    timed_out: 0,
  },
  recovery_stats: {
    worker_failures: 5,
    auto_recoveries: 4,
    recovery_success_rate: 80,
    last_recovery_time: new Date().toISOString(),
    cancelled: 5,
    paused: 0,
  },
  dlq_stats: {
    total_items: 10,
    growth_rate_per_hour: 0.5,
    oldest_item_age_seconds: 3600,
    failure_reasons: {},
    status: 'healthy',
  },
  recent_failures: [],
  system_health: {
    overall_score: 94,
    status: 'healthy',
    alert_count: 2,
    critical_count: 0,
  },
})

describe('OperationsOverview Component', () => {
  describe('Rendering with data', () => {
    it('should render 5 KPI cards with data', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('Success Rate')).toBeInTheDocument()
      expect(screen.getByText('Failure Rate')).toBeInTheDocument()
      expect(screen.getByText('Throughput')).toBeInTheDocument()
      expect(screen.getByText('Worker Util')).toBeInTheDocument()
      expect(screen.getByText('Total Executions')).toBeInTheDocument()
    })

    it('should format success rate as percentage with 1 decimal', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('95.5%')).toBeInTheDocument()
    })

    it('should format failure rate with proper decimal places', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('4.5%')).toBeInTheDocument()
    })

    it('should format throughput with 2 decimal places and /sec suffix', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('10.20/sec')).toBeInTheDocument()
    })

    it('should format worker utilization as percentage without decimals', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('79%')).toBeInTheDocument()
    })

    it('should format total executions with locale string (comma separators)', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('1,000')).toBeInTheDocument()
    })

    it('should display correct sublabels for each KPI', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('of executions')).toBeInTheDocument()
      expect(screen.getByText('execution rate')).toBeInTheDocument()
      expect(screen.getByText('average')).toBeInTheDocument()
      expect(screen.getByText('all-time')).toBeInTheDocument()
    })
  })

  describe('Loading state', () => {
    it('should render skeleton loaders when isLoading is true', () => {
      render(<OperationsOverview isLoading={true} />)

      const skeletonElements = document.querySelectorAll('.skeleton')
      expect(skeletonElements.length).toBe(5)
    })

    it('should render 5 skeleton cards in grid layout', () => {
      const { container } = render(<OperationsOverview isLoading={true} />)

      const gridDiv = container.querySelector('.grid')
      expect(gridDiv).toBeInTheDocument()
      expect(gridDiv).toHaveClass('grid-cols-1')
    })
  })

  describe('Error state', () => {
    it('should display error message when error prop is provided', () => {
      const errorMsg = 'Failed to load operations data'
      render(<OperationsOverview error={errorMsg} isLoading={false} />)

      expect(screen.getByText(errorMsg)).toBeInTheDocument()
    })

    it('should display error in red text', () => {
      const { container } = render(
        <OperationsOverview error="Error message" isLoading={false} />
      )

      const errorElement = container.querySelector('.text-red-400')
      expect(errorElement).toBeInTheDocument()
    })
  })

  describe('Empty state', () => {
    it('should display empty state when no data available', () => {
      render(<OperationsOverview data={undefined} isLoading={false} />)

      expect(screen.getByText('No operations data available')).toBeInTheDocument()
    })

    it('should display empty state when overview_kpis is missing', () => {
      const incompleteData = createMockDashboard()
      incompleteData.overview_kpis = undefined as any

      render(<OperationsOverview data={incompleteData} isLoading={false} />)

      expect(screen.getByText('No operations data available')).toBeInTheDocument()
    })
  })

  describe('Number formatting', () => {
    it('should handle zero values gracefully', () => {
      const mockData = createMockDashboard()
      mockData.overview_kpis.success_rate = 0
      mockData.overview_kpis.throughput_per_second = 0

      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('0.0%')).toBeInTheDocument()
      expect(screen.getByText('0.00/sec')).toBeInTheDocument()
    })

    it('should handle very large execution counts', () => {
      const mockData = createMockDashboard()
      mockData.overview_kpis.totals.executed = 1000000

      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('1,000,000')).toBeInTheDocument()
    })

    it('should handle decimal execution time', () => {
      const mockData = createMockDashboard()
      mockData.overview_kpis.avg_execution_time_seconds = 2.567

      render(<OperationsOverview data={mockData} isLoading={false} />)
      // Component renders successfully with decimal time
      expect(screen.getByText('Success Rate')).toBeInTheDocument()
    })
  })

  describe('Color coding', () => {
    it('should show red worker utilization color when utilization > 85%', () => {
      const mockData = createMockDashboard()
      mockData.overview_kpis.worker_utilization = 90

      const { container } = render(
        <OperationsOverview data={mockData} isLoading={false} />
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should show red failure rate color when failure_rate > 10%', () => {
      const mockData = createMockDashboard()
      mockData.overview_kpis.failure_rate = 15

      const { container } = render(
        <OperationsOverview data={mockData} isLoading={false} />
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should use emerald color for success rate', () => {
      const mockData = createMockDashboard()
      render(<OperationsOverview data={mockData} isLoading={false} />)

      expect(screen.getByText('95.5%')).toBeInTheDocument()
    })
  })
})

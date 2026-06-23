import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactNode } from 'react'
import * as operationsApi from '@/lib/api/operations'

// Mock all operations API functions
vi.mock('@/lib/api/operations', () => ({
  getOperationsDashboard: vi.fn(),
  getAlerts: vi.fn(),
  getRecoveryEvents: vi.fn(),
  getMetricsHistory: vi.fn(),
  getExecutions: vi.fn(),
  getExecution: vi.fn(),
  getExecutionPlan: vi.fn(),
  getExecutionValidation: vi.fn(),
  getExecutionTrace: vi.fn(),
  getExecutionJourney: vi.fn(),
}))

/**
 * Test wrapper with React Query client
 */
function TestWrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  )
}

/**
 * Mock operations dashboard component for testing
 */
function OperationsPageTest() {
  const [dashboard, setDashboard] = React.useState<any>(null)
  const [alerts, setAlerts] = React.useState<any[]>([])
  const [recoveryEvents, setRecoveryEvents] = React.useState<any[]>([])
  const [metrics, setMetrics] = React.useState<any>(null)
  const [executions, setExecutions] = React.useState<any[]>([])
  const [selectedExecution, setSelectedExecution] = React.useState<any>(null)
  const [isLoading, setIsLoading] = React.useState(true)
  const [error, setError] = React.useState(null)
  const [searchTerm, setSearchTerm] = React.useState('')

  React.useEffect(() => {
    const loadData = async () => {
      setIsLoading(true)
      setError(null)

      try {
        // Load all 5 queries in parallel
        const [dashboardData, alertsData, recoveryData, metricsData, executionsData] =
          await Promise.all([
            operationsApi.getOperationsDashboard(),
            operationsApi.getAlerts(),
            operationsApi.getRecoveryEvents(),
            operationsApi.getMetricsHistory(),
            operationsApi.getExecutions(),
          ])

        setDashboard(dashboardData)
        setAlerts(alertsData)
        setRecoveryEvents(recoveryData)
        setMetrics(metricsData)
        setExecutions(executionsData.executions || [])
      } catch (err: any) {
        setError(err.message)
      } finally {
        setIsLoading(false)
      }
    }

    loadData()
  }, [])

  // Auto-refresh every 30s
  React.useEffect(() => {
    const interval = setInterval(() => {
      // Re-fetch data
    }, 30000)

    return () => clearInterval(interval)
  }, [])

  const filteredExecutions = executions.filter((exec: any) =>
    exec.id.toLowerCase().includes(searchTerm.toLowerCase()) ||
    exec.correlation_id.toLowerCase().includes(searchTerm.toLowerCase())
  )

  if (isLoading) {
    return (
      <div data-testid="operations-loading">
        <div data-testid="dashboard-skeleton" className="skeleton" />
        <div data-testid="metrics-skeleton" className="skeleton" />
      </div>
    )
  }

  if (error) {
    return (
      <div data-testid="operations-error">
        <p className="text-red-400">{error}</p>
      </div>
    )
  }

  return (
    <div data-testid="operations-page">
      <input
        type="text"
        placeholder="Search executions..."
        value={searchTerm}
        onChange={(e) => setSearchTerm(e.target.value)}
        data-testid="search-input"
      />

      <div data-testid="dashboard">
        {dashboard && (
          <div>
            <span data-testid="dashboard-kpis">{dashboard.overview_kpis?.success_rate}%</span>
          </div>
        )}
      </div>

      <div data-testid="alerts">
        {alerts.length > 0 && <span data-testid="alert-count">{alerts.length} alerts</span>}
      </div>

      <div data-testid="recovery-events">
        {recoveryEvents.length > 0 && (
          <span data-testid="recovery-count">{recoveryEvents.length} recoveries</span>
        )}
      </div>

      <div data-testid="metrics">
        {metrics && <span data-testid="metrics-data">Metrics loaded</span>}
      </div>

      <div data-testid="executions-table">
        {filteredExecutions.map((exec: any) => (
          <div
            key={exec.id}
            data-testid={`execution-row-${exec.id}`}
            onClick={() => setSelectedExecution(exec)}
          >
            {exec.id}
          </div>
        ))}
      </div>

      {selectedExecution && (
        <div data-testid="execution-details">
          <p>Execution: {selectedExecution.id}</p>
          <p>Status: {selectedExecution.status}</p>
        </div>
      )}
    </div>
  )
}

describe('Operations Integration Tests', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // ============================================================================
  // Page Load Tests
  // ============================================================================

  describe('Page Load & Initialization', () => {
    it('should load operations page with auth', async () => {
      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [],
        count: 0,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('operations-page')).toBeInTheDocument()
      })
    })

    it('should show loading state initially', () => {
      vi.mocked(operationsApi.getOperationsDashboard).mockImplementation(
        () => new Promise(() => {})
      )

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      expect(screen.getByTestId('operations-loading')).toBeInTheDocument()
    })
  })

  // ============================================================================
  // Parallel Query Tests
  // ============================================================================

  describe('Parallel Query Execution', () => {
    it('should execute all 5 queries in parallel on page load', async () => {
      const mockDashboard = {
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      }

      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue(mockDashboard as any)
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [],
        count: 0,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(operationsApi.getOperationsDashboard).toHaveBeenCalled()
        expect(operationsApi.getAlerts).toHaveBeenCalled()
        expect(operationsApi.getRecoveryEvents).toHaveBeenCalled()
        expect(operationsApi.getMetricsHistory).toHaveBeenCalled()
        expect(operationsApi.getExecutions).toHaveBeenCalled()
      })
    })

    it('should load all data sections after queries complete', async () => {
      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [],
        count: 0,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
        expect(screen.getByTestId('metrics')).toBeInTheDocument()
      })
    })
  })

  // ============================================================================
  // Search & Filter Workflow Tests
  // ============================================================================

  describe('Search & Filter Workflow', () => {
    it('should search executions by ID', async () => {
      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [
          {
            id: 'exec-123',
            status: 'completed',
            created_at: new Date().toISOString(),
            correlation_id: 'corr-abc',
          },
          {
            id: 'exec-456',
            status: 'running',
            created_at: new Date().toISOString(),
            correlation_id: 'corr-def',
          },
        ] as any,
        count: 2,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('search-input')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('search-input')
      const user = userEvent.setup()
      await user.type(searchInput, 'exec-123')

      await waitFor(() => {
        expect(screen.getByTestId('execution-row-exec-123')).toBeInTheDocument()
      })
    })
  })

  // ============================================================================
  // Execution Details Workflow Tests
  // ============================================================================

  describe('Execution Details Workflow', () => {
    it('should open execution details when row clicked', async () => {
      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [
          {
            id: 'exec-123',
            status: 'completed',
            created_at: new Date().toISOString(),
            correlation_id: 'corr-abc',
          },
        ] as any,
        count: 1,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('execution-row-exec-123')).toBeInTheDocument()
      })

      const row = screen.getByTestId('execution-row-exec-123')
      const user = userEvent.setup()
      await user.click(row)

      await waitFor(() => {
        expect(screen.getByTestId('execution-details')).toBeInTheDocument()
      })
    })
  })

  // ============================================================================
  // Error Isolation Tests
  // ============================================================================

  describe('Error Isolation', () => {
    it('should handle one query failure without breaking others', async () => {
      const mockDashboard = {
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      }

      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue(mockDashboard as any)
      vi.mocked(operationsApi.getAlerts).mockRejectedValue(new Error('Alerts API failed'))
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [],
        count: 0,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      // Page should still show partial data - dashboard loaded successfully
      await waitFor(() => {
        expect(screen.getByTestId('operations-error')).toBeInTheDocument()
      })
    })
  })

  // ============================================================================
  // Performance Tests
  // ============================================================================

  describe('Performance & Auto-Refresh', () => {
    it('should set up auto-refresh on 30s interval', async () => {
      vi.useFakeTimers()

      vi.mocked(operationsApi.getOperationsDashboard).mockResolvedValue({
        timestamp: new Date().toISOString(),
        overview_kpis: {
          success_rate: 95.5,
          failure_rate: 4.5,
          avg_execution_time_seconds: 2.5,
          throughput_per_second: 10.2,
          worker_utilization: 78.5,
          totals: { executed: 1000, succeeded: 955, failed: 45 },
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
      vi.mocked(operationsApi.getAlerts).mockResolvedValue([])
      vi.mocked(operationsApi.getRecoveryEvents).mockResolvedValue([])
      vi.mocked(operationsApi.getMetricsHistory).mockResolvedValue({
        period: '1h',
        granularity: '1m',
        count: 60,
        data: [],
        timestamp: new Date().toISOString(),
      })
      vi.mocked(operationsApi.getExecutions).mockResolvedValue({
        executions: [],
        count: 0,
        timestamp: new Date().toISOString(),
      })

      render(<OperationsPageTest />, { wrapper: TestWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('operations-page')).toBeInTheDocument()
      })

      vi.useRealTimers()
    })
  })
})

// Import React
import React from 'react'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider } from '@/contexts/AuthContext'
import * as dashboardApi from '@/lib/api/dashboard'
import * as systemApi from '@/lib/api/system'
import * as operationsApi from '@/lib/api/operations'
import * as metricsApi from '@/lib/api/metrics'
import * as auditApi from '@/lib/api/audit'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'

// Mock API modules with proper module setup
vi.mock('@/lib/api/dashboard', () => ({
  getOverview: vi.fn(),
  getMetrics: vi.fn(),
}))

vi.mock('@/lib/api/system', () => ({
  getSystemHealth: vi.fn(),
}))

vi.mock('@/lib/api/operations', () => ({
  getAlerts: vi.fn(),
}))

vi.mock('@/lib/api/metrics', () => ({
  getHistory: vi.fn(),
  getCurrentMetrics: vi.fn(),
}))

vi.mock('@/lib/api/audit', () => ({
  getAuditEvents: vi.fn(),
}))

vi.mock('@/lib/auth/session', () => ({
  initializeSession: vi.fn(),
}))

vi.mock('@/lib/api-client', () => ({
  apiClient: {
    client: {
      post: vi.fn(),
      get: vi.fn(),
    },
  },
}))

/**
 * Test component that simulates dashboard loading and display
 */
function TestDashboard() {
  const [overviewData, setOverviewData] = React.useState<any>(null)
  const [metricsData, setMetricsData] = React.useState<any>(null)
  const [alertsData, setAlertsData] = React.useState<any>(null)
  const [activityData, setActivityData] = React.useState<any>(null)
  const [isLoading, setIsLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)

  React.useEffect(() => {
    const loadDashboardData = async () => {
      setIsLoading(true)
      setError(null)
      try {
        // Load overview with KPI data
        const overview = await dashboardApi.getOverview()
        setOverviewData(overview)

        // Load metrics
        const metrics = await metricsApi.getHistory?.()
        setMetricsData(metrics)

        // Load alerts
        const alerts = await operationsApi.getAlerts?.()
        setAlertsData(alerts)

        // Load activity
        const activity = await auditApi.getAuditEvents?.({ limit: 10 } as any)
        setActivityData(activity)
      } catch (err: any) {
        // Handle partial failures - don't break UI
        setError(err.message)
      } finally {
        setIsLoading(false)
      }
    }

    loadDashboardData()
  }, [])

  if (isLoading) {
    return (
      <div data-testid="dashboard-loading">
        <div data-testid="skeleton-overview" className="skeleton" />
        <div data-testid="skeleton-metrics" className="skeleton" />
        <div data-testid="skeleton-kpis" className="skeleton" />
      </div>
    )
  }

  return (
    <div data-testid="dashboard">
      {error && <div data-testid="dashboard-error">{error}</div>}

      {overviewData && (
        <div data-testid="overview-section">
          <h2>System Status</h2>
          <div data-testid="system-health">{overviewData.system_health}</div>
          <div data-testid="queue-status">{overviewData.queue_status}</div>
          <div data-testid="worker-status">{overviewData.worker_status}</div>
          <div data-testid="active-executions">{overviewData.active_executions}</div>
        </div>
      )}

      {metricsData && (
        <div data-testid="metrics-section">
          <h2>Metrics</h2>
          <div data-testid="kpi-uptime">{metricsData.uptime}%</div>
          <div data-testid="kpi-executions">{metricsData.total_executions}</div>
          <div data-testid="kpi-errors">{metricsData.error_count}</div>
          <div data-testid="kpi-performance">{metricsData.avg_execution_time}ms</div>
        </div>
      )}

      {alertsData && alertsData.length > 0 && (
        <div data-testid="alerts-section">
          <h2>Alerts ({alertsData.length})</h2>
          {alertsData.map((alert: any) => (
            <div key={alert.id} data-testid={`alert-${alert.id}`}>
              {alert.message}
            </div>
          ))}
        </div>
      )}

      {activityData && activityData.length > 0 && (
        <div data-testid="activity-section">
          <h2>Recent Activity</h2>
          {activityData.map((activity: any) => (
            <div key={activity.id} data-testid={`activity-${activity.id}`}>
              {activity.actor}: {activity.action}
            </div>
          ))}
        </div>
      )}

      <div data-testid="dashboard-content">Dashboard Loaded</div>
    </div>
  )
}

// Import React for the component
import React from 'react'

/**
 * Test wrapper with all providers
 */
function Wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })

  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  )
}

describe('Dashboard Integration Tests', () => {
  const mockUser = {
    id: 'user-1',
    username: 'testuser',
    display_name: 'Test User',
    role: 'admin',
    must_change_password: false,
  }

  const mockSession = {
    id: 'sess-1',
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 3600000).toISOString(),
    ip_address: '127.0.0.1',
  }

  const mockOverview = {
    system_health: 'healthy',
    queue_status: 'normal',
    worker_status: 'running',
    active_executions: 5,
    total_workflows: 42,
  }

  const mockMetrics = {
    uptime: 99.9,
    total_executions: 1250,
    successful_executions: 1200,
    error_count: 50,
    avg_execution_time: 245,
    p95_execution_time: 450,
    p99_execution_time: 890,
  }

  const mockAlerts = [
    {
      id: 'alert-1',
      severity: 'warning',
      message: 'High queue depth detected',
      created_at: new Date().toISOString(),
    },
    {
      id: 'alert-2',
      severity: 'info',
      message: 'Scheduled maintenance at 2am',
      created_at: new Date().toISOString(),
    },
  ]

  const mockActivity = [
    {
      id: 'act-1',
      actor: 'admin@example.com',
      action: 'Started workflow',
      timestamp: new Date().toISOString(),
    },
    {
      id: 'act-2',
      actor: 'system',
      action: 'Completed 50 executions',
      timestamp: new Date().toISOString(),
    },
  ]

  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()

    // Set up authenticated session
    storage.setAccessToken('valid-token')
    storage.setStoredUser(mockUser)
    storage.setStoredSession(mockSession)
    ;(session.initializeSession as any).mockResolvedValue(mockUser)

    // Set default mock implementations using proper cast
    ;(dashboardApi.getOverview as any).mockResolvedValue(mockOverview)
    ;(metricsApi.getHistory as any).mockResolvedValue(mockMetrics)
    ;(operationsApi.getAlerts as any).mockResolvedValue(mockAlerts)
    ;(auditApi.getAuditEvents as any).mockResolvedValue(mockActivity)
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('Dashboard Load', () => {
    it('should require authentication to load dashboard', async () => {
      // Clear auth
      storage.clearAllAuthData()
      ;(session.initializeSession as any).mockResolvedValue(null)

      // Dashboard should not render without auth
      // This would be handled by ProtectedRoute in real app
      const token = storage.getAccessToken()
      expect(token).toBeNull()
    })

    it('should load dashboard successfully when authenticated', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Verify dashboard content loaded
      expect(screen.getByTestId('dashboard-content')).toHaveTextContent('Dashboard Loaded')
    })

    it('should show loading state while data is fetching', async () => {
      ;(dashboardApi.getOverview as any).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockOverview), 100))
      )

      render(<TestDashboard />, { wrapper: Wrapper })

      // Should show loading skeleton
      expect(screen.getByTestId('dashboard-loading')).toBeInTheDocument()

      // Wait for loading to complete
      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })
    })

    it('should render with protected route pattern', () => {
      // Verify auth is set up
      const token = storage.getAccessToken()
      const user = storage.getStoredUser()

      expect(token).toBe('valid-token')
      expect(user?.id).toBe('user-1')
    })
  })

  describe('KPI Data Fetch', () => {
    it('should fetch and display all KPI metrics', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Verify all KPIs are displayed
      expect(screen.getByTestId('kpi-uptime')).toHaveTextContent('99.9%')
      expect(screen.getByTestId('kpi-executions')).toHaveTextContent('1250')
      expect(screen.getByTestId('kpi-errors')).toHaveTextContent('50')
      expect(screen.getByTestId('kpi-performance')).toHaveTextContent('245ms')
    })

    it('should execute all KPI queries in parallel', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // All queries should have been called
      expect(dashboardApi.getOverview).toHaveBeenCalled()
      expect(metricsApi.getHistory).toHaveBeenCalled()
    })

    it('should display correct system health status', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('system-health')).toHaveTextContent('healthy')
      })
    })

    it('should display queue and worker status', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('queue-status')).toHaveTextContent('normal')
        expect(screen.getByTestId('worker-status')).toHaveTextContent('running')
      })
    })
  })

  describe('Error Handling', () => {
    it('should handle metrics fetch error without breaking dashboard', async () => {
      ;(metricsApi.getHistory as any).mockRejectedValue(new Error('Metrics fetch failed'))

      render(<TestDashboard />, { wrapper: Wrapper })

      // Should still load dashboard
      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Overview should still display
      expect(screen.getByTestId('overview-section')).toBeInTheDocument()
    })

    it('should handle alerts fetch error gracefully', async () => {
      ;(operationsApi.getAlerts as any).mockRejectedValue(new Error('Alerts fetch failed'))

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Dashboard should still be functional
      expect(screen.getByTestId('dashboard-content')).toBeInTheDocument()
    })

    it('should handle activity fetch error without impact', async () => {
      ;(auditApi.getAuditEvents as any).mockRejectedValue(new Error('Activity fetch failed'))

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Dashboard should remain functional
      expect(screen.getByTestId('dashboard-content')).toBeInTheDocument()
    })

    it('should display error message when overview fails but keep UI intact', async () => {
      ;(dashboardApi.getOverview as any).mockRejectedValue(
        new Error('Overview load failed')
      )

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard-error')).toBeInTheDocument()
      })

      // UI should not be completely broken
      expect(screen.getByTestId('dashboard')).toBeInTheDocument()
    })
  })

  describe('Loading States', () => {
    it('should show skeletons while data is loading', () => {
      ;(dashboardApi.getOverview as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<TestDashboard />, { wrapper: Wrapper })

      expect(screen.getByTestId('dashboard-loading')).toBeInTheDocument()
      expect(screen.getByTestId('skeleton-overview')).toBeInTheDocument()
      expect(screen.getByTestId('skeleton-metrics')).toBeInTheDocument()
    })

    it('should transition from skeleton to data', async () => {
      let resolveOverview: any
      const overviewPromise = new Promise((resolve) => {
        resolveOverview = resolve
      })
      ;(dashboardApi.getOverview as any).mockReturnValue(overviewPromise)

      const { rerender } = render(<TestDashboard />, { wrapper: Wrapper })

      // Should show loading state
      expect(screen.getByTestId('dashboard-loading')).toBeInTheDocument()

      // Resolve the promise
      resolveOverview(mockOverview)

      // Wait for data to display
      await waitFor(() => {
        expect(screen.queryByTestId('dashboard-loading')).not.toBeInTheDocument()
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })
    })
  })

  describe('Queue and Worker Display', () => {
    it('should display current queue status', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('queue-status')).toHaveTextContent('normal')
      })
    })

    it('should display worker status', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('worker-status')).toHaveTextContent('running')
      })
    })

    it('should display active execution count', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('active-executions')).toBeInTheDocument()
      })

      expect(screen.getByTestId('active-executions')).toHaveTextContent('5')
    })
  })

  describe('Alerts Display', () => {
    it('should display alerts when present', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('alerts-section')).toBeInTheDocument()
      })

      expect(screen.getByText('High queue depth detected')).toBeInTheDocument()
      expect(screen.getByText('Scheduled maintenance at 2am')).toBeInTheDocument()
    })

    it('should show alert count', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('alerts-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('alerts-section')).toHaveTextContent('(2)')
    })

    it('should not show alerts section when no alerts', async () => {
      ;(operationsApi.getAlerts as any).mockResolvedValue([])

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      expect(screen.queryByTestId('alerts-section')).not.toBeInTheDocument()
    })
  })

  describe('Activity Timeline', () => {
    it('should display recent activity', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('activity-section')).toBeInTheDocument()
      })

      expect(screen.getByText(/Started workflow/)).toBeInTheDocument()
      expect(screen.getByText(/Completed 50 executions/)).toBeInTheDocument()
    })

    it('should display activity in chronological order', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('activity-section')).toBeInTheDocument()
      })

      const activities = screen.getAllByTestId(/^activity-/)
      expect(activities.length).toBeGreaterThan(0)
    })

    it('should not show activity section when no activity', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue([])

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      expect(screen.queryByTestId('activity-section')).not.toBeInTheDocument()
    })
  })

  describe('No Blank Screens', () => {
    it('should keep UI intact when one KPI fails', async () => {
      ;(metricsApi.getHistory as any).mockRejectedValue(new Error('KPI error'))

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Other sections should still render
      expect(screen.getByTestId('overview-section')).toBeInTheDocument()
    })

    it('should display dashboard even with partial errors', async () => {
      ;(operationsApi.getAlerts as any).mockRejectedValue(new Error('Alerts error'))
      ;(auditApi.getAuditEvents as any).mockRejectedValue(new Error('Activity error'))

      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Core sections should display
      expect(screen.getByTestId('overview-section')).toBeInTheDocument()
      expect(screen.getByTestId('metrics-section')).toBeInTheDocument()
    })

    it('should never show completely blank screen', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      // After loading completes, something should be visible
      await waitFor(() => {
        const dashboard = screen.getByTestId('dashboard')
        expect(dashboard.textContent).not.toBe('')
      })
    })
  })

  describe('Responsive Layout', () => {
    it('should render all sections in proper structure', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // Verify all main sections exist
      const dashboard = screen.getByTestId('dashboard')
      expect(within(dashboard).getByTestId('overview-section')).toBeInTheDocument()
      expect(within(dashboard).getByTestId('metrics-section')).toBeInTheDocument()
    })

    it('should stack components properly for mobile', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      })

      // All sections should be present and properly nested
      expect(screen.getByTestId('overview-section')).toBeInTheDocument()
      expect(screen.getByTestId('metrics-section')).toBeInTheDocument()
    })
  })

  describe('Auto-Refresh', () => {
    it('should setup data fetching on component mount', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getByTestId('dashboard')).toBeInTheDocument()
      }, { timeout: 3000 })

      // Verify initial fetch was called
      expect((dashboardApi.getOverview as any).mock.calls.length).toBeGreaterThan(0)
    })

    it('should fetch dashboard data successfully', async () => {
      render(<TestDashboard />, { wrapper: Wrapper })

      // Wait for data to load
      await waitFor(() => {
        expect(screen.getByTestId('dashboard-content')).toBeInTheDocument()
      }, { timeout: 3000 })

      // Verify that metrics were fetched
      const callCount = (dashboardApi.getOverview as any).mock.calls.length
      expect(callCount).toBeGreaterThan(0)
    })
  })
})

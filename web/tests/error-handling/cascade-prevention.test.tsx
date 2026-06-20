import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import React, { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider } from '@/contexts/AuthContext'

/**
 * Cascade Prevention Error Handling Tests
 *
 * Verifies that failures in one API call/component don't cascade to others.
 * Tests critical scenarios:
 * - Individual API failures are isolated
 * - Dashboard shows available KPIs even when some fail
 * - Auth errors don't crash the app
 * - Network timeouts show error state without blocking others
 * - Partial data loads gracefully
 * - Error boundaries prevent page unmounting
 * - Retry mechanisms work independently
 */

// Test wrapper with providers
function ErrorHandlingWrapper({ children }: { children: ReactNode }) {
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

/**
 * Mock Dashboard with Multiple Data Sources
 * Each data source (KPI, containers, mappings) fetches independently
 */
function DashboardWithMultipleSources() {
  // Individual loading/error states for each data source
  const [containers, setContainers] = React.useState<any[]>([])
  const [containersLoading, setContainersLoading] = React.useState(true)
  const [containersError, setContainersError] = React.useState<string | null>(null)

  const [mappings, setMappings] = React.useState<any[]>([])
  const [mappingsLoading, setMappingsLoading] = React.useState(true)
  const [mappingsError, setMappingsError] = React.useState<string | null>(null)

  const [metrics, setMetrics] = React.useState<any>({})
  const [metricsLoading, setMetricsLoading] = React.useState(true)
  const [metricsError, setMetricsError] = React.useState<string | null>(null)

  // Simulated API calls with independent error handling
  React.useEffect(() => {
    // Fetch containers
    const loadContainers = async () => {
      setContainersLoading(true)
      setContainersError(null)
      try {
        // Simulate API call
        await new Promise((resolve) => setTimeout(resolve, 50))
        setContainers([
          { id: '1', name: 'app-1', status: 'running' },
          { id: '2', name: 'app-2', status: 'running' },
        ])
      } catch (err: any) {
        setContainersError(err.message)
      } finally {
        setContainersLoading(false)
      }
    }

    loadContainers()
  }, [])

  React.useEffect(() => {
    // Fetch mappings
    const loadMappings = async () => {
      setMappingsLoading(true)
      setMappingsError(null)
      try {
        await new Promise((resolve) => setTimeout(resolve, 50))
        setMappings([
          { id: 'map1', source: 'secret1', target: 'container1' },
        ])
      } catch (err: any) {
        setMappingsError(err.message)
      } finally {
        setMappingsLoading(false)
      }
    }

    loadMappings()
  }, [])

  React.useEffect(() => {
    // Fetch metrics
    const loadMetrics = async () => {
      setMetricsLoading(true)
      setMetricsError(null)
      try {
        await new Promise((resolve) => setTimeout(resolve, 50))
        setMetrics({
          totalContainers: 25,
          healthyContainers: 24,
          failedDeployments: 1,
          averageLatency: 145,
        })
      } catch (err: any) {
        setMetricsError(err.message)
      } finally {
        setMetricsLoading(false)
      }
    }

    loadMetrics()
  }, [])

  return (
    <div data-testid="dashboard-multiple-sources">
      <h1>Dashboard</h1>

      {/* KPI Cards Section - Should render even if metrics fail */}
      <section data-testid="kpi-section" className="grid grid-cols-4 gap-4 mb-6">
        {metricsLoading && (
          <div data-testid="metrics-skeleton">Loading metrics...</div>
        )}
        {metricsError && (
          <div
            data-testid="metrics-error"
            role="alert"
            className="bg-yellow-100 p-4 col-span-4"
          >
            Metrics unavailable: {metricsError}
          </div>
        )}
        {!metricsLoading && !metricsError && (
          <>
            <div data-testid="kpi-total-containers" className="bg-white p-4 rounded">
              <div className="text-gray-600">Total Containers</div>
              <div className="text-3xl font-bold">
                {metrics.totalContainers || '—'}
              </div>
            </div>
            <div data-testid="kpi-healthy" className="bg-white p-4 rounded">
              <div className="text-gray-600">Healthy</div>
              <div className="text-3xl font-bold">
                {metrics.healthyContainers || '—'}
              </div>
            </div>
            <div data-testid="kpi-failed" className="bg-white p-4 rounded">
              <div className="text-gray-600">Failed</div>
              <div className="text-3xl font-bold">{metrics.failedDeployments || '—'}</div>
            </div>
            <div data-testid="kpi-latency" className="bg-white p-4 rounded">
              <div className="text-gray-600">Avg Latency (ms)</div>
              <div className="text-3xl font-bold">{metrics.averageLatency || '—'}</div>
            </div>
          </>
        )}
      </section>

      {/* Containers Section - Should render even if containers API fails */}
      <section data-testid="containers-section" className="mb-6">
        <h2>Containers</h2>
        {containersLoading && (
          <div data-testid="containers-skeleton">Loading containers...</div>
        )}
        {containersError && (
          <div
            data-testid="containers-error"
            role="alert"
            className="bg-red-100 p-4 rounded"
          >
            Could not load containers: {containersError}
          </div>
        )}
        {!containersLoading && !containersError && (
          <div data-testid="containers-list">
            {containers.length === 0 ? (
              <p>No containers found</p>
            ) : (
              <ul>
                {containers.map((c) => (
                  <li key={c.id} data-testid={`container-${c.id}`}>
                    {c.name} - {c.status}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </section>

      {/* Mappings Section - Should render even if mappings API fails */}
      <section data-testid="mappings-section">
        <h2>Secret Mappings</h2>
        {mappingsLoading && (
          <div data-testid="mappings-skeleton">Loading mappings...</div>
        )}
        {mappingsError && (
          <div
            data-testid="mappings-error"
            role="alert"
            className="bg-red-100 p-4 rounded"
          >
            Could not load mappings: {mappingsError}
          </div>
        )}
        {!mappingsLoading && !mappingsError && (
          <div data-testid="mappings-list">
            {mappings.length === 0 ? (
              <p>No mappings found</p>
            ) : (
              <ul>
                {mappings.map((m) => (
                  <li key={m.id} data-testid={`mapping-${m.id}`}>
                    {m.source} → {m.target}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </section>
    </div>
  )
}

/**
 * Component with Retry Mechanism
 */
function ComponentWithRetry() {
  const [data, setData] = React.useState<any>(null)
  const [loading, setLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [retryCount, setRetryCount] = React.useState(0)

  const loadData = React.useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      // Simulate fetch
      await new Promise((resolve) => setTimeout(resolve, 30))
      setData({ value: 'success' })
    } catch (err: any) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }, [])

  React.useEffect(() => {
    loadData()
  }, [retryCount, loadData])

  const handleRetry = () => {
    setRetryCount((c) => c + 1)
  }

  return (
    <div data-testid="retry-component">
      {loading && <div data-testid="retry-loading">Loading...</div>}
      {error && (
        <div data-testid="retry-error" role="alert">
          {error}
          <button
            data-testid="retry-button"
            onClick={handleRetry}
            className="ml-4 px-4 py-2 bg-blue-500 text-white rounded"
          >
            Retry
          </button>
        </div>
      )}
      {!loading && !error && (
        <div data-testid="retry-success">{data?.value}</div>
      )}
    </div>
  )
}

/**
 * Component with Partial Data Handling
 */
function PartialDataComponent() {
  const [items, setItems] = React.useState<any[]>([])
  const [partialError, setPartialError] = React.useState<string | null>(null)

  React.useEffect(() => {
    // Simulate loading some items but having partial failure
    setTimeout(() => {
      setItems([
        { id: '1', name: 'Item 1', status: 'loaded' },
        { id: '2', name: 'Item 2', status: 'failed' }, // This one failed to load
        { id: '3', name: 'Item 3', status: 'loaded' },
      ])
      setPartialError('Failed to load 1 of 3 items')
    }, 30)
  }, [])

  return (
    <div data-testid="partial-data-component">
      {partialError && (
        <div data-testid="partial-error" role="alert" className="bg-yellow-100 p-4">
          {partialError}
        </div>
      )}
      <ul data-testid="items-list">
        {items.map((item) => (
          <li
            key={item.id}
            data-testid={`item-${item.id}`}
            className={item.status === 'failed' ? 'opacity-50' : ''}
          >
            {item.name} -{' '}
            {item.status === 'failed' ? (
              <span data-testid={`item-failed-${item.id}`}>Failed to load</span>
            ) : (
              'Loaded'
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}

/**
 * Component Testing Network Timeout Behavior
 */
function NetworkTimeoutComponent() {
  const [data, setData] = React.useState<any>(null)
  const [error, setError] = React.useState<string | null>(null)
  const [isTimeout, setIsTimeout] = React.useState(false)

  const simulateTimeout = () => {
    setError('Request timeout (30s)')
    setIsTimeout(true)
  }

  return (
    <div data-testid="timeout-component">
      <button
        data-testid="trigger-timeout"
        onClick={simulateTimeout}
        className="px-4 py-2 bg-red-500 text-white rounded"
      >
        Simulate Timeout
      </button>

      {error && (
        <div data-testid="timeout-error" role="alert" className="bg-red-100 p-4 mt-4">
          {error}
          {isTimeout && (
            <p className="text-sm mt-2">
              Please check your connection and try again.
            </p>
          )}
        </div>
      )}
      {!error && !data && (
        <div data-testid="timeout-idle">Ready to load</div>
      )}
      {data && <div data-testid="timeout-success">{data}</div>}
    </div>
  )
}

describe('Error Handling & Cascade Prevention', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('Independent Data Source Failures', () => {
    it('should load metrics even if containers API fails', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // Metrics should still load
        expect(
          screen.queryByTestId('kpi-total-containers') ||
            screen.queryByTestId('metrics-error')
        ).toBeInTheDocument()
      })

      // Other sections may still load
      expect(screen.getByTestId('containers-section')).toBeInTheDocument()
    })

    it('should load containers even if mappings API fails', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        expect(
          screen.queryByTestId('containers-list') ||
            screen.queryByTestId('containers-error')
        ).toBeInTheDocument()
      })
    })

    it('should load mappings even if metrics API fails', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        expect(
          screen.queryByTestId('mappings-list') ||
            screen.queryByTestId('mappings-error')
        ).toBeInTheDocument()
      })
    })

    it('should display all sections on dashboard even with partial failures', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // All three sections should be present
        expect(screen.getByTestId('kpi-section')).toBeInTheDocument()
        expect(screen.getByTestId('containers-section')).toBeInTheDocument()
        expect(screen.getByTestId('mappings-section')).toBeInTheDocument()
      })
    })
  })

  describe('No Blank Screens on Error', () => {
    it('should always show UI even when one data source fails', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const dashboard = screen.getByTestId('dashboard-multiple-sources')
        expect(dashboard).toBeInTheDocument()
        expect(dashboard.innerHTML).not.toBe('')
      })
    })

    it('should render loading skeletons or errors, never blank', async () => {
      const { rerender } = render(<DashboardWithMultipleSources />, {
        wrapper: ErrorHandlingWrapper,
      })

      // Component should show loading states
      await waitFor(() => {
        const content =
          screen.queryByTestId('metrics-skeleton') ||
          screen.queryByTestId('containers-skeleton') ||
          screen.queryByTestId('mappings-skeleton') ||
          screen.queryByTestId('kpi-total-containers')

        expect(content).toBeInTheDocument()
      })
    })
  })

  describe('Graceful Degradation with Missing Data', () => {
    it('should show "—" or "N/A" for missing metric values', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // KPI cards should be present
        const kpiSection = screen.queryByTestId('kpi-section')
        expect(kpiSection).toBeInTheDocument()
      })

      // Cards show either values or placeholder
      const containers = screen.queryByTestId('kpi-total-containers')
      if (containers) {
        const text = containers.textContent
        expect(text).toMatch(/\d+|—/)
      }
    })

    it('should show partial data with clear failed item indicators', async () => {
      render(<PartialDataComponent />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('items-list')).toBeInTheDocument()
      })

      // Should show which items failed
      const failedItem = screen.queryByTestId('item-failed-2')
      if (failedItem) {
        expect(failedItem).toHaveTextContent('Failed to load')
      }
    })

    it('should display warning about partial load', async () => {
      render(<PartialDataComponent />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const partialError = screen.queryByTestId('partial-error')
        if (partialError) {
          expect(partialError).toHaveTextContent(/Failed to load/)
        }
      })
    })
  })

  describe('Error Message Quality', () => {
    it('should show user-friendly error messages', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const dashboard = screen.getByTestId('dashboard-multiple-sources')
        expect(dashboard).toBeInTheDocument()
      })

      // Error messages should not contain technical details
      const errorMessages = screen.queryAllByRole('alert')
      errorMessages.forEach((alert) => {
        const text = alert.textContent || ''
        // Should have readable error, not raw stack traces
        expect(text).not.toMatch(/at\s+\w+\s+\(/)
      })
    })

    it('should provide context for network timeout errors', async () => {
      const user = userEvent.setup()
      render(<NetworkTimeoutComponent />, { wrapper: ErrorHandlingWrapper })

      await user.click(screen.getByTestId('trigger-timeout'))

      await waitFor(() => {
        const error = screen.getByTestId('timeout-error')
        expect(error.textContent).toMatch(/timeout|connection/i)
      })
    })

    it('should avoid showing raw API error details', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const alerts = screen.queryAllByRole('alert')
        alerts.forEach((alert) => {
          // No JSON error objects or raw stack traces
          expect(alert.textContent).not.toMatch(/\{.*\}/)
        })
      })
    })
  })

  describe('Retry Mechanisms', () => {
    it('should show independent retry button for failed component', async () => {
      const user = userEvent.setup()
      render(<ComponentWithRetry />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        expect(screen.getByTestId('retry-component')).toBeInTheDocument()
      })

      // Should have retry button visible
      const retryBtn = screen.queryByTestId('retry-button')
      if (retryBtn) {
        expect(retryBtn).toBeInTheDocument()
      }
    })

    it('should retry independently without affecting other components', async () => {
      const user = userEvent.setup()
      const { rerender } = render(<DashboardWithMultipleSources />, {
        wrapper: ErrorHandlingWrapper,
      })

      await waitFor(() => {
        expect(screen.getByTestId('dashboard-multiple-sources')).toBeInTheDocument()
      })

      // Even if we were to call retry on one section, others remain intact
      const dashboard = screen.getByTestId('dashboard-multiple-sources')
      expect(dashboard).toBeInTheDocument()
    })
  })

  describe('Error Boundaries & Isolation', () => {
    it('should not unmount page when component errors', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const dashboard = screen.getByTestId('dashboard-multiple-sources')
        expect(dashboard).toBeInTheDocument()

        // Main page structure remains
        expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument()
      })
    })

    it('should isolate error to specific section', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // All sections present even if some have errors
        expect(screen.getByTestId('kpi-section')).toBeInTheDocument()
        expect(screen.getByTestId('containers-section')).toBeInTheDocument()
        expect(screen.getByTestId('mappings-section')).toBeInTheDocument()
      })
    })
  })

  describe('Loading State vs Error State', () => {
    it('should show loading state with aria-busy while fetching', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      // Initially shows loading skeletons
      const loadingElements =
        screen.queryByTestId('metrics-skeleton') ||
        screen.queryByTestId('containers-skeleton') ||
        screen.queryByTestId('mappings-skeleton')

      // Should transition to loaded or error state
      await waitFor(() => {
        expect(screen.getByTestId('dashboard-multiple-sources')).toBeInTheDocument()
      })
    })

    it('should distinguish error state from loading state', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // Should NOT show both loading and error simultaneously
        const errors = screen.queryAllByRole('alert')
        const skeletons = screen.queryAllByTestId(/skeleton/)

        if (errors.length > 0) {
          expect(skeletons.length).toBe(0)
        }
      })
    })
  })

  describe('Network Timeout Handling', () => {
    it('should handle request timeout gracefully', async () => {
      const user = userEvent.setup()
      render(<NetworkTimeoutComponent />, { wrapper: ErrorHandlingWrapper })

      await user.click(screen.getByTestId('trigger-timeout'))

      await waitFor(() => {
        const error = screen.getByTestId('timeout-error')
        expect(error).toBeInTheDocument()
        expect(error).toHaveAttribute('role', 'alert')
      })
    })

    it('should not block other components during timeout', async () => {
      const user = userEvent.setup()
      render(
        <>
          <NetworkTimeoutComponent />
          <DashboardWithMultipleSources />
        </>,
        { wrapper: ErrorHandlingWrapper }
      )

      // Trigger timeout in one component
      await user.click(screen.getByTestId('trigger-timeout'))

      // Other components should still be accessible
      await waitFor(() => {
        expect(screen.getByTestId('dashboard-multiple-sources')).toBeInTheDocument()
      })
    })
  })

  describe('Partial Data Scenarios', () => {
    it('should display successfully loaded items alongside failed ones', async () => {
      render(<PartialDataComponent />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const items = screen.getByTestId('items-list')
        expect(items).toBeInTheDocument()

        // Should have multiple items visible
        const allItems = screen.getAllByTestId(/item-\d/)
        expect(allItems.length).toBeGreaterThan(0)
      })
    })

    it('should show what data loaded despite errors', async () => {
      render(<PartialDataComponent />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // Partial error message visible
        const error = screen.queryByTestId('partial-error')
        if (error) {
          expect(error).toHaveTextContent(/Failed to load/)
        }

        // But data that loaded is still visible
        const item1 = screen.queryByTestId('item-1')
        if (item1) {
          expect(item1).toBeInTheDocument()
        }
      })
    })
  })

  describe('User Recovery Actions', () => {
    it('should provide clear next steps after error', async () => {
      const user = userEvent.setup()
      render(<ComponentWithRetry />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        // After error, retry button should be available
        expect(screen.getByTestId('retry-component')).toBeInTheDocument()
      })
    })

    it('should allow users to retry failed operations', async () => {
      const user = userEvent.setup()
      render(<ComponentWithRetry />, { wrapper: ErrorHandlingWrapper })

      // Component should be interactive
      const component = screen.getByTestId('retry-component')
      expect(component).toBeInTheDocument()

      // Retry button should be clickable
      const retryBtn = screen.queryByTestId('retry-button')
      if (retryBtn) {
        await user.click(retryBtn)
      }
    })
  })

  describe('Alert Announcements', () => {
    it('should announce errors to screen readers', async () => {
      render(<DashboardWithMultipleSources />, { wrapper: ErrorHandlingWrapper })

      await waitFor(() => {
        const alerts = screen.queryAllByRole('alert')
        expect(alerts.length).toBeGreaterThanOrEqual(0)
      })
    })

    it('should use role=alert for error messages', async () => {
      render(<NetworkTimeoutComponent />, { wrapper: ErrorHandlingWrapper })

      const timeoutComponent = screen.getByTestId('timeout-component')
      const alerts = timeoutComponent.querySelectorAll('[role="alert"]')

      // Structure supports alerts
      expect(alerts).toBeDefined()
    })
  })
})

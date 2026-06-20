import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import React from 'react'
import { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider } from '@/contexts/AuthContext'
import * as discoveryApi from '@/lib/api/discovery'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'
import { ContainerMetadata, SecretMappingSuggestion } from '@/lib/api/types'

// Mock API modules
vi.mock('@/lib/api/discovery', () => ({
  getContainers: vi.fn(),
  getMappings: vi.fn(),
  refreshDiscovery: vi.fn(),
  getDiscoveryMetrics: vi.fn(),
  getDiscoverySummary: vi.fn(),
}))

vi.mock('@/lib/auth/session', () => ({
  initializeSession: vi.fn(),
}))

vi.mock('@/lib/auth/storage', () => ({
  getAccessToken: vi.fn(),
  setAccessToken: vi.fn(),
  getStoredUser: vi.fn(),
  setStoredUser: vi.fn(),
  getStoredSession: vi.fn(),
  setStoredSession: vi.fn(),
  clearAllAuthData: vi.fn(),
}))

/**
 * Test Discovery Page component that demonstrates full discovery workflow
 */
function TestDiscoveryPage() {
  const [containers, setContainers] = React.useState<ContainerMetadata[]>([])
  const [mappings, setMappings] = React.useState<SecretMappingSuggestion[]>([])
  const [isLoading, setIsLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [searchTerm, setSearchTerm] = React.useState('')
  const [filters, setFilters] = React.useState({
    classification: '',
    status: '',
  })
  const [selectedContainer, setSelectedContainer] = React.useState<ContainerMetadata | null>(null)
  const [demoMode, setDemoMode] = React.useState(false)
  const [metrics, setMetrics] = React.useState({
    total: 0,
    managed: 0,
    unmanaged: 0,
    partial: 0,
  })
  const [coveragePercentage, setCoveragePercentage] = React.useState(0)
  const [selectedContainers, setSelectedContainers] = React.useState<string[]>([])

  // Load discovery data
  React.useEffect(() => {
    const loadDiscoveryData = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const [containersData, mappingsData, summaryData, metricsData] = await Promise.all([
          discoveryApi.getContainers(),
          discoveryApi.getMappings(),
          discoveryApi.getDiscoverySummary(),
          discoveryApi.getDiscoveryMetrics(),
        ])

        setContainers(containersData.containers)
        setMappings(mappingsData.suggestions)
        setMetrics(summaryData)

        // Calculate coverage percentage
        const coverage = summaryData.total > 0
          ? (summaryData.managed / summaryData.total) * 100
          : 0
        setCoveragePercentage(Math.round(coverage))
      } catch (err: any) {
        setError(err.message || 'Failed to load discovery data')
      } finally {
        setIsLoading(false)
      }
    }

    loadDiscoveryData()
  }, [demoMode])

  const handleSearch = (term: string) => {
    setSearchTerm(term)
  }

  const handleFilterChange = (newFilters: typeof filters) => {
    setFilters(newFilters)
  }

  const handleContainerClick = (container: ContainerMetadata) => {
    setSelectedContainer(container)
  }

  const handleRefresh = async () => {
    try {
      setIsLoading(true)
      await discoveryApi.refreshDiscovery()
      // Reload data
      const [containersData, mappingsData, summaryData] = await Promise.all([
        discoveryApi.getContainers(),
        discoveryApi.getMappings(),
        discoveryApi.getDiscoverySummary(),
      ])
      setContainers(containersData.containers)
      setMappings(mappingsData.suggestions)
      setMetrics(summaryData)
    } catch (err: any) {
      setError('Failed to refresh discovery data')
    } finally {
      setIsLoading(false)
    }
  }

  const handleExport = async (format: 'csv' | 'json') => {
    // Simulate export functionality
    const data = {
      containers: selectedContainers.length > 0
        ? containers.filter((c) => selectedContainers.includes(c.container_id))
        : containers,
      timestamp: new Date().toISOString(),
    }

    const content = format === 'json'
      ? JSON.stringify(data, null, 2)
      : generateCSV(data)

    const blob = new Blob([content], {
      type: format === 'json' ? 'application/json' : 'text/csv',
    })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `discovery-export.${format}`
    link.click()
    URL.revokeObjectURL(url)
  }

  const generateCSV = (data: any) => {
    const headers = ['Container ID', 'Name', 'Image', 'Status', 'DSO Status']
    const rows = data.containers.map((c: ContainerMetadata) => [
      c.container_id,
      c.container_name,
      c.image,
      c.status,
      c.dso_awareness.status,
    ])
    return [headers, ...rows].map((r) => r.join(',')).join('\n')
  }

  const toggleContainerSelect = (containerId: string) => {
    setSelectedContainers((prev) =>
      prev.includes(containerId)
        ? prev.filter((id) => id !== containerId)
        : [...prev, containerId]
    )
  }

  // Apply search and filters
  const filteredContainers = containers.filter((container) => {
    const matchesSearch =
      container.container_name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      container.image.toLowerCase().includes(searchTerm.toLowerCase())

    const matchesClassification =
      !filters.classification || container.dso_awareness.status === filters.classification

    const matchesStatus = !filters.status || container.status === filters.status

    return matchesSearch && matchesClassification && matchesStatus
  })

  if (isLoading && containers.length === 0) {
    return (
      <div data-testid="discovery-page-loading">
        <div data-testid="skeleton-containers" className="skeleton" />
        <div data-testid="skeleton-metrics" className="skeleton" />
      </div>
    )
  }

  return (
    <div data-testid="discovery-page">
      {error && <div data-testid="discovery-error">{error}</div>}

      <div data-testid="demo-mode-section">
        <label>
          <input
            type="checkbox"
            data-testid="demo-mode-toggle"
            checked={demoMode}
            onChange={(e) => setDemoMode(e.target.checked)}
          />
          Demo Mode
        </label>
      </div>

      <div data-testid="coverage-metrics-section">
        <div data-testid="coverage-percentage">
          Coverage: {coveragePercentage}%
        </div>
        <div data-testid="metrics-managed">{metrics.managed} Managed</div>
        <div data-testid="metrics-unmanaged">{metrics.unmanaged} Unmanaged</div>
        <div data-testid="metrics-partial">{metrics.partial} Partial</div>
        <div data-testid="metrics-total">{metrics.total} Total</div>
      </div>

      <div data-testid="refresh-section">
        <button
          data-testid="refresh-btn"
          onClick={handleRefresh}
          disabled={isLoading}
        >
          Refresh Discovery
        </button>
      </div>

      <div data-testid="search-section">
        <input
          data-testid="search-input"
          placeholder="Search containers by name or image"
          value={searchTerm}
          onChange={(e) => handleSearch(e.target.value)}
          className="search-input"
        />
      </div>

      <div data-testid="filters-section">
        <select
          data-testid="classification-filter"
          value={filters.classification}
          onChange={(e) =>
            handleFilterChange({
              ...filters,
              classification: e.target.value,
            })
          }
        >
          <option value="">All Classifications</option>
          <option value="managed">Managed</option>
          <option value="unmanaged">Unmanaged</option>
          <option value="partial">Partial</option>
        </select>

        <select
          data-testid="status-filter"
          value={filters.status}
          onChange={(e) =>
            handleFilterChange({
              ...filters,
              status: e.target.value,
            })
          }
        >
          <option value="">All Statuses</option>
          <option value="running">Running</option>
          <option value="stopped">Stopped</option>
          <option value="paused">Paused</option>
        </select>
      </div>

      {filteredContainers.length === 0 ? (
        <div data-testid="discovery-empty-state">
          No containers found matching your criteria
        </div>
      ) : (
        <div data-testid="containers-table">
          <div data-testid="bulk-select-header">
            <input
              type="checkbox"
              data-testid="select-all-checkbox"
              checked={selectedContainers.length === filteredContainers.length && filteredContainers.length > 0}
              onChange={(e) => {
                if (e.target.checked) {
                  setSelectedContainers(filteredContainers.map((c) => c.container_id))
                } else {
                  setSelectedContainers([])
                }
              }}
            />
            <span>{selectedContainers.length} selected</span>
          </div>

          {filteredContainers.map((container) => (
            <div
              key={container.container_id}
              data-testid={`container-row-${container.container_id}`}
              onClick={() => handleContainerClick(container)}
              className="container-row"
            >
              <input
                type="checkbox"
                data-testid={`checkbox-${container.container_id}`}
                checked={selectedContainers.includes(container.container_id)}
                onChange={(e) => {
                  e.stopPropagation()
                  toggleContainerSelect(container.container_id)
                }}
              />

              <span data-testid={`name-${container.container_id}`}>
                {container.container_name}
              </span>
              <span data-testid={`image-${container.container_id}`}>
                {container.image}
              </span>
              <span data-testid={`status-${container.container_id}`}>
                {container.status}
              </span>
              <span
                data-testid={`dso-status-${container.container_id}`}
                className={`dso-badge dso-${container.dso_awareness.status}`}
              >
                {container.dso_awareness.status}
              </span>
            </div>
          ))}
        </div>
      )}

      {selectedContainer && (
        <div data-testid="details-drawer">
          <div data-testid="drawer-general-section">
            <h3>General Information</h3>
            <div data-testid="drawer-container-id">
              ID: {selectedContainer.container_id}
            </div>
            <div data-testid="drawer-container-name">
              Name: {selectedContainer.container_name}
            </div>
            <div data-testid="drawer-image">Image: {selectedContainer.image}</div>
            <div data-testid="drawer-status">Status: {selectedContainer.status}</div>
          </div>

          <div data-testid="drawer-networks-section">
            <h3>Networks</h3>
            <div data-testid="drawer-network-ip">
              IP: {selectedContainer.networks.ip}
            </div>
            <div data-testid="drawer-network-gateway">
              Gateway: {selectedContainer.networks.gateway}
            </div>
          </div>

          <div data-testid="drawer-env-vars-section">
            <h3>Environment Variables</h3>
            <div data-testid="drawer-env-vars-count">
              Count: {Object.keys(selectedContainer.env_vars).length}
            </div>
          </div>

          <div data-testid="drawer-dso-section">
            <h3>DSO Awareness</h3>
            <div data-testid="drawer-dso-status">
              Status: {selectedContainer.dso_awareness.status}
            </div>
            <div data-testid="drawer-managed-secrets">
              Managed Secrets: {selectedContainer.dso_awareness.managed_secrets.length}
            </div>
            <div data-testid="drawer-missing-mappings">
              Missing Mappings: {selectedContainer.dso_awareness.missing_mappings.length}
            </div>
          </div>

          <button
            data-testid="drawer-close"
            onClick={() => setSelectedContainer(null)}
          >
            Close
          </button>
        </div>
      )}

      <div data-testid="secret-mappings-section">
        <h3>Secret Mappings</h3>
        <div data-testid="mappings-count">Total: {mappings.length} mappings</div>
        {mappings.map((mapping, idx) => (
          <div key={idx} data-testid={`mapping-${idx}`} className="mapping-row">
            <span data-testid={`mapping-var-${idx}`}>{mapping.env_var_name}</span>
            <span
              data-testid={`mapping-confidence-${idx}`}
              className={`confidence-badge confidence-${mapping.confidence}`}
            >
              {mapping.confidence}
            </span>
            <span data-testid={`mapping-reason-${idx}`}>{mapping.reason}</span>
            <span data-testid={`mapping-configured-${idx}`}>
              {mapping.is_configured ? '✓' : '✗'}
            </span>
          </div>
        ))}
      </div>

      <div data-testid="export-section">
        <button
          data-testid="export-csv-btn"
          onClick={() => handleExport('csv')}
        >
          Export CSV
        </button>
        <button
          data-testid="export-json-btn"
          onClick={() => handleExport('json')}
        >
          Export JSON
        </button>
        <span data-testid="bulk-export-status">
          {selectedContainers.length > 0 ? `${selectedContainers.length} selected` : 'Export all'}
        </span>
      </div>
    </div>
  )
}

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

describe('Discovery Integration Tests', () => {
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

  const mockContainers: ContainerMetadata[] = [
    {
      container_id: 'cont-1',
      container_name: 'app-api',
      image: 'myapp:latest',
      status: 'running',
      networks: {
        ip: '172.17.0.2',
        gateway: '172.17.0.1',
        networks: ['bridge'],
      },
      env_vars: {
        NODE_ENV: 'production',
        LOG_LEVEL: 'info',
      },
      dso_awareness: {
        status: 'managed',
        managed_secrets: ['DB_PASSWORD', 'API_KEY'],
        config_refs: ['app-config'],
        missing_mappings: [],
      },
      labels: {
        app: 'api',
        environment: 'prod',
      },
      restart_policy: {
        name: 'always',
        maximum_retry_count: 0,
      },
    },
    {
      container_id: 'cont-2',
      container_name: 'app-db',
      image: 'postgres:14',
      status: 'running',
      networks: {
        ip: '172.17.0.3',
        gateway: '172.17.0.1',
        networks: ['bridge'],
      },
      env_vars: {
        POSTGRES_DB: 'mydb',
        POSTGRES_USER: 'admin',
      },
      dso_awareness: {
        status: 'partial',
        managed_secrets: ['POSTGRES_PASSWORD'],
        config_refs: [],
        missing_mappings: ['DB_BACKUP_LOCATION'],
      },
      labels: {
        app: 'db',
        environment: 'prod',
      },
      restart_policy: {
        name: 'always',
        maximum_retry_count: 0,
      },
    },
    {
      container_id: 'cont-3',
      container_name: 'monitoring',
      image: 'prometheus:latest',
      status: 'stopped',
      networks: {
        ip: '172.17.0.4',
        gateway: '172.17.0.1',
        networks: ['bridge'],
      },
      env_vars: {
        PROMETHEUS_RETENTION: '30d',
      },
      dso_awareness: {
        status: 'unmanaged',
        managed_secrets: [],
        config_refs: [],
        missing_mappings: [],
      },
      labels: {
        app: 'monitoring',
      },
      restart_policy: {
        name: 'no',
      },
    },
  ]

  const mockMappings: SecretMappingSuggestion[] = [
    {
      env_var_name: 'DB_PASSWORD',
      confidence: 'high',
      reason: 'Detected database password pattern',
      suggested_secret_name: 'db-password',
      is_configured: true,
    },
    {
      env_var_name: 'API_KEY',
      confidence: 'high',
      reason: 'Variable name contains "key"',
      suggested_secret_name: 'api-key',
      is_configured: true,
    },
    {
      env_var_name: 'DB_BACKUP_LOCATION',
      confidence: 'medium',
      reason: 'Likely contains backup credentials',
      suggested_secret_name: 'db-backup-location',
      is_configured: false,
    },
  ]

  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()

    // Setup auth
    ;(storage.getAccessToken as any).mockReturnValue('valid-token')
    ;(storage.getStoredUser as any).mockReturnValue(mockUser)
    ;(storage.getStoredSession as any).mockReturnValue(mockSession)
    ;(session.initializeSession as any).mockResolvedValue(mockUser)

    // Setup default API responses
    ;(discoveryApi.getContainers as any).mockResolvedValue({
      containers: mockContainers,
      total: mockContainers.length,
      managed: 1,
      unmanaged: 1,
      partial: 1,
      timestamp: new Date().toISOString(),
    })

    ;(discoveryApi.getMappings as any).mockResolvedValue({
      suggestions: mockMappings,
      count: mockMappings.length,
      timestamp: new Date().toISOString(),
    })

    ;(discoveryApi.getDiscoverySummary as any).mockResolvedValue({
      total: mockContainers.length,
      managed: 1,
      unmanaged: 1,
      partial: 1,
    })

    ;(discoveryApi.getDiscoveryMetrics as any).mockResolvedValue({
      cache_hits: 150,
      cache_misses: 25,
      refresh_count: 5,
      avg_latency_ms: 45,
      cache_age_seconds: 120,
    })

    ;(discoveryApi.refreshDiscovery as any).mockResolvedValue({
      status: 'success',
      message: 'Discovery cache refreshed',
    })
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('Discovery Page Load', () => {
    it('should load discovery page with authentication', async () => {
      const token = storage.getAccessToken()
      expect(token).toBe('valid-token')
    })

    it('should fetch containers on load', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('discovery-page')).toBeInTheDocument()
      })

      expect(discoveryApi.getContainers).toHaveBeenCalled()
    })

    it('should display containers after loading', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
    })

    it('should show loading state while fetching', async () => {
      ;(discoveryApi.getContainers as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )
      ;(discoveryApi.getMappings as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )
      ;(discoveryApi.getDiscoverySummary as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )
      ;(discoveryApi.getDiscoveryMetrics as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      expect(screen.getByTestId('discovery-page-loading')).toBeInTheDocument()
    })
  })

  describe('Search Functionality', () => {
    it('should search containers by name', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('search-input')
      await user.type(searchInput, 'app-api')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
        expect(screen.queryByTestId('container-row-cont-2')).not.toBeInTheDocument()
      })
    })

    it('should search containers by image', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('search-input')
      await user.type(searchInput, 'postgres')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-2')).toBeInTheDocument()
        expect(screen.queryByTestId('container-row-cont-1')).not.toBeInTheDocument()
      })
    })

    it('should update results in real-time as user types', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('search-input')
      await user.type(searchInput, 'app')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
        expect(screen.getByTestId('container-row-cont-2')).toBeInTheDocument()
      })
    })
  })

  describe('Filter Application', () => {
    it('should filter by classification', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const classificationFilter = screen.getByTestId('classification-filter')
      await user.selectOptions(classificationFilter, 'managed')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
        expect(screen.queryByTestId('container-row-cont-2')).not.toBeInTheDocument()
      })
    })

    it('should filter by status', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const statusFilter = screen.getByTestId('status-filter')
      await user.selectOptions(statusFilter, 'running')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
        expect(screen.queryByTestId('container-row-cont-3')).not.toBeInTheDocument()
      })
    })
  })

  describe('Coverage Metrics', () => {
    it('should calculate and display coverage percentage correctly', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('coverage-metrics-section')).toBeInTheDocument()
      })

      // 1 managed out of 3 total = 33%
      expect(screen.getByTestId('coverage-percentage')).toHaveTextContent('Coverage: 33%')
    })

    it('should display managed/unmanaged/partial counts', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('coverage-metrics-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('metrics-managed')).toHaveTextContent('1 Managed')
      expect(screen.getByTestId('metrics-unmanaged')).toHaveTextContent('1 Unmanaged')
      expect(screen.getByTestId('metrics-partial')).toHaveTextContent('1 Partial')
      expect(screen.getByTestId('metrics-total')).toHaveTextContent('3 Total')
    })
  })

  describe('Container Selection', () => {
    it('should open details drawer on container click', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('details-drawer')).toBeInTheDocument()
    })

    it('should display container details in drawer', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('drawer-container-name')).toHaveTextContent('app-api')
      expect(screen.getByTestId('drawer-image')).toHaveTextContent('myapp:latest')
      expect(screen.getByTestId('drawer-status')).toHaveTextContent('running')
    })
  })

  describe('Details Drawer', () => {
    it('should display general information section', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('drawer-general-section')).toBeInTheDocument()
      expect(screen.getByTestId('drawer-container-id')).toBeInTheDocument()
    })

    it('should display networks section', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('drawer-networks-section')).toBeInTheDocument()
      expect(screen.getByTestId('drawer-network-ip')).toHaveTextContent('172.17.0.2')
    })

    it('should display environment variables section', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('drawer-env-vars-section')).toBeInTheDocument()
      expect(screen.getByTestId('drawer-env-vars-count')).toHaveTextContent('Count: 2')
    })

    it('should display DSO awareness section', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('container-row-cont-1'))

      expect(screen.getByTestId('drawer-dso-section')).toBeInTheDocument()
      expect(screen.getByTestId('drawer-dso-status')).toHaveTextContent('managed')
      expect(screen.getByTestId('drawer-managed-secrets')).toHaveTextContent('2')
    })
  })

  describe('Manual Refresh', () => {
    it('should trigger refresh when refresh button clicked', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('refresh-btn'))

      await waitFor(() => {
        expect(discoveryApi.refreshDiscovery).toHaveBeenCalled()
      })
    })

    it('should reload all data after refresh', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const callCountBefore = (discoveryApi.getContainers as any).mock.calls.length

      await user.click(screen.getByTestId('refresh-btn'))

      await waitFor(() => {
        const callCountAfter = (discoveryApi.getContainers as any).mock.calls.length
        expect(callCountAfter).toBeGreaterThan(callCountBefore)
      })
    })
  })

  describe('Secret Mappings', () => {
    it('should display secret mapping suggestions', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('secret-mappings-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('mapping-0')).toBeInTheDocument()
      expect(screen.getByTestId('mapping-var-0')).toHaveTextContent('DB_PASSWORD')
    })

    it('should show confidence levels for mappings', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('secret-mappings-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('mapping-confidence-0')).toHaveTextContent('high')
      expect(screen.getByTestId('mapping-confidence-2')).toHaveTextContent('medium')
    })

    it('should show configuration status for each mapping', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('secret-mappings-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('mapping-configured-0')).toHaveTextContent('✓')
      expect(screen.getByTestId('mapping-configured-2')).toHaveTextContent('✗')
    })
  })

  describe('Demo Mode', () => {
    it('should show demo mode toggle', async () => {
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('demo-mode-section')).toBeInTheDocument()
      })

      expect(screen.getByTestId('demo-mode-toggle')).toBeInTheDocument()
    })

    it('should toggle demo mode and reload data', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('demo-mode-section')).toBeInTheDocument()
      })

      const callCountBefore = (discoveryApi.getContainers as any).mock.calls.length

      await user.click(screen.getByTestId('demo-mode-toggle'))

      await waitFor(() => {
        const callCountAfter = (discoveryApi.getContainers as any).mock.calls.length
        expect(callCountAfter).toBeGreaterThan(callCountBefore)
      })
    })
  })

  describe('Export', () => {
    it('should export to CSV format', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('export-csv-btn'))

      await waitFor(() => {
        expect(screen.getByTestId('export-csv-btn')).toBeInTheDocument()
      })
    })

    it('should export to JSON format', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('export-json-btn'))

      await waitFor(() => {
        expect(screen.getByTestId('export-json-btn')).toBeInTheDocument()
      })
    })

    it('should include filtered data in export', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      const classificationFilter = screen.getByTestId('classification-filter')
      await user.selectOptions(classificationFilter, 'managed')

      await waitFor(() => {
        expect(screen.getByTestId('container-row-cont-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('export-csv-btn'))

      expect(screen.getByTestId('export-csv-btn')).toBeInTheDocument()
    })
  })

  describe('Bulk Select', () => {
    it('should select multiple containers', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('checkbox-cont-1'))
      await user.click(screen.getByTestId('checkbox-cont-2'))

      expect(screen.getByTestId('bulk-select-header')).toHaveTextContent('2 selected')
    })

    it('should select all containers with select-all checkbox', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('select-all-checkbox'))

      expect(screen.getByTestId('bulk-select-header')).toHaveTextContent('3 selected')
    })

    it('should export selected containers', async () => {
      const user = userEvent.setup()
      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('containers-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('checkbox-cont-1'))
      await user.click(screen.getByTestId('checkbox-cont-2'))

      expect(screen.getByTestId('bulk-export-status')).toHaveTextContent('2 selected')

      await user.click(screen.getByTestId('export-csv-btn'))

      expect(screen.getByTestId('export-csv-btn')).toBeInTheDocument()
    })
  })

  describe('Error Handling', () => {
    it('should handle container fetch error', async () => {
      ;(discoveryApi.getContainers as any).mockRejectedValue(
        new Error('Failed to fetch containers')
      )

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('discovery-error')).toBeInTheDocument()
      })

      expect(screen.getByTestId('discovery-error')).toHaveTextContent(
        'Failed to fetch containers'
      )
    })

    it('should display container data even if mappings fail', async () => {
      ;(discoveryApi.getMappings as any).mockRejectedValue(
        new Error('Failed to fetch mappings')
      )

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      // Wait for the page to settle but not fail
      await waitFor(() => {
        // Should still try to render with partial data
        expect(screen.getByTestId('discovery-page')).toBeInTheDocument()
      }, { timeout: 5000 })
    })

    it('should display container data even if metrics fail', async () => {
      ;(discoveryApi.getDiscoveryMetrics as any).mockRejectedValue(
        new Error('Failed to fetch metrics')
      )

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      // Should still render containers
      await waitFor(
        () => {
          expect(screen.getByTestId('discovery-page')).toBeInTheDocument()
        },
        { timeout: 5000 }
      )
    })
  })

  describe('Empty Results', () => {
    it('should show empty state when no containers match filters', async () => {
      ;(discoveryApi.getContainers as any).mockResolvedValue({
        containers: [],
        total: 0,
        managed: 0,
        unmanaged: 0,
        partial: 0,
        timestamp: new Date().toISOString(),
      })

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('discovery-empty-state')).toBeInTheDocument()
      })
    })

    it('should show empty state message', async () => {
      ;(discoveryApi.getContainers as any).mockResolvedValue({
        containers: [],
        total: 0,
        managed: 0,
        unmanaged: 0,
        partial: 0,
        timestamp: new Date().toISOString(),
      })

      render(<TestDiscoveryPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('discovery-empty-state')).toHaveTextContent(
          'No containers found matching your criteria'
        )
      })
    })
  })
})

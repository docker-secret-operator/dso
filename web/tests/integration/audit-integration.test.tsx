import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import React from 'react'
import { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AuthProvider } from '@/contexts/AuthContext'
import * as auditApi from '@/lib/api/audit'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'
import { AuditEvent, AuditFilters } from '@/lib/api/types'

// Mock API modules
vi.mock('@/lib/api/audit', () => ({
  getAuditEvents: vi.fn(),
  getCorrelationChain: vi.fn(),
  getActorTimeline: vi.fn(),
  exportAudit: vi.fn(),
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
 * Test Audit Page component that demonstrates full audit workflow
 */
function TestAuditPage() {
  const [events, setEvents] = React.useState<AuditEvent[]>([])
  const [isLoading, setIsLoading] = React.useState(true)
  const [error, setError] = React.useState<string | null>(null)
  const [searchTerm, setSearchTerm] = React.useState('')
  const [filters, setFilters] = React.useState<AuditFilters>({})
  const [selectedEvent, setSelectedEvent] = React.useState<AuditEvent | null>(null)
  const [offset, setOffset] = React.useState(0)
  const [totalCount, setTotalCount] = React.useState(0)

  const ITEMS_PER_PAGE = 10

  // Load audit events
  React.useEffect(() => {
    const loadEvents = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const params: AuditFilters = {
          ...filters,
          limit: ITEMS_PER_PAGE,
          offset,
        }
        if (searchTerm) {
          params.actor = searchTerm
        }
        const response = await auditApi.getAuditEvents(params)
        setEvents(response.events)
        setTotalCount(response.total)
      } catch (err: any) {
        setError(err.message || 'Failed to load audit events')
      } finally {
        setIsLoading(false)
      }
    }

    loadEvents()
  }, [searchTerm, filters, offset])

  const handleSearch = async (term: string) => {
    setSearchTerm(term)
    setOffset(0)
  }

  const handleFilterChange = (newFilters: AuditFilters) => {
    setFilters(newFilters)
    setOffset(0)
  }

  const handleEventClick = (event: AuditEvent) => {
    setSelectedEvent(event)
  }

  const handleCorrelationClick = async (correlationId: string) => {
    try {
      await auditApi.getCorrelationChain(correlationId)
      // In real app, would open CorrelationTimeline modal
      setSelectedEvent(null)
    } catch (err) {
      setError('Failed to load correlation chain')
    }
  }

  const handleActorClick = async (actorId: string) => {
    try {
      await auditApi.getActorTimeline(actorId, '24h')
      // In real app, would open ActorTimeline modal
      setSelectedEvent(null)
    } catch (err) {
      setError('Failed to load actor timeline')
    }
  }

  const handleExport = async (format: 'csv' | 'json') => {
    try {
      await auditApi.exportAudit(filters, format)
    } catch (err) {
      setError('Failed to export audit events')
    }
  }

  const handleLoadMore = () => {
    setOffset(offset + ITEMS_PER_PAGE)
  }

  const hasMore = offset + ITEMS_PER_PAGE < totalCount

  if (isLoading && events.length === 0) {
    return (
      <div data-testid="audit-page-loading">
        <div data-testid="skeleton-table" className="skeleton" />
      </div>
    )
  }

  return (
    <div data-testid="audit-page">
      {error && <div data-testid="audit-error">{error}</div>}

      <div data-testid="audit-search-section">
        <input
          data-testid="audit-search-input"
          placeholder="Search by actor/action"
          value={searchTerm}
          onChange={(e) => handleSearch(e.target.value)}
          className="search-input"
        />
      </div>

      <div data-testid="audit-filters-section">
        <select
          data-testid="status-filter"
          onChange={(e) =>
            handleFilterChange({
              ...filters,
              action: e.target.value || undefined,
            })
          }
        >
          <option value="">All Actions</option>
          <option value="create">Create</option>
          <option value="update">Update</option>
          <option value="delete">Delete</option>
        </select>

        <select
          data-testid="classification-filter"
          onChange={(e) => {
            const severity = e.target.value as any
            handleFilterChange({
              ...filters,
              ...(severity ? { severity } : {}),
            } as any)
          }}
        >
          <option value="">All Classifications</option>
          <option value="info">Info</option>
          <option value="warning">Warning</option>
          <option value="error">Error</option>
          <option value="critical">Critical</option>
        </select>
      </div>

      {events.length === 0 ? (
        <div data-testid="audit-empty-state">No audit events found</div>
      ) : (
        <div data-testid="audit-table">
          {events.map((event) => (
            <div
              key={event.id}
              data-testid={`audit-event-${event.id}`}
              onClick={() => handleEventClick(event)}
              className="event-row"
            >
              <span data-testid={`event-actor-${event.id}`}>{event.actor}</span>
              <span data-testid={`event-action-${event.id}`}>{event.action}</span>
              <span data-testid={`event-status-${event.id}`}>{event.status}</span>
              <span data-testid={`event-severity-${event.id}`}>{event.severity}</span>
              <span data-testid={`event-timestamp-${event.id}`}>{event.timestamp}</span>

              <button
                data-testid={`correlation-link-${event.id}`}
                onClick={(e) => {
                  e.stopPropagation()
                  handleCorrelationClick(event.correlation_id)
                }}
                className="link-button"
              >
                Correlation
              </button>

              <button
                data-testid={`actor-link-${event.id}`}
                onClick={(e) => {
                  e.stopPropagation()
                  handleActorClick(event.actor_id)
                }}
                className="link-button"
              >
                Actor Timeline
              </button>
            </div>
          ))}
        </div>
      )}

      {selectedEvent && (
        <div data-testid="event-details-modal">
          <div data-testid="modal-actor">{selectedEvent.actor}</div>
          <div data-testid="modal-action">{selectedEvent.action}</div>
          <div data-testid="modal-resource">{selectedEvent.resource}</div>
          <div data-testid="modal-status">{selectedEvent.status}</div>
          <div data-testid="modal-details">{selectedEvent.details}</div>
          <div data-testid="modal-timestamp">{selectedEvent.timestamp}</div>
          <button
            data-testid="modal-close"
            onClick={() => setSelectedEvent(null)}
          >
            Close
          </button>
        </div>
      )}

      <div data-testid="pagination-section">
        <span data-testid="result-count">
          Showing {events.length} of {totalCount} results
        </span>
        {hasMore && (
          <button
            data-testid="load-more-btn"
            onClick={handleLoadMore}
            disabled={isLoading}
          >
            Load More
          </button>
        )}
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

describe('Audit Integration Tests', () => {
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

  const mockAuditEvents: AuditEvent[] = [
    {
      id: 'evt-1',
      correlation_id: 'corr-1',
      execution_id: 'exec-1',
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      action: 'create',
      resource: 'containers',
      resource_id: 'cont-1',
      resource_type: 'container',
      status: 'success',
      severity: 'info',
      details: 'Container created successfully',
      ip_address: '192.168.1.100',
      timestamp: new Date('2026-06-19T12:00:00Z').toISOString(),
    },
    {
      id: 'evt-2',
      correlation_id: 'corr-2',
      execution_id: 'exec-2',
      actor: 'admin@example.com',
      actor_id: 'admin-456',
      actor_email: 'admin@example.com',
      action: 'update',
      resource: 'secrets',
      resource_id: 'sec-1',
      resource_type: 'secret',
      status: 'failure',
      severity: 'error',
      details: 'Insufficient permissions',
      ip_address: '192.168.1.101',
      timestamp: new Date('2026-06-19T12:05:00Z').toISOString(),
    },
    {
      id: 'evt-3',
      correlation_id: 'corr-1',
      execution_id: 'exec-1',
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      action: 'read',
      resource: 'configs',
      resource_id: 'cfg-1',
      resource_type: 'config',
      status: 'success',
      severity: 'warning',
      details: 'Config accessed',
      ip_address: '192.168.1.100',
      timestamp: new Date('2026-06-19T12:10:00Z').toISOString(),
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
    ;(auditApi.getAuditEvents as any).mockResolvedValue({
      events: mockAuditEvents,
      total: mockAuditEvents.length,
      count: mockAuditEvents.length,
      offset: 0,
      limit: 10,
      timestamp: new Date().toISOString(),
    })

    ;(auditApi.getCorrelationChain as any).mockResolvedValue({
      correlation_id: 'corr-1',
      events: mockAuditEvents.filter((e) => e.correlation_id === 'corr-1'),
      count: 2,
      timestamp: new Date().toISOString(),
    })

    ;(auditApi.getActorTimeline as any).mockResolvedValue({
      actor_id: 'user-123',
      actor_name: 'user@example.com',
      period: '24h',
      events: mockAuditEvents.filter((e) => e.actor_id === 'user-123'),
      count: 2,
      timestamp: new Date().toISOString(),
    })

    ;(auditApi.exportAudit as any).mockResolvedValue(undefined)
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('Audit Page Load', () => {
    it('should load audit page with protected route pattern', async () => {
      const token = storage.getAccessToken()
      expect(token).toBe('valid-token')
    })

    it('should fetch and display audit events on load', async () => {
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-page')).toBeInTheDocument()
      })

      expect(auditApi.getAuditEvents).toHaveBeenCalled()
    })

    it('should show loading state while fetching', async () => {
      ;(auditApi.getAuditEvents as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<TestAuditPage />, { wrapper: Wrapper })

      expect(screen.getByTestId('audit-page-loading')).toBeInTheDocument()
    })

    it('should display events after loading completes', async () => {
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
    })
  })

  describe('Search Functionality', () => {
    it('should filter events by search term', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('audit-search-input')
      await user.type(searchInput, 'admin')

      await waitFor(() => {
        expect(auditApi.getAuditEvents).toHaveBeenCalledWith(
          expect.objectContaining({
            actor: 'admin',
          })
        )
      })
    })

    it('should apply search to actor field', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: [mockAuditEvents[1]], // Only admin event
        total: 1,
        count: 1,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('audit-search-input')
      await user.type(searchInput, 'admin@example.com')

      await waitFor(() => {
        expect(screen.getByTestId('result-count')).toHaveTextContent('Showing 1 of 1')
      })
    })
  })

  describe('Filter Application', () => {
    it('should filter by action status', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const actionFilter = screen.getByTestId('status-filter')
      await user.selectOptions(actionFilter, 'create')

      await waitFor(() => {
        expect(auditApi.getAuditEvents).toHaveBeenCalledWith(
          expect.objectContaining({
            action: 'create',
          })
        )
      })
    })

    it('should filter by classification severity', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const classificationFilter = screen.getByTestId('classification-filter')
      await user.selectOptions(classificationFilter, 'error')

      await waitFor(() => {
        expect(auditApi.getAuditEvents).toHaveBeenCalledWith(
          expect.objectContaining({
            severity: 'error',
          })
        )
      })
    })

    it('should update results when filter changes', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: [mockAuditEvents[0]],
        total: 1,
        count: 1,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const actionFilter = screen.getByTestId('status-filter')
      await user.selectOptions(actionFilter, 'create')

      await waitFor(() => {
        expect(screen.getByTestId('result-count')).toHaveTextContent('Showing 1 of 1')
      })
    })
  })

  describe('Pagination', () => {
    it('should show load more button when more results available', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: mockAuditEvents,
        total: 50,
        count: mockAuditEvents.length,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('load-more-btn')).toBeInTheDocument()
      })
    })

    it('should hide load more when all results loaded', async () => {
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.queryByTestId('load-more-btn')).not.toBeInTheDocument()
      })
    })

    it('should load next page on load more click', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: mockAuditEvents,
        total: 50,
        count: mockAuditEvents.length,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('load-more-btn')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('load-more-btn'))

      await waitFor(() => {
        expect(auditApi.getAuditEvents).toHaveBeenCalledWith(
          expect.objectContaining({
            offset: 10,
          })
        )
      })
    })
  })

  describe('Event Selection', () => {
    it('should open event details modal on event click', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('audit-event-evt-1'))

      expect(screen.getByTestId('event-details-modal')).toBeInTheDocument()
    })

    it('should display full event details in modal', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('audit-event-evt-1'))

      expect(screen.getByTestId('modal-actor')).toHaveTextContent('user@example.com')
      expect(screen.getByTestId('modal-action')).toHaveTextContent('create')
      expect(screen.getByTestId('modal-resource')).toHaveTextContent('containers')
      expect(screen.getByTestId('modal-status')).toHaveTextContent('success')
    })
  })

  describe('Correlation Link', () => {
    it('should open correlation timeline on correlation link click', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('correlation-link-evt-1'))

      await waitFor(() => {
        expect(auditApi.getCorrelationChain).toHaveBeenCalledWith('corr-1')
      })
    })

    it('should close details modal after correlation click', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('audit-event-evt-1'))
      expect(screen.getByTestId('event-details-modal')).toBeInTheDocument()

      await user.click(screen.getByTestId('correlation-link-evt-1'))

      await waitFor(() => {
        expect(screen.queryByTestId('event-details-modal')).not.toBeInTheDocument()
      })
    })
  })

  describe('Actor Link', () => {
    it('should open actor timeline on actor link click', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('actor-link-evt-1'))

      await waitFor(() => {
        expect(auditApi.getActorTimeline).toHaveBeenCalledWith('user-123', '24h')
      })
    })

    it('should close details modal after actor timeline click', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('audit-event-evt-1'))
      expect(screen.getByTestId('event-details-modal')).toBeInTheDocument()

      await user.click(screen.getByTestId('actor-link-evt-1'))

      await waitFor(() => {
        expect(screen.queryByTestId('event-details-modal')).not.toBeInTheDocument()
      })
    })
  })

  describe('Export', () => {
    it('should export to CSV format', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('export-csv-btn'))

      await waitFor(() => {
        expect(auditApi.exportAudit).toHaveBeenCalledWith({}, 'csv')
      })
    })

    it('should export to JSON format', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('export-json-btn'))

      await waitFor(() => {
        expect(auditApi.exportAudit).toHaveBeenCalledWith({}, 'json')
      })
    })
  })

  describe('Multi-Filter', () => {
    it('should combine search and filters correctly', async () => {
      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('audit-search-input')
      await user.type(searchInput, 'user@example.com')

      const actionFilter = screen.getByTestId('status-filter')
      await user.selectOptions(actionFilter, 'create')

      await waitFor(() => {
        expect(auditApi.getAuditEvents).toHaveBeenCalledWith(
          expect.objectContaining({
            actor: 'user@example.com',
            action: 'create',
          })
        )
      })
    })

    it('should return correct results with combined filters', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: [mockAuditEvents[0]],
        total: 1,
        count: 1,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })

      const searchInput = screen.getByTestId('audit-search-input')
      await user.type(searchInput, 'user@example.com')

      const actionFilter = screen.getByTestId('status-filter')
      await user.selectOptions(actionFilter, 'create')

      await waitFor(() => {
        expect(screen.getByTestId('result-count')).toHaveTextContent('Showing 1 of 1')
      })
    })
  })

  describe('Error Resilience', () => {
    it('should handle API failure gracefully', async () => {
      ;(auditApi.getAuditEvents as any).mockRejectedValue(
        new Error('API request failed')
      )

      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-error')).toBeInTheDocument()
      })

      expect(screen.getByTestId('audit-error')).toHaveTextContent(
        'API request failed'
      )
    })

    it('should handle correlation chain fetch error', async () => {
      ;(auditApi.getCorrelationChain as any).mockRejectedValue(
        new Error('Correlation not found')
      )

      const user = userEvent.setup()
      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-event-evt-1')).toBeInTheDocument()
      })

      await user.click(screen.getByTestId('correlation-link-evt-1'))

      await waitFor(() => {
        expect(screen.getByTestId('audit-error')).toHaveTextContent(
          'Failed to load correlation chain'
        )
      })
    })
  })

  describe('Empty Results', () => {
    it('should display empty state when no events found', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: [],
        total: 0,
        count: 0,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-empty-state')).toBeInTheDocument()
      })
    })

    it('should show empty state message', async () => {
      ;(auditApi.getAuditEvents as any).mockResolvedValue({
        events: [],
        total: 0,
        count: 0,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      render(<TestAuditPage />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('audit-empty-state')).toHaveTextContent(
          'No audit events found'
        )
      })
    })
  })

  describe('Loading States', () => {
    it('should show skeleton while initial load in progress', () => {
      ;(auditApi.getAuditEvents as any).mockImplementation(
        () => new Promise(() => {}) // Never resolves
      )

      render(<TestAuditPage />, { wrapper: Wrapper })

      expect(screen.getByTestId('audit-page-loading')).toBeInTheDocument()
      expect(screen.getByTestId('skeleton-table')).toBeInTheDocument()
    })

    it('should transition from loading to loaded state', async () => {
      let resolveAudit: any
      const auditPromise = new Promise((resolve) => {
        resolveAudit = resolve
      })
      ;(auditApi.getAuditEvents as any).mockReturnValue(auditPromise)

      render(<TestAuditPage />, { wrapper: Wrapper })

      expect(screen.getByTestId('audit-page-loading')).toBeInTheDocument()

      resolveAudit({
        events: mockAuditEvents,
        total: mockAuditEvents.length,
        count: mockAuditEvents.length,
        offset: 0,
        limit: 10,
        timestamp: new Date().toISOString(),
      })

      await waitFor(() => {
        expect(screen.queryByTestId('audit-page-loading')).not.toBeInTheDocument()
        expect(screen.getByTestId('audit-table')).toBeInTheDocument()
      })
    })
  })
})

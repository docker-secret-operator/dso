import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/events',
}))

const fetchEventsMock = vi.fn()
vi.mock('@/lib/api/events', () => ({
  fetchEvents: () => fetchEventsMock(),
}))

const useWebSocketMock = vi.fn()
vi.mock('@/hooks/useWebSocket', async () => {
  const actual = await vi.importActual<typeof import('@/hooks/useWebSocket')>('@/hooks/useWebSocket')
  return {
    ...actual,
    useWebSocket: (...args: unknown[]) => useWebSocketMock(...args),
  }
})

import { EventsContent } from '@/components/events-content'

const disconnected = { events: [], connectionState: 'disconnected' as const }
const connected = { events: [], connectionState: 'connected' as const }

const rotationSucceeded = {
  timestamp: '2026-08-14T10:00:00Z',
  secret: 'db-password',
  container: 'a84f2c91e73b4c1a9e2f6d8b0c3a5f7e9d1b3c5a7f9e1d3b5c7a9f1e3d5b7c9a',
  event_type: 'rotation_succeeded',
  status: 'success',
}
const injectionFailed = {
  timestamp: '2026-08-14T09:00:00Z',
  secret: 'api-key',
  event_type: 'injection_failed',
  status: 'failure',
  error: 'provider timeout after 30s',
}

describe('EventsContent', () => {
  beforeEach(() => {
    fetchEventsMock.mockReset()
    useWebSocketMock.mockReset()
  })

  it('shows a table-shaped loading state before the initial history resolves', () => {
    fetchEventsMock.mockReturnValue(new Promise(() => {}))
    useWebSocketMock.mockReturnValue(disconnected)

    render(<EventsContent />)
    const loading = screen.getByTestId('events-loading')
    expect(loading).toBeInTheDocument()
    expect(loading).toHaveAttribute('aria-busy', 'true')
  })

  it('renders real events loaded from GET /api/events', async () => {
    fetchEventsMock.mockResolvedValue([rotationSucceeded])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getByText('rotation succeeded')).toBeInTheDocument()
  })

  it('shows the honest empty state when there is no history and no live events', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-empty')).toBeInTheDocument())
    expect(screen.getByText('No recent events')).toBeInTheDocument()
    expect(screen.queryByTestId('events-error')).not.toBeInTheDocument()
  })

  it('shows the unavailable/error state when the request fails, distinct from empty', async () => {
    fetchEventsMock.mockRejectedValue(new Error('Failed to load events (500)'))
    useWebSocketMock.mockReturnValue(disconnected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-error')).toBeInTheDocument())
    expect(screen.getByText('Unable to load events')).toBeInTheDocument()
    expect(screen.queryByTestId('events-empty')).not.toBeInTheDocument()
  })

  it('merges in a live incoming event from the WebSocket hook', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue({
      events: [{ timestamp: '2026-08-14T11:00:00Z', secret: 'api-key', event_type: 'injection_succeeded', status: 'success' }],
      connectionState: 'connected',
    })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    expect(screen.getByText('api-key')).toBeInTheDocument()
  })

  it('reflects the reconnecting connection state', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'reconnecting' })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('ws-connection-state')).toHaveTextContent('reconnecting'))
  })

  it('renders status with icon + text via the shared StatusBadge, not color alone', async () => {
    fetchEventsMock.mockResolvedValue([rotationSucceeded])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    const statusCell = screen.getByText('success')
    expect(statusCell.closest('div')?.querySelector('svg')).toBeTruthy()
  })

  it('renders a relative time with the exact timestamp available on hover/title', async () => {
    fetchEventsMock.mockResolvedValue([rotationSucceeded])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    const table = screen.getByRole('table')
    const timeCell = within(table).getAllByText(/ago$/)[0]
    expect(timeCell).toHaveAttribute('title')
  })

  describe('search', () => {
    it('narrows visible rows by event type, resource, or message', async () => {
      fetchEventsMock.mockResolvedValue([rotationSucceeded, injectionFailed])
      useWebSocketMock.mockReturnValue(connected)
      const user = userEvent.setup()
      render(<EventsContent />)

      await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('events-search'), 'db-password')

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.queryByText('api-key')).not.toBeInTheDocument()
    })

    it('shows a distinct "no match" state when the search matches nothing', async () => {
      fetchEventsMock.mockResolvedValue([rotationSucceeded])
      useWebSocketMock.mockReturnValue(connected)
      const user = userEvent.setup()
      render(<EventsContent />)

      await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('events-search'), 'nonexistent')

      expect(screen.getByTestId('events-no-match')).toBeInTheDocument()
    })
  })

  describe('filters', () => {
    it('filters by status', async () => {
      fetchEventsMock.mockResolvedValue([rotationSucceeded, injectionFailed])
      useWebSocketMock.mockReturnValue(connected)
      const user = userEvent.setup()
      render(<EventsContent />)

      await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-status'), 'failure')

      expect(screen.queryByText('db-password')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('filters by event type', async () => {
      fetchEventsMock.mockResolvedValue([rotationSucceeded, injectionFailed])
      useWebSocketMock.mockReturnValue(connected)
      const user = userEvent.setup()
      render(<EventsContent />)

      await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-event-type'), 'injection_failed')

      expect(screen.queryByText('db-password')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('shows a clear-filters action only when a filter is active, and it resets state', async () => {
      fetchEventsMock.mockResolvedValue([rotationSucceeded, injectionFailed])
      useWebSocketMock.mockReturnValue(connected)
      const user = userEvent.setup()
      render(<EventsContent />)

      await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()

      await user.selectOptions(screen.getByTestId('filter-status'), 'failure')
      expect(screen.getByTestId('clear-filters')).toBeInTheDocument()

      await user.click(screen.getByTestId('clear-filters'))
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()
      expect(screen.getByText('db-password')).toBeInTheDocument()
    })
  })

  it('sorts by event type when the Event column header is activated', async () => {
    fetchEventsMock.mockResolvedValue([rotationSucceeded, injectionFailed])
    useWebSocketMock.mockReturnValue(connected)
    const user = userEvent.setup()
    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: /^event$/i }))

    const rows = screen.getAllByRole('row').map((r) => r.textContent)
    // Ascending by event_type: "injection_failed" before "rotation_succeeded".
    expect(rows.findIndex((t) => t?.includes('api-key'))).toBeLessThan(rows.findIndex((t) => t?.includes('db-password')))
  })

  it('expands a row to show safe metadata detail, including the message, without a value field', async () => {
    fetchEventsMock.mockResolvedValue([injectionFailed])
    useWebSocketMock.mockReturnValue(connected)
    const user = userEvent.setup()
    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    await user.click(screen.getByText('injection failed'))

    const detail = screen.getByTestId(/event-detail-/)
    expect(within(detail).getByText('provider timeout after 30s')).toBeInTheDocument()
    expect(within(detail).queryByText(/reveal/i)).not.toBeInTheDocument()
  })

  it('never renders a plaintext/value field even if the backend response includes one', async () => {
    fetchEventsMock.mockResolvedValue([{ ...rotationSucceeded, value: 'super-secret-plaintext-value', plaintext: 'another-secret-plaintext-value' }])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    expect(screen.queryByText(/super-secret-plaintext-value/)).not.toBeInTheDocument()
    expect(screen.queryByText(/another-secret-plaintext-value/)).not.toBeInTheDocument()
  })

  it('never renders the event-secret sentinel anywhere except an explicitly safe field', async () => {
    const sentinel = 'DSO_EVENT_SECRET_SENTINEL_7d21a4'
    fetchEventsMock.mockResolvedValue([{ ...rotationSucceeded, value: sentinel }])
    useWebSocketMock.mockReturnValue(connected)

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-content')).toBeInTheDocument())
    expect(document.body.textContent ?? '').not.toContain(sentinel)
  })
})

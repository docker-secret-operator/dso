import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

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

describe('EventsContent', () => {
  beforeEach(() => {
    fetchEventsMock.mockReset()
    useWebSocketMock.mockReset()
  })

  it('shows a loading state before the initial history resolves', () => {
    fetchEventsMock.mockReturnValue(new Promise(() => {}))
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'disconnected' })

    render(<EventsContent />)
    expect(screen.getByTestId('events-loading')).toBeInTheDocument()
  })

  it('renders events loaded from GET /api/events', async () => {
    fetchEventsMock.mockResolvedValue([
      { timestamp: '2026-08-14T10:00:00Z', secret: 'db-password', event_type: 'rotate', status: 'success' },
    ])
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'connected' })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-list')).toBeInTheDocument())
    expect(screen.getByText(/db-password/)).toBeInTheDocument()
  })

  it('shows an honest empty state when there is no history and no live events', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'connected' })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-empty')).toBeInTheDocument())
  })

  it('merges in a live incoming event from the WebSocket hook', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue({
      events: [{ timestamp: '2026-08-14T11:00:00Z', secret: 'api-key', event_type: 'inject', status: 'success' }],
      connectionState: 'connected',
    })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('events-list')).toBeInTheDocument())
    expect(screen.getByText(/api-key/)).toBeInTheDocument()
  })

  it('reflects the reconnecting connection state', async () => {
    fetchEventsMock.mockResolvedValue([])
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'reconnecting' })

    render(<EventsContent />)

    await waitFor(() => expect(screen.getByTestId('ws-connection-state')).toHaveTextContent('reconnecting'))
  })
})

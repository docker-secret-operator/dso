import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, waitFor, act } from '@testing-library/react'
import { useWebSocket, type ConnectionState } from '@/hooks/useWebSocket'

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  closed = false

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  close() {
    this.closed = true
    this.onclose?.()
  }

  send() {}
}

function TestComponent({ onState }: { onState: (s: { events: unknown[]; isConnected: boolean; connectionState: ConnectionState }) => void }) {
  const state = useWebSocket('/api/events/ws')
  onState({ events: state.events, isConnected: state.isConnected, connectionState: state.connectionState })
  return null
}

function TestComponentWithCallbacks({
  onState,
  seed,
}: {
  onState: (s: { events: unknown[] }) => void
  seed: number
}) {
  // A new inline callback reference on every render -- this is what
  // previously caused the socket to be torn down and reopened.
  const state = useWebSocket('/api/events/ws', {
    onConnect: () => { void seed },
  })
  onState({ events: state.events })
  return null
}

function latestState<T>(onState: ReturnType<typeof vi.fn>): T {
  return onState.mock.calls[onState.mock.calls.length - 1][0]
}

describe('useWebSocket', () => {
  beforeEach(() => {
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('opens exactly one connection on mount and closes it on unmount (no duplicate connections)', async () => {
    const onState = vi.fn()
    const { unmount } = render(<TestComponent onState={onState} />)

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))
    const sock = FakeWebSocket.instances[0]
    expect(sock.closed).toBe(false)

    unmount()
    expect(sock.closed).toBe(true)
  })

  it('ignores malformed events instead of crashing', async () => {
    const onState = vi.fn()
    render(<TestComponent onState={onState} />)

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))
    const sock = FakeWebSocket.instances[0]
    sock.onopen?.()

    expect(() => {
      sock.onmessage?.({ data: JSON.stringify({ nonsense: true }) } as MessageEvent)
    }).not.toThrow()

    expect(() => {
      sock.onmessage?.({ data: 'not json' } as MessageEvent)
    }).not.toThrow()
  })

  it('accepts a valid real-shaped event (timestamp/secret/event_type/status)', async () => {
    const onState = vi.fn()
    render(<TestComponent onState={onState} />)

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))
    const sock = FakeWebSocket.instances[0]
    sock.onopen?.()
    sock.onmessage?.({
      data: JSON.stringify({ timestamp: '2026-08-14T10:00:00Z', secret: 'db-password', event_type: 'rotate', status: 'success' }),
    } as MessageEvent)

    await waitFor(() => {
      const last = onState.mock.calls[onState.mock.calls.length - 1][0]
      expect(last.events.length).toBe(1)
    })
  })

  it('transitions to disconnected once MAX_RECONNECT_ATTEMPTS is exhausted', async () => {
    vi.useFakeTimers()
    try {
      const onState = vi.fn()
      render(<TestComponent onState={onState} />)

      // Fail the connection over and over; each onclose schedules another
      // connect() after the backoff delay. Drive through all 20 attempts.
      for (let i = 0; i < 20; i++) {
        await act(async () => {
          await vi.waitFor(() => expect(FakeWebSocket.instances.length).toBe(i + 1))
        })
        const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
        act(() => {
          sock.onclose?.()
        })
        act(() => {
          vi.advanceTimersByTime(30000)
        })
      }

      const last = latestState<{ connectionState: ConnectionState }>(onState)
      expect(last.connectionState).toBe('disconnected')
    } finally {
      vi.useRealTimers()
    }
  })

  it('cancels a pending reconnect on unmount (no connection created after)', async () => {
    vi.useFakeTimers()
    try {
      const onState = vi.fn()
      const { unmount } = render(<TestComponent onState={onState} />)

      await act(async () => {
        await vi.waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))
      })
      const sock = FakeWebSocket.instances[0]
      act(() => {
        sock.onclose?.()
      })

      // A reconnect is now scheduled (1s backoff). Unmount before it fires.
      unmount()

      act(() => {
        vi.advanceTimersByTime(60000)
      })

      // No new socket should have been created after unmount.
      expect(FakeWebSocket.instances.length).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does not open a new socket when the caller passes new inline callback references on re-render', async () => {
    const onState = vi.fn()
    const { rerender } = render(<TestComponentWithCallbacks onState={onState} seed={1} />)

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))

    // Re-render with a brand new inline onConnect function reference.
    rerender(<TestComponentWithCallbacks onState={onState} seed={2} />)
    rerender(<TestComponentWithCallbacks onState={onState} seed={3} />)

    expect(FakeWebSocket.instances.length).toBe(1)
    expect(FakeWebSocket.instances[0].closed).toBe(false)
  })

  it('guards against overlapping connect() calls creating duplicate sockets', async () => {
    const onState = vi.fn()
    render(<TestComponent onState={onState} />)

    await waitFor(() => expect(FakeWebSocket.instances.length).toBe(1))
    // Only one socket should ever be live for a single mounted hook instance.
    expect(FakeWebSocket.instances.length).toBe(1)
  })
})

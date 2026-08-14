import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { useWebSocket } from '@/hooks/useWebSocket'

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
  }

  send() {}
}

function TestComponent({ onState }: { onState: (s: { events: unknown[]; isConnected: boolean }) => void }) {
  const state = useWebSocket('/api/events/ws')
  onState({ events: state.events, isConnected: state.isConnected })
  return null
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
})

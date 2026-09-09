'use client'

import { useEffect, useState, useCallback, useRef } from 'react'

/**
 * PASS 3 SCOPE: this Event type matches the REAL backend event shape, not
 * the aspirational feature/web-ui shape Pass 1 stubbed in. The Go side
 * (internal/server/eventstore.go) defines `Event` as a bare
 * `map[string]interface{}` -- the actual fields written onto it come from
 * internal/injector/docker.go's LogInjectionEvent:
 *
 *   { "timestamp": RFC3339 string, "secret": string, "container": string,
 *     "event_type": string, "status": string, "error"?: string }
 *
 * There is no "action", "severity", or "message" field server-side. Fields
 * beyond timestamp/status are treated as optional/best-effort since the
 * EventStore is a generic map and other producers could shape events
 * differently in the future.
 */
export interface Event {
  timestamp: string
  secret?: string
  container?: string
  event_type?: string
  status?: string
  error?: string
  // Any other producer-specific fields are tolerated but not relied on.
  [key: string]: unknown
}

export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'

// Runtime validation for Event schema. Deliberately lenient: the server-side
// EventStore type is a bare map with only "timestamp" guaranteed by
// convention, so over-constraining this would silently drop real events.
function isValidEvent(data: unknown): data is Event {
  if (typeof data !== 'object' || data === null) {
    return false
  }

  const obj = data as Record<string, unknown>
  return typeof obj.timestamp === 'string'
}

// Fixed backoff sequence: 1s, 2s, 5s, 10s, 30s (stays at 30s afterward)
const BACKOFF_DELAYS = [1000, 2000, 5000, 10000, 30000]
const MAX_RECONNECT_ATTEMPTS = 20

interface UseWebSocketOptions {
  path?: string
  maxMessageHistory?: number
  onError?: (error: Error) => void
  onConnect?: () => void
  onReconnect?: () => void
  onDisconnect?: () => void
}

/**
 * Connects to DSO's event WebSocket. Authentication rides on the browser's
 * automatic same-origin cookie attachment for the WS upgrade handshake --
 * the Go backend's CheckHandshake (internal/webuiauth/middleware.go) reads
 * the same `dso_webui_session` cookie that HTTP requests use. There is no
 * token to append as a `?token=` query param (feature/web-ui's version did
 * this via a localStorage-stored bearer token, which no longer exists).
 */
export function useWebSocket(path = '/api/events/ws', options: UseWebSocketOptions = {}) {
  const {
    maxMessageHistory = 100,
    onError,
    onConnect,
    onReconnect,
    onDisconnect,
  } = options

  const [events, setEvents] = useState<Event[]>([])
  const [connectionState, setConnectionState] = useState<ConnectionState>('disconnected')
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<NodeJS.Timeout>()
  const reconnectAttemptsRef = useRef(0)
  const isFirstConnectRef = useRef(true)
  const mountedRef = useRef(true)
  const connectingRef = useRef(false)

  // Latest-callback refs: onConnect/onReconnect/onDisconnect/onError are
  // read from these inside `connect`, so a caller passing new inline
  // function references on every render doesn't change `connect`'s own
  // identity and doesn't tear down/reopen the socket.
  const onErrorRef = useRef(onError)
  const onConnectRef = useRef(onConnect)
  const onReconnectRef = useRef(onReconnect)
  const onDisconnectRef = useRef(onDisconnect)
  useEffect(() => {
    onErrorRef.current = onError
    onConnectRef.current = onConnect
    onReconnectRef.current = onReconnect
    onDisconnectRef.current = onDisconnect
  }, [onError, onConnect, onReconnect, onDisconnect])

  const connect = useCallback(() => {
    if (typeof window === 'undefined' || !mountedRef.current) return
    // Guard against overlapping connect() calls creating duplicate sockets
    // (e.g. a reconnect timer firing while a previous connect is still
    // pending, or a caller invoking the manual reconnect while one is live).
    if (connectingRef.current || wsRef.current) return

    try {
      // Check max reconnection attempts
      if (reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
        setConnectionState('disconnected')
        const error = new Error('WebSocket: Max reconnection attempts reached')
        onErrorRef.current?.(error)
        return
      }

      connectingRef.current = true

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const host = window.location.host

      // No token to attach -- the session cookie rides along automatically
      // with the WS upgrade request for same-origin connections.
      const wsUrl = `${protocol}//${host}${path}`

      const ws = new WebSocket(wsUrl)

      ws.onopen = () => {
        connectingRef.current = false
        if (!mountedRef.current) { ws.close(); return }
        setConnectionState('connected')
        const wasReconnect = !isFirstConnectRef.current
        isFirstConnectRef.current = false
        reconnectAttemptsRef.current = 0
        if (wasReconnect) {
          onReconnectRef.current?.()
        } else {
          onConnectRef.current?.()
        }
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)

          // Validate message schema - must have required Event fields
          if (!isValidEvent(data)) {
            return
          }

          const validEvent = data as Event
          setEvents((prev) => {
            // Add new event and maintain bounded history
            // Slice first to avoid unbounded growth during state updates
            const bounded = prev.slice(0, maxMessageHistory - 1)
            return [validEvent, ...bounded]
          })
        } catch {
          // Malformed frame -- drop it silently, nothing actionable to log
          // without a real logging convention in this project.
        }
      }

      ws.onerror = () => {
        onErrorRef.current?.(new Error('WebSocket error'))
      }

      ws.onclose = () => {
        connectingRef.current = false
        wsRef.current = null
        if (!mountedRef.current) return
        onDisconnectRef.current?.()

        // Check if we've exceeded max reconnection attempts
        if (reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
          setConnectionState('disconnected')
          const error = new Error('WebSocket: Max reconnection attempts reached')
          onErrorRef.current?.(error)
          return
        }

        setConnectionState('reconnecting')

        // Progress through fixed delay sequence
        const idx = Math.min(reconnectAttemptsRef.current, BACKOFF_DELAYS.length - 1)
        const delay = BACKOFF_DELAYS[idx]
        reconnectAttemptsRef.current += 1
        // Schedule reconnection with error handling
        reconnectTimeoutRef.current = setTimeout(() => {
          if (!mountedRef.current) return
          try {
            connect()
          } catch (err) {
            const error = err instanceof Error ? err : new Error('Reconnection failed')
            onErrorRef.current?.(error)
          }
        }, delay)
      }

      wsRef.current = ws
    } catch (err) {
      connectingRef.current = false
      onErrorRef.current?.(err instanceof Error ? err : new Error('Unknown WebSocket error'))
    }
  }, [path, maxMessageHistory])

  // Safe send wrapper that validates connection state before sending
  const send = useCallback((message: string) => {
    if (connectionState !== 'connected' || !wsRef.current) {
      const error = new Error('WebSocket not connected')
      onErrorRef.current?.(error)
      throw error
    }
    try {
      wsRef.current.send(message)
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Failed to send message')
      onErrorRef.current?.(error)
      throw error
    }
  }, [connectionState])

  useEffect(() => {
    mountedRef.current = true
    connect()

    return () => {
      mountedRef.current = false
      connectingRef.current = false
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current)
      if (wsRef.current) wsRef.current.close()
    }
  }, [connect])

  return {
    events,
    isConnected: connectionState === 'connected',
    connectionState,
    ws: wsRef.current,
    send, // Safe send wrapper
  }
}

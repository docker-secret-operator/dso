'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import { Event } from '@/lib/api-client'

export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected'

// Runtime validation for Event schema
function isValidEvent(data: unknown): data is Event {
  if (typeof data !== 'object' || data === null) {
    return false
  }

  const obj = data as Record<string, unknown>

  // Required fields for Event
  return (
    typeof obj.timestamp === 'string' &&
    typeof obj.action === 'string' &&
    typeof obj.severity === 'string' &&
    typeof obj.message === 'string' &&
    ['info', 'warning', 'error'].includes(obj.severity as string)
  )
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

  const connect = useCallback(() => {
    if (typeof window === 'undefined' || !mountedRef.current) return

    try {
      // Check max reconnection attempts
      if (reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
        const error = new Error('WebSocket: Max reconnection attempts reached')
        onError?.(error)
        return
      }

      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const host = window.location.host

      // Include authentication token in URL (localStorage — consistent app-wide)
      let wsUrl = `${protocol}//${host}${path}`
      const token = typeof localStorage !== 'undefined' ? localStorage.getItem('dso_api_token') : null
      if (token) {
        wsUrl += `?token=${encodeURIComponent(token)}`
      }

      const ws = new WebSocket(wsUrl)

      ws.onopen = () => {
        if (!mountedRef.current) { ws.close(); return }
        if (process.env.NODE_ENV === 'development') console.log('[WebSocket] Connected')
        setConnectionState('connected')
        const wasReconnect = !isFirstConnectRef.current
        isFirstConnectRef.current = false
        reconnectAttemptsRef.current = 0
        if (wasReconnect) {
          onReconnect?.()
        } else {
          onConnect?.()
        }
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)

          // Validate message schema - must have required Event fields
          if (!isValidEvent(data)) {
            if (process.env.NODE_ENV === 'development') {
              console.error('[WebSocket] Invalid event schema:', data)
            }
            return
          }

          const validEvent = data as Event
          setEvents((prev) => {
            // Add new event and maintain bounded history
            // Slice first to avoid unbounded growth during state updates
            const bounded = prev.slice(0, maxMessageHistory - 1)
            return [validEvent, ...bounded]
          })
        } catch (err) {
          if (process.env.NODE_ENV === 'development') {
            console.error('[WebSocket] Failed to parse message:', err)
          }
        }
      }

      ws.onerror = (err) => {
        if (process.env.NODE_ENV === 'development') console.error('[WebSocket] Error:', err)
        onError?.(new Error('WebSocket error'))
      }

      ws.onclose = () => {
        if (!mountedRef.current) return
        if (process.env.NODE_ENV === 'development') console.log('[WebSocket] Disconnected')
        setConnectionState('reconnecting')
        onDisconnect?.()

        // Check if we've exceeded max reconnection attempts
        if (reconnectAttemptsRef.current >= MAX_RECONNECT_ATTEMPTS) {
          const error = new Error('WebSocket: Max reconnection attempts reached')
          onError?.(error)
          return
        }

        // Progress through fixed delay sequence
        const idx = Math.min(reconnectAttemptsRef.current, BACKOFF_DELAYS.length - 1)
        const delay = BACKOFF_DELAYS[idx]
        reconnectAttemptsRef.current += 1
        if (process.env.NODE_ENV === 'development') {
          console.log(`[WebSocket] Reconnecting in ${delay}ms (attempt ${reconnectAttemptsRef.current})`)
        }
        // Schedule reconnection with error handling
        reconnectTimeoutRef.current = setTimeout(() => {
          try {
            connect()
          } catch (err) {
            const error = err instanceof Error ? err : new Error('Reconnection failed')
            onError?.(error)
          }
        }, delay)
      }

      wsRef.current = ws
    } catch (err) {
      onError?.(err instanceof Error ? err : new Error('Unknown WebSocket error'))
    }
  }, [path, maxMessageHistory, onConnect, onReconnect, onDisconnect, onError])

  // Safe send wrapper that validates connection state before sending
  const send = useCallback((message: string) => {
    if (connectionState !== 'connected' || !wsRef.current) {
      const error = new Error('WebSocket not connected')
      onError?.(error)
      throw error
    }
    try {
      wsRef.current.send(message)
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Failed to send message')
      onError?.(error)
      throw error
    }
  }, [connectionState, onError])

  useEffect(() => {
    mountedRef.current = true
    connect()

    return () => {
      mountedRef.current = false
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

'use client'

import { useEffect, useState, useCallback, useRef } from 'react'
import { Event } from '@/lib/api-client'

interface UseWebSocketOptions {
  path?: string
  maxMessageHistory?: number
  onError?: (error: Error) => void
  onConnect?: () => void
  onDisconnect?: () => void
}

export function useWebSocket(path = '/api/events/ws', options: UseWebSocketOptions = {}) {
  const {
    maxMessageHistory = 100,
    onError,
    onConnect,
    onDisconnect,
  } = options

  const [events, setEvents] = useState<Event[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<NodeJS.Timeout>()
  const reconnectAttemptsRef = useRef(0)
  const maxReconnectAttemptsRef = useRef(5)
  const reconnectDelayRef = useRef(1000)

  const connect = useCallback(() => {
    if (typeof window === 'undefined') return

    try {
      // Use same origin as dashboard server (which proxies to REST API)
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const host = window.location.host // Includes port from dashboard
      const wsUrl = `${protocol}//${host}${path}`

      const ws = new WebSocket(wsUrl)

      ws.onopen = () => {
        console.log('[WebSocket] Connected')
        setIsConnected(true)
        reconnectAttemptsRef.current = 0
        reconnectDelayRef.current = 1000
        onConnect?.()
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as Event
          setEvents((prev) => {
            const updated = [data, ...prev]
            return updated.slice(0, maxMessageHistory)
          })
        } catch (err) {
          console.error('[WebSocket] Failed to parse message:', err)
        }
      }

      ws.onerror = (err) => {
        console.error('[WebSocket] Error:', err)
        const error = new Error('WebSocket error')
        onError?.(error)
      }

      ws.onclose = () => {
        console.log('[WebSocket] Disconnected')
        setIsConnected(false)
        onDisconnect?.()

        // Attempt to reconnect with exponential backoff
        if (reconnectAttemptsRef.current < maxReconnectAttemptsRef.current) {
          reconnectAttemptsRef.current += 1
          const delay = Math.min(
            reconnectDelayRef.current * Math.pow(2, reconnectAttemptsRef.current - 1),
            30000
          )
          reconnectDelayRef.current = delay

          console.log(
            `[WebSocket] Reconnecting in ${delay}ms (attempt ${reconnectAttemptsRef.current})`
          )
          reconnectTimeoutRef.current = setTimeout(connect, delay)
        }
      }

      wsRef.current = ws
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Unknown WebSocket error')
      onError?.(error)
    }
  }, [path, maxMessageHistory, onConnect, onDisconnect, onError])

  useEffect(() => {
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [connect])

  return {
    events,
    isConnected,
    ws: wsRef.current,
  }
}

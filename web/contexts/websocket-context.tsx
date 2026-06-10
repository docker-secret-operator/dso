'use client'

import { createContext, useContext, useCallback, ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useWebSocket, ConnectionState } from '@/hooks/useWebSocket'
import { Event } from '@/lib/api-client'

interface WebSocketContextValue {
  events: Event[]
  connectionState: ConnectionState
  isConnected: boolean
}

const WebSocketContext = createContext<WebSocketContextValue>({
  events: [],
  connectionState: 'disconnected',
  isConnected: false,
})

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()

  // FG6: invalidate key caches after reconnect so stale data is refreshed
  const handleReconnect = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['health'] })
    queryClient.invalidateQueries({ queryKey: ['secrets'] })
    queryClient.invalidateQueries({ queryKey: ['events'] })
    queryClient.invalidateQueries({ queryKey: ['sessions'] })
    queryClient.invalidateQueries({ queryKey: ['users'] })
    queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    queryClient.invalidateQueries({ queryKey: ['operations'] })
  }, [queryClient])

  const { events, connectionState, isConnected } = useWebSocket('/api/events/ws', {
    maxMessageHistory: 200,
    onReconnect: handleReconnect,
  })

  return (
    <WebSocketContext.Provider value={{ events, connectionState, isConnected }}>
      {children}
    </WebSocketContext.Provider>
  )
}

export function useWebSocketContext() {
  return useContext(WebSocketContext)
}

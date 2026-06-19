'use client'

import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { GlobalSearch } from './global-search'
import { Badge } from '@/components/ui/badge'
import { AlertCircle, CheckCircle, Loader2, LogOut, Wifi, WifiOff, RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuth } from '@/contexts/AuthContext'
import { useWebSocketContext } from '@/contexts/websocket-context'
import { NotificationCenter } from '@/components/notification-center'

function roleBadgeClass(role: string): string {
  switch (role) {
    case 'admin':    return 'bg-red-100 text-red-700'
    case 'approver': return 'bg-purple-100 text-purple-700'
    case 'reviewer': return 'bg-blue-100 text-blue-700'
    case 'operator': return 'bg-green-100 text-green-700'
    default:         return 'bg-gray-100 text-gray-600'
  }
}

function ConnectionIndicator() {
  const { connectionState } = useWebSocketContext()

  if (connectionState === 'connected') {
    return (
      <div className="flex items-center gap-1.5 text-xs text-green-600" title="WebSocket Connected">
        <div className="w-2 h-2 rounded-full bg-green-500" />
        <Wifi className="h-3.5 w-3.5" />
      </div>
    )
  }
  if (connectionState === 'reconnecting') {
    return (
      <div className="flex items-center gap-1.5 text-xs text-yellow-600" title="Reconnecting…">
        <div className="w-2 h-2 rounded-full bg-yellow-500 animate-pulse" />
        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
      </div>
    )
  }
  return (
    <div className="flex items-center gap-1.5 text-xs text-red-600" title="WebSocket Offline">
      <div className="w-2 h-2 rounded-full bg-red-500" />
      <WifiOff className="h-3.5 w-3.5" />
    </div>
  )
}

export function Header() {
  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 5000,
  })

  const { user, role, isLoading: userLoading, logout } = useAuth()

  const isHealthy = health?.status === 'up'

  return (
    <header className="border-b border-border bg-card">
      <div className="flex items-center justify-between px-6 py-4">
        <div>
          <h2 className="text-2xl font-semibold text-foreground">
            DSO Dashboard
          </h2>
          <p className="text-sm text-muted-foreground">
            Docker Secret Operator
          </p>
        </div>

        <div className="flex items-center gap-6">
          {/* Global Search */}
          <GlobalSearch />

          {/* Agent Status */}
          <div className="flex items-center gap-3">
            <div className="text-right">
              <p className="text-sm font-medium text-foreground">Agent Status</p>
              <p className="text-xs text-muted-foreground">
                {health?.version || 'v3.6.0'}
              </p>
            </div>

            {healthLoading ? (
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            ) : isHealthy ? (
              <CheckCircle className="h-5 w-5 text-green-600" />
            ) : (
              <AlertCircle className="h-5 w-5 text-red-600" />
            )}

            <Badge variant={isHealthy ? 'default' : 'destructive'}>
              {isHealthy ? 'UP' : 'DOWN'}
            </Badge>
          </div>

          {/* FG4: Connection status + Notifications */}
          <div className="flex items-center gap-2 border-l border-border pl-4">
            <ConnectionIndicator />
            <NotificationCenter />
          </div>

          {/* User Info */}
          <div className="flex items-center gap-3 border-l border-border pl-6">
            {userLoading ? (
              <div className="flex items-center gap-2">
                <div className="h-4 w-20 rounded bg-muted animate-pulse" />
                <div className="h-5 w-12 rounded bg-muted animate-pulse" />
              </div>
            ) : user ? (
              <>
                <div className="text-right">
                  <p className="text-sm font-medium text-foreground leading-none">
                    {user.display_name || user.username}
                  </p>
                  <p className="text-xs text-muted-foreground mt-0.5">@{user.username}</p>
                </div>
                <span className={cn('inline-flex px-2 py-0.5 rounded text-xs font-medium capitalize', roleBadgeClass(role))}>
                  {role}
                </span>
                <button
                  onClick={logout}
                  title="Sign out"
                  className="flex items-center justify-center h-8 w-8 rounded-md hover:bg-muted transition-colors"
                >
                  <LogOut className="h-4 w-4 text-muted-foreground" />
                </button>
              </>
            ) : (
              <div className="h-8 w-8 rounded-full bg-muted" />
            )}
          </div>
        </div>
      </div>
    </header>
  )
}

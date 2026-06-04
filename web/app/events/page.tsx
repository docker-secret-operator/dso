'use client'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ErrorBoundary } from '@/components/error-boundary'
import { Badge } from '@/components/ui/badge'
import { formatTime } from '@/lib/utils'
import { Loader2, AlertCircle } from 'lucide-react'
import { useWebSocket } from '@/hooks/useWebSocket'

export default function EventsPage() {
  const { events, isConnected } = useWebSocket('/api/events/ws')

  return (
    <ErrorBoundary>
      <div className="space-y-8 p-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold text-foreground">Events</h1>
          <p className="text-muted-foreground">Real-time event stream</p>
        </div>
        <div className="flex items-center gap-2">
          <div
            className={`h-2 w-2 rounded-full ${
              isConnected ? 'bg-green-500' : 'bg-red-500'
            }`}
          />
          <span className="text-sm text-muted-foreground">
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Live Events</CardTitle>
          <CardDescription>Real-time updates from the DSO agent</CardDescription>
        </CardHeader>
        <CardContent>
          {!isConnected && (
            <div className="mb-4 rounded-md bg-yellow-50 p-3 text-sm text-yellow-800">
              Connecting to event stream...
            </div>
          )}

          {events.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-muted-foreground">
              <AlertCircle className="mb-2 h-6 w-6" />
              <p>No events yet</p>
            </div>
          ) : (
            <div className="space-y-3">
              {events.map((event, idx) => (
                <div
                  key={idx}
                  className="flex items-start gap-4 border-l-4 border-muted bg-muted/50 p-4"
                  style={{
                    borderLeftColor:
                      event.severity === 'error'
                        ? '#dc2626'
                        : event.severity === 'warning'
                          ? '#f59e0b'
                          : '#3b82f6',
                  }}
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <Badge
                        variant={
                          event.severity === 'error'
                            ? 'destructive'
                            : event.severity === 'warning'
                              ? 'secondary'
                              : 'default'
                        }
                      >
                        {event.severity.toUpperCase()}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {formatTime(event.timestamp)}
                      </span>
                    </div>
                    <p className="mt-1 text-sm font-medium text-foreground">
                      {event.message}
                    </p>
                    {(event.secret_name || event.provider || event.error) && (
                      <div className="mt-1 space-y-0.5 text-xs text-muted-foreground">
                        {event.secret_name && (
                          <p>Secret: <span className="font-mono">{event.secret_name}</span></p>
                        )}
                        {event.provider && (
                          <p>Provider: <span className="capitalize">{event.provider}</span></p>
                        )}
                        {event.error && (
                          <p>Error: <span className="text-red-600">{event.error}</span></p>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
    </ErrorBoundary>
  )
}

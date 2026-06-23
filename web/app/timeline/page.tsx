'use client'

import { useQuery } from '@tanstack/react-query'
import { apiFetch } from "@/lib/api-fetch"
import { useWebSocket } from '@/hooks/useWebSocket'
import { apiClient } from '@/lib/api-client'
import { ErrorBoundary } from '@/components/error-boundary'
import { TimelineEntry, type TimelineEvent, type TimelineSeverity } from '@/components/timeline-entry'
import { TimelineFilter, type TimelineFilterType } from '@/components/timeline-filter'
import { EmptyState } from '@/components/empty-state'
import { Card, CardContent } from '@/components/ui/card'
import { useState, useMemo, useCallback } from 'react'

export default function TimelinePage() {
  // Fetch events
  const { data: events = [], isLoading: eventsLoading } = useQuery({
    queryKey: ['events'],
    queryFn: async () => {
      try {
        const response = await apiFetch('/api/events')
        if (!response.ok) return []
        const data = await response.json()
        return data.events || []
      } catch (err) {
        console.error('Failed to fetch events:', err)
        return []
      }
    },
    refetchInterval: 10000,
  })

  // Get real-time events
  const { events: wsEvents } = useWebSocket('/api/events/ws')

  // Combine all events
  const allEvents = useMemo(() => {
    const combined: TimelineEvent[] = []
    const seen = new Set<string>()

    // Add websocket events first (newer)
    wsEvents.forEach((event: any) => {
      if (!seen.has(event.id)) {
        combined.push({
          id: event.id,
          timestamp: event.timestamp || new Date().toISOString(),
          severity: mapSeverity(event.level),
          source: 'event',
          title: event.message || 'Event',
          message: event.message || '',
          metadata: event.metadata,
          execution_id: event.execution_id || (event.metadata?.execution_id as string | undefined),
        })
        seen.add(event.id)
      }
    })

    // Add fetched events
    events.forEach((event: any) => {
      if (!seen.has(event.id || event.message)) {
        combined.push({
          id: event.id || `event-${Date.now()}`,
          timestamp: event.timestamp || new Date().toISOString(),
          severity: mapSeverity(event.level || 'info'),
          source: 'event',
          title: event.message || 'Event',
          message: event.message || '',
          metadata: event.metadata,
          execution_id: event.execution_id || (event.metadata?.execution_id as string | undefined),
        })
        seen.add(event.id || event.message)
      }
    })

    // Sort by timestamp (newest first)
    return combined.sort(
      (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
    )
  }, [events, wsEvents])

  // Filter state
  const [selectedSeverities, setSelectedSeverities] = useState<TimelineFilterType[]>([])
  const [searchQuery, setSearchQuery] = useState('')

  // Filter and search
  const filteredEvents = useMemo(() => {
    let filtered = allEvents

    // Filter by severity
    if (selectedSeverities.length > 0) {
      filtered = filtered.filter((event) => selectedSeverities.includes(event.severity))
    }

    // Filter by search
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(
        (event) =>
          event.title.toLowerCase().includes(query) ||
          event.message.toLowerCase().includes(query) ||
          event.container?.toLowerCase().includes(query) ||
          event.secret?.toLowerCase().includes(query)
      )
    }

    return filtered
  }, [allEvents, selectedSeverities, searchQuery])

  const handleClearAll = useCallback(() => {
    setSelectedSeverities([])
    setSearchQuery('')
  }, [])

  return (
    <ErrorBoundary>
      <div className="p-6 space-y-6">
        {/* Header */}
        <div>
          <h1 className="text-3xl font-bold">Event Timeline</h1>
          <p className="text-gray-600 mt-1">
            Unified operational activity feed across all DSO components
          </p>
        </div>

        {/* Layout */}
        <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
          {/* Filters */}
          <div className="lg:col-span-1">
            <Card>
              <CardContent className="pt-6">
                <TimelineFilter
                  selectedSeverities={selectedSeverities}
                  onSeverityChange={setSelectedSeverities}
                  searchQuery={searchQuery}
                  onSearchChange={setSearchQuery}
                  onClearAll={handleClearAll}
                />
              </CardContent>
            </Card>
          </div>

          {/* Timeline */}
          <div className="lg:col-span-3">
            {eventsLoading ? (
              <div className="flex items-center justify-center h-64">
                <div className="text-center">
                  <div className="w-8 h-8 border-2 border-gray-300 border-t-blue-600 rounded-full animate-spin mx-auto mb-2" />
                  <p className="text-sm text-gray-600">Loading timeline...</p>
                </div>
              </div>
            ) : allEvents.length === 0 ? (
              <EmptyState
                type="empty"
                title="No events yet"
                description="Operational events will appear here as they occur"
              />
            ) : filteredEvents.length === 0 ? (
              <EmptyState
                type="no-results"
                title="No events match your filters"
                description={`No events found for: ${searchQuery}`}
                action={{
                  label: 'Clear filters',
                  onClick: handleClearAll,
                }}
              />
            ) : (
              <div className="space-y-3">
                {/* Summary */}
                <div className="text-sm text-gray-600 mb-4">
                  Showing {filteredEvents.length} of {allEvents.length} events
                </div>

                {/* Timeline entries */}
                {filteredEvents.map((event) => (
                  <TimelineEntry key={event.id} event={event} />
                ))}

                {/* Load more hint */}
                {filteredEvents.length > 0 && (
                  <div className="text-center py-6 border-t">
                    <p className="text-sm text-gray-600">
                      Showing latest events. Scroll to see older events.
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Info Banner */}
        <Card className="bg-blue-50 border-blue-200">
          <CardContent className="pt-6">
            <p className="text-sm text-blue-900">
              <strong>Real-time Monitoring:</strong> This timeline updates automatically with new events
              from your DSO deployment. Events are sorted by timestamp with newest first.
            </p>
          </CardContent>
        </Card>
      </div>
    </ErrorBoundary>
  )
}

function mapSeverity(level: string): TimelineSeverity {
  const levelLower = (level || 'info').toLowerCase()
  if (levelLower.includes('error')) return 'error'
  if (levelLower.includes('warn')) return 'warning'
  return 'info'
}

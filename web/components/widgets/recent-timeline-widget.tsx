'use client'

import { useWebSocket } from '@/hooks/useWebSocket'
import { TimelineEntry, type TimelineEvent } from '@/components/timeline-entry'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { ArrowRight } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'

export function RecentTimelineWidget() {
  const { events } = useWebSocket('/api/events/ws')

  // Get recent 10 events
  const recentEvents = useMemo(() => {
    return events
      .slice(0, 10)
      .map((event: any, idx: number) => ({
        id: event.id || `event-${idx}`,
        timestamp: event.timestamp || new Date().toISOString(),
        severity: mapSeverity(event.level),
        source: 'event' as const,
        title: event.message || 'Event',
        message: event.message || '',
        metadata: event.metadata,
      })) as TimelineEvent[]
  }, [events])

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>Recent Activity</CardTitle>
          <CardDescription>Last 10 operational events</CardDescription>
        </div>
        <Link href="/timeline">
          <Button variant="outline" size="sm" className="gap-2">
            View all
            <ArrowRight className="w-4 h-4" />
          </Button>
        </Link>
      </CardHeader>
      <CardContent>
        {recentEvents.length === 0 ? (
          <div className="text-center py-6 text-gray-600 text-sm">No recent events</div>
        ) : (
          <div className="space-y-2 max-h-96 overflow-y-auto">
            {recentEvents.map((event) => (
              <div key={event.id} className="text-sm">
                <TimelineEntry event={event} />
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function mapSeverity(level: string) {
  const levelLower = (level || 'info').toLowerCase()
  if (levelLower.includes('error')) return 'error'
  if (levelLower.includes('warn')) return 'warning'
  return 'info'
}

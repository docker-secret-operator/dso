'use client'

import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { apiClient } from '@/lib/api-client'
import { ErrorBoundary } from '@/components/error-boundary'
import {
  StatCard,
  HealthCard,
  ActivityFeed,
  CacheMetricsCard,
  DiscoverySummaryCard,
  RecentTimelineWidget,
  OperationalHotspotsWidget,
  type Activity,
  type HealthStatus,
  type CacheMetrics,
  type DiscoverySummary,
} from '@/components/widgets'
import { ConfigurationDriftWidget } from '@/components/widgets/configuration-drift-widget'
import { RecommendedActionsWidget } from '@/components/widgets/recommended-actions-widget'
import { PendingChangeSetsWidget } from '@/components/widgets/pending-changesets-widget'
import { DraftWorkspaceWidget } from '@/components/widgets/draft-workspace-widget'
import { DraftReviewsWidget } from '@/components/widgets/draft-reviews-widget'
import { Zap, TrendingUp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { LineChart } from '@/components/charts/line-chart'
import { useVisibleInterval } from '@/hooks/useVisibilityRefetch'

export default function DashboardPage() {
  const router = useRouter()

  // Fetch health
  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 5000,
  })

  // Fetch secrets
  const { data: secrets = [], isLoading: secretsLoading } = useQuery({
    queryKey: ['secrets'],
    queryFn: () => apiClient.getSecrets(),
    refetchInterval: 30000,
  })

  // Fetch discovery data
  const { data: discovery, isLoading: discoveryLoading } = useQuery({
    queryKey: ['discovery'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/discovery/docker')
        if (!response.ok) return null
        return response.json()
      } catch {
        return null
      }
    },
    refetchInterval: 30000,
  })

  // Fetch cache metrics
  const { data: cacheMetrics, isLoading: metricsLoading } = useQuery({
    queryKey: ['metrics'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/discovery/metrics')
        if (!response.ok) return null
        return response.json()
      } catch {
        return null
      }
    },
    refetchInterval: 10000,
  })

  // Fetch recent events
  const { data: events = [], isLoading: eventsLoading } = useQuery({
    queryKey: ['events', 20],
    queryFn: () => apiClient.getEvents(20),
    refetchInterval: 5000,
  })

  // FG4: compact metrics history for dashboard sparklines
  const metricsInterval = useVisibleInterval(60000)
  const { data: metricsHistory } = useQuery({
    queryKey: ['metrics', 'history', '1h', '1m'],
    queryFn: () => apiClient.getMetricsHistory({ period: '1h', granularity: '1m' }),
    refetchInterval: metricsInterval,
  })
  const mPts = metricsHistory?.data ?? []

  // Build system health statuses
  const healthStatuses: HealthStatus[] = [
    {
      name: 'DSO Agent',
      status: health?.status === 'up' ? 'healthy' : 'error',
      message: health?.status === 'up' ? 'Running normally' : 'Not responding',
    },
    {
      name: 'API Server',
      status: health ? 'healthy' : 'error',
      message: health ? 'Connected' : 'Unavailable',
    },
    {
      name: 'WebSocket',
      status: 'healthy',
      message: 'Connected',
    },
  ]

  // Build activity feed from events
  const activities: Activity[] = (events || []).map((event: any) => ({
    id: `${event.timestamp}-${event.action}`,
    title: event.action || 'Event',
    description: event.message || event.error,
    severity:
      (event.severity as 'info' | 'warning' | 'error') ||
      (event.status === 'failure' ? 'error' : 'info'),
    timestamp: event.timestamp,
  }))

  // Build cache metrics
  const metrics: CacheMetrics | undefined = cacheMetrics
    ? {
        hits: cacheMetrics.cache_hits || 0,
        misses: cacheMetrics.cache_misses || 0,
        hitRate: cacheMetrics.cache_hits
          ? (cacheMetrics.cache_hits / (cacheMetrics.cache_hits + cacheMetrics.cache_misses)) *
            100
          : 0,
        age: cacheMetrics.cache_age_ms || 0,
        isFresh: cacheMetrics.is_fresh !== false,
      }
    : undefined

  // Build discovery summary
  const discoverySummary: DiscoverySummary | undefined = discovery
    ? {
        totalContainers: discovery.total_count || 0,
        managedContainers: discovery.managed_count || 0,
        partialContainers: discovery.partial_count || 0,
        unmanagedContainers: discovery.unmanaged_count || 0,
      }
    : undefined

  const totalSecrets = secrets.length
  const secretsByProvider = secrets.reduce(
    (acc, secret: any) => {
      acc[secret.provider] = (acc[secret.provider] || 0) + 1
      return acc
    },
    {} as Record<string, number>
  )

  const rotatingSoon = secrets.filter((s: any) => {
    // Filter secrets rotating in next 7 days
    if (!s.next_rotation) return false
    const nextRotation = new Date(s.next_rotation).getTime()
    const sevenDaysFromNow = Date.now() + 7 * 24 * 60 * 60 * 1000
    return nextRotation <= sevenDaysFromNow && nextRotation > Date.now()
  }).length

  const rotationFailures = secrets.filter((s: any) => s.status === 'error').length

  return (
    <ErrorBoundary>
      <div className="space-y-8 p-8">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold">Operational Dashboard</h1>
        <p className="text-gray-600 mt-1">
          Real-time monitoring and operational insights for Docker Secret Operator
        </p>
      </div>

      {/* Row 1: Key Metrics */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
        <StatCard
          title="Total Secrets"
          value={totalSecrets}
          subtitle={`${Object.keys(secretsByProvider).length} providers`}
          icon={<Zap className="w-5 h-5" />}
          color="blue"
          loading={secretsLoading}
        />
        <StatCard
          title="Rotating Soon"
          value={rotatingSoon}
          subtitle="Next 7 days"
          color="yellow"
          loading={secretsLoading}
        />
        <StatCard
          title="Rotation Failures"
          value={rotationFailures}
          subtitle={rotationFailures === 0 ? 'All healthy' : 'Needs attention'}
          color={rotationFailures === 0 ? 'green' : 'red'}
          loading={secretsLoading}
        />
        <StatCard
          title="Total Containers"
          value={discovery?.total_count || '-'}
          subtitle="Discovered"
          color="blue"
          loading={discoveryLoading}
        />
      </div>

      {/* Row 2: Health & Discovery */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <HealthCard title="System Health" statuses={healthStatuses} loading={healthLoading} />
        <div className="md:col-span-2">
          <DiscoverySummaryCard
            summary={discoverySummary}
            loading={discoveryLoading}
            onViewDetails={() => router.push('/discovery')}
          />
        </div>
      </div>

      {/* Row 3: Cache Metrics & Activity */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <CacheMetricsCard metrics={metrics} loading={metricsLoading} />
        <ActivityFeed title="Recent Activity" activities={activities} loading={eventsLoading} />
      </div>

      {/* Row 3.5: Recent Timeline & Operational Hotspots & Drift Detection */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <RecentTimelineWidget />
        <OperationalHotspotsWidget />
        <ConfigurationDriftWidget />
      </div>

      {/* Row 3.6: Recommended Actions & Pending Change Sets & Workspace */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <RecommendedActionsWidget />
        <PendingChangeSetsWidget />
        <DraftWorkspaceWidget />
      </div>

      {/* Row 3.7: Draft Reviews */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-1">
        <DraftReviewsWidget />
      </div>

      {/* Row 3.8: Metrics Sparklines */}
      {mPts.length > 0 && (
        <div className="rounded-lg border border-border bg-card p-4">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <TrendingUp className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-sm font-semibold">Last Hour — Key Metrics</h2>
            </div>
            <button
              onClick={() => router.push('/analytics')}
              className="text-xs text-primary hover:underline"
            >
              Full analytics →
            </button>
          </div>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.sr }))} label="Success Rate" color="#22c55e" height={56} formatY={v => `${(v*100).toFixed(0)}%`} />
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.fr }))} label="Failure Rate" color="#ef4444" height={56} formatY={v => `${(v*100).toFixed(0)}%`} />
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.qd }))} label="Queue Depth" color="#f59e0b" height={56} formatY={v => v.toFixed(0)} />
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.wu }))} label="Worker Util." color="#8b5cf6" height={56} formatY={v => `${(v*100).toFixed(0)}%`} />
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.mm }))} label="Memory (MB)" color="#06b6d4" height={56} formatY={v => v.toFixed(0)} />
            <LineChart data={mPts.map(p => ({ x: p.ts, y: p.ae }))} label="Active Exec." color="#f97316" height={56} formatY={v => v.toFixed(0)} />
          </div>
        </div>
      )}

      {/* Row 4: Quick Actions */}
      <div className="border-t pt-8">
        <h2 className="text-lg font-semibold mb-4">Quick Actions</h2>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <Button
            variant="outline"
            onClick={() => router.push('/discovery')}
            className="justify-start"
          >
            Discover Containers
          </Button>
          <Button
            variant="outline"
            onClick={() => router.push('/secrets')}
            className="justify-start"
          >
            Manage Secrets
          </Button>
          <Button
            variant="outline"
            onClick={() => router.push('/events')}
            className="justify-start"
          >
            View Events
          </Button>
          <Button
            variant="outline"
            onClick={() => router.push('/configuration')}
            className="justify-start"
          >
            Configuration
          </Button>
        </div>
      </div>
    </div>
    </ErrorBoundary>
  )
}

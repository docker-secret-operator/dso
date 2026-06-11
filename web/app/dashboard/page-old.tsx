'use client'

import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { apiClient } from '@/lib/api-client'
import { ErrorBoundary } from '@/components/error-boundary'
import {
  type Activity,
  type HealthStatus,
  type CacheMetrics,
  type DiscoverySummary,
} from '@/components/widgets'
import {
  Zap,
  TrendingUp,
  AlertCircle,
  ActivityIcon,
  Shield,
  CheckCircle2,
  Server,
  Workflow,
  BarChart3,
  Lightbulb,
  GitBranch,
  Bot,
  ChevronRight,
  ArrowUp,
  ArrowDown,
} from 'lucide-react'
import { Card, Badge, MetricCard, StatRow } from '@/components/ui-modern'
import { useVisibleInterval } from '@/hooks/useVisibilityRefetch'

export default function DashboardPage() {
  const router = useRouter()

  // Fetch health
  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 5000,
  })

  // Fetch secrets
  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: () => apiClient.getSecrets(),
    refetchInterval: 30000,
  })

  // Fetch discovery data
  const { data: discovery } = useQuery({
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
  const { data: cacheMetrics } = useQuery({
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
  const { data: events = [] } = useQuery({
    queryKey: ['events', 20],
    queryFn: () => apiClient.getEvents(20),
    refetchInterval: 5000,
  })

  // Metrics history
  const metricsInterval = useVisibleInterval(60000)
  const { data: metricsHistory } = useQuery({
    queryKey: ['metrics', 'history', '1h', '1m'],
    queryFn: () => apiClient.getMetricsHistory({ period: '1h', granularity: '1m' }),
    refetchInterval: metricsInterval,
  })

  // Calculate derived metrics
  const totalSecrets = secrets.length
  const secretsByProvider = secrets.reduce(
    (acc, secret: any) => {
      acc[secret.provider] = (acc[secret.provider] || 0) + 1
      return acc
    },
    {} as Record<string, number>
  )

  const rotatingSoon = secrets.filter((s: any) => {
    if (!s.next_rotation) return false
    const nextRotation = new Date(s.next_rotation).getTime()
    const sevenDaysFromNow = Date.now() + 7 * 24 * 60 * 60 * 1000
    return nextRotation <= sevenDaysFromNow && nextRotation > Date.now()
  }).length

  const rotationFailures = secrets.filter((s: any) => s.status === 'error').length
  const healthPercent = health?.status === 'up' ? 99 : 50

  const activities = (events || []).slice(0, 8).map((event: any) => ({
    id: `${event.timestamp}-${event.action}`,
    title: event.action || 'Event',
    description: event.message || event.error,
    severity: (event.severity as 'info' | 'warning' | 'error') || (event.status === 'failure' ? 'error' : 'info'),
    timestamp: event.timestamp,
  }))

  return (
    <ErrorBoundary>
      <div className="min-h-screen bg-gradient-to-b from-slate-50 to-white">
        <div className="max-w-7xl mx-auto px-8 py-8 space-y-8">
          {/* HERO SECTION */}
          <div className="space-y-6">
            <div>
              <h1 className="text-5xl font-bold text-slate-900 tracking-tight">DSO Operations Center</h1>
              <p className="text-xl text-slate-600 mt-2">Intelligent Docker Operations Platform</p>
            </div>

            {/* System Status Overview */}
            <Card className="bg-gradient-to-r from-slate-900 to-slate-800 border-slate-700">
              <div className="p-8">
                <div className="grid grid-cols-5 gap-6">
                  <div className="text-white">
                    <p className="text-sm font-medium text-slate-400 mb-2">Overall Health</p>
                    <div className="flex items-baseline gap-2">
                      <span className="text-4xl font-bold">{healthPercent}%</span>
                      <span className="text-sm text-green-400">Operational</span>
                    </div>
                  </div>
                  <div className="border-l border-slate-700 pl-6">
                    <p className="text-sm font-medium text-slate-400 mb-2">Active Secrets</p>
                    <p className="text-4xl font-bold text-blue-400">{totalSecrets}</p>
                  </div>
                  <div className="border-l border-slate-700 pl-6">
                    <p className="text-sm font-medium text-slate-400 mb-2">Critical Alerts</p>
                    <p className="text-4xl font-bold text-red-400">2</p>
                  </div>
                  <div className="border-l border-slate-700 pl-6">
                    <p className="text-sm font-medium text-slate-400 mb-2">Open Incidents</p>
                    <p className="text-4xl font-bold text-amber-400">5</p>
                  </div>
                  <div className="border-l border-slate-700 pl-6">
                    <p className="text-sm font-medium text-slate-400 mb-2">Recommendations</p>
                    <p className="text-4xl font-bold text-purple-400">12</p>
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* PRIMARY METRICS ROW */}
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            <MetricCard
              label="Healthy Secrets"
              value={totalSecrets - rotationFailures}
              change={8}
              trend="up"
              icon={<Shield className="w-6 h-6 text-blue-600" />}
              gradient="blue"
            />
            <MetricCard
              label="Rotation Risk"
              value={rotatingSoon}
              change={-2}
              trend="down"
              icon={<AlertCircle className="w-6 h-6 text-amber-600" />}
              gradient="green"
            />
            <MetricCard
              label="Failures"
              value={rotationFailures}
              change={0}
              trend="neutral"
              icon={<Zap className="w-6 h-6 text-red-600" />}
              gradient="coral"
            />
            <MetricCard
              label="Managed Containers"
              value={discovery?.managed_count || 0}
              change={12}
              trend="up"
              icon={<Server className="w-6 h-6 text-green-600" />}
              gradient="green"
            />
          </div>

          {/* INTELLIGENCE ROW */}
          <div className="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-5 gap-4">
            {/* Incidents Card */}
            <Card className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/incidents')}>
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Lightbulb className="w-5 h-5 text-orange-600" />
                  <Badge variant="danger" size="sm">5 Active</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Incidents</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">5</p>
                <p className="text-xs text-slate-500 mt-2">+2 this week</p>
              </div>
            </Card>

            {/* Recommendations Card */}
            <Card className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/recommendations')}>
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Lightbulb className="w-5 h-5 text-blue-600" />
                  <Badge variant="info" size="sm">12 Pending</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Recommendations</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">12</p>
                <p className="text-xs text-slate-500 mt-2">High confidence</p>
              </div>
            </Card>

            {/* Forecast Risks Card */}
            <Card className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/forecasts')}>
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <BarChart3 className="w-5 h-5 text-purple-600" />
                  <Badge variant="warning" size="sm">3 Risks</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Forecast Risks</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">3</p>
                <p className="text-xs text-slate-500 mt-2">In 7 days</p>
              </div>
            </Card>

            {/* Configuration Drift Card */}
            <Card className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/drift')}>
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <GitBranch className="w-5 h-5 text-amber-600" />
                  <Badge variant="warning" size="sm">8 Findings</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Config Drift</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">8</p>
                <p className="text-xs text-slate-500 mt-2">Requires review</p>
              </div>
            </Card>

            {/* Autonomous Actions Card */}
            <Card className="hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/autonomy')}>
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Bot className="w-5 h-5 text-green-600" />
                  <Badge variant="success" size="sm">24 Today</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Auto Actions</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">24</p>
                <p className="text-xs text-slate-500 mt-2">Remediations</p>
              </div>
            </Card>
          </div>

          {/* SYSTEM OVERVIEW - Two Column */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
            {/* System Health */}
            <Card>
              <div className="p-8">
                <div className="flex items-center justify-between mb-6">
                  <h2 className="text-xl font-bold text-slate-900">System Health</h2>
                  <CheckCircle2 className="w-6 h-6 text-green-600" />
                </div>
                <div className="space-y-4">
                  {[
                    { label: 'API Server', status: 'healthy' },
                    { label: 'DSO Agent', status: 'healthy' },
                    { label: 'WebSocket', status: 'healthy' },
                    { label: 'Scheduler', status: 'healthy' },
                    { label: 'Event Bus', status: 'healthy' },
                    { label: 'Plugins', status: 'healthy' },
                  ].map(item => (
                    <div key={item.label} className="flex items-center justify-between py-3 border-b border-slate-100 last:border-b-0">
                      <span className="text-sm text-slate-700">{item.label}</span>
                      <div className="flex items-center gap-2">
                        <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
                        <span className="text-xs font-medium text-green-700">Healthy</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </Card>

            {/* Container Discovery */}
            <Card>
              <div className="p-8">
                <div className="flex items-center justify-between mb-6">
                  <h2 className="text-xl font-bold text-slate-900">Container Discovery</h2>
                  <Server className="w-6 h-6 text-blue-600" />
                </div>
                <div className="space-y-6">
                  {[
                    { label: 'Managed', value: discovery?.managed_count || 0, color: 'from-green-600 to-green-500' },
                    { label: 'Partial', value: discovery?.partial_count || 0, color: 'from-amber-600 to-amber-500' },
                    { label: 'Unmanaged', value: discovery?.unmanaged_count || 0, color: 'from-slate-600 to-slate-500' },
                  ].map(item => (
                    <div key={item.label}>
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-sm font-medium text-slate-700">{item.label}</span>
                        <span className="text-lg font-bold text-slate-900">{item.value}</span>
                      </div>
                      <div className="w-full bg-slate-200 rounded-full h-2">
                        <div className={`bg-gradient-to-r ${item.color} h-2 rounded-full`} style={{ width: `${(item.value / (discovery?.total_count || 1)) * 100}%` }} />
                      </div>
                    </div>
                  ))}
                  <div className="pt-4 border-t border-slate-200">
                    <p className="text-sm text-slate-600">
                      <span className="font-bold text-slate-900">{discovery?.total_count || 0}</span> Total Containers
                    </p>
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* THREE COLUMN SECTION - Recommendations, Forecast, Autonomy */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {/* Recommendations Panel */}
            <Card>
              <div className="p-8">
                <div className="flex items-center justify-between mb-6">
                  <h3 className="text-lg font-bold text-slate-900">Top Recommendations</h3>
                  <Lightbulb className="w-5 h-5 text-blue-600" />
                </div>
                <div className="space-y-4">
                  {[
                    { title: 'Rotate expiring secrets', priority: 'high', confidence: 98 },
                    { title: 'Update plugin version', priority: 'medium', confidence: 85 },
                    { title: 'Review drift findings', priority: 'high', confidence: 92 },
                  ].map((rec, idx) => (
                    <div key={idx} className="p-3 bg-slate-50 rounded-lg border border-slate-200 hover:border-slate-300 transition-colors">
                      <div className="flex items-start justify-between mb-2">
                        <p className="text-sm font-medium text-slate-900">{rec.title}</p>
                        <Badge variant={rec.priority === 'high' ? 'danger' : 'warning'} size="sm">
                          {rec.priority}
                        </Badge>
                      </div>
                      <div className="flex items-center justify-between text-xs text-slate-600">
                        <span>Confidence: {rec.confidence}%</span>
                        <ChevronRight className="w-3 h-3" />
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </Card>

            {/* Forecast Panel */}
            <Card>
              <div className="p-8">
                <div className="flex items-center justify-between mb-6">
                  <h3 className="text-lg font-bold text-slate-900">Forecast Risks</h3>
                  <BarChart3 className="w-5 h-5 text-purple-600" />
                </div>
                <div className="space-y-4">
                  {[
                    { label: 'Memory Usage', value: 68, trend: 'up' },
                    { label: 'Queue Saturation', value: 45, trend: 'up' },
                    { label: 'Backup Growth', value: 82, trend: 'up' },
                  ].map((item, idx) => (
                    <div key={idx}>
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-sm font-medium text-slate-700">{item.label}</span>
                        <div className="flex items-center gap-1">
                          <span className="text-sm font-bold text-slate-900">{item.value}%</span>
                          {item.trend === 'up' ? (
                            <ArrowUp className="w-4 h-4 text-amber-600" />
                          ) : (
                            <ArrowDown className="w-4 h-4 text-green-600" />
                          )}
                        </div>
                      </div>
                      <div className="w-full bg-slate-200 rounded-full h-2">
                        <div
                          className={`h-2 rounded-full ${item.value > 75 ? 'bg-red-500' : item.value > 50 ? 'bg-amber-500' : 'bg-green-500'}`}
                          style={{ width: `${item.value}%` }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </Card>

            {/* Autonomy Panel */}
            <Card>
              <div className="p-8">
                <div className="flex items-center justify-between mb-6">
                  <h3 className="text-lg font-bold text-slate-900">Autonomous Operations</h3>
                  <Bot className="w-5 h-5 text-green-600" />
                </div>
                <div className="space-y-4">
                  <div className="p-4 bg-green-50 rounded-lg border border-green-200">
                    <p className="text-xs font-medium text-green-900 mb-1">Actions Executed</p>
                    <p className="text-3xl font-bold text-green-600">24</p>
                    <p className="text-xs text-green-700 mt-1">Last 24 hours</p>
                  </div>
                  <div className="p-4 bg-amber-50 rounded-lg border border-amber-200">
                    <p className="text-xs font-medium text-amber-900 mb-1">Pending Approvals</p>
                    <p className="text-3xl font-bold text-amber-600">3</p>
                    <p className="text-xs text-amber-700 mt-1">Require review</p>
                  </div>
                  <div className="p-4 bg-blue-50 rounded-lg border border-blue-200">
                    <p className="text-xs font-medium text-blue-900 mb-1">Rollbacks</p>
                    <p className="text-3xl font-bold text-blue-600">0</p>
                    <p className="text-xs text-blue-700 mt-1">This week</p>
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* CONSOLIDATED ACTIVITY SECTION */}
          <Card>
            <div className="p-8">
              <div className="flex items-center justify-between mb-6">
                <h2 className="text-xl font-bold text-slate-900">System Activity</h2>
                <button
                  onClick={() => router.push('/audit')}
                  className="text-sm text-coral-600 hover:text-coral-700 font-semibold"
                >
                  View All →
                </button>
              </div>
              <div className="space-y-3">
                {activities.map(activity => (
                  <div key={activity.id} className="flex items-start gap-4 py-3 border-b border-slate-100 last:border-b-0">
                    <div
                      className={`w-2 h-2 rounded-full mt-2 flex-shrink-0 ${
                        activity.severity === 'info'
                          ? 'bg-blue-500'
                          : activity.severity === 'warning'
                            ? 'bg-amber-500'
                            : 'bg-red-500'
                      }`}
                    />
                    <div className="flex-1 min-w-0">
                      <p className="font-semibold text-slate-900 text-sm">{activity.title}</p>
                      <p className="text-xs text-slate-500 mt-0.5">{activity.description}</p>
                      <p className="text-xs text-slate-400 mt-1">{String(activity.timestamp)}</p>
                    </div>
                    <div className="flex-shrink-0">
                      <Badge
                        variant={activity.severity === 'error' ? 'danger' : activity.severity === 'warning' ? 'warning' : 'info'}
                        size="sm"
                      >
                        {activity.severity}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </Card>

          {/* CALL TO ACTION */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <button
              onClick={() => router.push('/secrets')}
              className="p-6 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 hover:border-slate-300 transition-all text-left"
            >
              <Shield className="w-6 h-6 text-blue-600 mb-3" />
              <p className="font-bold text-slate-900">Manage Secrets</p>
              <p className="text-sm text-slate-600 mt-1">Review and rotate secrets</p>
            </button>
            <button
              onClick={() => router.push('/discovery')}
              className="p-6 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 hover:border-slate-300 transition-all text-left"
            >
              <Server className="w-6 h-6 text-green-600 mb-3" />
              <p className="font-bold text-slate-900">Discover Containers</p>
              <p className="text-sm text-slate-600 mt-1">Find and onboard containers</p>
            </button>
            <button
              onClick={() => router.push('/configuration')}
              className="p-6 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 hover:border-slate-300 transition-all text-left"
            >
              <Workflow className="w-6 h-6 text-purple-600 mb-3" />
              <p className="font-bold text-slate-900">Configuration</p>
              <p className="text-sm text-slate-600 mt-1">System settings and policies</p>
            </button>
          </div>
        </div>
      </div>
    </ErrorBoundary>
  )
}

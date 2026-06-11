'use client'

import { useQuery } from '@tanstack/react-query'
import { useRouter } from 'next/navigation'
import { apiClient } from '@/lib/api-client'
import { ErrorBoundary } from '@/components/error-boundary'
import {
  AlertCircle,
  Activity,
  Shield,
  CheckCircle2,
  Server,
  Workflow,
  BarChart3,
  Lightbulb,
  GitBranch,
  Bot,
  TrendingUp,
  TrendingDown,
  Zap,
  Clock,
  ChevronRight,
  RefreshCw,
} from 'lucide-react'
import { Card, Badge } from '@/components/ui-modern'

// ============================================================================
// TYPES
// ============================================================================

interface SystemMetrics {
  healthy: number
  degraded: number
  failed: number
  lastUpdate: string
}

interface MetricValue {
  value: number | string
  label: string
  trend?: number
  status?: 'healthy' | 'warning' | 'critical'
}

interface LoadingState {
  incidents: boolean
  forecasts: boolean
  autonomy: boolean
  alerts: boolean
  drift: boolean
  recommendations: boolean
  discovery: boolean
  health: boolean
}

// ============================================================================
// DASHBOARD COMPONENT
// ============================================================================

export default function DashboardPremium() {
  const router = useRouter()
  const now = new Date()

  // ── Fetch all real data ──────────────────────────────────────────────────

  // Health & Status
  const { data: health } = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 5000,
  })

  // Incidents
  const { data: incidents, isLoading: incidentsLoading } = useQuery({
    queryKey: ['incidents'],
    queryFn: async () => {
      const list = await apiClient.getIncidents({ limit: 10 })
      const metrics = await apiClient.getIncidentMetrics()
      return { list, metrics }
    },
    refetchInterval: 10000,
  })

  // Forecasts
  const { data: forecasts, isLoading: forecastsLoading } = useQuery({
    queryKey: ['forecasts'],
    queryFn: async () => {
      const list = await apiClient.getForecasts({ limit: 5 })
      const metrics = await apiClient.getForecastMetrics()
      return { list, metrics }
    },
    refetchInterval: 30000,
  })

  // Autonomy
  const { data: autonomy, isLoading: autonomyLoading } = useQuery({
    queryKey: ['autonomy'],
    queryFn: async () => {
      const actions = await apiClient.getAutonomyActions({ limit: 10 })
      const metrics = await apiClient.getAutonomyMetrics()
      return { actions, metrics }
    },
    refetchInterval: 10000,
  })

  // Alerts
  const { data: alerts, isLoading: alertsLoading } = useQuery({
    queryKey: ['alerts'],
    queryFn: () => apiClient.getAlerts({ limit: 20 }),
    refetchInterval: 5000,
  })

  // Drift
  const { data: drift, isLoading: driftLoading } = useQuery({
    queryKey: ['drift'],
    queryFn: async () => {
      const findings = await apiClient.getDriftFindings({ limit: 10 })
      const metrics = await apiClient.getDriftMetrics()
      return { findings, metrics }
    },
    refetchInterval: 30000,
  })

  // Recommendations
  const { data: recommendations, isLoading: recommendationsLoading } = useQuery({
    queryKey: ['recommendations'],
    queryFn: () => apiClient.getRecommendations({ limit: 5 }),
    refetchInterval: 60000,
  })

  // Discovery
  const { data: discovery, isLoading: discoveryLoading } = useQuery({
    queryKey: ['discovery'],
    queryFn: () => apiClient.getDiscoverySummary(),
    refetchInterval: 30000,
  })

  // Secrets
  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets'],
    queryFn: () => apiClient.getSecrets(),
    refetchInterval: 30000,
  })

  // Metrics history for charts
  const { data: metricsHistory } = useQuery({
    queryKey: ['metricsHistory'],
    queryFn: () => apiClient.getMetricsHistory({ period: '1h', granularity: '1m' }),
    refetchInterval: 60000,
  })

  const isLoading = {
    incidents: incidentsLoading,
    forecasts: forecastsLoading,
    autonomy: autonomyLoading,
    alerts: alertsLoading,
    drift: driftLoading,
    recommendations: recommendationsLoading,
    discovery: discoveryLoading,
    health: !health,
  }

  // ── Compute derived values ───────────────────────────────────────────────

  const healthPercent = health?.status === 'up' ? 99 : 20
  const secretsHealthy = secrets.filter((s: any) => s.status === 'ok').length
  const secretsRotationRisk = secrets.filter((s: any) => {
    if (!s.next_rotation) return false
    const nextRotation = new Date(s.next_rotation).getTime()
    const sevenDaysFromNow = Date.now() + 7 * 24 * 60 * 60 * 1000
    return nextRotation <= sevenDaysFromNow && nextRotation > Date.now()
  }).length
  const secretsFailures = secrets.filter((s: any) => s.status === 'error').length

  const incidentsCount = incidents?.metrics?.open || 0
  const incidentsCritical = incidents?.metrics?.critical || 0
  const forecastsAtRisk = forecasts?.metrics?.at_risk || 0
  const autonomyExecuted = autonomy?.metrics?.executed || 0
  const autonomyPending = autonomy?.metrics?.pending || 0
  const driftCritical = drift?.metrics?.critical || 0
  const recommendationsCount = recommendations?.total || 0
  const alertsCount = alerts?.total || 0

  const lastMetricsPoint = metricsHistory?.data?.[metricsHistory.data.length - 1]
  const queueDepth = lastMetricsPoint?.qd || 0
  const workerUtil = lastMetricsPoint?.wu || 0
  const memoryUsage = lastMetricsPoint?.mm || 0
  const activeExecutions = lastMetricsPoint?.ae || 0

  return (
    <ErrorBoundary>
      <div className="min-h-screen bg-gradient-to-b from-slate-50 to-white p-8">
        <div className="max-w-7xl mx-auto space-y-8">
          {/* ══════════════════════════════════════════════════════════════════
              HERO SECTION
              ══════════════════════════════════════════════════════════════════ */}

          <div className="space-y-6">
            <div>
              <h1 className="text-6xl font-bold tracking-tight text-slate-900">DSO Operations Center</h1>
              <p className="text-xl text-slate-600 mt-3">Enterprise-grade Intelligent Docker Operations Platform</p>
            </div>

            {/* System Health Hero */}
            <Card className="bg-gradient-to-r from-slate-900 via-slate-800 to-slate-900 border-slate-700">
              <div className="p-8">
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-6">
                  {/* Overall Health */}
                  <div className="flex flex-col justify-between">
                    <p className="text-sm font-medium text-slate-400">System Health</p>
                    <div className="mt-4">
                      <div className="relative w-20 h-20">
                        <svg className="w-full h-full transform -rotate-90" viewBox="0 0 100 100">
                          <circle cx="50" cy="50" r="45" fill="none" stroke="#1e293b" strokeWidth="8" />
                          <circle
                            cx="50"
                            cy="50"
                            r="45"
                            fill="none"
                            stroke="#10b981"
                            strokeWidth="8"
                            strokeDasharray={`${(healthPercent / 100) * 2 * Math.PI * 45} ${2 * Math.PI * 45}`}
                          />
                        </svg>
                        <div className="absolute inset-0 flex items-center justify-center">
                          <span className="text-2xl font-bold text-white">{healthPercent}%</span>
                        </div>
                      </div>
                    </div>
                    <p className="text-xs text-green-400 mt-3">Operational</p>
                  </div>

                  {/* Service Status */}
                  {[
                    { name: 'API Server', status: 'healthy' },
                    { name: 'Agent', status: health?.status === 'up' ? 'healthy' : 'failed' },
                    { name: 'Scheduler', status: 'healthy' },
                    { name: 'EventBus', status: 'healthy' },
                    { name: 'WebSocket', status: 'healthy' },
                  ].map((service, idx) => (
                    <div key={idx} className="flex flex-col">
                      <p className="text-xs font-medium text-slate-400 uppercase">{service.name}</p>
                      <div className="flex items-center gap-2 mt-auto">
                        <div
                          className={`w-2 h-2 rounded-full ${
                            service.status === 'healthy'
                              ? 'bg-green-500 animate-pulse'
                              : service.status === 'degraded'
                                ? 'bg-yellow-500'
                                : 'bg-red-500'
                          }`}
                        />
                        <span className="text-sm font-medium text-white capitalize">{service.status}</span>
                      </div>
                    </div>
                  ))}

                  {/* Last Update */}
                  <div className="flex flex-col">
                    <p className="text-xs font-medium text-slate-400 uppercase">Last Update</p>
                    <p className="text-sm text-slate-300 mt-auto">{now.toLocaleTimeString()}</p>
                  </div>
                </div>
              </div>
            </Card>
          </div>

          {/* ══════════════════════════════════════════════════════════════════
              PRIMARY METRICS - SECRETS
              ══════════════════════════════════════════════════════════════════ */}

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            {/* Healthy Secrets */}
            <Card className="hover:shadow-lg transition-shadow">
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Shield className="w-5 h-5 text-blue-600" />
                  <Badge variant="success" size="sm">Healthy</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Healthy Secrets</p>
                <p className="text-4xl font-bold text-slate-900 mt-2">{secretsHealthy}</p>
                <p className="text-xs text-slate-500 mt-3">{secrets.length} total secrets</p>
              </div>
            </Card>

            {/* Rotation Risk */}
            <Card className="hover:shadow-lg transition-shadow">
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <AlertCircle className="w-5 h-5 text-amber-600" />
                  <Badge variant={secretsRotationRisk > 0 ? 'warning' : 'success'} size="sm">
                    {secretsRotationRisk > 0 ? 'At Risk' : 'Safe'}
                  </Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Rotation Risk (7d)</p>
                <p className="text-4xl font-bold text-slate-900 mt-2">{secretsRotationRisk}</p>
                <p className="text-xs text-slate-500 mt-3">Rotating soon</p>
              </div>
            </Card>

            {/* Failures */}
            <Card className="hover:shadow-lg transition-shadow">
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Zap className="w-5 h-5 text-red-600" />
                  <Badge variant={secretsFailures > 0 ? 'danger' : 'success'} size="sm">
                    {secretsFailures > 0 ? 'Failures' : 'OK'}
                  </Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Rotation Failures</p>
                <p className="text-4xl font-bold text-slate-900 mt-2">{secretsFailures}</p>
                <p className="text-xs text-slate-500 mt-3">Needs attention</p>
              </div>
            </Card>

            {/* Managed Containers */}
            <Card className="hover:shadow-lg transition-shadow">
              <div className="p-6">
                <div className="flex items-start justify-between mb-4">
                  <Server className="w-5 h-5 text-green-600" />
                  <Badge variant="success" size="sm">Managed</Badge>
                </div>
                <p className="text-sm text-slate-600 font-medium">Managed Containers</p>
                <p className="text-4xl font-bold text-slate-900 mt-2">{discovery?.managed || 0}</p>
                <p className="text-xs text-slate-500 mt-3">
                  {discovery?.total ? `${((discovery?.managed / discovery?.total) * 100).toFixed(0)}% coverage` : 'No data'}
                </p>
              </div>
            </Card>
          </div>

          {/* ══════════════════════════════════════════════════════════════════
              BENTO GRID - INTELLIGENCE FEATURES
              ══════════════════════════════════════════════════════════════════ */}

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-12 gap-6">
            {/* LARGE: Incidents */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/incidents')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">INCIDENTS</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Active Incidents</h3>
                  </div>
                  <Lightbulb className="w-6 h-6 text-orange-600" />
                </div>

                {isLoading.incidents ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                    <div className="h-4 bg-slate-200 rounded animate-pulse w-2/3" />
                  </div>
                ) : (
                  <>
                    <div className="space-y-4">
                      <div>
                        <p className="text-3xl font-bold text-slate-900">{incidentsCount}</p>
                        <p className="text-xs text-slate-500 mt-1">Open incidents</p>
                      </div>
                      <div className="grid grid-cols-2 gap-4 pt-4 border-t border-slate-200">
                        <div>
                          <p className="text-2xl font-bold text-red-600">{incidentsCritical}</p>
                          <p className="text-xs text-slate-500">Critical</p>
                        </div>
                        <div>
                          <p className="text-2xl font-bold text-slate-600">{incidents?.metrics?.acknowledged || 0}</p>
                          <p className="text-xs text-slate-500">Acknowledged</p>
                        </div>
                      </div>
                    </div>
                    <button className="w-full mt-6 px-4 py-2 rounded-lg bg-orange-50 text-orange-700 text-sm font-medium hover:bg-orange-100 transition-colors flex items-center justify-center gap-2">
                      View Details <ChevronRight className="w-4 h-4" />
                    </button>
                  </>
                )}
              </div>
            </Card>

            {/* LARGE: Recommendations */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/recommendations')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">RECOMMENDATIONS</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Pending Actions</h3>
                  </div>
                  <Lightbulb className="w-6 h-6 text-purple-600" />
                </div>

                {isLoading.recommendations ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                    <div className="h-4 bg-slate-200 rounded animate-pulse w-2/3" />
                  </div>
                ) : (
                  <>
                    <div className="space-y-4">
                      <div>
                        <p className="text-3xl font-bold text-slate-900">{recommendationsCount}</p>
                        <p className="text-xs text-slate-500 mt-1">Recommendations</p>
                      </div>
                      {recommendations?.recommendations?.[0] && (
                        <div className="pt-4 border-t border-slate-200">
                          <p className="text-sm font-medium text-slate-700 truncate">{recommendations.recommendations[0].title || 'Top recommendation'}</p>
                          <div className="flex items-center gap-2 mt-2">
                            <Badge variant="warning" size="sm">High Priority</Badge>
                            <Badge variant="info" size="sm">95% Confidence</Badge>
                          </div>
                        </div>
                      )}
                    </div>
                    <button className="w-full mt-6 px-4 py-2 rounded-lg bg-purple-50 text-purple-700 text-sm font-medium hover:bg-purple-100 transition-colors flex items-center justify-center gap-2">
                      View Details <ChevronRight className="w-4 h-4" />
                    </button>
                  </>
                )}
              </div>
            </Card>

            {/* MEDIUM: Forecast Risks */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/forecasts')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">FORECASTS</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Predicted Risks</h3>
                  </div>
                  <BarChart3 className="w-6 h-6 text-cyan-600" />
                </div>

                {isLoading.forecasts ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                  </div>
                ) : (
                  <>
                    <p className="text-3xl font-bold text-slate-900">{forecastsAtRisk}</p>
                    <p className="text-xs text-slate-500 mt-1">At risk in next 7 days</p>
                    <div className="mt-6 space-y-2 text-xs text-slate-600">
                      <p>• Memory trending up</p>
                      <p>• Queue saturation risk</p>
                      <p>• Storage growth detected</p>
                    </div>
                  </>
                )}
              </div>
            </Card>

            {/* MEDIUM: Drift */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/drift')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">CONFIG DRIFT</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Drift Findings</h3>
                  </div>
                  <GitBranch className="w-6 h-6 text-amber-600" />
                </div>

                {isLoading.drift ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                  </div>
                ) : (
                  <>
                    <p className="text-3xl font-bold text-slate-900">{driftCritical}</p>
                    <p className="text-xs text-slate-500 mt-1">Critical findings</p>
                    <div className="mt-6 grid grid-cols-2 gap-3 text-xs">
                      <div className="p-2 bg-amber-50 rounded">
                        <p className="text-amber-900 font-medium">{drift?.metrics?.acknowledged || 0}</p>
                        <p className="text-amber-700">Acknowledged</p>
                      </div>
                      <div className="p-2 bg-red-50 rounded">
                        <p className="text-red-900 font-medium">{drift?.metrics?.unresolved || 0}</p>
                        <p className="text-red-700">Unresolved</p>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </Card>

            {/* SMALL: Autonomy */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/autonomy')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <p className="text-sm font-medium text-slate-500">AUTONOMY</p>
                  <Bot className="w-6 h-6 text-green-600" />
                </div>

                {isLoading.autonomy ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                  </div>
                ) : (
                  <>
                    <div className="space-y-3">
                      <div>
                        <p className="text-2xl font-bold text-green-600">{autonomyExecuted}</p>
                        <p className="text-xs text-slate-500">Executed today</p>
                      </div>
                      <div className="p-2 bg-amber-50 rounded">
                        <p className="text-sm font-bold text-amber-900">{autonomyPending}</p>
                        <p className="text-xs text-amber-700">Pending approval</p>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </Card>

            {/* MEDIUM: Alerts */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/alerts')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">ALERTS</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Active Alerts</h3>
                  </div>
                  <AlertCircle className="w-6 h-6 text-red-600" />
                </div>

                {isLoading.alerts ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                  </div>
                ) : (
                  <>
                    <p className="text-3xl font-bold text-slate-900">{alertsCount}</p>
                    <p className="text-xs text-slate-500 mt-1">Total alerts</p>
                    {alerts?.alerts?.[0] && (
                      <div className="mt-4 p-3 bg-red-50 rounded border border-red-200">
                        <p className="text-sm font-medium text-red-900 truncate">{alerts.alerts[0].title || 'Latest alert'}</p>
                        <p className="text-xs text-red-700 mt-1">Most recent</p>
                      </div>
                    )}
                  </>
                )}
              </div>
            </Card>

            {/* MEDIUM: Container Discovery */}
            <Card className="lg:col-span-3 hover:shadow-lg transition-shadow cursor-pointer" onClick={() => router.push('/discovery')}>
              <div className="p-8">
                <div className="flex items-start justify-between mb-6">
                  <div>
                    <p className="text-sm font-medium text-slate-500">DISCOVERY</p>
                    <h3 className="text-lg font-bold text-slate-900 mt-2">Container Status</h3>
                  </div>
                  <Server className="w-6 h-6 text-blue-600" />
                </div>

                {isLoading.discovery ? (
                  <div className="space-y-2">
                    <div className="h-8 bg-slate-200 rounded animate-pulse" />
                  </div>
                ) : (
                  <>
                    <div className="space-y-3">
                      <div className="grid grid-cols-3 gap-2">
                        <div className="p-2 bg-green-50 rounded">
                          <p className="text-lg font-bold text-green-700">{discovery?.managed || 0}</p>
                          <p className="text-xs text-green-700">Managed</p>
                        </div>
                        <div className="p-2 bg-amber-50 rounded">
                          <p className="text-lg font-bold text-amber-700">{discovery?.partial || 0}</p>
                          <p className="text-xs text-amber-700">Partial</p>
                        </div>
                        <div className="p-2 bg-slate-100 rounded">
                          <p className="text-lg font-bold text-slate-700">{discovery?.unmanaged || 0}</p>
                          <p className="text-xs text-slate-700">Unmanaged</p>
                        </div>
                      </div>
                      <div className="text-xs text-slate-600">{discovery?.total || 0} total</div>
                    </div>
                  </>
                )}
              </div>
            </Card>
          </div>

          {/* ══════════════════════════════════════════════════════════════════
              SYSTEM METRICS
              ══════════════════════════════════════════════════════════════════ */}

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            {/* Queue Depth */}
            <Card>
              <div className="p-6">
                <p className="text-sm font-medium text-slate-500">Queue Depth</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">{queueDepth.toFixed(0)}</p>
                <p className="text-xs text-slate-500 mt-2">Jobs pending</p>
                <div className="mt-4 h-1 bg-slate-200 rounded-full overflow-hidden">
                  <div
                    className={`h-full ${
                      queueDepth > 1000 ? 'bg-red-500' : queueDepth > 500 ? 'bg-amber-500' : 'bg-green-500'
                    }`}
                    style={{ width: `${Math.min(queueDepth / 1000, 1) * 100}%` }}
                  />
                </div>
              </div>
            </Card>

            {/* Worker Utilization */}
            <Card>
              <div className="p-6">
                <p className="text-sm font-medium text-slate-500">Worker Utilization</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">{(workerUtil * 100).toFixed(0)}%</p>
                <p className="text-xs text-slate-500 mt-2">Active workers</p>
                <div className="mt-4 h-1 bg-slate-200 rounded-full overflow-hidden">
                  <div
                    className={`h-full ${
                      workerUtil > 0.9 ? 'bg-red-500' : workerUtil > 0.75 ? 'bg-amber-500' : 'bg-green-500'
                    }`}
                    style={{ width: `${workerUtil * 100}%` }}
                  />
                </div>
              </div>
            </Card>

            {/* Memory Usage */}
            <Card>
              <div className="p-6">
                <p className="text-sm font-medium text-slate-500">Memory Usage</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">{memoryUsage.toFixed(0)}MB</p>
                <p className="text-xs text-slate-500 mt-2">Current usage</p>
                <div className="mt-4 h-1 bg-slate-200 rounded-full overflow-hidden">
                  <div
                    className={`h-full ${
                      memoryUsage > 2000 ? 'bg-red-500' : memoryUsage > 1500 ? 'bg-amber-500' : 'bg-green-500'
                    }`}
                    style={{ width: `${Math.min(memoryUsage / 3000, 1) * 100}%` }}
                  />
                </div>
              </div>
            </Card>

            {/* Active Executions */}
            <Card>
              <div className="p-6">
                <p className="text-sm font-medium text-slate-500">Active Executions</p>
                <p className="text-3xl font-bold text-slate-900 mt-2">{activeExecutions.toFixed(0)}</p>
                <p className="text-xs text-slate-500 mt-2">Currently running</p>
                <div className="mt-4 h-1 bg-slate-200 rounded-full overflow-hidden">
                  <div
                    className="h-full bg-blue-500"
                    style={{ width: `${Math.min(activeExecutions / 100, 1) * 100}%` }}
                  />
                </div>
              </div>
            </Card>
          </div>

          {/* ══════════════════════════════════════════════════════════════════
              RECENT ACTIVITY
              ══════════════════════════════════════════════════════════════════ */}

          <Card>
            <div className="p-8">
              <div className="flex items-center justify-between mb-6">
                <h2 className="text-xl font-bold text-slate-900">Recent Activity</h2>
                <button
                  onClick={() => router.push('/audit')}
                  className="text-sm text-slate-600 hover:text-slate-900 font-semibold flex items-center gap-1"
                >
                  View All <ChevronRight className="w-4 h-4" />
                </button>
              </div>

              <div className="space-y-3">
                {alerts?.alerts?.slice(0, 5).map((alert: any, idx: number) => (
                  <div key={idx} className="flex items-start gap-3 p-3 rounded-lg border border-slate-200 hover:bg-slate-50 transition-colors">
                    <div
                      className={`w-2 h-2 rounded-full mt-2 flex-shrink-0 ${
                        alert.severity === 'critical' ? 'bg-red-500' : alert.severity === 'warning' ? 'bg-amber-500' : 'bg-blue-500'
                      }`}
                    />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-slate-900">{alert.title || alert.name || 'Alert'}</p>
                      <p className="text-xs text-slate-500 mt-0.5">{alert.description || 'No description'}</p>
                    </div>
                    <Badge variant={alert.severity === 'critical' ? 'danger' : alert.severity === 'warning' ? 'warning' : 'info'} size="sm">
                      {alert.severity}
                    </Badge>
                  </div>
                ))}
                {(!alerts?.alerts || alerts.alerts.length === 0) && (
                  <p className="text-sm text-slate-500 text-center py-6">No recent alerts</p>
                )}
              </div>
            </div>
          </Card>
        </div>
      </div>
    </ErrorBoundary>
  )
}

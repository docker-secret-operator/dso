'use client'

import { useState, useEffect } from 'react'
import {
  BarChart3,
  TrendingUp,
  AlertCircle,
  CheckCircle2,
  Clock,
  Zap,
  Activity,
  Shield,
  Server,
  Workflow,
} from 'lucide-react'
import { Card, Button, Badge, MetricCard, StatusIndicator, StatRow } from '@/components/ui-modern'

interface DashboardMetrics {
  totalExecutions: number
  executionsChange: number
  activeAlerts: number
  alertsChange: number
  systemHealth: 'healthy' | 'warning' | 'critical'
  uptime: string
}

interface RecentActivity {
  id: string
  type: 'execution' | 'alert' | 'policy' | 'deployment'
  title: string
  description: string
  timestamp: string
  status: 'success' | 'warning' | 'error'
}

export default function DashboardModern() {
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null)
  const [activities, setActivities] = useState<RecentActivity[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // Fetch dashboard data
    const mockMetrics: DashboardMetrics = {
      totalExecutions: 1247,
      executionsChange: 12,
      activeAlerts: 3,
      alertsChange: -5,
      systemHealth: 'healthy',
      uptime: '99.98%',
    }

    const mockActivities: RecentActivity[] = [
      {
        id: '1',
        type: 'execution',
        title: 'Secret Rotation Completed',
        description: 'Production secrets rotated successfully across 8 clusters',
        timestamp: '2 minutes ago',
        status: 'success',
      },
      {
        id: '2',
        type: 'alert',
        title: 'Drift Detected',
        description: 'Configuration drift detected in staging environment',
        timestamp: '1 hour ago',
        status: 'warning',
      },
      {
        id: '3',
        type: 'policy',
        title: 'Policy Updated',
        description: 'Secret validation policy updated by ops team',
        timestamp: '3 hours ago',
        status: 'success',
      },
      {
        id: '4',
        type: 'deployment',
        title: 'Deployment Successful',
        description: 'New version deployed to production',
        timestamp: '5 hours ago',
        status: 'success',
      },
    ]

    setMetrics(mockMetrics)
    setActivities(mockActivities)
    setLoading(false)
  }, [])

  if (loading) {
    return (
      <div className="p-8">
        <div className="animate-pulse space-y-4">
          <div className="h-64 bg-slate-200 rounded-2xl" />
          <div className="grid grid-cols-4 gap-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-32 bg-slate-200 rounded-2xl" />
            ))}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gradient-to-b from-slate-50 to-white p-8">
      {/* Header */}
      <div className="mb-12">
        <h1 className="text-4xl font-bold text-slate-900 mb-2">Dashboard</h1>
        <p className="text-lg text-slate-600">Welcome back. Here's your platform overview.</p>
      </div>

      {/* Top Metrics */}
      {metrics && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-8">
          <MetricCard
            label="Total Executions"
            value={metrics.totalExecutions.toLocaleString()}
            change={metrics.executionsChange}
            trend="up"
            icon={<Zap className="w-6 h-6 text-coral-600" />}
            gradient="coral"
          />
          <MetricCard
            label="Active Alerts"
            value={metrics.activeAlerts}
            change={metrics.alertsChange}
            trend="down"
            icon={<AlertCircle className="w-6 h-6 text-yellow-600" />}
            gradient="green"
          />
          <MetricCard
            label="System Health"
            value="99.98%"
            icon={<Shield className="w-6 h-6 text-green-600" />}
            gradient="green"
          />
          <MetricCard
            label="Uptime"
            value={metrics.uptime}
            icon={<Activity className="w-6 h-6 text-blue-600" />}
            gradient="blue"
          />
        </div>
      )}

      {/* Main Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
        {/* Left Column - Charts and Analysis */}
        <div className="lg:col-span-2 space-y-8">
          {/* Execution Trends */}
          <Card variant="gradient">
            <div className="p-8">
              <div className="flex items-center justify-between mb-8">
                <div>
                  <h2 className="text-xl font-bold text-slate-900">Execution Trends</h2>
                  <p className="text-sm text-slate-600 mt-1">Last 30 days</p>
                </div>
                <TrendingUp className="w-6 h-6 text-coral-600" />
              </div>

              {/* Simple Chart Placeholder */}
              <div className="h-64 bg-gradient-to-b from-slate-100 to-white rounded-xl p-4 flex items-end justify-between">
                {[65, 58, 87, 72, 93, 78, 85, 92, 88, 76].map((height, i) => (
                  <div
                    key={i}
                    className="bg-gradient-to-t from-coral-600 to-coral-500 rounded-t-lg"
                    style={{ height: `${(height / 100) * 200}px`, width: '8%' }}
                  />
                ))}
              </div>

              <div className="mt-6 pt-6 border-t border-slate-200 flex items-center justify-between">
                <div>
                  <p className="text-sm text-slate-600">Peak execution rate</p>
                  <p className="text-2xl font-bold text-slate-900">2,847</p>
                </div>
                <Badge variant="success">↑ 24% from last week</Badge>
              </div>
            </div>
          </Card>

          {/* System Status */}
          <div className="grid grid-cols-2 gap-6">
            {/* Core Services */}
            <Card>
              <div className="p-8">
                <h3 className="text-lg font-bold text-slate-900 mb-6">Core Services</h3>
                <div className="space-y-4">
                  <StatRow
                    label="Execution Engine"
                    value="Healthy"
                    icon={<CheckCircle2 className="w-4 h-4 text-green-600" />}
                  />
                  <StatRow
                    label="Auth Service"
                    value="Healthy"
                    icon={<CheckCircle2 className="w-4 h-4 text-green-600" />}
                  />
                  <StatRow
                    label="API Gateway"
                    value="Healthy"
                    icon={<CheckCircle2 className="w-4 h-4 text-green-600" />}
                  />
                  <StatRow
                    label="Database"
                    value="99.98%"
                    icon={<CheckCircle2 className="w-4 h-4 text-green-600" />}
                  />
                </div>
              </div>
            </Card>

            {/* Quick Actions */}
            <Card>
              <div className="p-8">
                <h3 className="text-lg font-bold text-slate-900 mb-6">Quick Actions</h3>
                <div className="space-y-3">
                  <Button variant="secondary" className="w-full justify-start">
                    <Workflow className="w-4 h-4 mr-2" />
                    New Execution
                  </Button>
                  <Button variant="secondary" className="w-full justify-start">
                    <AlertCircle className="w-4 h-4 mr-2" />
                    View Alerts
                  </Button>
                  <Button variant="secondary" className="w-full justify-start">
                    <Server className="w-4 h-4 mr-2" />
                    Configuration
                  </Button>
                </div>
              </div>
            </Card>
          </div>
        </div>

        {/* Right Column - Activity and Alerts */}
        <div className="space-y-8">
          {/* Recent Activity */}
          <Card>
            <div className="p-8">
              <h2 className="text-xl font-bold text-slate-900 mb-6">Recent Activity</h2>
              <div className="space-y-4">
                {activities.map(activity => (
                  <div key={activity.id} className="pb-4 border-b border-slate-100 last:border-b-0 last:pb-0">
                    <div className="flex items-start gap-3">
                      <div
                        className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                          activity.status === 'success'
                            ? 'bg-green-500'
                            : activity.status === 'warning'
                              ? 'bg-yellow-500'
                              : 'bg-red-500'
                        }`}
                      />
                      <div className="flex-1 min-w-0">
                        <p className="font-semibold text-slate-900 text-sm">{activity.title}</p>
                        <p className="text-xs text-slate-500 mt-1">{activity.description}</p>
                        <p className="text-xs text-slate-400 mt-2">{activity.timestamp}</p>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </Card>

          {/* Platform Health */}
          <Card variant="bordered">
            <div className="p-8">
              <h2 className="text-xl font-bold text-slate-900 mb-6">Platform Health</h2>
              <div className="space-y-4">
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium text-slate-700">API Response Time</span>
                    <span className="text-sm font-bold text-green-600">45ms</span>
                  </div>
                  <div className="w-full bg-slate-200 rounded-full h-2">
                    <div className="bg-gradient-to-r from-green-500 to-green-400 h-2 rounded-full" style={{ width: '100%' }} />
                  </div>
                </div>
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium text-slate-700">Database Load</span>
                    <span className="text-sm font-bold text-yellow-600">62%</span>
                  </div>
                  <div className="w-full bg-slate-200 rounded-full h-2">
                    <div className="bg-gradient-to-r from-yellow-500 to-yellow-400 h-2 rounded-full" style={{ width: '62%' }} />
                  </div>
                </div>
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-sm font-medium text-slate-700">Memory Usage</span>
                    <span className="text-sm font-bold text-green-600">28%</span>
                  </div>
                  <div className="w-full bg-slate-200 rounded-full h-2">
                    <div className="bg-gradient-to-r from-green-500 to-green-400 h-2 rounded-full" style={{ width: '28%' }} />
                  </div>
                </div>
              </div>
            </div>
          </Card>
        </div>
      </div>
    </div>
  )
}

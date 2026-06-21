'use client'

import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import { ErrorBoundary } from '@/components/error-boundary'
import { PageHeader, Card } from '@/components/ui-modern'
import { Server, Shield, Zap, Activity, Database, Users } from 'lucide-react'

function DashboardSimple() {
  return (
    <div className="p-6 space-y-6">
      <PageHeader
        title="DSO Operations Dashboard"
        description="Welcome to the Docker Secret Operator control center"
      />

      {/* Quick Navigation Cards */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Shield className="w-8 h-8 text-emerald-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Secrets</h3>
              <p className="text-sm text-slate-400">Manage your secrets and rotations</p>
            </div>
          </div>
        </Card>

        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Server className="w-8 h-8 text-blue-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Discovery</h3>
              <p className="text-sm text-slate-400">Discover and manage containers</p>
            </div>
          </div>
        </Card>

        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Activity className="w-8 h-8 text-purple-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Operations</h3>
              <p className="text-sm text-slate-400">Monitor executions and queue health</p>
            </div>
          </div>
        </Card>

        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Database className="w-8 h-8 text-amber-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Audit</h3>
              <p className="text-sm text-slate-400">Review audit logs and events</p>
            </div>
          </div>
        </Card>

        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Zap className="w-8 h-8 text-yellow-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Events</h3>
              <p className="text-sm text-slate-400">View system events and alerts</p>
            </div>
          </div>
        </Card>

        <Card className="p-6 hover:border-indigo-500/50 transition-colors cursor-pointer">
          <div className="flex items-start gap-4">
            <Users className="w-8 h-8 text-pink-400 flex-shrink-0 mt-1" />
            <div>
              <h3 className="font-semibold text-slate-100 mb-1">Settings</h3>
              <p className="text-sm text-slate-400">Manage users and configuration</p>
            </div>
          </div>
        </Card>
      </div>

      {/* System Status Section */}
      <Card className="p-6 border-emerald-500/20">
        <h2 className="text-lg font-semibold text-slate-100 mb-4">System Status</h2>
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <span className="text-slate-400">Backend API</span>
            <span className="px-3 py-1 rounded-full text-xs font-medium bg-emerald-500/20 text-emerald-300">Connected</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-slate-400">Database</span>
            <span className="px-3 py-1 rounded-full text-xs font-medium bg-emerald-500/20 text-emerald-300">Ready</span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-slate-400">Authentication</span>
            <span className="px-3 py-1 rounded-full text-xs font-medium bg-emerald-500/20 text-emerald-300">Active</span>
          </div>
        </div>
      </Card>

      {/* Help Section */}
      <Card className="p-6 bg-blue-500/5 border-blue-500/20">
        <h2 className="text-lg font-semibold text-slate-100 mb-2">Getting Started</h2>
        <ul className="space-y-2 text-sm text-slate-400">
          <li>• Navigate to <strong>Discovery</strong> to see your containers</li>
          <li>• Visit <strong>Operations</strong> to monitor execution health</li>
          <li>• Check <strong>Audit</strong> for system activity logs</li>
          <li>• Manage <strong>Secrets</strong> for your infrastructure</li>
        </ul>
      </Card>
    </div>
  )
}

export default function Dashboard() {
  return (
    <ProtectedRoute>
      <ErrorBoundary>
        <DashboardSimple />
      </ErrorBoundary>
    </ProtectedRoute>
  )
}

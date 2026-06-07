import React, { useEffect, useState } from 'react';
import { getDashboard, getAlerts } from '@/lib/api';
import { Dashboard, Alert } from '@/types/operations';

// Health Score Component
const HealthScore: React.FC<{ score: number; status: string }> = ({ score, status }) => {
  const getColor = () => {
    if (status === 'critical') return 'text-red-600 bg-red-50';
    if (status === 'warning') return 'text-yellow-600 bg-yellow-50';
    return 'text-green-600 bg-green-50';
  };

  const getStatusIcon = () => {
    if (status === 'critical') return '❌';
    if (status === 'warning') return '⚠️';
    return '✅';
  };

  return (
    <div className={`p-4 rounded-lg ${getColor()}`}>
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium uppercase tracking-wide">Health Score</p>
          <p className="text-3xl font-bold mt-1">{score}/100</p>
        </div>
        <span className="text-4xl">{getStatusIcon()}</span>
      </div>
      <p className="text-xs mt-2 capitalize">{status}</p>
    </div>
  );
};

// KPI Card Component
const KPICard: React.FC<{ label: string; value: string; unit?: string }> = ({ label, value, unit }) => (
  <div className="bg-white p-4 rounded-lg border border-gray-200">
    <p className="text-sm font-medium text-gray-600 uppercase tracking-wide">{label}</p>
    <div className="flex items-baseline gap-2 mt-2">
      <p className="text-2xl font-bold text-gray-900">{value}</p>
      {unit && <p className="text-sm text-gray-500">{unit}</p>}
    </div>
  </div>
);

// Status Distribution Component
const StatusDistribution: React.FC<{ data: Record<string, number> }> = ({ data }) => {
  const total = Object.values(data).reduce((a, b) => a + b, 0);

  const getColor = (status: string) => {
    const colors: Record<string, string> = {
      completed: 'bg-green-500',
      running: 'bg-blue-500',
      queued: 'bg-yellow-500',
      failed: 'bg-red-500',
      cancelled: 'bg-gray-500',
      paused: 'bg-orange-500',
      timed_out: 'bg-red-700',
    };
    return colors[status] || 'bg-gray-500';
  };

  return (
    <div className="bg-white p-6 rounded-lg border border-gray-200">
      <h3 className="text-lg font-semibold text-gray-900 mb-4">Execution Status Distribution</h3>
      <div className="space-y-3">
        {Object.entries(data).map(([status, count]) => {
          const percentage = total > 0 ? (count / total) * 100 : 0;
          return (
            <div key={status}>
              <div className="flex justify-between items-center mb-1">
                <p className="text-sm font-medium text-gray-700 capitalize">{status.replace('_', ' ')}</p>
                <p className="text-sm font-semibold text-gray-900">{count} ({percentage.toFixed(1)}%)</p>
              </div>
              <div className="w-full bg-gray-200 rounded-full h-2">
                <div
                  className={`h-2 rounded-full ${getColor(status)}`}
                  style={{ width: `${percentage}%` }}
                ></div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

// Worker Health Component
const WorkerHealthCard: React.FC<{ data: any }> = ({ data }) => (
  <div className="bg-white p-6 rounded-lg border border-gray-200">
    <h3 className="text-lg font-semibold text-gray-900 mb-4">Worker Health</h3>
    <div className="grid grid-cols-2 gap-4">
      <div>
        <p className="text-sm text-gray-600 uppercase tracking-wide">Healthy</p>
        <p className="text-2xl font-bold text-green-600 mt-1">{data.healthy_workers}/{data.total_workers}</p>
      </div>
      <div>
        <p className="text-sm text-gray-600 uppercase tracking-wide">Utilization</p>
        <p className="text-2xl font-bold text-blue-600 mt-1">{(data.avg_utilization * 100).toFixed(1)}%</p>
      </div>
    </div>
    <div className="mt-4 p-3 bg-gray-50 rounded">
      <p className="text-xs text-gray-600 uppercase tracking-wide">Health Score</p>
      <p className="text-xl font-bold text-gray-900 mt-1">{data.health_score}/100</p>
    </div>
  </div>
);

// Queue Health Component
const QueueHealthCard: React.FC<{ data: any }> = ({ data }) => (
  <div className="bg-white p-6 rounded-lg border border-gray-200">
    <h3 className="text-lg font-semibold text-gray-900 mb-4">Queue Health</h3>
    <div className="space-y-3">
      <div className="flex justify-between items-center">
        <p className="text-sm text-gray-600">Queue Depth</p>
        <p className="text-lg font-semibold text-gray-900">{data.depth}</p>
      </div>
      <div className="flex justify-between items-center">
        <p className="text-sm text-gray-600">Oldest Item Age</p>
        <p className="text-lg font-semibold text-gray-900">{data.oldest_item_age}</p>
      </div>
      <div className="flex justify-between items-center">
        <p className="text-sm text-gray-600">Completion Rate</p>
        <p className="text-lg font-semibold text-gray-900">{data.completion_rate_per_sec.toFixed(1)} ops/s</p>
      </div>
      <div className="mt-3 p-3 bg-gray-50 rounded">
        <p className="text-xs text-gray-600 uppercase tracking-wide">Health Score</p>
        <p className="text-xl font-bold text-gray-900 mt-1">{data.health_score}/100</p>
      </div>
    </div>
  </div>
);

// Recent Failures Component
const RecentFailures: React.FC<{ failures: any[] }> = ({ failures }) => (
  <div className="bg-white p-6 rounded-lg border border-gray-200">
    <h3 className="text-lg font-semibold text-gray-900 mb-4">Recent Failures</h3>
    {failures.length === 0 ? (
      <p className="text-sm text-gray-500 text-center py-8">No recent failures 🎉</p>
    ) : (
      <div className="space-y-2 max-h-96 overflow-y-auto">
        {failures.slice(0, 10).map((failure) => (
          <div key={failure.id} className="p-3 bg-red-50 rounded border border-red-200">
            <div className="flex justify-between items-start">
              <div className="flex-1">
                <p className="text-sm font-medium text-gray-900">{failure.execution_id}</p>
                <p className="text-xs text-gray-600 mt-1">{failure.reason}</p>
              </div>
              <p className="text-xs text-gray-500 whitespace-nowrap ml-2">
                {new Date(failure.timestamp).toLocaleTimeString()}
              </p>
            </div>
            <p className="text-xs text-gray-500 mt-2">Correlation: {failure.correlation_id}</p>
          </div>
        ))}
      </div>
    )}
  </div>
);

// Main Dashboard Page
export default function OperationsDashboard() {
  const [dashboard, setDashboard] = useState<Dashboard | null>(null);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());

  useEffect(() => {
    const fetchData = async () => {
      try {
        setError(null);
        const [dashData, alertsData] = await Promise.all([getDashboard(), getAlerts()]);
        setDashboard(dashData);
        setAlerts(alertsData.filter((a) => !a.dismissed));
        setLastUpdate(new Date());
      } catch (err) {
        setError(err instanceof Error ? err.message : 'An error occurred');
      } finally {
        setLoading(false);
      }
    };

    fetchData();

    // Auto-refresh every 10 seconds
    const interval = setInterval(fetchData, 10000);
    return () => clearInterval(interval);
  }, []);

  if (loading && !dashboard) {
    return (
      <div className="min-h-screen bg-gray-50 p-8">
        <div className="space-y-4">
          <div className="h-12 bg-gray-200 rounded animate-pulse w-32"></div>
          <div className="grid grid-cols-4 gap-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-32 bg-gray-200 rounded animate-pulse"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="min-h-screen bg-gray-50 p-8">
        <div className="bg-red-50 border border-red-200 rounded-lg p-6 text-red-800">
          <h3 className="font-semibold mb-2">Error Loading Dashboard</h3>
          <p>{error}</p>
          <button
            onClick={() => window.location.reload()}
            className="mt-4 px-4 py-2 bg-red-600 text-white rounded hover:bg-red-700"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!dashboard) return null;

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-7xl mx-auto px-8 py-4">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-gray-900">Operations Dashboard</h1>
              <p className="text-sm text-gray-500 mt-1">Last updated: {lastUpdate.toLocaleTimeString()}</p>
            </div>
            {alerts.length > 0 && (
              <div className="flex items-center gap-2 px-4 py-2 bg-yellow-50 rounded-lg border border-yellow-200">
                <span className="text-2xl">⚠️</span>
                <span className="font-semibold text-yellow-900">{alerts.length} Active Alerts</span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-7xl mx-auto px-8 py-8">
        {/* System Health & Overview KPIs */}
        <div className="grid grid-cols-4 gap-6 mb-8">
          <HealthScore score={dashboard.system_health.overall_score} status={dashboard.system_health.status} />
          <KPICard label="Success Rate" value={`${(dashboard.overview_kpis.success_rate * 100).toFixed(1)}%`} />
          <KPICard label="Failure Rate" value={`${(dashboard.overview_kpis.failure_rate * 100).toFixed(1)}%`} />
          <KPICard label="Throughput" value={dashboard.overview_kpis.throughput_per_sec.toFixed(1)} unit="ops/s" />
        </div>

        {/* Queue and Worker Health */}
        <div className="grid grid-cols-2 gap-6 mb-8">
          <QueueHealthCard data={dashboard.queue_health} />
          <WorkerHealthCard data={dashboard.worker_health} />
        </div>

        {/* Execution Status and Recent Failures */}
        <div className="grid grid-cols-2 gap-6 mb-8">
          <StatusDistribution data={dashboard.execution_status} />
          <RecentFailures failures={dashboard.recent_failures} />
        </div>

        {/* Recovery and DLQ Stats */}
        <div className="grid grid-cols-2 gap-6">
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">Recovery Statistics</h3>
            <div className="space-y-3">
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Auto Recoveries</p>
                <p className="font-semibold text-gray-900">{dashboard.recovery_stats.auto_recoveries}</p>
              </div>
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Success Rate</p>
                <p className="font-semibold text-gray-900">{(dashboard.recovery_stats.recovery_success_rate * 100).toFixed(1)}%</p>
              </div>
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Cancelled</p>
                <p className="font-semibold text-gray-900">{dashboard.recovery_stats.cancelled_count}</p>
              </div>
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Paused</p>
                <p className="font-semibold text-gray-900">{dashboard.recovery_stats.paused_count}</p>
              </div>
            </div>
          </div>

          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">Dead Letter Queue</h3>
            <div className="space-y-3">
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Total Items</p>
                <p className="font-semibold text-gray-900">{dashboard.dlq_stats.total_items}</p>
              </div>
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Growth Rate</p>
                <p className="font-semibold text-gray-900">{dashboard.dlq_stats.growth_rate_per_hour.toFixed(1)}/hr</p>
              </div>
              <div className="flex justify-between">
                <p className="text-sm text-gray-600">Oldest Item</p>
                <p className="font-semibold text-gray-900">{dashboard.dlq_stats.oldest_item_age}</p>
              </div>
              <div className="mt-4 p-3 rounded" style={{
                backgroundColor: dashboard.dlq_stats.status === 'critical' ? '#fee2e2' :
                  dashboard.dlq_stats.status === 'warning' ? '#fef3c7' : '#dcfce7'
              }}>
                <p className="text-xs font-medium uppercase" style={{
                  color: dashboard.dlq_stats.status === 'critical' ? '#991b1b' :
                    dashboard.dlq_stats.status === 'warning' ? '#92400e' : '#15803d'
                }}>
                  Status: {dashboard.dlq_stats.status}
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

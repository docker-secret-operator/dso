import React, { useState } from 'react';
import { getDashboard, getRecoveryEvents, getDLQStats, getAlerts } from '@/lib/api';

const ReportsCenter: React.FC = () => {
  const [exporting, setExporting] = useState<string | null>(null);
  const [exported, setExported] = useState<string | null>(null);

  // Download helper
  const downloadJSON = (data: any, filename: string) => {
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  };

  // Export Operations Summary
  const exportOperationsSummary = async () => {
    setExporting('operations');
    try {
      const dashboard = await getDashboard();
      const timestamp = new Date().toISOString();

      const report = {
        export_date: timestamp,
        report_type: 'operations_summary',
        period: 'current_snapshot',
        overview: {
          total_executed: dashboard.overview_kpis.total_executed,
          total_succeeded: dashboard.overview_kpis.total_succeeded,
          total_failed: dashboard.overview_kpis.total_failed,
          success_rate: dashboard.overview_kpis.success_rate,
          failure_rate: dashboard.overview_kpis.failure_rate,
          avg_duration: dashboard.overview_kpis.avg_execution_time,
          throughput: dashboard.overview_kpis.throughput_per_sec,
        },
        queue_stats: {
          current_depth: dashboard.queue_health.depth,
          oldest_item_age: dashboard.queue_health.oldest_item_age,
          avg_wait_time: dashboard.queue_health.avg_wait_time,
          health_score: dashboard.queue_health.health_score,
          status: dashboard.queue_health.status,
        },
        worker_stats: {
          total_workers: dashboard.worker_health.total_workers,
          healthy_workers: dashboard.worker_health.healthy_workers,
          unhealthy_workers: dashboard.worker_health.unhealthy_workers,
          avg_utilization: dashboard.worker_health.avg_utilization,
          health_score: dashboard.worker_health.health_score,
          status: dashboard.worker_health.status,
        },
        execution_status: dashboard.execution_status,
        recovery_stats: dashboard.recovery_stats,
        dlq_stats: dashboard.dlq_stats,
        system_health: dashboard.system_health,
      };

      downloadJSON(report, `operations-summary-${new Date().getTime()}.json`);
      setExported('operations');
      setTimeout(() => setExported(null), 3000);
    } catch (err) {
      console.error('Export failed:', err);
      alert('Export failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setExporting(null);
    }
  };

  // Export Recovery Report
  const exportRecoveryReport = async () => {
    setExporting('recovery');
    try {
      const events = await getRecoveryEvents();
      const dashboard = await getDashboard();
      const timestamp = new Date().toISOString();

      const report = {
        export_date: timestamp,
        report_type: 'recovery_report',
        period: 'last_24_hours',
        summary: {
          total_recovery_events: events.length,
          worker_failures: events.filter(e => e.type === 'worker_failure').length,
          queue_recoveries: events.filter(e => e.type === 'queue_recovery').length,
          execution_cancellations: events.filter(e => e.type === 'execution_cancelled').length,
          execution_pauses: events.filter(e => e.type === 'execution_paused').length,
          auto_recovery_success_rate: dashboard.recovery_stats.recovery_success_rate,
        },
        recovery_events: events.map(e => ({
          id: e.id,
          type: e.type,
          execution_id: e.execution_id,
          correlation_id: e.correlation_id,
          worker_id: e.worker_id,
          details: e.details,
          timestamp: e.timestamp,
        })),
      };

      downloadJSON(report, `recovery-report-${new Date().getTime()}.json`);
      setExported('recovery');
      setTimeout(() => setExported(null), 3000);
    } catch (err) {
      console.error('Export failed:', err);
      alert('Export failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setExporting(null);
    }
  };

  // Export DLQ Report
  const exportDLQReport = async () => {
    setExporting('dlq');
    try {
      const dlqStats = await getDLQStats();
      const timestamp = new Date().toISOString();

      const report = {
        export_date: timestamp,
        report_type: 'dlq_report',
        summary: {
          total_items: dlqStats.total_items,
          retryable_count: dlqStats.retryable_count,
          permanent_count: dlqStats.permanent_count,
          oldest_item_age: dlqStats.oldest_item_age,
          status: dlqStats.status,
        },
        failure_breakdown: dlqStats.reason_breakdown,
        stats: dlqStats,
      };

      downloadJSON(report, `dlq-report-${new Date().getTime()}.json`);
      setExported('dlq');
      setTimeout(() => setExported(null), 3000);
    } catch (err) {
      console.error('Export failed:', err);
      alert('Export failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setExporting(null);
    }
  };

  // Export Alert Report
  const exportAlertReport = async () => {
    setExporting('alerts');
    try {
      const alerts = await getAlerts();
      const timestamp = new Date().toISOString();

      const report = {
        export_date: timestamp,
        report_type: 'alert_report',
        summary: {
          total_alerts: alerts.length,
          critical: alerts.filter(a => a.severity === 'critical').length,
          warning: alerts.filter(a => a.severity === 'warning').length,
          info: alerts.filter(a => a.severity === 'info').length,
        },
        alerts: alerts.map(a => ({
          id: a.id,
          type: a.type,
          severity: a.severity,
          message: a.message,
          value: a.value,
          threshold: a.threshold,
          timestamp: a.timestamp,
        })),
      };

      downloadJSON(report, `alert-report-${new Date().getTime()}.json`);
      setExported('alerts');
      setTimeout(() => setExported(null), 3000);
    } catch (err) {
      console.error('Export failed:', err);
      alert('Export failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setExporting(null);
    }
  };

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-6xl mx-auto px-8 py-6">
          <h1 className="text-3xl font-bold text-gray-900">Export Center</h1>
          <p className="text-sm text-gray-500 mt-1">Download operational reports and data exports</p>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-6xl mx-auto px-8 py-8">
        {/* Export Cards */}
        <div className="grid grid-cols-2 gap-6">
          {/* Operations Summary */}
          <div className="bg-white p-8 rounded-lg border border-gray-200 hover:shadow-lg transition-shadow">
            <div className="flex items-center gap-3 mb-4">
              <span className="text-3xl">📊</span>
              <h2 className="text-xl font-semibold text-gray-900">Operations Summary</h2>
            </div>
            <p className="text-gray-600 mb-6">
              Snapshot of current KPIs, queue health, worker status, and system metrics
            </p>
            <div className="space-y-2 mb-6 text-sm text-gray-600">
              <p>✓ Success rate</p>
              <p>✓ Queue depth & flow</p>
              <p>✓ Worker health</p>
              <p>✓ System score</p>
            </div>
            <button
              onClick={exportOperationsSummary}
              disabled={exporting === 'operations'}
              className="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting === 'operations' ? 'Exporting...' : '⬇️ Export JSON'}
            </button>
            {exported === 'operations' && (
              <p className="mt-2 text-sm text-green-600">✓ Downloaded successfully</p>
            )}
          </div>

          {/* Recovery Report */}
          <div className="bg-white p-8 rounded-lg border border-gray-200 hover:shadow-lg transition-shadow">
            <div className="flex items-center gap-3 mb-4">
              <span className="text-3xl">🔄</span>
              <h2 className="text-xl font-semibold text-gray-900">Recovery Report</h2>
            </div>
            <p className="text-gray-600 mb-6">
              Complete recovery event history and resilience metrics
            </p>
            <div className="space-y-2 mb-6 text-sm text-gray-600">
              <p>✓ Recovery events</p>
              <p>✓ Worker failures</p>
              <p>✓ Queue recoveries</p>
              <p>✓ Success rates</p>
            </div>
            <button
              onClick={exportRecoveryReport}
              disabled={exporting === 'recovery'}
              className="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting === 'recovery' ? 'Exporting...' : '⬇️ Export JSON'}
            </button>
            {exported === 'recovery' && (
              <p className="mt-2 text-sm text-green-600">✓ Downloaded successfully</p>
            )}
          </div>

          {/* DLQ Report */}
          <div className="bg-white p-8 rounded-lg border border-gray-200 hover:shadow-lg transition-shadow">
            <div className="flex items-center gap-3 mb-4">
              <span className="text-3xl">⚠️</span>
              <h2 className="text-xl font-semibold text-gray-900">Dead Letter Queue</h2>
            </div>
            <p className="text-gray-600 mb-6">
              Failed executions and failure analysis breakdown
            </p>
            <div className="space-y-2 mb-6 text-sm text-gray-600">
              <p>✓ Failed items</p>
              <p>✓ Failure reasons</p>
              <p>✓ Retry status</p>
              <p>✓ Age analysis</p>
            </div>
            <button
              onClick={exportDLQReport}
              disabled={exporting === 'dlq'}
              className="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting === 'dlq' ? 'Exporting...' : '⬇️ Export JSON'}
            </button>
            {exported === 'dlq' && (
              <p className="mt-2 text-sm text-green-600">✓ Downloaded successfully</p>
            )}
          </div>

          {/* Alert Report */}
          <div className="bg-white p-8 rounded-lg border border-gray-200 hover:shadow-lg transition-shadow">
            <div className="flex items-center gap-3 mb-4">
              <span className="text-3xl">🔔</span>
              <h2 className="text-xl font-semibold text-gray-900">Alert Report</h2>
            </div>
            <p className="text-gray-600 mb-6">
              Active alerts and alert history by severity
            </p>
            <div className="space-y-2 mb-6 text-sm text-gray-600">
              <p>✓ Critical alerts</p>
              <p>✓ Warnings</p>
              <p>✓ Info messages</p>
              <p>✓ Timestamps</p>
            </div>
            <button
              onClick={exportAlertReport}
              disabled={exporting === 'alerts'}
              className="w-full px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting === 'alerts' ? 'Exporting...' : '⬇️ Export JSON'}
            </button>
            {exported === 'alerts' && (
              <p className="mt-2 text-sm text-green-600">✓ Downloaded successfully</p>
            )}
          </div>
        </div>

        {/* Export Information */}
        <div className="mt-12 bg-blue-50 border border-blue-200 rounded-lg p-6">
          <h3 className="font-semibold text-blue-900 mb-3">Export Format</h3>
          <p className="text-blue-800 text-sm mb-3">
            All exports are in JSON format with complete metadata and timestamps. Files are downloaded with ISO format timestamps for easy sorting and versioning.
          </p>
          <p className="text-blue-800 text-sm">
            <strong>Use cases:</strong> Compliance reporting, data analysis, long-term archival, integration with external systems, compliance audits.
          </p>
        </div>
      </div>
    </div>
  );
};

export default ReportsCenter;

import React, { useEffect, useState } from 'react';
import { getAlerts } from '@/lib/api';
import { Alert } from '@/types/operations';

const AlertsPage: React.FC = () => {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [dismissedAlerts, setDismissedAlerts] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState<'all' | 'critical' | 'warning' | 'info'>('all');
  const [search, setSearch] = useState('');
  const [loading, setLoading] = useState(true);

  // Load dismissed alerts from localStorage
  useEffect(() => {
    const stored = localStorage.getItem('dismissedAlerts');
    if (stored) {
      setDismissedAlerts(new Set(JSON.parse(stored)));
    }
  }, []);

  // Fetch alerts
  useEffect(() => {
    const fetchAlerts = async () => {
      try {
        const data = await getAlerts();
        setAlerts(data);
      } catch (err) {
        console.error('Failed to fetch alerts:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchAlerts();
    const interval = setInterval(fetchAlerts, 10000);
    return () => clearInterval(interval);
  }, []);

  const handleDismissAlert = (alertId: string) => {
    const newDismissed = new Set(dismissedAlerts);
    newDismissed.add(alertId);
    setDismissedAlerts(newDismissed);
    localStorage.setItem('dismissedAlerts', JSON.stringify([...newDismissed]));
  };

  const handleRestoreAlert = (alertId: string) => {
    const newDismissed = new Set(dismissedAlerts);
    newDismissed.delete(alertId);
    setDismissedAlerts(newDismissed);
    localStorage.setItem('dismissedAlerts', JSON.stringify([...newDismissed]));
  };

  // Filter alerts
  const activeAlerts = alerts.filter(a => !dismissedAlerts.has(a.id));
  const dismissedAlertsList = alerts.filter(a => dismissedAlerts.has(a.id));

  const filteredAlerts = activeAlerts.filter(alert => {
    if (filter !== 'all' && alert.severity !== filter) return false;
    if (search && !alert.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  const getSeverityColor = (severity: string) => {
    switch (severity) {
      case 'critical':
        return 'bg-red-50 border-red-200 text-red-900';
      case 'warning':
        return 'bg-yellow-50 border-yellow-200 text-yellow-900';
      case 'info':
        return 'bg-blue-50 border-blue-200 text-blue-900';
      default:
        return 'bg-gray-50 border-gray-200 text-gray-900';
    }
  };

  const getSeverityIcon = (severity: string) => {
    switch (severity) {
      case 'critical':
        return '🔴';
      case 'warning':
        return '🟡';
      case 'info':
        return '🔵';
      default:
        return '⚪';
    }
  };

  const alertCount = {
    critical: activeAlerts.filter(a => a.severity === 'critical').length,
    warning: activeAlerts.filter(a => a.severity === 'warning').length,
    info: activeAlerts.filter(a => a.severity === 'info').length,
  };

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-6xl mx-auto px-8 py-6">
          <h1 className="text-3xl font-bold text-gray-900">Alert Center</h1>
          <p className="text-sm text-gray-500 mt-1">Manage and investigate system alerts</p>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-6xl mx-auto px-8 py-8">
        {/* Alert Summary Cards */}
        <div className="grid grid-cols-3 gap-4 mb-8">
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Critical Alerts</p>
            <p className="text-3xl font-bold text-red-600 mt-2">{alertCount.critical}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Warnings</p>
            <p className="text-3xl font-bold text-yellow-600 mt-2">{alertCount.warning}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Info</p>
            <p className="text-3xl font-bold text-blue-600 mt-2">{alertCount.info}</p>
          </div>
        </div>

        {/* Filters and Search */}
        <div className="bg-white p-6 rounded-lg border border-gray-200 mb-8">
          <div className="flex gap-4 mb-4">
            <button
              onClick={() => setFilter('all')}
              className={`px-4 py-2 rounded ${
                filter === 'all'
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              All ({activeAlerts.length})
            </button>
            <button
              onClick={() => setFilter('critical')}
              className={`px-4 py-2 rounded ${
                filter === 'critical'
                  ? 'bg-red-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              Critical ({alertCount.critical})
            </button>
            <button
              onClick={() => setFilter('warning')}
              className={`px-4 py-2 rounded ${
                filter === 'warning'
                  ? 'bg-yellow-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              Warning ({alertCount.warning})
            </button>
            <button
              onClick={() => setFilter('info')}
              className={`px-4 py-2 rounded ${
                filter === 'info'
                  ? 'bg-blue-600 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
            >
              Info ({alertCount.info})
            </button>
          </div>

          <input
            type="text"
            placeholder="Search alerts..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
        </div>

        {/* Active Alerts */}
        {filteredAlerts.length === 0 ? (
          <div className="bg-white p-12 rounded-lg border border-gray-200 text-center">
            <p className="text-gray-500">No alerts to display 🎉</p>
          </div>
        ) : (
          <div className="space-y-4 mb-8">
            {filteredAlerts.map((alert) => (
              <div
                key={alert.id}
                className={`p-6 rounded-lg border ${getSeverityColor(alert.severity)}`}
              >
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-2">
                      <span className="text-2xl">{getSeverityIcon(alert.severity)}</span>
                      <span className="text-sm font-semibold uppercase tracking-wide">{alert.severity}</span>
                      <span className="text-xs text-gray-500">
                        {new Date(alert.timestamp).toLocaleTimeString()}
                      </span>
                    </div>
                    <p className="font-semibold text-lg mb-2">{alert.message}</p>
                    <div className="text-sm opacity-75">
                      <p>Value: {alert.value.toFixed(2)} | Threshold: {alert.threshold.toFixed(2)}</p>
                      <p>Type: {alert.type}</p>
                    </div>
                  </div>
                  <button
                    onClick={() => handleDismissAlert(alert.id)}
                    className="ml-4 px-4 py-2 text-sm font-medium rounded hover:opacity-75"
                  >
                    ✕ Dismiss
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Dismissed Alerts */}
        {dismissedAlertsList.length > 0 && (
          <div className="mt-12">
            <h2 className="text-xl font-semibold text-gray-900 mb-4">Dismissed Alerts ({dismissedAlertsList.length})</h2>
            <div className="space-y-2">
              {dismissedAlertsList.map((alert) => (
                <div key={alert.id} className="bg-gray-100 p-4 rounded-lg border border-gray-300 flex items-center justify-between">
                  <div className="flex-1">
                    <p className="text-sm text-gray-700">{alert.message}</p>
                    <p className="text-xs text-gray-500 mt-1">
                      {new Date(alert.timestamp).toLocaleTimeString()}
                    </p>
                  </div>
                  <button
                    onClick={() => handleRestoreAlert(alert.id)}
                    className="ml-4 px-3 py-1 text-sm bg-gray-300 text-gray-900 rounded hover:bg-gray-400"
                  >
                    Restore
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default AlertsPage;

'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';

interface IntegrationMetrics {
  plugin_id: string;
  total_events: number;
  successful_count: number;
  failed_count: number;
  last_success_time?: string;
  last_error_time?: string;
  last_error?: string;
}

interface Integration {
  plugin_id: string;
  enabled: boolean;
  endpoint: string;
  auth_type: string;
  updated_at: string;
  metrics: IntegrationMetrics;
}

interface QueueStats {
  pending: number;
  dead_letter: number;
  total: number;
}

export default function IntegrationsPage() {
  const router = useRouter();
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [queueStats, setQueueStats] = useState<QueueStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedIntegration, setSelectedIntegration] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      const [integrationsRes, queueRes] = await Promise.all([
        fetch('/api/integrations'),
        fetch('/api/integrations/queue'),
      ]);

      if (!integrationsRes.ok) {
        if (integrationsRes.status === 403) {
          router.push('/');
          return;
        }
        throw new Error('Failed to fetch integrations');
      }

      setIntegrations(await integrationsRes.json());

      if (queueRes.ok) {
        setQueueStats(await queueRes.json());
      }

      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
    const interval = setInterval(fetchData, 30000);
    return () => clearInterval(interval);
  }, []);

  const handleTest = async (pluginID: string) => {
    try {
      const res = await fetch(`/api/integrations/${pluginID}/test`, { method: 'POST' });
      if (res.ok) {
        await fetchData();
        alert('Test delivery succeeded');
      } else {
        alert('Test delivery failed');
      }
    } catch (err) {
      alert(`Error: ${err instanceof Error ? err.message : 'unknown'}`);
    }
  };

  const handleEnable = async (pluginID: string) => {
    try {
      const res = await fetch(`/api/integrations/${pluginID}/enable`, { method: 'POST' });
      if (res.ok) {
        await fetchData();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'unknown error');
    }
  };

  const handleDisable = async (pluginID: string) => {
    try {
      const res = await fetch(`/api/integrations/${pluginID}/disable`, { method: 'POST' });
      if (res.ok) {
        await fetchData();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'unknown error');
    }
  };

  const getHealthColor = (integration: Integration) => {
    if (!integration.enabled) return 'bg-gray-100 text-gray-800';
    if (integration.metrics.failed_count > integration.metrics.successful_count) {
      return 'bg-yellow-100 text-yellow-800';
    }
    return 'bg-green-100 text-green-800';
  };

  const getHealthText = (integration: Integration) => {
    if (!integration.enabled) return 'disabled';
    if (integration.metrics.failed_count > integration.metrics.successful_count) {
      return 'degraded';
    }
    return 'healthy';
  };

  if (loading) {
    return <div className="flex items-center justify-center h-screen">Loading...</div>;
  }

  const healthyCount = integrations.filter(
    (i) => i.enabled && i.metrics.failed_count <= i.metrics.successful_count
  ).length;
  const failedCount = integrations.filter(
    (i) => i.enabled && i.metrics.failed_count > i.metrics.successful_count
  ).length;
  const disabledCount = integrations.filter((i) => !i.enabled).length;

  return (
    <div className="min-h-screen bg-gray-50 p-6">
      <div className="max-w-7xl mx-auto">
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900 mb-2">Integrations</h1>
          <p className="text-gray-600">Manage webhooks and external system integrations</p>
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg text-red-700">
            {error}
          </div>
        )}

        {/* Summary Cards */}
        <div className="grid grid-cols-4 gap-4 mb-6">
          <div className="bg-white p-4 rounded-lg shadow">
            <div className="text-gray-600 text-sm">Total Integrations</div>
            <div className="text-2xl font-bold text-gray-900">{integrations.length}</div>
          </div>
          <div className="bg-white p-4 rounded-lg shadow">
            <div className="text-gray-600 text-sm">Healthy</div>
            <div className="text-2xl font-bold text-green-600">{healthyCount}</div>
          </div>
          <div className="bg-white p-4 rounded-lg shadow">
            <div className="text-gray-600 text-sm">Degraded</div>
            <div className="text-2xl font-bold text-yellow-600">{failedCount}</div>
          </div>
          <div className="bg-white p-4 rounded-lg shadow">
            <div className="text-gray-600 text-sm">Pending Queue</div>
            <div className="text-2xl font-bold text-blue-600">{queueStats?.pending || 0}</div>
          </div>
        </div>

        {/* Integrations Table */}
        {integrations.length === 0 ? (
          <div className="bg-white rounded-lg shadow p-8 text-center">
            <p className="text-gray-500">No integrations configured</p>
          </div>
        ) : (
          <div className="bg-white rounded-lg shadow overflow-hidden">
            <table className="w-full">
              <thead className="bg-gray-50 border-b border-gray-200">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Plugin
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Health
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Endpoint
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Deliveries
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Failures
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Last Success
                  </th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200">
                {integrations.map((integration) => (
                  <tr key={integration.plugin_id} className="hover:bg-gray-50">
                    <td className="px-6 py-4 whitespace-nowrap font-medium text-gray-900">
                      {integration.plugin_id}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className={`px-2 py-1 text-xs font-medium rounded ${getHealthColor(integration)}`}>
                        {getHealthText(integration)}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-gray-700 truncate">
                      {integration.endpoint}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-700">
                      {integration.metrics.total_events}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className="text-sm text-red-600">
                        {integration.metrics.failed_count}
                      </span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-700">
                      {integration.metrics.last_success_time
                        ? new Date(integration.metrics.last_success_time).toLocaleDateString()
                        : 'Never'}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm space-x-2">
                      <button
                        onClick={() => handleTest(integration.plugin_id)}
                        className="text-blue-600 hover:text-blue-900 font-medium"
                      >
                        Test
                      </button>
                      {integration.enabled ? (
                        <button
                          onClick={() => handleDisable(integration.plugin_id)}
                          className="text-red-600 hover:text-red-900 font-medium"
                        >
                          Disable
                        </button>
                      ) : (
                        <button
                          onClick={() => handleEnable(integration.plugin_id)}
                          className="text-green-600 hover:text-green-900 font-medium"
                        >
                          Enable
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

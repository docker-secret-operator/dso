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

function getAuthHeaders(): Record<string, string> {
  const token = typeof window !== 'undefined' ? localStorage.getItem('dso_api_token') : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export default function IntegrationsPage() {
  const router = useRouter();
  const [integrations, setIntegrations] = useState<Integration[]>([]);
  const [queueStats, setQueueStats] = useState<QueueStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState<{ text: string; type: 'success' | 'error' } | null>(null);
  const [selectedIntegration, setSelectedIntegration] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      const headers = getAuthHeaders();
      const [integrationsRes, queueRes] = await Promise.all([
        fetch('/api/integrations', { headers }),
        fetch('/api/integrations/queue', { headers }),
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
      const res = await fetch(`/api/integrations/${pluginID}/test`, { method: 'POST', headers: getAuthHeaders() });
      if (res.ok) {
        await fetchData();
        setActionMessage({ text: 'Test delivery succeeded', type: 'success' });
      } else {
        setActionMessage({ text: 'Test delivery failed', type: 'error' });
      }
    } catch (err) {
      setActionMessage({ text: `Error: ${err instanceof Error ? err.message : 'unknown'}`, type: 'error' });
    }
  };

  const handleEnable = async (pluginID: string) => {
    try {
      const res = await fetch(`/api/integrations/${pluginID}/enable`, { method: 'POST', headers: getAuthHeaders() });
      if (res.ok) {
        await fetchData();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'unknown error');
    }
  };

  const handleDisable = async (pluginID: string) => {
    try {
      const res = await fetch(`/api/integrations/${pluginID}/disable`, { method: 'POST', headers: getAuthHeaders() });
      if (res.ok) {
        await fetchData();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'unknown error');
    }
  };

  const getHealthColor = (integration: Integration) => {
    if (!integration.enabled) return 'bg-slate-700/30 text-slate-400';
    if (integration.metrics.failed_count > integration.metrics.successful_count) {
      return 'bg-amber-500/15 text-amber-300';
    }
    return 'bg-emerald-500/15 text-emerald-300';
  };

  const getHealthText = (integration: Integration) => {
    if (!integration.enabled) return 'disabled';
    if (integration.metrics.failed_count > integration.metrics.successful_count) {
      return 'degraded';
    }
    return 'healthy';
  };

  if (loading) {
    return <div className="flex items-center justify-center h-screen text-slate-200">Loading...</div>;
  }

  const healthyCount = integrations.filter(
    (i) => i.enabled && i.metrics.failed_count <= i.metrics.successful_count
  ).length;
  const failedCount = integrations.filter(
    (i) => i.enabled && i.metrics.failed_count > i.metrics.successful_count
  ).length;
  const disabledCount = integrations.filter((i) => !i.enabled).length;

  return (
    <div className="min-h-screen p-6">
      <div className="max-w-7xl mx-auto">
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-slate-100 mb-2">Integrations</h1>
          <p className="text-slate-400">Manage webhooks and external system integrations</p>
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-500/10 border border-red-500/30 rounded-lg text-red-300">
            {error}
          </div>
        )}

        {actionMessage && (
          <div className={`mb-4 p-4 rounded-lg flex items-center justify-between ${
            actionMessage.type === 'success'
              ? 'bg-emerald-500/10 border border-emerald-500/30 text-emerald-300'
              : 'bg-red-500/10 border border-red-500/30 text-red-300'
          }`}>
            <span>{actionMessage.text}</span>
            <button onClick={() => setActionMessage(null)} className="ml-4 font-bold opacity-70 hover:opacity-100">×</button>
          </div>
        )}

        {/* Summary Cards */}
        <div className="grid grid-cols-4 gap-4 mb-6">
          <div className="bg-[#111318] border border-slate-700/50 p-4 rounded-lg">
            <div className="text-slate-400 text-sm">Total Integrations</div>
            <div className="text-2xl font-bold text-slate-100">{integrations.length}</div>
          </div>
          <div className="bg-[#111318] border border-slate-700/50 p-4 rounded-lg">
            <div className="text-slate-400 text-sm">Healthy</div>
            <div className="text-2xl font-bold text-emerald-400">{healthyCount}</div>
          </div>
          <div className="bg-[#111318] border border-slate-700/50 p-4 rounded-lg">
            <div className="text-slate-400 text-sm">Degraded</div>
            <div className="text-2xl font-bold text-amber-400">{failedCount}</div>
          </div>
          <div className="bg-[#111318] border border-slate-700/50 p-4 rounded-lg">
            <div className="text-slate-400 text-sm">Pending Queue</div>
            <div className="text-2xl font-bold text-blue-400">{queueStats?.pending || 0}</div>
          </div>
        </div>

        {/* Integrations Table */}
        {integrations.length === 0 ? (
          <div className="bg-[#111318] border border-slate-700/50 rounded-lg p-8 text-center">
            <p className="text-slate-500">No integrations configured</p>
          </div>
        ) : (
          <div className="bg-[#111318] border border-slate-700/50 rounded-lg overflow-hidden">
            <table className="w-full">
              <thead className="bg-[#0f1015] border-b border-slate-700/50">
                <tr>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Plugin</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Health</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Endpoint</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Deliveries</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Failures</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Last Success</th>
                  <th className="px-6 py-3 text-left text-xs font-medium text-slate-400 uppercase">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-700/30">
                {integrations.map((integration) => (
                  <tr key={integration.plugin_id} className="hover:bg-slate-800/50/[0.02]">
                    <td className="px-6 py-4 whitespace-nowrap font-medium text-slate-200">
                      {integration.plugin_id}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className={`px-2 py-1 text-xs font-medium rounded ${getHealthColor(integration)}`}>
                        {getHealthText(integration)}
                      </span>
                    </td>
                    <td className="px-6 py-4 text-sm text-slate-400 truncate">
                      {integration.endpoint}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-slate-400">
                      {integration.metrics.total_events}
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap">
                      <span className="text-sm text-red-400">
                        {integration.metrics.failed_count}
                      </span>
                    </td>
                    <td className="px-6 py-4 whitespace-nowrap text-sm text-slate-400">
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

'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';

interface Plugin {
  id: string;
  name: string;
  version: string;
  type: string;
  description: string;
  enabled: boolean;
  status: string;
  health: string;
  capabilities: string[];
  dependencies: string[];
  error_message?: string;
  loaded_at?: string;
  enabled_at?: string;
  disabled_at?: string;
  last_error_time?: string;
  last_heartbeat?: string;
  restart_count: number;
  event_count: number;
  uptime_ms: number;
  error_count: number;
}

interface PluginStatus {
  status: string;
  total_plugins: number;
  healthy: number;
  degraded: number;
  failed: number;
  disabled: number;
  timestamp: string;
}

export default function PluginsPage() {
  const router = useRouter();
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [status, setStatus] = useState<PluginStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedPlugin, setSelectedPlugin] = useState<string | null>(null);

  const fetchData = async () => {
    try {
      const [pluginsRes, statusRes] = await Promise.all([
        fetch('/api/plugins'),
        fetch('/api/plugins/status'),
      ]);

      if (!pluginsRes.ok) {
        if (pluginsRes.status === 403) {
          router.push('/');
          return;
        }
        throw new Error('Failed to fetch plugins');
      }

      if (statusRes.ok) {
        setStatus(await statusRes.json());
      }

      setPlugins(await pluginsRes.json());
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

  const handleEnable = async (pluginId: string) => {
    try {
      const res = await fetch(`/api/plugins/${pluginId}/enable`, { method: 'POST' });
      if (res.ok) {
        await fetchData();
      } else {
        setError('Failed to enable plugin');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleDisable = async (pluginId: string) => {
    try {
      const res = await fetch(`/api/plugins/${pluginId}/disable`, { method: 'POST' });
      if (res.ok) {
        await fetchData();
      } else {
        setError('Failed to disable plugin');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const getHealthColor = (health: string) => {
    switch (health) {
      case 'healthy':
        return 'bg-green-100 text-green-800';
      case 'degraded':
        return 'bg-yellow-100 text-yellow-800';
      case 'failed':
        return 'bg-red-100 text-red-800';
      case 'disabled':
        return 'bg-gray-100 text-gray-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'enabled':
        return 'text-green-600';
      case 'disabled':
        return 'text-gray-600';
      case 'failed':
        return 'text-red-600';
      default:
        return 'text-gray-600';
    }
  };

  const formatUptime = (ms: number) => {
    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (days > 0) return `${days}d ${hours % 24}h`;
    if (hours > 0) return `${hours}h ${minutes % 60}m`;
    if (minutes > 0) return `${minutes}m`;
    return `${seconds}s`;
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="text-gray-500">Loading plugins...</div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50 p-6">
      <div className="max-w-7xl mx-auto">
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900 mb-2">Plugins</h1>
          <p className="text-gray-600">Manage and monitor system plugins</p>
        </div>

        {error && (
          <div className="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg text-red-700">
            {error}
          </div>
        )}

        {status && (
          <div className="grid grid-cols-4 gap-4 mb-6">
            <div className="bg-white p-4 rounded-lg shadow">
              <div className="text-gray-600 text-sm">Total Plugins</div>
              <div className="text-2xl font-bold text-gray-900">{status.total_plugins}</div>
            </div>
            <div className="bg-white p-4 rounded-lg shadow">
              <div className="text-gray-600 text-sm">Healthy</div>
              <div className="text-2xl font-bold text-green-600">{status.healthy}</div>
            </div>
            <div className="bg-white p-4 rounded-lg shadow">
              <div className="text-gray-600 text-sm">Degraded</div>
              <div className="text-2xl font-bold text-yellow-600">{status.degraded}</div>
            </div>
            <div className="bg-white p-4 rounded-lg shadow">
              <div className="text-gray-600 text-sm">Failed</div>
              <div className="text-2xl font-bold text-red-600">{status.failed}</div>
            </div>
          </div>
        )}

        {plugins.length === 0 ? (
          <div className="bg-white rounded-lg shadow p-8 text-center">
            <p className="text-gray-500">No plugins available</p>
          </div>
        ) : (
          <div className="bg-white rounded-lg shadow overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead className="bg-gray-50 border-b border-gray-200">
                  <tr>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Name
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Type
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Version
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Status
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Health
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Uptime
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Events
                    </th>
                    <th className="px-6 py-3 text-left text-xs font-medium text-gray-700 uppercase tracking-wider">
                      Actions
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {plugins.map((plugin) => (
                    <tr key={plugin.id} className="hover:bg-gray-50">
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div>
                          <div className="font-medium text-gray-900">{plugin.name}</div>
                          <div className="text-sm text-gray-500">{plugin.id}</div>
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className="px-2 py-1 text-xs font-medium bg-blue-50 text-blue-700 rounded">
                          {plugin.type}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-700">
                        {plugin.version}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`font-medium ${getStatusColor(plugin.status)}`}>
                          {plugin.status}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <span className={`px-2 py-1 text-xs font-medium rounded ${getHealthColor(plugin.health)}`}>
                          {plugin.health}
                        </span>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-700">
                        {formatUptime(plugin.uptime_ms)}
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap">
                        <div className="flex items-center gap-2">
                          <span className="text-sm text-gray-700">{plugin.event_count}</span>
                          {plugin.error_count > 0 && (
                            <span className="text-xs px-2 py-1 bg-red-50 text-red-700 rounded">
                              {plugin.error_count} errors
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-6 py-4 whitespace-nowrap text-sm space-x-2">
                        {plugin.enabled ? (
                          <button
                            onClick={() => handleDisable(plugin.id)}
                            className="text-red-600 hover:text-red-900 font-medium"
                          >
                            Disable
                          </button>
                        ) : (
                          <button
                            onClick={() => handleEnable(plugin.id)}
                            className="text-green-600 hover:text-green-900 font-medium"
                          >
                            Enable
                          </button>
                        )}
                        <button
                          onClick={() => setSelectedPlugin(selectedPlugin === plugin.id ? null : plugin.id)}
                          className="text-blue-600 hover:text-blue-900 font-medium"
                        >
                          Details
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {selectedPlugin && (
          <div className="mt-6 bg-white rounded-lg shadow p-6">
            {plugins.find((p) => p.id === selectedPlugin) && (
              <div>
                <h3 className="text-lg font-bold text-gray-900 mb-4">Plugin Details</h3>
                <div className="grid grid-cols-2 gap-6">
                  <div>
                    <label className="block text-sm font-medium text-gray-700 mb-1">
                      Description
                    </label>
                    <p className="text-gray-700">
                      {plugins.find((p) => p.id === selectedPlugin)?.description}
                    </p>
                  </div>
                  {plugins.find((p) => p.id === selectedPlugin)?.capabilities.length! > 0 && (
                    <div>
                      <label className="block text-sm font-medium text-gray-700 mb-1">
                        Capabilities
                      </label>
                      <div className="flex flex-wrap gap-2">
                        {plugins
                          .find((p) => p.id === selectedPlugin)
                          ?.capabilities.map((cap) => (
                            <span
                              key={cap}
                              className="px-2 py-1 text-xs font-medium bg-green-50 text-green-700 rounded"
                            >
                              {cap}
                            </span>
                          ))}
                      </div>
                    </div>
                  )}
                  {plugins.find((p) => p.id === selectedPlugin)?.error_message && (
                    <div className="col-span-2">
                      <label className="block text-sm font-medium text-gray-700 mb-1">
                        Error Message
                      </label>
                      <div className="p-3 bg-red-50 border border-red-200 rounded text-sm text-red-700">
                        {plugins.find((p) => p.id === selectedPlugin)?.error_message}
                      </div>
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

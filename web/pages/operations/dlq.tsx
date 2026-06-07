import React, { useEffect, useState } from 'react';
import { getDLQItems, getDLQStats, exportDLQ } from '@/lib/api';
import { DLQItem } from '@/types/operations';

const DLQPage: React.FC = () => {
  const [items, setItems] = useState<DLQItem[]>([]);
  const [stats, setStats] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [sortBy, setSortBy] = useState<'age' | 'retry_count' | 'id'>('age');
  const [exporting, setExporting] = useState(false);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [itemsData, statsData] = await Promise.all([getDLQItems(), getDLQStats()]);
        setItems(itemsData);
        setStats(statsData);
      } catch (err) {
        console.error('Failed to fetch DLQ data:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 10000);
    return () => clearInterval(interval);
  }, []);

  const handleExport = async () => {
    setExporting(true);
    try {
      await exportDLQ();
    } catch (err) {
      console.error('Export failed:', err);
      alert('Export failed: ' + (err instanceof Error ? err.message : 'Unknown error'));
    } finally {
      setExporting(false);
    }
  };

  const filteredItems = items
    .filter(item =>
      search === '' ||
      item.execution_id.toLowerCase().includes(search.toLowerCase()) ||
      item.correlation_id.toLowerCase().includes(search.toLowerCase()) ||
      item.reason.toLowerCase().includes(search.toLowerCase())
    )
    .sort((a, b) => {
      if (sortBy === 'age') {
        return new Date(b.enqueued_at).getTime() - new Date(a.enqueued_at).getTime();
      } else if (sortBy === 'retry_count') {
        return b.retry_count - a.retry_count;
      }
      return a.id.localeCompare(b.id);
    });

  const getReasonColor = (reason: string) => {
    if (reason.includes('timeout')) return 'bg-red-100 text-red-800';
    if (reason.includes('retries')) return 'bg-orange-100 text-orange-800';
    if (reason.includes('crash')) return 'bg-purple-100 text-purple-800';
    return 'bg-gray-100 text-gray-800';
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-50 p-8">
        <div className="space-y-4">
          <div className="h-12 bg-gray-200 rounded animate-pulse w-32"></div>
          <div className="grid grid-cols-3 gap-4">
            {[...Array(3)].map((_, i) => (
              <div key={i} className="h-32 bg-gray-200 rounded animate-pulse"></div>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-7xl mx-auto px-8 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-gray-900">Dead Letter Queue</h1>
              <p className="text-sm text-gray-500 mt-1">Failed executions awaiting investigation</p>
            </div>
            <button
              onClick={handleExport}
              disabled={exporting || items.length === 0}
              className="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
            >
              {exporting ? 'Exporting...' : '⬇️ Export Report'}
            </button>
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-7xl mx-auto px-8 py-8">
        {/* Stats Cards */}
        {stats && (
          <div className="grid grid-cols-4 gap-4 mb-8">
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <p className="text-sm font-medium text-gray-600 uppercase">Total Items</p>
              <p className="text-3xl font-bold text-gray-900 mt-2">{stats.total_items}</p>
            </div>
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <p className="text-sm font-medium text-gray-600 uppercase">Retryable</p>
              <p className="text-3xl font-bold text-blue-600 mt-2">{stats.retryable_count}</p>
            </div>
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <p className="text-sm font-medium text-gray-600 uppercase">Permanent</p>
              <p className="text-3xl font-bold text-red-600 mt-2">{stats.permanent_count}</p>
            </div>
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <p className="text-sm font-medium text-gray-600 uppercase">Oldest Item</p>
              <p className="text-xl font-bold text-gray-900 mt-2">{stats.oldest_item_age}</p>
            </div>
          </div>
        )}

        {/* Failure Reason Breakdown */}
        {stats && stats.reason_breakdown && (
          <div className="bg-white p-6 rounded-lg border border-gray-200 mb-8">
            <h3 className="text-lg font-semibold text-gray-900 mb-4">Failure Reason Breakdown</h3>
            <div className="space-y-3">
              {stats.reason_breakdown.map((breakdown: any) => (
                <div key={breakdown.reason}>
                  <div className="flex justify-between items-center mb-1">
                    <p className="text-sm font-medium text-gray-700">{breakdown.reason}</p>
                    <p className="text-sm font-semibold text-gray-900">
                      {breakdown.count} ({breakdown.percentage.toFixed(1)}%)
                    </p>
                  </div>
                  <div className="w-full bg-gray-200 rounded-full h-2">
                    <div
                      className="h-2 rounded-full bg-blue-600"
                      style={{ width: `${breakdown.percentage}%` }}
                    ></div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Search and Sort */}
        <div className="bg-white p-6 rounded-lg border border-gray-200 mb-8">
          <div className="flex gap-4">
            <div className="flex-1">
              <input
                type="text"
                placeholder="Search by execution ID, correlation ID, or reason..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as any)}
              className="px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="age">Sort by Age</option>
              <option value="retry_count">Sort by Retries</option>
              <option value="id">Sort by ID</option>
            </select>
          </div>
        </div>

        {/* Items Table */}
        {filteredItems.length === 0 ? (
          <div className="bg-white p-12 rounded-lg border border-gray-200 text-center">
            <p className="text-gray-500 text-lg">
              {search ? 'No items match your search' : 'Dead letter queue is empty 🎉'}
            </p>
          </div>
        ) : (
          <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="bg-gray-50 border-b border-gray-200">
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Execution ID</th>
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Correlation ID</th>
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Reason</th>
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Retries</th>
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Age</th>
                    <th className="px-6 py-4 text-left text-sm font-semibold text-gray-700">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredItems.map((item) => (
                    <tr key={item.id} className="border-b border-gray-200 hover:bg-gray-50">
                      <td className="px-6 py-4">
                        <p className="font-mono text-sm text-gray-900">{item.execution_id}</p>
                      </td>
                      <td className="px-6 py-4">
                        <p className="font-mono text-xs text-gray-600">{item.correlation_id}</p>
                      </td>
                      <td className="px-6 py-4">
                        <span className={`inline-block px-2 py-1 rounded text-xs font-semibold ${getReasonColor(item.reason)}`}>
                          {item.reason}
                        </span>
                      </td>
                      <td className="px-6 py-4">
                        <p className="text-sm font-semibold text-gray-900">
                          {item.retry_count}/{item.max_retries}
                        </p>
                      </td>
                      <td className="px-6 py-4">
                        <p className="text-sm text-gray-600">{item.age}</p>
                      </td>
                      <td className="px-6 py-4">
                        <span
                          className={`inline-block px-2 py-1 rounded text-xs font-semibold ${
                            item.retryable
                              ? 'bg-green-100 text-green-800'
                              : 'bg-red-100 text-red-800'
                          }`}
                        >
                          {item.retryable ? 'Retryable' : 'Permanent'}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="px-6 py-4 bg-gray-50 border-t border-gray-200 text-sm text-gray-600">
              Showing {filteredItems.length} of {items.length} items
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default DLQPage;

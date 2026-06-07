import React, { useState } from 'react';
import { getTrace, searchTrace } from '@/lib/api';
import { TraceExplorer } from '@/types/operations';

const TraceExplorerPage: React.FC = () => {
  const [correlationId, setCorrelationId] = useState('');
  const [trace, setTrace] = useState<TraceExplorer | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!correlationId.trim()) return;

    setLoading(true);
    setError(null);
    try {
      const data = await getTrace(correlationId);
      setTrace(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load trace');
      setTrace(null);
    } finally {
      setLoading(false);
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'text-green-600';
      case 'failed':
        return 'text-red-600';
      case 'cancelled':
        return 'text-gray-600';
      case 'paused':
        return 'text-orange-600';
      default:
        return 'text-blue-600';
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'completed':
        return '✅';
      case 'failed':
        return '❌';
      case 'cancelled':
        return '⏹️';
      case 'paused':
        return '⏸️';
      default:
        return '⏳';
    }
  };

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <div className="bg-white border-b border-gray-200 sticky top-0 z-50">
        <div className="max-w-6xl mx-auto px-8 py-6">
          <h1 className="text-3xl font-bold text-gray-900">Trace Explorer</h1>
          <p className="text-sm text-gray-500 mt-1">Investigate execution traces by correlation ID</p>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-6xl mx-auto px-8 py-8">
        {/* Search Form */}
        <div className="bg-white p-6 rounded-lg border border-gray-200 mb-8">
          <form onSubmit={handleSearch}>
            <div className="flex gap-2">
              <input
                type="text"
                placeholder="Enter Correlation ID..."
                value={correlationId}
                onChange={(e) => setCorrelationId(e.target.value)}
                className="flex-1 px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
              <button
                type="submit"
                disabled={loading}
                className="px-6 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
              >
                {loading ? 'Searching...' : 'Search'}
              </button>
            </div>
          </form>
        </div>

        {error && (
          <div className="bg-red-50 border border-red-200 rounded-lg p-6 mb-8 text-red-800">
            <p className="font-semibold">Error</p>
            <p className="text-sm mt-1">{error}</p>
          </div>
        )}

        {trace && (
          <div className="space-y-8">
            {/* Trace Header */}
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <div className="grid grid-cols-2 gap-6">
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Execution ID</p>
                  <p className="text-lg font-mono font-semibold text-gray-900 mt-1">{trace.execution_id}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Correlation ID</p>
                  <p className="text-lg font-mono font-semibold text-gray-900 mt-1">{trace.correlation_id}</p>
                </div>
              </div>

              <div className="grid grid-cols-4 gap-4 mt-6">
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Status</p>
                  <p className={`text-xl font-semibold mt-1 flex items-center gap-2 ${getStatusColor(trace.status)}`}>
                    <span>{getStatusIcon(trace.status)}</span>
                    <span className="capitalize">{trace.status}</span>
                  </p>
                </div>
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Duration</p>
                  <p className="text-xl font-semibold text-gray-900 mt-1">{trace.duration}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Events</p>
                  <p className="text-xl font-semibold text-gray-900 mt-1">{trace.event_count}</p>
                </div>
                <div>
                  <p className="text-sm text-gray-600 uppercase tracking-wide">Start Time</p>
                  <p className="text-sm font-mono text-gray-700 mt-1">
                    {new Date(trace.start_time).toLocaleTimeString()}
                  </p>
                </div>
              </div>
            </div>

            {/* Failure Details */}
            {trace.failure_details && (
              <div className="bg-red-50 border border-red-200 rounded-lg p-6">
                <h3 className="text-lg font-semibold text-red-900 mb-4">Failure Details</h3>
                <div className="space-y-2">
                  <div>
                    <p className="text-sm text-red-700 font-medium">Reason</p>
                    <p className="text-red-900 mt-1">{trace.failure_details.reason}</p>
                  </div>
                  {trace.failure_details.error_message && (
                    <div>
                      <p className="text-sm text-red-700 font-medium">Error Message</p>
                      <p className="text-red-900 mt-1 font-mono">{trace.failure_details.error_message}</p>
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Timeline */}
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <h3 className="text-lg font-semibold text-gray-900 mb-6">Execution Timeline</h3>
              <div className="space-y-0">
                {trace.timeline.map((event, index) => {
                  const nextEvent = trace.timeline[index + 1];
                  const duration = nextEvent
                    ? new Date(nextEvent.timestamp).getTime() - new Date(event.timestamp).getTime()
                    : 0;

                  return (
                    <div key={event.id} className="relative">
                      {/* Timeline line */}
                      {index < trace.timeline.length - 1 && (
                        <div className="absolute left-4 top-12 bottom-0 w-0.5 bg-gray-300"></div>
                      )}

                      {/* Event */}
                      <div className="pl-16 pb-6">
                        <div className="absolute left-0 top-1 w-9 h-9 bg-blue-100 border-2 border-blue-500 rounded-full flex items-center justify-center">
                          <span className="text-sm">•</span>
                        </div>

                        <div className="bg-gray-50 p-4 rounded border border-gray-200">
                          <div className="flex items-center gap-3 mb-2">
                            <p className="font-semibold text-gray-900">{event.action}</p>
                            <p className="text-xs text-gray-500">
                              {new Date(event.timestamp).toLocaleTimeString()}
                            </p>
                          </div>

                          {event.details && (
                            <p className="text-sm text-gray-700 mb-2">{event.details}</p>
                          )}

                          {duration > 0 && (
                            <p className="text-xs text-gray-500">
                              Duration: {(duration / 1000).toFixed(2)}s
                            </p>
                          )}
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>

            {/* Status Transitions */}
            {trace.status_transitions.length > 0 && (
              <div className="bg-white p-6 rounded-lg border border-gray-200">
                <h3 className="text-lg font-semibold text-gray-900 mb-4">Status Transitions</h3>
                <div className="space-y-3">
                  {trace.status_transitions.map((transition, index) => (
                    <div key={index} className="flex items-center gap-4 p-3 bg-gray-50 rounded">
                      <div className="flex items-center gap-2 flex-1">
                        <span className="font-semibold text-gray-900 capitalize">
                          {transition.from_status}
                        </span>
                        <span className="text-gray-400">→</span>
                        <span className="font-semibold text-gray-900 capitalize">
                          {transition.to_status}
                        </span>
                      </div>
                      <div>
                        <p className="text-xs text-gray-600">
                          {new Date(transition.time).toLocaleTimeString()}
                        </p>
                        {transition.reason && (
                          <p className="text-xs text-gray-500 mt-1">{transition.reason}</p>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* All Events */}
            <div className="bg-white p-6 rounded-lg border border-gray-200">
              <h3 className="text-lg font-semibold text-gray-900 mb-4">All Events ({trace.event_count})</h3>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-gray-200">
                      <th className="text-left py-2 px-4 font-semibold text-gray-700">Time</th>
                      <th className="text-left py-2 px-4 font-semibold text-gray-700">Action</th>
                      <th className="text-left py-2 px-4 font-semibold text-gray-700">Status</th>
                      <th className="text-left py-2 px-4 font-semibold text-gray-700">Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {trace.events.map((event) => (
                      <tr key={event.id} className="border-b border-gray-200 hover:bg-gray-50">
                        <td className="py-2 px-4 text-gray-600">
                          {new Date(event.timestamp).toLocaleTimeString()}
                        </td>
                        <td className="py-2 px-4 font-medium text-gray-900">{event.action}</td>
                        <td className="py-2 px-4">
                          <span className="px-2 py-1 rounded text-xs font-semibold bg-green-100 text-green-800">
                            {event.status}
                          </span>
                        </td>
                        <td className="py-2 px-4 text-gray-600 truncate">{event.details}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}

        {!trace && !loading && !error && (
          <div className="bg-white p-12 rounded-lg border border-gray-200 text-center">
            <p className="text-gray-500 text-lg">Enter a Correlation ID to explore execution traces</p>
          </div>
        )}
      </div>
    </div>
  );
};

export default TraceExplorerPage;

import React, { useEffect, useState } from 'react';
import { getRecoveryEvents } from '@/lib/api';
import { RecoveryEvent } from '@/types/operations';

const RecoveryDashboard: React.FC = () => {
  const [events, setEvents] = useState<RecoveryEvent[]>([]);
  const [filteredEvents, setFilteredEvents] = useState<RecoveryEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<'all' | 'worker_failure' | 'queue_recovery' | 'execution_cancelled' | 'execution_paused' | 'execution_resumed'>('all');
  const [selectedEvent, setSelectedEvent] = useState<RecoveryEvent | null>(null);
  const [timeRange, setTimeRange] = useState<'all' | 'today' | 'week'>('all');

  // Fetch recovery events
  useEffect(() => {
    const fetchEvents = async () => {
      try {
        const data = await getRecoveryEvents();
        setEvents(data);
      } catch (err) {
        console.error('Failed to fetch recovery events:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchEvents();
    const interval = setInterval(fetchEvents, 10000);
    return () => clearInterval(interval);
  }, []);

  // Filter events
  useEffect(() => {
    let filtered = events;

    // Filter by type
    if (filter !== 'all') {
      filtered = filtered.filter(e => e.type === filter);
    }

    // Filter by search (correlation ID or execution ID)
    if (search) {
      filtered = filtered.filter(e =>
        e.correlation_id?.toLowerCase().includes(search.toLowerCase()) ||
        e.execution_id?.toLowerCase().includes(search.toLowerCase())
      );
    }

    // Filter by time range
    const now = new Date();
    if (timeRange === 'today') {
      const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      filtered = filtered.filter(e => new Date(e.timestamp) >= today);
    } else if (timeRange === 'week') {
      const weekAgo = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
      filtered = filtered.filter(e => new Date(e.timestamp) >= weekAgo);
    }

    setFilteredEvents(filtered);
  }, [events, filter, search, timeRange]);

  // Get event statistics
  const stats = {
    total: events.length,
    worker_failures: events.filter(e => e.type === 'worker_failure').length,
    queue_recoveries: events.filter(e => e.type === 'queue_recovery').length,
    cancellations: events.filter(e => e.type === 'execution_cancelled').length,
    pauses: events.filter(e => e.type === 'execution_paused').length,
  };

  // Get event icon and color
  const getEventIcon = (type: string) => {
    switch (type) {
      case 'worker_failure':
        return '🔴';
      case 'queue_recovery':
        return '🔄';
      case 'execution_cancelled':
        return '❌';
      case 'execution_paused':
        return '⏸️';
      case 'execution_resumed':
        return '▶️';
      default:
        return '⚪';
    }
  };

  const getEventColor = (type: string) => {
    switch (type) {
      case 'worker_failure':
        return 'bg-red-50 border-red-200';
      case 'queue_recovery':
        return 'bg-blue-50 border-blue-200';
      case 'execution_cancelled':
        return 'bg-gray-50 border-gray-200';
      case 'execution_paused':
        return 'bg-orange-50 border-orange-200';
      case 'execution_resumed':
        return 'bg-green-50 border-green-200';
      default:
        return 'bg-gray-50 border-gray-200';
    }
  };

  const getTypeLabel = (type: string) => {
    switch (type) {
      case 'worker_failure':
        return 'Worker Failure';
      case 'queue_recovery':
        return 'Queue Recovery';
      case 'execution_cancelled':
        return 'Execution Cancelled';
      case 'execution_paused':
        return 'Execution Paused';
      case 'execution_resumed':
        return 'Execution Resumed';
      default:
        return type;
    }
  };

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-50 p-8">
        <div className="space-y-4">
          <div className="h-12 bg-gray-200 rounded animate-pulse w-32"></div>
          <div className="grid grid-cols-5 gap-4">
            {[...Array(5)].map((_, i) => (
              <div key={i} className="h-24 bg-gray-200 rounded animate-pulse"></div>
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
          <h1 className="text-3xl font-bold text-gray-900">Recovery Dashboard</h1>
          <p className="text-sm text-gray-500 mt-1">Monitor execution recovery and resilience events</p>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-7xl mx-auto px-8 py-8">
        {/* Statistics Cards */}
        <div className="grid grid-cols-5 gap-4 mb-8">
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Total Events</p>
            <p className="text-3xl font-bold text-gray-900 mt-2">{stats.total}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Worker Failures</p>
            <p className="text-3xl font-bold text-red-600 mt-2">{stats.worker_failures}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Queue Recoveries</p>
            <p className="text-3xl font-bold text-blue-600 mt-2">{stats.queue_recoveries}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Cancellations</p>
            <p className="text-3xl font-bold text-gray-600 mt-2">{stats.cancellations}</p>
          </div>
          <div className="bg-white p-6 rounded-lg border border-gray-200">
            <p className="text-sm font-medium text-gray-600 uppercase">Paused</p>
            <p className="text-3xl font-bold text-orange-600 mt-2">{stats.pauses}</p>
          </div>
        </div>

        {/* Filters */}
        <div className="bg-white p-6 rounded-lg border border-gray-200 mb-8">
          <div className="grid grid-cols-4 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">Event Type</label>
              <select
                value={filter}
                onChange={(e) => setFilter(e.target.value as any)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="all">All Events</option>
                <option value="worker_failure">Worker Failure</option>
                <option value="queue_recovery">Queue Recovery</option>
                <option value="execution_cancelled">Execution Cancelled</option>
                <option value="execution_paused">Execution Paused</option>
                <option value="execution_resumed">Execution Resumed</option>
              </select>
            </div>

            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">Time Range</label>
              <select
                value={timeRange}
                onChange={(e) => setTimeRange(e.target.value as any)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="all">All Time</option>
                <option value="today">Today</option>
                <option value="week">Last 7 Days</option>
              </select>
            </div>

            <div className="col-span-2">
              <label className="block text-sm font-medium text-gray-700 mb-2">Search</label>
              <input
                type="text"
                placeholder="Search by Correlation ID or Execution ID..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
        </div>

        {/* Timeline */}
        {filteredEvents.length === 0 ? (
          <div className="bg-white p-12 rounded-lg border border-gray-200 text-center">
            <p className="text-gray-500 text-lg">No recovery events {search ? 'matching your search' : 'found'} 🎉</p>
          </div>
        ) : (
          <div className="space-y-0">
            {filteredEvents.map((event, index) => (
              <div key={event.id}>
                {/* Timeline line */}
                {index < filteredEvents.length - 1 && (
                  <div className="absolute left-6 top-20 bottom-0 w-0.5 bg-gray-300 ml-4"></div>
                )}

                {/* Event */}
                <div
                  className={`p-6 rounded-lg border cursor-pointer transition-all hover:shadow-md mb-4 ${getEventColor(event.type)}`}
                  onClick={() => setSelectedEvent(event)}
                >
                  <div className="flex items-start gap-4">
                    <div className="text-3xl mt-1">{getEventIcon(event.type)}</div>

                    <div className="flex-1">
                      <div className="flex items-center gap-3 mb-2">
                        <h3 className="font-semibold text-gray-900">{getTypeLabel(event.type)}</h3>
                        <span className="text-xs text-gray-500">{new Date(event.timestamp).toLocaleString()}</span>
                      </div>

                      <p className="text-sm text-gray-700 mb-2">{event.details}</p>

                      <div className="flex gap-6 text-xs text-gray-600">
                        {event.execution_id && (
                          <div>
                            <span className="font-medium">Execution:</span> {event.execution_id}
                          </div>
                        )}
                        {event.correlation_id && (
                          <div>
                            <span className="font-medium">Correlation:</span> {event.correlation_id}
                          </div>
                        )}
                        {event.worker_id && (
                          <div>
                            <span className="font-medium">Worker:</span> {event.worker_id}
                          </div>
                        )}
                      </div>
                    </div>

                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setSelectedEvent(event);
                      }}
                      className="px-3 py-1 text-sm bg-blue-600 text-white rounded hover:bg-blue-700"
                    >
                      Details
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Detail Drawer */}
      {selectedEvent && (
        <div className="fixed inset-0 bg-black bg-opacity-50 z-50 flex items-center justify-center">
          <div className="bg-white rounded-lg shadow-xl max-w-2xl w-full mx-4 max-h-[90vh] overflow-y-auto">
            <div className="p-6 border-b border-gray-200 flex items-center justify-between">
              <h2 className="text-2xl font-bold text-gray-900">{getTypeLabel(selectedEvent.type)}</h2>
              <button
                onClick={() => setSelectedEvent(null)}
                className="text-gray-500 hover:text-gray-700 text-2xl leading-none"
              >
                ✕
              </button>
            </div>

            <div className="p-6 space-y-4">
              <div>
                <p className="text-sm font-medium text-gray-600 uppercase">Event ID</p>
                <p className="text-gray-900 font-mono mt-1">{selectedEvent.id}</p>
              </div>

              <div>
                <p className="text-sm font-medium text-gray-600 uppercase">Details</p>
                <p className="text-gray-900 mt-1">{selectedEvent.details}</p>
              </div>

              {selectedEvent.execution_id && (
                <div>
                  <p className="text-sm font-medium text-gray-600 uppercase">Execution ID</p>
                  <p className="text-gray-900 font-mono mt-1">{selectedEvent.execution_id}</p>
                </div>
              )}

              {selectedEvent.correlation_id && (
                <div>
                  <p className="text-sm font-medium text-gray-600 uppercase">Correlation ID</p>
                  <p className="text-gray-900 font-mono mt-1">{selectedEvent.correlation_id}</p>
                </div>
              )}

              {selectedEvent.worker_id && (
                <div>
                  <p className="text-sm font-medium text-gray-600 uppercase">Worker ID</p>
                  <p className="text-gray-900 font-mono mt-1">{selectedEvent.worker_id}</p>
                </div>
              )}

              <div>
                <p className="text-sm font-medium text-gray-600 uppercase">Timestamp</p>
                <p className="text-gray-900 mt-1">{new Date(selectedEvent.timestamp).toLocaleString()}</p>
              </div>

              {selectedEvent.correlation_id && (
                <div className="pt-4">
                  <a
                    href={`/operations/trace?correlation_id=${selectedEvent.correlation_id}`}
                    className="text-blue-600 hover:text-blue-700 font-medium"
                  >
                    → View Full Execution Trace
                  </a>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default RecoveryDashboard;

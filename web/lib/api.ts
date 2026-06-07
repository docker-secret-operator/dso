import {
  Dashboard,
  Alert,
  RecoveryEvent,
  DLQItem,
  TraceExplorer,
  MetricsSnapshot,
} from '@/types/operations';

const API_BASE = '/api/operations';

// Dashboard API
export async function getDashboard(): Promise<Dashboard> {
  const response = await fetch(`${API_BASE}/dashboard`);
  if (!response.ok) throw new Error('Failed to fetch dashboard');
  return response.json();
}

// Alerts API
export async function getAlerts(): Promise<Alert[]> {
  const response = await fetch(`${API_BASE}/alerts`);
  if (!response.ok) throw new Error('Failed to fetch alerts');
  const data = await response.json();
  return data.alerts || [];
}

// Recovery Events API
export async function getRecoveryEvents(): Promise<RecoveryEvent[]> {
  const response = await fetch(`${API_BASE}/recovery-events`);
  if (!response.ok) throw new Error('Failed to fetch recovery events');
  const data = await response.json();
  return data.events || [];
}

// Metrics History API
export async function getMetricsHistory(): Promise<MetricsSnapshot[]> {
  const response = await fetch(`${API_BASE}/metrics-history`);
  if (!response.ok) throw new Error('Failed to fetch metrics history');
  const data = await response.json();
  return data.snapshots || [];
}

// DLQ Items API
export async function getDLQItems(): Promise<DLQItem[]> {
  const response = await fetch(`${API_BASE}/dlq/items`);
  if (!response.ok) throw new Error('Failed to fetch DLQ items');
  const data = await response.json();
  return data.items || [];
}

// DLQ Stats API
export async function getDLQStats() {
  const response = await fetch(`${API_BASE}/dlq/stats`);
  if (!response.ok) throw new Error('Failed to fetch DLQ stats');
  return response.json();
}

// Export DLQ
export async function exportDLQ(): Promise<void> {
  const response = await fetch(`${API_BASE}/dlq/export`);
  if (!response.ok) throw new Error('Failed to export DLQ');

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `dlq-export-${Date.now()}.json`;
  a.click();
  URL.revokeObjectURL(url);
}

// Trace Explorer API
export async function getTrace(correlationId: string): Promise<TraceExplorer> {
  const response = await fetch(`${API_BASE}/trace/${correlationId}`);
  if (!response.ok) throw new Error('Failed to fetch trace');
  return response.json();
}

// Search Trace
export async function searchTrace(correlationId: string): Promise<TraceExplorer> {
  const response = await fetch(`${API_BASE}/trace/search?correlation_id=${encodeURIComponent(correlationId)}`);
  if (!response.ok) throw new Error('Failed to search trace');
  return response.json();
}

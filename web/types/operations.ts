// Operations API Types

export interface OverviewKPIs {
  success_rate: number;
  failure_rate: number;
  avg_execution_time: string;
  throughput_per_sec: number;
  worker_utilization: number;
  total_executed: number;
  total_succeeded: number;
  total_failed: number;
}

export interface QueueHealth {
  depth: number;
  oldest_item_age: string;
  incoming_rate_per_sec: number;
  completion_rate_per_sec: number;
  health_score: number; // 0-100
  status: 'healthy' | 'warning' | 'critical';
  avg_wait_time: string;
}

export interface WorkerHealthDetail {
  id: string;
  state: string;
  healthy: boolean;
  capacity: number;
  running: number;
  utilization: number;
  completed_count: number;
  failed_count: number;
  last_heartbeat: string;
}

export interface WorkerHealth {
  total_workers: number;
  healthy_workers: number;
  unhealthy_workers: number;
  avg_capacity: number;
  avg_utilization: number;
  health_score: number;
  status: 'healthy' | 'warning' | 'critical';
  workers: WorkerHealthDetail[];
}

export interface ExecutionStatusDist {
  queued: number;
  running: number;
  completed: number;
  failed: number;
  cancelled: number;
  paused: number;
  timed_out: number;
  [key: string]: number;
}

export interface RecoveryStats {
  worker_failures: number;
  auto_recoveries: number;
  recovery_success_rate: number;
  last_recovery_time?: string;
  cancelled_count: number;
  paused_count: number;
}

export interface DLQStats {
  total_items: number;
  growth_rate_per_hour: number;
  oldest_item_age: string;
  failure_reasons: Record<string, number>;
  status: 'healthy' | 'warning' | 'critical';
}

export interface FailureEvent {
  id: string;
  execution_id: string;
  correlation_id: string;
  reason: string;
  timestamp: string;
  worker_id?: string;
}

export interface SystemHealth {
  overall_score: number;
  status: 'healthy' | 'warning' | 'critical';
  alert_count: number;
  critical_count: number;
}

export interface Dashboard {
  timestamp: string;
  overview_kpis: OverviewKPIs;
  queue_health: QueueHealth;
  worker_health: WorkerHealth;
  execution_status: ExecutionStatusDist;
  recovery_stats: RecoveryStats;
  dlq_stats: DLQStats;
  recent_failures: FailureEvent[];
  system_health: SystemHealth;
}

export interface Alert {
  id: string;
  type: string;
  severity: 'info' | 'warning' | 'critical';
  message: string;
  value: number;
  threshold: number;
  timestamp: string;
  dismissed?: boolean;
}

export interface RecoveryEvent {
  id: string;
  type: string;
  execution_id?: string;
  correlation_id?: string;
  worker_id?: string;
  details: string;
  timestamp: string;
}

export interface DLQItem {
  id: string;
  execution_id: string;
  correlation_id: string;
  reason: string;
  error_message: string;
  retry_count: number;
  max_retries: number;
  enqueued_at: string;
  age: string;
  retryable: boolean;
}

export interface TraceEvent {
  id: string;
  correlation_id: string;
  execution_id: string;
  action: string;
  status: string;
  details: string;
  timestamp: string;
  duration_ms?: number;
}

export interface StatusTransition {
  from_status: string;
  to_status: string;
  time: string;
  reason: string;
}

export interface TraceExplorer {
  correlation_id: string;
  execution_id: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'paused';
  start_time: string;
  end_time?: string;
  duration: string;
  event_count: number;
  events: TraceEvent[];
  timeline: TraceEvent[];
  status_transitions: StatusTransition[];
  failure_details?: {
    reason: string;
    error_message: string;
    timestamp: string;
  };
}

export interface MetricsSnapshot {
  timestamp: string;
  success_rate: number;
  failure_rate: number;
  throughput_per_sec: number;
  queue_depth: number;
  worker_utilization: number;
  dlq_count: number;
}

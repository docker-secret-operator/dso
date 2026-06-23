/**
 * Comprehensive TypeScript interfaces for all DSO Backend APIs
 * Keep aligned with Go struct definitions
 */

// ============================================================================
// Error Types
// ============================================================================

export class ApiError extends Error {
  constructor(
    public message: string,
    public status?: number,
    public details?: Record<string, unknown>
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export class UnauthorizedError extends ApiError {
  constructor(message: string = 'Unauthorized') {
    super(message, 401)
    this.name = 'UnauthorizedError'
  }
}

export class ForbiddenError extends ApiError {
  constructor(message: string = 'Forbidden') {
    super(message, 403)
    this.name = 'ForbiddenError'
  }
}

export class NotFoundError extends ApiError {
  constructor(message: string = 'Not Found') {
    super(message, 404)
    this.name = 'NotFoundError'
  }
}

export class ConflictError extends ApiError {
  constructor(message: string = 'Conflict') {
    super(message, 409)
    this.name = 'ConflictError'
  }
}

export class ValidationError extends ApiError {
  constructor(message: string = 'Validation Error', public validationErrors?: Record<string, string>) {
    super(message, 400)
    this.name = 'ValidationError'
  }
}

// ============================================================================
// Auth Types
// ============================================================================

export interface LoginRequest {
  username: string
  password: string
}

export interface UserInfo {
  id: string
  username: string
  display_name: string
  role: string
  must_change_password: boolean
  password_expires_at?: string
}

export interface SessionInfo {
  id: string
  created_at: string
  expires_at: string
  ip_address: string
}

export interface LoginResponse {
  token: string
  expires_at: string
  user: UserInfo
  session: SessionInfo
}

export interface LogoutResponse {
  success: boolean
  message: string
}

export interface ChangePasswordRequest {
  current_password: string
  new_password: string
}

export interface RefreshResponse {
  expires_at: string
}

// ============================================================================
// User Management Types
// ============================================================================

export interface User {
  id: string
  username: string
  display_name: string
  role: string
  disabled: boolean
  locked: boolean
  locked_until?: string
  must_change_password: boolean
  created_at: string
  updated_at: string
}

export interface CreateUserRequest {
  username: string
  password: string
  display_name?: string
  role: string
}

export interface UpdateUserRequest {
  display_name?: string
  role?: string
  disabled?: boolean
  unlock?: boolean
  force_password_reset?: boolean
}

export interface ListUsersResponse {
  users: User[]
  count: number
  page: number
}

export interface Session {
  id: string
  user_id: string
  ip_address: string
  user_agent: string
  created_at: string
  expires_at: string
  last_activity: string
  is_current?: boolean
  username?: string
}

export interface ListSessionsResponse {
  sessions: Session[]
  count: number
}

// ============================================================================
// System/Health Types
// ============================================================================

export interface Check {
  name: string
  status: 'ok' | 'warning' | 'error'
  message: string
  timestamp: string
}

export interface PersistenceInfo {
  enabled: boolean
  driver: string
  status: string
  migration_version: string
  database_size: number
  wal_mode: boolean
  last_check_time: string
}

export interface HealthResponse {
  status: 'up' | 'down'
  timestamp: string
  uptime: number
  persistence: PersistenceInfo
  checks: Check[]
  goroutines?: number
  memory_mb?: number
  memory_sys_mb?: number
  num_gc?: number
  version?: string
}

export interface ReadyResponse {
  ready: boolean
}

export interface StorageResponse {
  driver: string
  status: string
  wal_mode: boolean
  database_size: number
}

// ============================================================================
// Audit Explorer Types
// ============================================================================

export interface AuditEvent {
  id: string
  correlation_id: string
  execution_id: string
  action: string
  actor: string
  actor_id: string
  actor_email: string
  resource: string
  resource_id: string
  resource_type: string
  status: string
  severity: 'info' | 'warning' | 'error' | 'critical'
  details: string
  ip_address: string
  timestamp: string
}

export interface AuditExplorerResponse {
  total: number
  count: number
  offset: number
  limit: number
  events: AuditEvent[]
  timestamp: string
}

export interface CorrelationChainResponse {
  correlation_id: string
  count: number
  events: AuditEvent[]
  timestamp: string
}

export interface ActorTimelineResponse {
  actor_id: string
  actor_name: string
  period: string
  count: number
  events: AuditEvent[]
  timestamp: string
}

export interface AuditFilters {
  correlation_id?: string
  execution_id?: string
  action?: string
  actor?: string
  actor_id?: string
  resource?: string
  resource_id?: string
  resource_type?: string
  start_time?: string
  end_time?: string
  limit?: number
  offset?: number
}

// ============================================================================
// Discovery Types
// ============================================================================

export interface NetworkInfo {
  ip: string
  gateway: string
  networks: string[]
}

export interface RestartPolicyInfo {
  name: string
  maximum_retry_count?: number
}

export interface DSOAwarenessInfo {
  status: 'managed' | 'unmanaged' | 'partial'
  managed_secrets: string[]
  config_refs: string[]
  missing_mappings: string[]
}

export interface ContainerMetadata {
  container_id: string
  container_name: string
  image: string
  status: string
  networks: NetworkInfo
  env_vars: Record<string, string>
  dso_awareness: DSOAwarenessInfo
  labels: Record<string, string>
  restart_policy: RestartPolicyInfo
}

export interface DiscoveryResponse {
  containers: ContainerMetadata[]
  total_count: number
  managed_count: number
  unmanaged_count: number
  partial_count: number
  timestamp: string
}

export interface SecretMappingSuggestion {
  env_var_name: string
  confidence: 'high' | 'medium' | 'low'
  reason: string
  suggested_secret_name: string
  is_configured: boolean
}

export interface MappingResponse {
  suggestions: SecretMappingSuggestion[]
  count: number
  timestamp: string
}

export interface RefreshResponse {
  status: string
  message: string
}

export interface DiscoveryMetrics {
  cache_hits: number
  cache_misses: number
  refresh_count: number
  avg_latency_ms: number
  cache_age_seconds: number
}

// ============================================================================
// Execution Types
// ============================================================================

export interface ExecutionResponse {
  id: string
  draft_id: string
  approval_id: string
  status: string
  created_at: string
  expires_at: string
  readiness_score: number
  timestamp: string
}

export interface StepResponse {
  sequence: number
  name: string
  description: string
  action: string
  estimated_time_seconds: number
  risk_level: string
  rollback_available: boolean
}

export interface ExecutionPlanResponse {
  plan_id: string
  status: string
  total_steps: number
  estimated_duration_seconds: number
  risk_score: number
  affected_resources: string[]
  rollback_available: boolean
  steps: StepResponse[]
  timestamp: string
}

export interface ValidationResponse {
  ready: boolean
  score: number
  approval_valid: boolean
  governance_valid: boolean
  version_valid: boolean
  safety_valid: boolean
  messages: string[]
  timestamp: string
}

export interface JourneyStep {
  step: string
  action: string
  status: string
  actor: string
  actor_id: string
  correlation_id: string
  details: string
  timestamp: string
}

export interface ExecutionJourneyResponse {
  execution_id: string
  correlation_id: string
  total_steps: number
  duration_ms: number
  steps: JourneyStep[]
  timestamp: string
}

export interface ListExecutionsResponse {
  executions: ExecutionResponse[]
  count: number
  limit: number
  offset: number
  timestamp: string
}

export interface CreateExecutionRequest {
  draft_id: string
  approval_id: string
}

// ============================================================================
// Operations Dashboard Types
// ============================================================================

export interface OverviewKPIs {
  success_rate: number
  failure_rate: number
  avg_execution_time_seconds: number
  throughput_per_second: number
  worker_utilization: number
  totals: {
    executed: number
    succeeded: number
    failed: number
  }
}

export interface QueueHealth {
  depth: number
  oldest_item_age_seconds: number
  incoming_rate: number
  completion_rate: number
  health_score: number
  status: 'healthy' | 'warning' | 'critical'
  avg_wait_time_seconds: number
}

export interface WorkerHealthDetail {
  id: string
  state: string
  healthy: boolean
  capacity: number
  running: number
  utilization: number
  completed_count: number
  failed_count: number
  last_heartbeat: string
}

export interface WorkerHealth {
  total_workers: number
  healthy_workers: number
  unhealthy_workers: number
  avg_capacity: number
  avg_utilization: number
  health_score: number
  status: 'healthy' | 'warning' | 'critical'
  workers: WorkerHealthDetail[]
}

export interface ExecutionStatusDist {
  queued: number
  running: number
  completed: number
  failed: number
  cancelled: number
  paused: number
  timed_out: number
}

export interface RecoveryStats {
  worker_failures: number
  auto_recoveries: number
  recovery_success_rate: number
  last_recovery_time: string
  cancelled: number
  paused: number
}

export interface DLQStats {
  total_items: number
  growth_rate_per_hour: number
  oldest_item_age_seconds: number
  failure_reasons: Record<string, number>
  status: 'healthy' | 'warning' | 'critical'
}

export interface FailureEvent {
  id: string
  execution_id: string
  correlation_id: string
  reason: string
  timestamp: string
  worker_id: string
}

export interface SystemHealth {
  overall_score: number
  status: 'healthy' | 'warning' | 'critical'
  alert_count: number
  critical_count: number
}

export interface OperationsDashboardResponse {
  timestamp: string
  overview_kpis: OverviewKPIs
  queue_health: QueueHealth
  worker_health: WorkerHealth
  execution_status: ExecutionStatusDist
  recovery_stats: RecoveryStats
  dlq_stats: DLQStats
  recent_failures: FailureEvent[]
  system_health: SystemHealth
}

export interface AlertResponse {
  id: string
  type: string
  severity: string
  message: string
  value: number
  threshold: number
  timestamp: string
  dismissed: boolean
}

export interface ListAlertsResponse {
  alerts: AlertResponse[]
  count: number
  timestamp: string
}

export interface RecoveryEventResponse {
  id: string
  type: string
  execution_id: string
  correlation_id: string
  worker_id: string
  details: Record<string, unknown>
  timestamp: string
}

export interface ListRecoveryEventsResponse {
  events: RecoveryEventResponse[]
  count: number
  timestamp: string
}

// ============================================================================
// Operations Console API Types (Phase 5C)
// ============================================================================

export interface OperationsDashboard {
  timestamp: string
  overview_kpis: OverviewKPIs
  queue_health: QueueHealth
  worker_health: WorkerHealth
  execution_status: ExecutionStatusDist
  recovery_stats: RecoveryStats
  dlq_stats: DLQStats
  recent_failures: FailureEvent[]
  system_health: SystemHealth
}

export interface Execution {
  id: string
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'paused' | 'timed_out'
  created_at: string
  started_at?: string
  completed_at?: string
  duration_ms?: number
  correlation_id: string
}

export interface ExecutionList {
  executions: Execution[]
  count: number
  limit?: number
  offset?: number
  timestamp: string
}

export interface ExecutionStep {
  sequence: number
  name: string
  description: string
  action: string
  estimated_time_seconds: number
  risk_level: 'low' | 'medium' | 'high' | 'critical'
  rollback_available: boolean
}

export interface ExecutionPlan {
  plan_id: string
  execution_id: string
  status: string
  total_steps: number
  estimated_duration_seconds: number
  risk_score: number
  affected_resources: string[]
  rollback_available: boolean
  steps: ExecutionStep[]
  timestamp: string
}

export interface ExecutionValidation {
  ready: boolean
  score: number
  approval_valid: boolean
  governance_valid: boolean
  version_valid: boolean
  safety_valid: boolean
  messages: string[]
  timestamp: string
}

export interface TraceEvent {
  sequence: number
  event_type: string
  description: string
  status: string
  timestamp: string
  correlation_id?: string
}

export interface ExecutionTrace {
  execution_id: string
  correlation_id: string
  execution: Execution
  plan?: ExecutionPlan
  events: TraceEvent[]
  timestamp: string
}

export interface JourneyEvent {
  step: string
  action: string
  status: string
  actor: string
  actor_id: string
  correlation_id: string
  details: string
  timestamp: string
}

export interface ExecutionJourney {
  execution_id: string
  correlation_id: string
  total_steps: number
  duration_ms: number
  events: JourneyEvent[]
  timestamp: string
}

export interface Alert {
  id: string
  type: string
  severity: 'info' | 'warning' | 'error' | 'critical'
  message: string
  value: number
  threshold: number
  timestamp: string
  dismissed: boolean
}

export interface RecoveryEvent {
  id: string
  type: string
  execution_id: string
  correlation_id: string
  worker_id: string
  details: Record<string, unknown>
  timestamp: string
}

export interface MetricsHistory {
  period: string
  granularity: string
  count: number
  data: DataPoint[]
  trends?: Record<string, unknown>
  forecast?: Record<string, unknown>
  anomalies?: Array<Record<string, unknown>>
  timestamp: string
}

export interface DataPoint {
  timestamp: number
  success_rate: number
  failure_rate: number
  throughput: number
  queue_depth: number
  worker_utilization: number
  avg_execution_time_ms?: number
}

export interface Worker {
  id: string
  state: string
  healthy: boolean
  capacity: number
  running: number
  utilization: number
  completed_count: number
  failed_count: number
  last_heartbeat: string
}

// ============================================================================
// Metrics Types
// ============================================================================

export interface MetricsPoint {
  ts: number
  sr: number
  fr: number
  tp: number
  qd: number
  wu: number
  ae: number
  mm: number
  gr: number
}

export interface TrendInfo {
  direction: 'improving' | 'stable' | 'degrading'
  arrow: string
  slope: number
  moving_avg: number
}

export interface TrendReport {
  success_rate: TrendInfo
  failure_rate: TrendInfo
  queue_depth: TrendInfo
  worker_utilization: TrendInfo
  memory_mb: TrendInfo
}

export interface ForecastResult {
  queue_saturation_hours: number
  worker_exhaustion_hours: number
  queue_status: 'healthy' | 'warning' | 'critical'
  worker_status: 'healthy' | 'warning' | 'critical'
}

export interface AnomalyInfo {
  field: string
  timestamp: string
  value: number
  baseline: number
  deviation: number
  message: string
}

export interface MetricsHistoryResponse {
  period: string
  granularity: string
  count: number
  data: MetricsPoint[]
  trends: TrendReport
  forecast: ForecastResult
  anomalies: AnomalyInfo[]
  timestamp: string
}

export interface OpsMetricsSnapshot {
  timestamp: string
  success_rate: number
  failure_rate: number
  throughput: number
  queue_depth: number
  worker_utilization: number
  dlq_count: number
}

// ============================================================================
// Dashboard Types
// ============================================================================

export interface DashboardOverviewResponse {
  timestamp: string
  [key: string]: unknown
}

export interface DashboardMetricsResponse {
  timestamp: string
  [key: string]: unknown
}

// ============================================================================
// Common pagination/filtering
// ============================================================================

export interface PaginationParams {
  limit?: number
  offset?: number
  page?: number
  page_size?: number
}

export interface ListResponse<T> {
  items: T[]
  count: number
  total?: number
  limit?: number
  offset?: number
  page?: number
}

// ============================================================================
// Drift Detection (P4)
// ============================================================================

export type DriftSeverity = 'critical' | 'high' | 'medium' | 'low'
export type DriftStatus = 'detected' | 'acknowledged' | 'resolved'
export type DriftType = 'version_mismatch' | 'stale_secret' | 'missing_secret' | 'rotation_lag'

export interface DriftFinding {
  id: string
  type: DriftType
  severity: DriftSeverity
  status: DriftStatus
  resource: string
  description: string
  metadata?: Record<string, unknown>
  secret_name?: string
  container?: string
  provider?: string
  expected_version?: string
  actual_version?: string
  detected_at: number   // epoch ms
  acknowledged_at?: number
  resolved_at?: number
}

export interface DriftMetrics {
  TotalFindings: number
  CriticalFindings: number
  OpenFindings: number
  Scans: number
  AverageDuration: number
  LastScan?: string
  FindingsByType: Record<DriftType, number>
  FindingsBySeverity: Record<DriftSeverity, number>
}

export interface DriftScanRecord {
  ID: string
  DetectorID: string
  FindingsCount: number
  Duration: number
  Success: boolean
  Error?: string
  CreatedAt: string
}

export interface DriftListResponse {
  findings: DriftFinding[]
  total: number
}

export interface DriftHistoryResponse {
  scans: DriftScanRecord[]
  total: number
}

# Phase 5C: Operations Console — Design Specification

**Date:** 2026-06-19  
**Phase:** 5C  
**Status:** Design  
**Context:** Built on Phase 5.75 (736+ tests) — Dashboard, Audit, Discovery production-ready

---

## Overview

Build a central operational control plane for executions, queues, workers, alerts, and recovery management. Single-page layout with reusable components following established patterns.

**Key Principle:** Keep the page thin. Reuse Dashboard/Audit/Discovery patterns. No redesign. Component-driven.

---

## Architecture

### Page Structure

```
OperationsPage (app/operations/page.tsx)
│
├── ProtectedRoute wrapper (auth required)
│
├── State Management
│   ├── React Query hooks (8 independent queries)
│   ├── No Context, no Redux
│   └── Error isolated per query
│
└── Layout (single scrollable column)
    ├── OperationsOverview (KPI style, 5 cards)
    ├── QueueHealthCard (depth, age, rates, health score)
    ├── WorkerHealthCard (count, health, utilization, expandable)
    ├── ExecutionTable (search, pagination, filters)
    ├── AlertsPanel (severity badges, timeline)
    ├── RecoveryEventsTable (failures, recoveries, timeline)
    ├── MetricsHistoryChart (throughput, queue, workers, success)
    └── ExecutionDetailsDrawer (modal with 5 collapsible sections)
```

### Data Flow

**Queries (All independent, auto-refresh 30s):**
- `['operations', 'dashboard']` → OperationsOverview
- `['operations', 'alerts']` → AlertsPanel
- `['operations', 'recovery-events']` → RecoveryEventsTable
- `['operations', 'metrics-history']` → MetricsHistoryChart
- `['executions']` → ExecutionTable
- `['execution', id]` → ExecutionDetailsDrawer
- `['execution-plan', id]` → Drawer section (lazy)
- `['execution-validation', id]` → Drawer section (lazy)

**Error Handling:**
- Dashboard failure: Show error banner, hide KPIs, don't block page
- Alerts failure: Show "Unable to load alerts", non-blocking
- Metrics failure: Show "No metrics data", lowest priority
- Executions failure: Show error, allow retry

**No Cascading Failures:** Each section handles its own state independently

---

## API Layer

**File:** `web/lib/api/operations.ts`

```typescript
// Operations dashboard summary
export async function getOperationsDashboard(): Promise<OperationsDashboard> {
  return apiClient.client.get('/api/operations/dashboard').then(r => r.data)
}

// Alerts
export async function getAlerts(): Promise<Alert[]> {
  return apiClient.client.get('/api/operations/alerts').then(r => r.data.alerts)
}

// Recovery events
export async function getRecoveryEvents(): Promise<RecoveryEvent[]> {
  return apiClient.client.get('/api/operations/recovery-events').then(r => r.data.events)
}

// Metrics history
export async function getMetricsHistory(): Promise<MetricsHistory> {
  return apiClient.client.get('/api/operations/metrics-history').then(r => r.data)
}

// Executions list
export async function getExecutions(params?: { limit?: number; offset?: number }): Promise<ExecutionList> {
  return apiClient.client.get('/api/executions', { params }).then(r => r.data)
}

// Single execution
export async function getExecution(id: string): Promise<Execution> {
  return apiClient.client.get(`/api/executions/${id}`).then(r => r.data)
}

// Execution plan (lazy load)
export async function getExecutionPlan(id: string): Promise<ExecutionPlan> {
  return apiClient.client.get(`/api/executions/${id}/plan`).then(r => r.data)
}

// Execution validation (lazy load)
export async function getExecutionValidation(id: string): Promise<ExecutionValidation> {
  return apiClient.client.get(`/api/executions/${id}/validation`).then(r => r.data)
}

// Execution trace (lazy load)
export async function getExecutionTrace(id: string): Promise<ExecutionTrace> {
  return apiClient.client.get(`/api/executions/${id}/trace`).then(r => r.data)
}

// Execution journey (lazy load)
export async function getExecutionJourney(id: string): Promise<ExecutionJourney> {
  return apiClient.client.get(`/api/executions/${id}/journey`).then(r => r.data)
}

// Create execution
export async function createExecution(request: CreateExecutionRequest): Promise<Execution> {
  return apiClient.client.post('/api/executions', request).then(r => r.data)
}
```

**File:** `web/lib/api/types.ts` (add these types)

```typescript
// Operations
export interface OperationsDashboard {
  success_rate: number
  failure_rate: number
  throughput_per_sec: number
  worker_utilization: number
  total_executions: number
  timestamp: string
}

export interface QueueHealth {
  queue_depth: number
  oldest_item_age_seconds: number
  incoming_rate: number
  completion_rate: number
  health_status: 'healthy' | 'warning' | 'critical'
}

export interface WorkerHealth {
  total_workers: number
  healthy_workers: number
  unhealthy_workers: number
  average_utilization: number
  workers: Worker[]
}

export interface Worker {
  id: string
  status: 'healthy' | 'degraded' | 'failed'
  utilization: number
  active_executions: number
}

export interface Execution {
  id: string
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'paused'
  created_at: string
  started_at?: string
  completed_at?: string
  readiness_score: number
  correlation_id: string
  error?: string
}

export interface ExecutionList {
  executions: Execution[]
  total: number
  offset: number
  limit: number
}

export interface ExecutionPlan {
  id: string
  steps: ExecutionStep[]
  estimated_duration_seconds: number
}

export interface ExecutionStep {
  id: string
  name: string
  type: string
  depends_on: string[]
  status: 'pending' | 'running' | 'completed' | 'failed'
}

export interface ExecutionValidation {
  id: string
  is_valid: boolean
  warnings: string[]
  errors: string[]
}

export interface ExecutionTrace {
  id: string
  events: TraceEvent[]
}

export interface TraceEvent {
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  context?: Record<string, any>
}

export interface ExecutionJourney {
  id: string
  events: JourneyEvent[]
  total_duration_seconds: number
}

export interface JourneyEvent {
  timestamp: string
  event_type: string
  description: string
  status: 'success' | 'failure' | 'info'
}

export interface Alert {
  id: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  threshold?: string
  current_value?: string
  timestamp: string
}

export interface RecoveryEvent {
  id: string
  type: 'worker_failure' | 'recovery' | 'pause' | 'cancellation'
  worker_id?: string
  execution_id?: string
  timestamp: string
  details: string
}

export interface MetricsHistory {
  timestamp: string
  throughput_history: DataPoint[]
  queue_depth_history: DataPoint[]
  worker_utilization_history: DataPoint[]
  success_rate_history: DataPoint[]
}

export interface DataPoint {
  timestamp: string
  value: number
}

export interface CreateExecutionRequest {
  plan_id: string
  parameters?: Record<string, any>
}
```

---

## Components

### 1. OperationsOverview.tsx
Display 5 KPI cards (success rate, failure rate, throughput/sec, worker utilization, total executions).
- Skeleton loaders while loading
- Error message if query fails
- Format: percentages, numbers with commas, throughput with 2 decimals
- Reuse Dashboard KPI card styling (emerald/slate backgrounds)

### 2. QueueHealthCard.tsx
Single card component showing:
- Queue depth (large number)
- Oldest item age (relative time or "N/A")
- Incoming rate (items/sec)
- Completion rate (items/sec)
- Health score (0-100) with color indicator
- Expandable details (optional)

Color states: healthy (emerald), warning (amber), critical (red)

### 3. WorkerHealthCard.tsx
Card with:
- Total workers count
- Healthy count and %
- Unhealthy count
- Average utilization %
- Expandable worker list (shows individual worker status, utilization, active executions)
- Worker status badges (healthy, degraded, failed)

### 4. ExecutionTable.tsx + ExecutionRow.tsx
Table with columns:
- Execution ID (linkable to drawer)
- Status badge (queued/running/completed/failed/cancelled/paused)
- Created timestamp (relative time)
- Readiness score (0-100, color coded)
- Correlation ID (copy button)

Features:
- Search by ID, correlation ID, status
- Pagination (default 20 items)
- Filters for status
- Click row to open ExecutionDetailsDrawer
- Loading skeleton rows
- Empty state when no executions

### 5. ExecutionDetailsDrawer.tsx
Modal drawer with 5 collapsible sections:
1. **General** - ID, status, created/started/completed times, error message
2. **Plan** - Steps as timeline, dependencies, estimated duration
3. **Validation** - Valid/invalid status, warnings, errors
4. **Trace** - Log events with level (debug/info/warn/error), timestamps
5. **Journey** - Timeline events, status colors, total duration

Lazy-load each section (only fetch when section expands).
Close button and ESC key support.

### 6. AlertsPanel.tsx
Panel displaying alerts in a list:
- Severity badge (info/warning/critical) with colors
- Message text
- Threshold and current value (if applicable)
- Timestamp (relative time)
- Empty state when no alerts
- Auto-refresh with other queries

### 7. RecoveryEventsTable.tsx
Timeline-style table showing:
- Event type icon (worker failure, recovery, pause, cancellation)
- Event type badge
- Details text
- Timestamp (relative time)
- Worker ID or Execution ID link
- Empty state when no events

### 8. MetricsHistoryChart.tsx
Chart component with 4 lines/areas:
- Throughput (executions/sec)
- Queue depth (items)
- Worker utilization (%)
- Success rate (%)

Use existing chart library from Dashboard (Recharts or similar).
Time range selector (1h, 6h, 24h, 7d).
Loading state with skeleton.
Error message if query fails.

### 9. EmptyState.tsx (reusable, shared with other pages)
Message and icon for:
- No executions
- No alerts
- No recovery events

---

## Page Layout

```
OperationsPage (ProtectedRoute)
│
├── Page Header
│   ├── Title: "Operations Console"
│   └── Last Refresh: "Xs ago"
│
├── OperationsOverview (5 KPI cards, grid)
│
├── Divider
│
├── Health Section (2 cards in grid)
│   ├── QueueHealthCard
│   └── WorkerHealthCard
│
├── Divider
│
├── ExecutionTable with search/filter/pagination
│
├── Divider
│
├── 2-column grid
│   ├── AlertsPanel (left, 1/3)
│   └── RecoveryEventsTable (right, 2/3) OR full width alternating
│
├── Divider
│
├── MetricsHistoryChart (full width)
│
└── ExecutionDetailsDrawer (modal overlay)
```

**Single scrollable page. No tabs. Responsive grid layout.**

---

## Auto-Refresh Strategy

**React Query Configuration (all queries):**
```typescript
refetchInterval: 30000 // 30 seconds
staleTime: 25000      // Stale after 25 seconds
retry: 2              // Retry failed requests
refetchOnWindowFocus: false
```

**No manual intervals. No timers. React Query handles all refresh.**

---

## Error Handling

**Principle:** One section's failure doesn't break others.

| Component | Failure | Behavior |
|-----------|---------|----------|
| OperationsOverview | Dashboard API fails | Show error banner, hide cards |
| QueueHealthCard | Health API fails | Show "Unable to load" message |
| WorkerHealthCard | Health API fails | Show "Unable to load" message |
| ExecutionTable | Executions API fails | Show error, allow retry |
| AlertsPanel | Alerts API fails | Show "No alerts available" |
| RecoveryEventsTable | Recovery API fails | Show empty state |
| MetricsHistoryChart | Metrics API fails | Show "No data" message |

**Global:** No blank screens. No redirect on error. Always show something.

---

## File Structure

```
web/
├── app/
│   └── operations/
│       └── page.tsx (main page, ProtectedRoute wrapper)
│
├── components/
│   └── operations/
│       ├── OperationsOverview.tsx
│       ├── QueueHealthCard.tsx
│       ├── WorkerHealthCard.tsx
│       ├── ExecutionTable.tsx
│       ├── ExecutionRow.tsx
│       ├── ExecutionDetailsDrawer.tsx
│       ├── AlertsPanel.tsx
│       ├── RecoveryEventsTable.tsx
│       ├── MetricsHistoryChart.tsx
│       └── EmptyState.tsx
│
└── lib/
    ├── api/
    │   ├── operations.ts (new)
    │   └── types.ts (updated with new types)
    │
    └── hooks/
        └── useOperations.ts (optional, if custom hooks needed)
```

---

## Query Keys

```typescript
['operations', 'dashboard']      // OperationsOverview
['operations', 'alerts']         // AlertsPanel
['operations', 'recovery-events'] // RecoveryEventsTable
['operations', 'metrics-history'] // MetricsHistoryChart
['executions']                   // ExecutionTable
['execution', id]                // ExecutionDetailsDrawer (general)
['execution-plan', id]           // Drawer section (lazy)
['execution-validation', id]     // Drawer section (lazy)
['execution-trace', id]          // Drawer section (lazy)
['execution-journey', id]        // Drawer section (lazy)
```

**All queries use consistent React Query config with 30s auto-refresh, no manual intervals.**

---

## Testing Strategy

Following Phase 5.75 patterns (736+ tests already built).

### Unit Tests
- API functions (operations.ts)
- Formatting utilities (time, numbers)
- Type validation

### Component Tests
- Each component renders correctly
- Query loading/error states
- User interactions (click, search, filter, expand)
- Data display accuracy

### Integration Tests
- Full page load with all queries
- Error scenarios (one fails, others succeed)
- Search/filter interactions across table
- Drawer open/close lifecycle

### E2E Tests
- Complete user journey (login → operations → view execution → logout)
- Search and filter executions
- Open execution details

### Accessibility Tests
- ARIA labels on buttons
- Keyboard navigation
- Table semantics
- Modal focus trap

### Performance Tests
- No duplicate queries
- Proper query deduplication
- Efficient re-renders
- Memory cleanup on unmount

---

## Success Criteria

✅ Page loads without auth → redirect to login  
✅ All 4 operations queries execute in parallel  
✅ All 5 execution queries available for drawer  
✅ Search/filter works on execution table  
✅ One API failure doesn't break other sections  
✅ 30s auto-refresh all data  
✅ No blank screens on error  
✅ Lazy-load drawer sections  
✅ Responsive on mobile/tablet/desktop  
✅ All tests passing (unit, component, integration, E2E, accessibility, performance)  
✅ No regressions in existing pages  

---

## Architecture Decisions

| Decision | Rationale |
|----------|-----------|
| No Context, no Redux | Keep it simple. React Query for state. |
| One-page layout | Comprehensive view at a glance. |
| Independent queries | Failures don't cascade. Each section self-contained. |
| 30s auto-refresh | Real-time enough without overwhelming server. |
| Lazy-load drawer | Reduce initial load, load on demand. |
| Reuse patterns | Dashboard, Audit, Discovery patterns proven. |
| Responsive grid | Works on all screen sizes. |
| Query key hierarchy | Follows established ['domain', 'resource', id?] pattern. |

---

## Validation Checklist

Before marking Phase 5C complete:

- [ ] Operations page loads with protected route
- [ ] All 5 KPI cards display correctly
- [ ] Queue health and worker health render
- [ ] Execution table shows data with pagination
- [ ] Search and filters work
- [ ] Click execution opens details drawer
- [ ] All 5 drawer sections load (general, plan, validation, trace, journey)
- [ ] All sections expand/collapse
- [ ] Alerts panel displays
- [ ] Recovery events timeline shows
- [ ] Metrics history chart renders
- [ ] Auto-refresh fires every 30 seconds (React Query)
- [ ] One API failure doesn't break page
- [ ] Error messages are user-friendly
- [ ] No blank screens on error
- [ ] Responsive design works (mobile/tablet/desktop)
- [ ] Accessibility audit passes (WCAG AA)
- [ ] All tests passing (736 existing + new tests)
- [ ] No TypeScript errors (strict mode)
- [ ] No console warnings/errors

---

## Future Considerations (Phase 5D+)

- Execution creation form (currently GET only)
- Worker management (restart, drain, etc.)
- Alert suppression/acknowledgment
- Real-time WebSocket updates (vs 30s polling)
- Custom time range selection for metrics
- Export operations data (CSV/JSON)
- Bulk execution actions
- Integration with external monitoring (Prometheus, Datadog)

---

**End of Design Specification**


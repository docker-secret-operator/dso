# Phase 5C: Operations Console — Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development for task-by-task execution with reviews.

**Goal:** Build Operations Console (operations/page.tsx + 9 components) with full test coverage. Reuse Dashboard/Audit/Discovery patterns. 

**Architecture:** Single-page, component-driven, 8 independent React Query hooks, no Redux/Context.

**Tech Stack:** Next.js, React Query, React Testing Library, Playwright, Vitest

---

## Phase Breakdown

### Phase A: Foundations (2 tasks)
1. API layer (operations.ts, type definitions)
2. Main page skeleton (ProtectedRoute, layout structure)

### Phase B: Components Core (4 tasks)
3. OperationsOverview (KPI cards)
4. QueueHealthCard + WorkerHealthCard
5. ExecutionTable + ExecutionRow
6. ExecutionDetailsDrawer (5 collapsible sections)

### Phase C: Components Supporting (3 tasks)
7. AlertsPanel + RecoveryEventsTable
8. MetricsHistoryChart
9. EmptyState component (reusable)

### Phase D: Testing (5 tasks)
10. Unit tests (API, formatting)
11. Component tests (all 9 components)
12. Integration tests (full page workflows)
13. E2E tests (Playwright journeys)
14. Accessibility + Performance tests

### Phase E: Validation (1 task)
15. Full validation checklist + CI/CD verification

---

## Task Details

### Task 1: API Layer & Types

**Files:**
- Create: `web/lib/api/operations.ts`
- Modify: `web/lib/api/types.ts` (add new types)
- Modify: `web/lib/api/index.ts` (export operations)

**Code - operations.ts:**

```typescript
import { apiClient } from '@/lib/api-client'
import {
  OperationsDashboard,
  Alert,
  RecoveryEvent,
  MetricsHistory,
  ExecutionList,
  Execution,
  ExecutionPlan,
  ExecutionValidation,
  ExecutionTrace,
  ExecutionJourney,
  CreateExecutionRequest,
} from './types'

// Operations dashboard
export async function getOperationsDashboard(): Promise<OperationsDashboard> {
  const response = await apiClient.client.get('/api/operations/dashboard')
  return response.data
}

// Alerts
export async function getAlerts(): Promise<Alert[]> {
  const response = await apiClient.client.get('/api/operations/alerts')
  return response.data.alerts || []
}

// Recovery events
export async function getRecoveryEvents(): Promise<RecoveryEvent[]> {
  const response = await apiClient.client.get('/api/operations/recovery-events')
  return response.data.events || []
}

// Metrics history
export async function getMetricsHistory(): Promise<MetricsHistory> {
  const response = await apiClient.client.get('/api/operations/metrics-history')
  return response.data
}

// Executions list
export async function getExecutions(params?: {
  limit?: number
  offset?: number
}): Promise<ExecutionList> {
  const response = await apiClient.client.get('/api/executions', { params })
  return response.data
}

// Single execution
export async function getExecution(id: string): Promise<Execution> {
  const response = await apiClient.client.get(`/api/executions/${id}`)
  return response.data
}

// Execution plan
export async function getExecutionPlan(id: string): Promise<ExecutionPlan> {
  const response = await apiClient.client.get(`/api/executions/${id}/plan`)
  return response.data
}

// Execution validation
export async function getExecutionValidation(
  id: string
): Promise<ExecutionValidation> {
  const response = await apiClient.client.get(
    `/api/executions/${id}/validation`
  )
  return response.data
}

// Execution trace
export async function getExecutionTrace(id: string): Promise<ExecutionTrace> {
  const response = await apiClient.client.get(`/api/executions/${id}/trace`)
  return response.data
}

// Execution journey
export async function getExecutionJourney(
  id: string
): Promise<ExecutionJourney> {
  const response = await apiClient.client.get(`/api/executions/${id}/journey`)
  return response.data
}

// Create execution
export async function createExecution(
  request: CreateExecutionRequest
): Promise<Execution> {
  const response = await apiClient.client.post('/api/executions', request)
  return response.data
}
```

**Type Definitions (add to types.ts):**

```typescript
// Operations Dashboard
export interface OperationsDashboard {
  success_rate: number
  failure_rate: number
  throughput_per_sec: number
  worker_utilization: number
  total_executions: number
  timestamp: string
}

// Queue Health
export interface QueueHealth {
  queue_depth: number
  oldest_item_age_seconds: number
  incoming_rate: number
  completion_rate: number
  health_status: 'healthy' | 'warning' | 'critical'
}

// Worker Health
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

// Execution
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

// Execution Details
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

// Alerts
export interface Alert {
  id: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  threshold?: string
  current_value?: string
  timestamp: string
}

// Recovery Events
export interface RecoveryEvent {
  id: string
  type: 'worker_failure' | 'recovery' | 'pause' | 'cancellation'
  worker_id?: string
  execution_id?: string
  timestamp: string
  details: string
}

// Metrics
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

// Create Execution
export interface CreateExecutionRequest {
  plan_id: string
  parameters?: Record<string, any>
}
```

**Steps:**
1. Create web/lib/api/operations.ts with all 10 functions
2. Add all type definitions to web/lib/api/types.ts
3. Export operations from web/lib/api/index.ts
4. Verify TypeScript compilation
5. Commit: "feat: add operations API layer and type definitions"

---

### Task 2: Main Page Skeleton

**Files:**
- Create: `web/app/operations/page.tsx`

**Code:**

```typescript
'use client'

import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ProtectedRoute } from '@/components/auth/ProtectedRoute'
import * as operationsApi from '@/lib/api/operations'
import { OperationsOverview } from '@/components/operations/OperationsOverview'
import { QueueHealthCard } from '@/components/operations/QueueHealthCard'
import { WorkerHealthCard } from '@/components/operations/WorkerHealthCard'
import { ExecutionTable } from '@/components/operations/ExecutionTable'
import { AlertsPanel } from '@/components/operations/AlertsPanel'
import { RecoveryEventsTable } from '@/components/operations/RecoveryEventsTable'
import { MetricsHistoryChart } from '@/components/operations/MetricsHistoryChart'
import { ExecutionDetailsDrawer } from '@/components/operations/ExecutionDetailsDrawer'
import { Execution } from '@/lib/api/types'

const QUERY_CONFIG = {
  refetchInterval: 30000,
  staleTime: 25000,
  retry: 2,
  refetchOnWindowFocus: false,
}

export default function OperationsPage() {
  const [selectedExecution, setSelectedExecution] = useState<Execution | null>(null)
  const queryClient = useQueryClient()

  // Operations dashboard
  const { data: dashboard, isLoading: dashboardLoading, error: dashboardError } = useQuery({
    queryKey: ['operations', 'dashboard'],
    queryFn: operationsApi.getOperationsDashboard,
    ...QUERY_CONFIG,
  })

  // Alerts
  const { data: alerts = [], isLoading: alertsLoading, error: alertsError } = useQuery({
    queryKey: ['operations', 'alerts'],
    queryFn: operationsApi.getAlerts,
    ...QUERY_CONFIG,
  })

  // Recovery events
  const { data: recoveryEvents = [], isLoading: recoveryLoading, error: recoveryError } = useQuery({
    queryKey: ['operations', 'recovery-events'],
    queryFn: operationsApi.getRecoveryEvents,
    ...QUERY_CONFIG,
  })

  // Metrics history
  const { data: metricsHistory, isLoading: metricsLoading, error: metricsError } = useQuery({
    queryKey: ['operations', 'metrics-history'],
    queryFn: operationsApi.getMetricsHistory,
    ...QUERY_CONFIG,
  })

  // Executions
  const { data: executionList, isLoading: executionsLoading, error: executionsError } = useQuery({
    queryKey: ['executions'],
    queryFn: () => operationsApi.getExecutions({ limit: 20, offset: 0 }),
    ...QUERY_CONFIG,
  })

  return (
    <ProtectedRoute>
      <div className="min-h-screen bg-slate-950">
        <div className="mx-auto max-w-7xl space-y-6 px-4 py-8 sm:px-6 lg:px-8">
          {/* Header */}
          <div className="flex items-center justify-between">
            <h1 className="text-3xl font-bold text-white">Operations Console</h1>
          </div>

          {/* Operations Overview */}
          <OperationsOverview
            data={dashboard}
            isLoading={dashboardLoading}
            error={dashboardError}
          />

          {/* Health Section */}
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            <QueueHealthCard isLoading={dashboardLoading} error={dashboardError} />
            <WorkerHealthCard isLoading={dashboardLoading} error={dashboardError} />
          </div>

          {/* Execution Table */}
          <ExecutionTable
            executions={executionList?.executions || []}
            total={executionList?.total || 0}
            isLoading={executionsLoading}
            error={executionsError}
            onSelectExecution={setSelectedExecution}
          />

          {/* Alerts & Recovery */}
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
            <AlertsPanel alerts={alerts} isLoading={alertsLoading} error={alertsError} />
            <div className="lg:col-span-2">
              <RecoveryEventsTable
                events={recoveryEvents}
                isLoading={recoveryLoading}
                error={recoveryError}
              />
            </div>
          </div>

          {/* Metrics */}
          <MetricsHistoryChart data={metricsHistory} isLoading={metricsLoading} error={metricsError} />
        </div>
      </div>

      {/* Execution Details Drawer */}
      {selectedExecution && (
        <ExecutionDetailsDrawer
          execution={selectedExecution}
          isOpen={!!selectedExecution}
          onClose={() => setSelectedExecution(null)}
        />
      )}
    </ProtectedRoute>
  )
}
```

**Steps:**
1. Create web/app/operations/page.tsx
2. Set up 5 independent React Query hooks
3. Wire all components (placeholder for now)
4. Verify page loads with ProtectedRoute
5. Check for TypeScript errors
6. Commit: "feat: create operations page skeleton with query setup"

---

### Task 3: OperationsOverview Component

**File:** `web/components/operations/OperationsOverview.tsx`

Create 5 KPI cards: success rate, failure rate, throughput/sec, worker utilization, total executions.

Reuse Dashboard KPI card styling (Card component, emerald/slate backgrounds, skeleton loaders).

**Steps:**
1. Create component file
2. Implement 5 KPI cards
3. Add loading and error states
4. Verify rendering and data display
5. Commit: "feat: add OperationsOverview component"

---

### Task 4: QueueHealthCard + WorkerHealthCard

**Files:**
- Create: `web/components/operations/QueueHealthCard.tsx`
- Create: `web/components/operations/WorkerHealthCard.tsx`

**QueueHealthCard displays:**
- Queue depth
- Oldest item age
- Incoming rate
- Completion rate
- Health score with color (emerald/amber/red)

**WorkerHealthCard displays:**
- Total workers count
- Healthy workers count and %
- Unhealthy workers count
- Average utilization %
- Expandable worker list

**Steps:**
1. Create both component files
2. Implement card structure and data display
3. Add health score color coding
4. Add expandable worker list
5. Verify rendering
6. Commit: "feat: add queue and worker health cards"

---

### Task 5: ExecutionTable + ExecutionRow Components

**Files:**
- Create: `web/components/operations/ExecutionTable.tsx`
- Create: `web/components/operations/ExecutionRow.tsx`

**Table columns:**
- Execution ID
- Status badge
- Created timestamp
- Readiness score
- Correlation ID

**Features:**
- Search by ID/correlation ID/status
- Pagination (20 items per page)
- Status filter
- Click row to open drawer

**Steps:**
1. Create both component files
2. Implement table structure
3. Add search and pagination
4. Add status badges
5. Wire click handler to open drawer
6. Verify rendering and interactions
7. Commit: "feat: add ExecutionTable and ExecutionRow components"

---

### Task 6: ExecutionDetailsDrawer Component

**File:** `web/components/operations/ExecutionDetailsDrawer.tsx`

5 collapsible sections:
1. General (ID, status, timestamps, error)
2. Plan (steps timeline)
3. Validation (valid/invalid, warnings, errors)
4. Trace (log events)
5. Journey (timeline events)

Lazy-load each section.
Close button and ESC key support.

**Steps:**
1. Create component file
2. Implement drawer modal structure
3. Add 5 collapsible sections
4. Implement lazy-loading for each section
5. Add close button and ESC handler
6. Verify rendering and interactions
7. Commit: "feat: add ExecutionDetailsDrawer component"

---

### Task 7: AlertsPanel + RecoveryEventsTable Components

**Files:**
- Create: `web/components/operations/AlertsPanel.tsx`
- Create: `web/components/operations/RecoveryEventsTable.tsx`

**AlertsPanel displays:**
- Alert severity badge
- Message
- Threshold and current value
- Timestamp

**RecoveryEventsTable displays:**
- Event type icon/badge
- Details
- Timestamp
- Worker/Execution ID link

**Steps:**
1. Create both component files
2. Implement alert list display
3. Implement recovery events timeline
4. Add severity color coding
5. Add event type icons
6. Verify rendering
7. Commit: "feat: add AlertsPanel and RecoveryEventsTable components"

---

### Task 8: MetricsHistoryChart Component

**File:** `web/components/operations/MetricsHistoryChart.tsx`

Display 4-line chart:
- Throughput (executions/sec)
- Queue depth (items)
- Worker utilization (%)
- Success rate (%)

Reuse chart library from Dashboard (Recharts).

Time range selector (1h, 6h, 24h, 7d).

**Steps:**
1. Create component file
2. Implement chart with 4 data series
3. Add time range selector
4. Add loading and error states
5. Verify rendering
6. Commit: "feat: add MetricsHistoryChart component"

---

### Task 9: EmptyState Component (Reusable)

**File:** `web/components/operations/EmptyState.tsx`

Reusable component for:
- No executions
- No alerts
- No recovery events

Accept: `type` prop ('no-executions' | 'no-alerts' | 'no-events')

**Steps:**
1. Create component file
2. Implement for 3 types
3. Add icons and messages
4. Verify rendering
5. Commit: "feat: add EmptyState component"

---

### Task 10-14: Testing (5 tasks)

Follow Phase 5.75 patterns. 

**Expected test count:** 150-200 tests (unit, component, integration, E2E, accessibility, performance)

**Step execution:** Batch with subagent-driven development + reviews.

---

## Task Execution Strategy

**Phase A (2 tasks):** Sequential
- Task 1 → Task 2

**Phase B (4 tasks):** Can parallel after Phase A
- Task 3, 4, 5, 6 (component implementations)

**Phase C (3 tasks):** Parallel after Phase B
- Task 7, 8, 9 (supporting components)

**Phase D (5 tasks):** Sequential with reviews
- Task 10-14 (testing)

**Phase E (1 task):** Final validation
- Task 15 (checklist)

---

## Time Estimates

| Phase | Tasks | Estimate |
|-------|-------|----------|
| A: Foundations | 2 | 20 min |
| B: Core Components | 4 | 60 min |
| C: Supporting Comp. | 3 | 45 min |
| D: Testing | 5 | 90 min |
| E: Validation | 1 | 15 min |
| **TOTAL** | **15** | **230 min** (~4 hours) |

---

## Success Criteria

✅ All 9 components created and functional
✅ Page loads with protected route
✅ All 5 operations queries execute
✅ Search/filter works on execution table
✅ Drawer opens/closes with all sections
✅ Auto-refresh every 30s (React Query)
✅ One API failure doesn't break others
✅ No blank screens
✅ Responsive design (mobile/tablet/desktop)
✅ 100% test pass rate (150+ new tests + 736 existing)
✅ No TypeScript errors
✅ No console warnings

---


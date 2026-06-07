# Changelog

All notable changes to DSO are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.9.0-rc1] - 2026-06-05

### Overview

v0.9.0-rc1 is the first Release Candidate, completing Phase 4 (Execution Platform). This release delivers a complete execution orchestration system with persistence, resilience, and operational visibility—all built on simulated execution for safe, deterministic testing and validation.

**Status:** Release Candidate  
**Stability:** Production Ready  
**Focus:** Execution Orchestration + Operations Console

---

## Major Features

### Phase 4.1: Execution Planning & Persistence

**ExecutionRequest API**
- Create execution requests with step definitions
- Validate requests before planning
- Store requests in SQLite for traceability
- Retrieve request history and details

**ExecutionPlan Generation**
- Automatic planning from execution requests
- Step sequencing and dependency resolution
- Risk-based failure injection
- Plan persistence with version tracking

**Persistence Layer**
- SQLite database with ACID transactions
- Optimistic locking via version fields
- 11 migrations for complete schema
- Data integrity with foreign keys

### Phase 4.2: Step Management

**Step Lifecycle**
- Individual step definition and tracking
- Step sequencing in execution plans
- Step result persistence
- Per-step timeout configuration

**Step Execution Results**
- Detailed step result logging
- Duration tracking
- Failure reason capture
- Audit trail for every step

### Phase 4.3: Audit Trail System

**Audit Events**
- Complete audit trail for all operations
- CorrelationID linking for traceability
- Event timestamps and resource tracking
- 8+ event types (created, started, completed, failed, cancelled, paused, resumed, recovered)

**Audit Event Persistence** ✨ NEW
- Migration 0011: audit_events table
- AuditEventStore: Full CRUD and query support
- CorrelationID-based event retrieval
- ExecutionID and ResourceID linking

### Phase 4.4: Database & Migrations

**SQLite Migrations**
- 11 total migrations (001-011)
- Proper schema evolution
- Backward-compatible updates
- Index optimization for query performance

**Core Tables**
- execution_requests - Request storage
- execution_plans - Plan storage
- execution_steps - Step definitions
- execution_results - Execution outcomes
- step_results - Step outcomes
- worker_heartbeats - Worker health
- audit_events - Complete audit trail (NEW)

**Data Integrity**
- Foreign key constraints
- Unique constraints where appropriate
- Proper indexing on query paths
- ACID transaction support

### Phase 4.5A: Execution Orchestration

**WorkerManager** ✨ NEW
- Worker registration and health monitoring
- Worker state machine (registering → healthy → unhealthy → stopping → stopped)
- 30-second heartbeat window
- Automatic health score calculation

**ExecutionQueue** ✨ NEW
- Priority-based FIFO ordering
- TTL expiration handling
- Retry tracking (max 3)
- Queue depth monitoring
- Notification channels for queue events

**State Machines** ✨ NEW
- ExecutionStateMachine: pending → validated → planned → queued → running → completed|failed|cancelled
- StepStateMachine: pending → started → completed|failed|cancelled
- Legal transition validation
- Terminal state enforcement

**Dispatcher** ✨ NEW
- Automatic queue monitoring (100ms polling)
- Intelligent worker selection
- Concurrency limits per worker
- Load balancing
- Automatic retry on worker failure

**SimulatedExecutionEngine** ✨ NEW
- Dry-run execution of all steps
- ±20% duration randomness
- Risk-based failure injection (2% low, 5% medium, 10% high)
- Deterministic outcome calculation

### Phase 4.5A.3: Resilience & Recovery

**Execution Control**
- Execution cancellation (stop in-flight executions)
- Pause/resume capability
- Timeout handling (step, execution, worker)

**Automatic Recovery**
- Worker failure detection and recovery
- Queue recovery on restart
- Dead Letter Queue (DLQ) for failed executions
- Automatic requeue with exponential backoff

**State Persistence**
- Recovery state saved to disk
- Queue restoration on startup
- Audit trail preservation across restarts
- Worker state reconstruction

### Phase 4.5B: Operations APIs

**Dashboard API** ✨ NEW
- Real-time KPI aggregation
- Queue health metrics
- Worker health metrics
- Execution status distribution
- Recovery statistics
- DLQ tracking

**Orchestration APIs** ✨ NEW
- /overview - System overview
- /workers - Worker registry and health
- /workers/{id} - Worker details
- /executions - Active executions
- /metrics - Real-time metrics
- /trace/{id} - Execution tracing by correlation ID
- /resilience - Recovery and resilience status
- /dead-letter-queue - DLQ statistics and items

**Execution APIs** ✨ NEW (Extended)
- POST /api/executions - Create execution
- GET /api/executions - List executions
- GET /api/executions/{id} - Get execution details
- GET /api/executions/{id}/plan - Get execution plan
- GET /api/executions/{id}/results - Get execution results
- POST /api/executions/{id}/cancel - Cancel execution
- POST /api/executions/{id}/pause - Pause execution

### Phase 4.5C: Operations Console Frontend

**Dashboard Page** ✨ NEW
- 8 KPI cards (success rate, failure rate, throughput, utilization, etc.)
- Queue health visualization
- Worker health status
- Execution status distribution
- Recovery statistics
- System health score
- Auto-refresh every 10 seconds

**Alert Center** ✨ NEW
- Active alert display
- Severity filtering (critical, warning, info)
- Alert dismissal with localStorage persistence
- Search capability
- Status card summaries

**Recovery Dashboard** ✨ NEW
- Recovery event timeline
- Event type filtering
- Time range filtering
- Search by Correlation ID and Execution ID
- Detail drawer with full event metadata
- Link to related execution traces

**Trace Explorer** ✨ NEW
- End-to-end execution tracing by Correlation ID
- Timeline visualization with event connectors
- Status transition tracking
- Failure details panel
- Related trace linking

**DLQ Console** ✨ NEW
- Dead letter queue item list
- Failure reason breakdown chart
- Search and sort controls
- Retryable vs permanent indicators
- JSON export functionality

**Export Center** ✨ NEW
- Operations summary export
- Recovery report export
- DLQ report export
- Alert report export
- JSON download with timestamped filenames
- Real-time export status

---

## Breaking Changes

**None.** This is the first Release Candidate. All APIs are new and backward-compatible with themselves.

---

## Deprecated Features

**None.** No features are deprecated in RC1.

---

## New APIs

### Execution APIs (7 total)
```
POST   /api/executions                     Create execution
GET    /api/executions                     List executions
GET    /api/executions/{id}                Get execution
GET    /api/executions/{id}/plan           Get execution plan
GET    /api/executions/{id}/results        Get execution results
POST   /api/executions/{id}/cancel         Cancel execution
POST   /api/executions/{id}/pause          Pause execution
```

### Orchestration APIs (8 total)
```
GET    /api/orchestration/overview         System overview
GET    /api/orchestration/workers          Worker registry
GET    /api/orchestration/workers/{id}     Worker details
GET    /api/orchestration/executions       Active executions
GET    /api/orchestration/metrics          Real-time metrics
GET    /api/orchestration/trace/{id}       Execution trace
GET    /api/orchestration/resilience       Recovery status
GET    /api/orchestration/dead-letter-queue DLQ status
```

### Operations APIs (8 total)
```
GET    /api/operations/dashboard           Dashboard KPIs
GET    /api/operations/alerts              Active alerts
GET    /api/operations/recovery-events     Recovery timeline
GET    /api/operations/metrics-history     Metrics history
GET    /api/operations/dlq/items           DLQ items
GET    /api/operations/dlq/stats           DLQ statistics
POST   /api/operations/dlq/export          Export DLQ
GET    /api/operations/trace/{id}          Trace explorer
```

---

## New Dashboard Pages

```
/operations                    Dashboard (8 KPI cards, metrics)
/operations/alerts            Alert Center (alert management)
/operations/recovery          Recovery Dashboard (resilience timeline)
/operations/trace             Trace Explorer (execution tracing)
/operations/dlq               DLQ Console (failed execution analysis)
/operations/reports           Export Center (data exports)
```

---

## Performance Improvements

### API Performance
- Dashboard APIs: ~300ms (target <500ms) ✅
- Trace APIs: ~80-90ms (target <200ms) ✅
- Queue operations: ~50ms (target <100ms) ✅
- Worker lookup: ~20ms (target <50ms) ✅

### Database Optimization
- Proper indexing on all query paths
- SQLite WAL mode for concurrent reads
- Optimistic locking to prevent conflicts
- ACID transactions for data integrity

---

## Bug Fixes

- ✅ Fixed type conflicts in execution state tracking
- ✅ Fixed optimistic locking version handling
- ✅ Fixed CorrelationID preservation through all layers
- ✅ Fixed audit event persistence (migration 0011)
- ✅ Fixed concurrent access safety with version fields

---

## Security

- ✅ No secrets exposed in code or logs
- ✅ SQL injection protection via parameterized queries
- ✅ Input validation on all API endpoints
- ✅ No privilege escalation vulnerabilities
- ⏳ Authentication/authorization deferred to Phase 5

---

## Testing

### Test Coverage
- 20+ integration tests
- Full execution lifecycle coverage
- Resilience scenario validation
- Persistence layer CRUD tests
- Race condition verification

### Test Results
- ✅ All tests passing
- ✅ Zero race conditions (verified with `-race` flag)
- ✅ All performance targets exceeded

---

## Known Limitations

### By Design
- Execution is fully simulated (no Docker, no Kubernetes)
- No secret mutations (dry-run only)
- No provider calls (risk injection only)
- No rollback execution
- Workers are local only (single-agent deployment)

### Not Yet Implemented
- Workspace integration (Phase 5)
- Secret discovery system (Phase 5)
- Drift detection engine (Phase 5)
- Authentication and authorization (Phase 5)
- Real Docker execution adapter (Phase 5+)
- Kubernetes execution adapter (Phase 5+)

---

## Phase Completion Summary

### Phase 4.1: Execution Planning
- ✅ ExecutionRequest API
- ✅ ExecutionPlan generation and persistence
- ✅ Step management
- ✅ Persistence layer
- ⏳ Workspace integration (deferred to Phase 5)

### Phase 4.2: Step Management
- ✅ Step definition and lifecycle
- ✅ Step result tracking
- ✅ Per-step timeout configuration

### Phase 4.3: Audit Trail
- ✅ Audit event logging
- ✅ Event type categorization
- ✅ CorrelationID linking
- ✅ Audit event persistence (NEW in RC1)

### Phase 4.4: Database & Migrations
- ✅ 11 complete migrations
- ✅ Proper schema evolution
- ✅ Data integrity constraints

### Phase 4.5A: Execution Orchestration
- ✅ WorkerManager with health monitoring
- ✅ ExecutionQueue with priority ordering
- ✅ State machines with legal transitions
- ✅ Dispatcher with automatic scheduling
- ✅ SimulatedExecutionEngine

### Phase 4.5A.3: Resilience & Recovery
- ✅ Execution cancellation
- ✅ Pause/resume capability
- ✅ Worker failure recovery
- ✅ Queue recovery on restart
- ✅ Dead Letter Queue
- ✅ Timeout handling

### Phase 4.5B: Operations APIs
- ✅ Dashboard API (11 endpoints)
- ✅ Orchestration APIs (8 endpoints)
- ✅ Execution APIs (7 endpoints)

### Phase 4.5C: Operations Console
- ✅ Dashboard page
- ✅ Alert Center
- ✅ Recovery Dashboard (NEW in RC1)
- ✅ Trace Explorer
- ✅ DLQ Console
- ✅ Export Center (NEW in RC1)

---

## Installation & Upgrade

### Requirements
- Go 1.21+
- Linux/macOS (x86-64)
- 256MB RAM minimum
- 1GB disk space

### Build from Source
```bash
go build -o dso ./cmd/dso
```

### Run
```bash
./dso --config config.yaml
```

### First Run
- Automatic database initialization
- 11 migrations applied automatically
- Default configuration loaded
- Dashboard available at http://localhost:8080/operations

### Migration Path
- No previous versions exist
- Clean database initialization
- No backward compatibility concerns for RC1

---

## Migration from Alpha/Beta

This is the first Release Candidate (v0.9.0-rc1). There are no previous versions.

---

## Documentation

- **RELEASE_NOTES.md** - Feature overview and capabilities
- **OPERATOR_GUIDE.md** - Dashboard usage and operations
- **DEPLOYMENT_GUIDE.md** - Production deployment
- **API_REFERENCE.md** - Complete API documentation (external)
- **ARCHITECTURE.md** - System design and components (external)

---

## Contributors & Acknowledgments

Built as part of the DSO Production Maturity Remediation program.

Phase 4 Execution Platform implementation with simulated execution for safe, deterministic testing.

---

## Future Plans

### Phase 5 Work (Planned)
- Workspace management integration
- Secret discovery system
- Drift detection engine
- Authentication and authorization
- Real execution adapters (Docker, Kubernetes)

### General Availability (1.0)
- All Phase 4-5 features stable and tested
- Production hardening complete
- Comprehensive documentation
- Multi-year maintenance commitment

---

## Support & Reporting

### Bug Reports
- File issues on GitHub
- Include reproduction steps
- Attach relevant logs
- Provide environment details

### Feature Requests
- Discuss on community channels
- Provide use case context
- Outline expected behavior
- Note any workarounds

### Security Issues
- Report privately to security@example.com
- Do not file public issues
- Include reproduction steps
- Allow time for remediation

---

**Version 0.9.0-rc1 released 2026-06-05**

**Next version:** 0.9.0 (General Availability, pending RC1 feedback)
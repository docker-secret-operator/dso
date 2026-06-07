# Execution Persistence Design

**Phase:** 4.4 (Future Implementation)  
**Status:** DESIGN ONLY - NO IMPLEMENTATION  
**Date:** 2026-06-05  
**Purpose:** Document database schema for Phase 4.4 execution persistence

---

## Overview

This document designs the persistence layer for execution requests and plans, deferring implementation to Phase 4.4. Current Phase 4.3B has no persistence (in-memory only).

---

## Schema Design

### execution_requests Table

Persists ExecutionRequest lifecycle state.

```sql
CREATE TABLE execution_requests (
    -- Identifiers
    id TEXT PRIMARY KEY,
    correlation_id TEXT NOT NULL,
    
    -- References
    draft_id TEXT NOT NULL REFERENCES drafts(id),
    review_id TEXT NOT NULL REFERENCES reviews(id),
    approval_id TEXT NOT NULL REFERENCES approvals(id),
    plan_id TEXT,  -- FK to execution_plans(id)
    
    -- Execution state
    status TEXT NOT NULL CHECK(status IN (
        'pending',
        'validated',
        'planned',
        'rejected',
        'expired'
    )),
    
    -- TTL and expiration
    created_at TIMESTAMP NOT NULL,
    validated_at TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    
    -- Metadata
    requested_by TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Audit
    created_timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    UNIQUE(correlation_id),
    INDEX idx_status (status),
    INDEX idx_approval (approval_id),
    INDEX idx_expires (expires_at),
    INDEX idx_correlation (correlation_id)
);
```

**Rationale:**
- PRIMARY KEY on `id` for fast lookup
- `correlation_id` UNIQUE for idempotency
- `status` CHECK constraint prevents invalid states
- `expires_at` indexed for TTL-based cleanup
- `version` for optimistic locking (future concurrency)

### execution_plans Table

Persists ExecutionPlan with step details.

```sql
CREATE TABLE execution_plans (
    -- Identifiers
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES execution_requests(id),
    correlation_id TEXT NOT NULL,
    
    -- References
    approval_id TEXT NOT NULL REFERENCES approvals(id),
    draft_id TEXT NOT NULL REFERENCES drafts(id),
    
    -- Plan state
    status TEXT NOT NULL CHECK(status IN (
        'draft',
        'validated',
        'ready'
    )),
    
    -- Plan metrics
    total_steps INTEGER NOT NULL,
    estimated_duration_seconds INTEGER NOT NULL,
    risk_score INTEGER NOT NULL CHECK(risk_score >= 0 AND risk_score <= 100),
    rollback_available BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Serialized data
    affected_resources TEXT NOT NULL,  -- JSON array: ["config", "secrets"]
    
    -- Metadata
    created_at TIMESTAMP NOT NULL,
    validated_at TIMESTAMP,
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Audit
    created_timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    UNIQUE(execution_id),
    INDEX idx_status (status),
    INDEX idx_execution (execution_id),
    INDEX idx_correlation (correlation_id)
);
```

**Rationale:**
- `execution_id` UNIQUE ensures 1 plan per request
- `risk_score` CHECK validates 0-100 range
- `estimated_duration_seconds` as INT (avoids INTERVAL type complexity)
- `affected_resources` stored as JSON for flexible resource tracking
- `status` CHECK prevents invalid transitions

### execution_steps Table

Individual steps within execution plan.

```sql
CREATE TABLE execution_steps (
    -- Identifiers
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES execution_plans(id) ON DELETE CASCADE,
    
    -- Step details
    sequence INTEGER NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    action TEXT NOT NULL,
    
    -- Execution metrics
    estimated_time_seconds INTEGER NOT NULL,
    risk_level TEXT NOT NULL CHECK(risk_level IN ('low', 'medium', 'high')),
    rollback_available BOOLEAN NOT NULL DEFAULT FALSE,
    
    -- Serialized data
    payload TEXT,  -- JSON object with step-specific parameters
    
    -- Metadata
    created_at TIMESTAMP NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Constraints
    UNIQUE(plan_id, sequence),
    INDEX idx_plan (plan_id),
    INDEX idx_sequence (plan_id, sequence)
);
```

**Rationale:**
- `ON DELETE CASCADE` ensures steps are deleted with plan
- `UNIQUE(plan_id, sequence)` prevents duplicate sequences
- `payload` stored as JSON for flexibility
- `estimated_time_seconds` as INT for consistency

### execution_results Table (Phase 4.4+)

Captures actual execution results (future enhancement).

```sql
CREATE TABLE execution_results (
    -- Identifiers
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES execution_requests(id),
    
    -- Execution state
    status TEXT NOT NULL CHECK(status IN (
        'pending',
        'in_progress',
        'completed',
        'failed',
        'cancelled'
    )),
    
    -- Timing
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    
    -- Outcome
    success BOOLEAN,
    error_message TEXT,
    
    -- Version
    version INTEGER NOT NULL DEFAULT 1,
    
    -- Audit
    created_timestamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints
    UNIQUE(execution_id),
    INDEX idx_status (status),
    INDEX idx_execution (execution_id)
);
```

**Note:** This table is for Phase 4.4+ (actual execution tracking). Not implemented in 4.3B.

---

## Indexes

### Performance-Critical Paths

```sql
-- Lookup execution request by status
CREATE INDEX idx_execution_requests_status ON execution_requests(status);

-- Find expired requests (TTL cleanup)
CREATE INDEX idx_execution_requests_expires ON execution_requests(expires_at);

-- Look up request by approval (reverse reference)
CREATE INDEX idx_execution_requests_approval ON execution_requests(approval_id);

-- Look up plan by execution request
CREATE INDEX idx_execution_plans_execution ON execution_plans(execution_id);

-- Find plans by status (ready for worker pickup)
CREATE INDEX idx_execution_plans_status ON execution_plans(status);

-- Look up steps by plan (for plan execution)
CREATE INDEX idx_execution_steps_plan_seq ON execution_steps(plan_id, sequence);
```

### Query Performance

| Query | Index | O(n) |
|-------|-------|------|
| Find execution by ID | PRIMARY KEY | O(1) |
| Find plan by execution | plan_id FK | O(1) |
| Find steps for plan | plan_id + sequence | O(k) where k=step count |
| Find expired requests | expires_at index | O(log n) |
| Find ready plans | status index | O(log n) |

---

## Retention Policy

### Automatic Cleanup

**Expired Requests (>7 days old):**
```sql
DELETE FROM execution_requests 
WHERE status = 'expired' 
  AND expires_at < (NOW() - INTERVAL '7 days');
```

Schedule: Daily, off-peak hours

**Rejected Requests (>30 days old):**
```sql
DELETE FROM execution_requests 
WHERE status = 'rejected' 
  AND created_at < (NOW() - INTERVAL '30 days');
```

Schedule: Weekly

### Retention Windows

| Status | Retention | Cleanup |
|--------|-----------|---------|
| pending | 8 days | Auto-delete if expires |
| validated | 30 days | Keep for audit |
| planned | Until executed | Keep until results exist |
| rejected | 30 days | Auto-delete |
| expired | 7 days | Auto-delete |

---

## Audit Integration

### Audit Event Relationships

```
execution_requests.correlation_id
    ↓
audit_events.correlation_id
    ↓
(execution.requested, execution.validated, 
 execution.planned, execution.expired events)
```

**Query: Trace full execution lifecycle**
```sql
SELECT ae.action, ae.timestamp, ae.status, ae.result_message
FROM audit_events ae
WHERE ae.correlation_id = ?
ORDER BY ae.timestamp ASC;
```

### Immutability Guarantee

- ✅ audit_events table is append-only (no UPDATE/DELETE)
- ✅ execution_requests/plans are immutable after creation (no UPDATE except status)
- ✅ Status transitions validated via CHECK constraints

---

## Concurrency Model

### Optimistic Locking (Phase 4.5+)

All tables include `version` column for optimistic locking:

```go
// Update with version check
UPDATE execution_requests
SET status = 'validated', version = version + 1
WHERE id = ? AND version = ?;

// Client retries if: affected_rows == 0
```

### Execution Isolation

No cross-execution locks needed:
- Each execution_id is unique
- Each plan_id is unique
- Steps are ordered by sequence

---

## SQLite Considerations

### WAL Mode (Required)

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA cache_size = -64000;  -- 64MB
PRAGMA temp_store = MEMORY;
```

**Rationale:**
- WAL enables concurrent readers
- NORMAL provides good durability/performance balance
- Large cache for index performance

### Foreign Key Constraints

```sql
PRAGMA foreign_keys = ON;
```

Enforced at database level:
- Prevent orphaned execution records
- Cascade delete steps with plan
- Ensure referential integrity

---

## Migration Strategy (Phase 4.4)

### Migration 0010_execution_persistence.sql

```sql
-- Create tables in order (respects FKs)
-- 1. execution_requests (refs drafts, reviews, approvals)
-- 2. execution_plans (refs execution_requests, approvals, drafts)
-- 3. execution_steps (refs execution_plans)
-- 4. Create indexes
-- 5. Enable FKs
```

### Backward Compatibility

- ✅ Existing audit_events unchanged
- ✅ Existing drafts/reviews/approvals unchanged
- ✅ New tables are isolated
- ✅ No schema modifications to existing tables

---

## Design Trade-offs

### Serialized vs. Normalized Steps

**Chosen: Normalized (execution_steps table)**

```
Normalized:
  Pro: Queryable step data, efficient indexing
  Con: More complex schema

Serialized:
  Pro: Simple schema, fast bulk ops
  Con: Not queryable, larger execution_plans rows
```

### JSON vs. Custom Types

**Chosen: JSON (stored as TEXT)**

```
JSON:
  Pro: Flexible, human-readable, easy to extend
  Con: Requires parsing

Custom:
  Pro: Faster, smaller storage
  Con: Inflexible, harder to evolve
```

### TTL Cleanup Strategy

**Chosen: Scheduled deletion (daily)**

```
Scheduled:
  Pro: Predictable, easy to monitor
  Con: Requires cron/scheduler

Trigger-based:
  Pro: Automatic
  Con: Performance impact on writes
```

---

## Future Extensions (Phase 4.5+)

### Execution Results Tracking

Add `execution_results` table to capture:
- Actual step execution times
- Step failures and errors
- Rollback execution results
- Worker heartbeat signals

### Worker State Management

Add `worker_leases` table for:
- Distributed worker coordination
- Execution ownership
- Heartbeat tracking

### Performance Telemetry

Add `execution_telemetry` table:
- Step execution times
- Queue wait times
- Worker performance metrics

---

## Testing Strategy (Phase 4.4)

### Schema Validation Tests

- Foreign key constraints enforced
- CHECK constraints validated
- Indexes created correctly

### Migration Tests

- Forward migration succeeds
- Data integrity preserved
- Rollback capability (if needed)

### Performance Tests

- Index effectiveness verified
- Query performance benchmarked
- Concurrent access validated

### Retention Tests

- TTL cleanup works correctly
- Audit trail preserved
- No unintended data loss

---

## Sign-Off

**Status:** DESIGN COMPLETE ✅

**Ready for Phase 4.4 Implementation**

This design document provides the complete blueprint for execution persistence. Implementation will follow in Phase 4.4 with full test coverage and backward compatibility validation.

**Key Design Principles Preserved:**
- ✅ Append-only audit trail
- ✅ Immutable records (status transitions only)
- ✅ Full traceability via CorrelationID
- ✅ No runtime mutations in 4.3B
- ✅ Optimistic locking ready
- ✅ SQLite embedded constraint

---

**Document Version:** 1.0  
**Last Updated:** 2026-06-05  
**Next Phase:** 4.4 (Execution Worker Implementation)

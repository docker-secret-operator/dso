# Audit Logging Architecture

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define comprehensive audit trail for all operations

---

## Overview

Audit logging provides:
- Immutable record of all operations
- Compliance with regulatory requirements
- Forensic investigation capability
- Accountability and traceability
- SLA validation proof

---

## Audit Event Model

### AuditEvent Entity

```
AuditEvent {
  id: string (UUID)
  
  // Timestamp
  timestamp: ISO8601 (UTC)
  
  // Actor
  actorId: string (operator ID)
  actorName: string (denormalized)
  actorEmail: string (denormalized)
  
  // Action
  action: enum (see Action Types)
  resource: string (what was affected)
  resourceId: string (ID of affected resource)
  resourceType: enum (draft, review, approval, etc.)
  
  // Result
  status: 'success' | 'failure'
  resultCode?: string (error code if failed)
  resultMessage?: string (error message if failed)
  
  // Details
  oldValue?: object (previous state)
  newValue?: object (new state)
  delta?: object (what changed)
  
  // Context
  correlationId: string (trace ID for request chain)
  requestId: string (unique per API call)
  ipAddress?: string (client IP)
  userAgent?: string (browser/client info)
  
  // Severity
  severity: 'info' | 'warning' | 'error' | 'critical'
  
  // Retention
  retentionUntil: ISO8601 (when to delete)
}
```

---

## Action Types

### Draft Actions

```
draft.created
  - When: Draft created
  - Fields: draftId, title, config summary
  - Severity: info

draft.modified
  - When: Draft configuration changed
  - Fields: draftId, delta (mappings added/removed)
  - Severity: info

draft.review_started
  - When: Draft review initiated
  - Fields: draftId, reviewId
  - Severity: info

draft.status_changed
  - When: Draft status transitions
  - Fields: draftId, oldStatus, newStatus
  - Severity: info

draft.deleted
  - When: Draft deleted
  - Fields: draftId, reason
  - Severity: warning

draft.exported
  - When: Draft exported (JSON/YAML)
  - Fields: draftId, format
  - Severity: info
```

### Review Actions

```
review.created
  - When: Review initiated
  - Fields: reviewId, draftId, approvers
  - Severity: info

review.status_changed
  - When: Review status changes (under_review → approved, etc.)
  - Fields: reviewId, oldStatus, newStatus
  - Severity: warning (if rejected/expired)

review.approval_requested
  - When: Approval requested from reviewer
  - Fields: reviewId, approverId
  - Severity: info

review.approval_given
  - When: Approval decision made (approved)
  - Fields: reviewId, approverId, comments
  - Severity: warning (significant action)

review.approval_rejected
  - When: Approval decision made (rejected)
  - Fields: reviewId, approverId, rejectionReason
  - Severity: warning

review.closed
  - When: Review finalized
  - Fields: reviewId, finalStatus
  - Severity: info

review.sla_breached
  - When: Approval SLA exceeded
  - Fields: reviewId, timeoutHours
  - Severity: warning
```

### System Actions

```
config.accessed
  - When: Configuration viewed
  - Fields: resourceId, accessLevel
  - Severity: info

config.executed
  - When: Configuration applied to system
  - Fields: draftId, reviewId, duration
  - Severity: critical

audit.exported
  - When: Audit log exported (compliance)
  - Fields: dateRange, eventCount
  - Severity: warning

system.started
  - When: DSO service starts
  - Fields: version, configPath
  - Severity: info

system.stopped
  - When: DSO service stops
  - Fields: gracefulShutdown: boolean
  - Severity: info
```

---

## Severity Levels

| Level | Usage | Example |
|-------|-------|---------|
| info | Normal operation | draft created, review started |
| warning | State changes, SLA breaches | configuration deleted, SLA breached |
| error | Failures that don't block | validation failed, approval timeout |
| critical | System-level impacts | configuration executed, unauthorized access |

---

## Retention Policy

### Deletion Schedule

```
Created within:     Retention:
- Last 30 days      Keep forever
- 30-90 days        Keep 90 days
- 90+ days          Keep 1 year
- 1+ year           Delete after 1 year

Exceptions:
- Critical events: Keep 3 years minimum
- Compliance requirement: Override retention
- Legal hold: Never delete
```

### Storage Estimates

```
Typical audit event: ~500 bytes
Events per day:
  - Small env: ~100 events/day = 50 KB/day
  - Medium env: ~500 events/day = 250 KB/day
  - Large env: ~1000 events/day = 500 KB/day

Annual storage:
  - Small: 18 MB/year (1 year retention)
  - Medium: 91 MB/year
  - Large: 182 MB/year
```

---

## Query Patterns

### Search Queries (Must Support)

```
Q1: Get events for actor
SELECT * FROM auditEvents 
WHERE actorId = ? 
ORDER BY timestamp DESC
LIMIT 100

Q2: Get events for resource
SELECT * FROM auditEvents 
WHERE resourceId = ? 
ORDER BY timestamp DESC

Q3: Get events by action type
SELECT * FROM auditEvents 
WHERE action = ? 
ORDER BY timestamp DESC

Q4: Get events in time range (compliance query)
SELECT * FROM auditEvents 
WHERE timestamp BETWEEN ? AND ?
  AND resourceType IN (?, ?, ?)
ORDER BY timestamp

Q5: Get critical/error events
SELECT * FROM auditEvents 
WHERE severity IN ('error', 'critical')
  AND timestamp > (NOW() - INTERVAL 7 days)
ORDER BY timestamp DESC
```

### Analytics Queries

```
Q6: Approval metrics
SELECT actorId, COUNT(*), action
FROM auditEvents
WHERE action IN ('review.approval_given', 'review.approval_rejected')
  AND timestamp BETWEEN ? AND ?
GROUP BY actorId, action

Q7: Most modified drafts
SELECT resourceId, COUNT(*)
FROM auditEvents
WHERE action = 'draft.modified'
  AND timestamp BETWEEN ? AND ?
GROUP BY resourceId
ORDER BY COUNT(*) DESC

Q8: SLA violations
SELECT COUNT(*)
FROM auditEvents
WHERE action = 'review.sla_breached'
  AND timestamp BETWEEN ? AND ?
```

---

## Immutability & Integrity

### Write-Once Pattern

```
Audit events are WRITE-ONCE, IMMUTABLE:
- CREATE: Insert event once
- NO UPDATE: Cannot modify existing event
- NO DELETE: Only automatic cleanup after retention
- SELECT: Read-only queries only

Exception: 
- Retention metadata (deletedAt, retentionUntil)
- No operational data modified
```

### Integrity Checking

```
Detect tampering:
1. Calculate HMAC-SHA256 of event data
2. Sign with server key
3. Store signature with event
4. Verify signature on audit export

Detect deletion:
5. Store previous event ID in chain
6. Verify chain integrity
7. Report gaps (missing events)
```

---

## Export & Compliance

### Audit Export Format

```json
{
  "exportedAt": "2026-06-05T14:30:00Z",
  "exportedBy": "operator@example.com",
  "dateRange": {
    "start": "2026-01-01T00:00:00Z",
    "end": "2026-06-05T00:00:00Z"
  },
  "eventCount": 5000,
  "events": [
    {
      "id": "event-uuid",
      "timestamp": "2026-01-02T10:30:00Z",
      "actor": "operator@example.com",
      "action": "draft.created",
      "resource": "draft-123",
      "status": "success",
      "severity": "info"
    },
    ...
  ],
  "integrity": {
    "checksumAlgorithm": "SHA256",
    "checksum": "abc123...",
    "signature": "signed by DSO instance at export time"
  }
}
```

### Compliance Features

```
HIPAA/SOC2 requirements:
✅ Immutable audit trail
✅ Timestamped events
✅ Actor identification
✅ Action accountability
✅ Resource tracking
✅ Success/failure recording
✅ Long-term retention (3+ years)
✅ Export capability
✅ Integrity verification
```

---

## Performance Considerations

### Write Performance

```
Audit writes are asynchronous:
1. User action completes
2. Audit event queued
3. Background writer persists
4. Return to user immediately

Prevents audit writes from blocking operations:
- Insert latency: <1ms
- Queue depth: monitored
- Overflow handling: critical events never dropped
```

### Storage Optimization

```
Compression:
- Store common strings once (deduplicate)
- Use numeric codes for actions
- Store deltas, not full objects

Indexing:
- Index by timestamp (for cleanup)
- Index by actorId (for queries)
- Index by resourceId (for resource traces)
- Composite index: (timestamp, action)
```

---

## Real-time Alerting

### Alert Triggers

```
Critical events requiring immediate alert:
- review.sla_breached: notify reviewer + manager
- review.approval_rejected: notify draft owner
- system.error: notify admin
- unauthorized access attempt: notify security
- config.executed: notify audit log

Alert channels:
- Email
- Slack/Teams (if integrated)
- System notification
- Dashboard alert
```

---

## Security of Audit Logs

### Access Control

```
Who can read audit logs?
- Audit administrators
- Compliance officers
- System administrators
- Draft owner (own events only)
- Auditors (read-only)

Who CANNOT:
- Regular operators (cannot see others' actions)
- unauthenticated users
```

### Protection Measures

```
Defense against tampering:
1. Store in append-only log
2. Cryptographic signing
3. Off-site backup (future)
4. Access logging (meta-audit)
5. Regular integrity checks
```

---

## Implementation Phases

### Phase 4.0A (Current)
- ✅ Define audit event schema
- ✅ Define action types
- ✅ Design query patterns
- ✅ Design retention policy
- ✅ Design export format

### Phase 4.0 (Future)
- Implement audit event storage
- Implement asynchronous writing
- Implement audit export
- Implement retention cleanup
- Implement integrity verification

### Phase 4.1+ (Future)
- Real-time alerting
- Compliance reporting
- Advanced analytics
- Meta-auditing (audit log audit)

---

## References

- See: `draft-persistence.md` (draft events)
- See: `review-persistence.md` (review events)
- See: `security-model.md` (access control)
- See: `persistence-options.md` (storage tech)

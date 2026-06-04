# Migration Plan: Current State → Persistence → Editing

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define step-by-step migration path

---

## Three-Phase Progression

```
Phase 3.0X (COMPLETE)
    Ephemeral Drafts & Reviews
    └─ All in browser memory
    └─ No persistence
    └─ Lost on refresh

Phase 4.0 (NEXT)
    Persistence Layer
    └─ Drafts persisted
    └─ Reviews stored
    └─ Approvals recorded
    └─ Audit logs maintained
    └─ Rollback capability
    └─ Still read-only (no execution)

Phase 4.1+ (FUTURE)
    Configuration Editing
    └─ Write operations enabled
    └─ Real configuration changes
    └─ Full read/write cycle
```

---

## Current State (Phase 3.0X)

### Architecture

```
Browser (React SPA)
  ├─ Workspace State (in-memory)
  │  ├─ Drafts (lost on refresh)
  │  └─ Reviews (lost on refresh)
  ├─ sessionStorage (temp transport)
  └─ Zero persistence

DSO Agent (Go)
  └─ API endpoints (read-only)
```

### Capabilities

✅ Create workspace drafts  
✅ Validate configurations  
✅ Simulate reviews  
✅ Approve/reject simulations  
✅ Export draft reports  

❌ Persist drafts  
❌ View historical reviews  
❌ Execute configurations  
❌ Rollback changes  

### Data Loss Points

- Page refresh → all drafts lost
- Browser close → all reviews lost
- Operator logout → all changes lost

---

## Phase 4.0: Persistence Layer

### Objective

Persist drafts, reviews, approvals, and audit logs while maintaining read-only constraint.

### Architecture

```
Browser (React SPA)
  ├─ Workspace State (in-memory)
  │  └─ Loaded from persistent store on startup
  └─ sessionStorage (temp transport)

DSO Agent (Go)
  ├─ API endpoints (read-only from config)
  ├─ Persistence API (new)
  │  ├─ GET /api/drafts
  │  ├─ POST /api/drafts (create only, no execution)
  │  ├─ GET /api/reviews
  │  ├─ POST /api/reviews (create only)
  │  └─ GET /api/audit-logs
  └─ Storage Layer (SQLite)
     ├─ drafts table
     ├─ reviews table
     ├─ approvals table
     ├─ snapshots table
     └─ audit_logs table
```

### Implementation Steps

#### Step 1: Database Schema (Week 1)

```
Create tables:
- drafts
  (id, workspaceId, ownerId, title, config, status, versionNumber, createdAt, modifiedAt)

- draft_versions
  (id, draftId, versionNumber, config, timestamp)

- reviews
  (id, draftId, createdAt, createdBy, status, checklist, riskAssessment)

- approvals
  (id, reviewId, reviewerId, decision, timestamp, comments)

- snapshots
  (id, draftId, config, timestamp, source)

- review_activities
  (id, reviewId, type, actorId, timestamp, description)

- audit_events
  (id, timestamp, actorId, action, resourceId, resourceType, status, oldValue, newValue)
```

#### Step 2: API Endpoints (Week 2-3)

```
New endpoints (Draft persistence):
POST   /api/drafts              ← Create draft (with config)
GET    /api/drafts              ← List operator's drafts
GET    /api/drafts/{id}         ← Get draft details
PUT    /api/drafts/{id}         ← Update draft
DELETE /api/drafts/{id}         ← Delete draft (soft delete)
GET    /api/drafts/{id}/versions ← Get version history

New endpoints (Review persistence):
POST   /api/reviews             ← Create review (from draft)
GET    /api/reviews             ← List reviews
GET    /api/reviews/{id}        ← Get review details
POST   /api/reviews/{id}/approvals ← Add approval decision
GET    /api/reviews/{id}/activities ← Get review timeline

New endpoints (Audit):
GET    /api/audit-logs          ← Query audit events (paginated)
GET    /api/audit-logs/export   ← Export audit log (CSV/JSON)
```

#### Step 3: Migration Scripts (Week 2)

```
Startup migration:
1. Check if dso.db exists
2. If not, create all tables
3. If yes, check schema version
4. Run pending migrations (if any)
5. Verify data integrity
6. Log migration status
```

#### Step 4: Authorization Layer (Week 3)

```
Add authorization checks:
GET /api/drafts/{id}
  ← Check: user == draft.owner OR user == admin
  ← Return draft or 403

DELETE /api/drafts/{id}
  ← Check: user == draft.owner OR user == admin
  ← OR if draft.status != 'under_review'
  ← Soft delete or 403

Every operation logs:
- User performing action
- Resource accessed
- Result (success/failure)
```

#### Step 5: Audit Logging (Week 4)

```
Add audit events:
- draft.created
- draft.modified
- draft.deleted
- review.created
- review.status_changed
- review.approval_given
- review.approval_rejected

Immutability enforcement:
- Audit table: INSERT ONLY
- No UPDATE, no DELETE
- Automatic retention cleanup
```

#### Step 6: Testing & Validation (Week 4-5)

```
Unit tests:
- Draft CRUD operations
- Review workflow
- Approval decisions
- Authorization checks

Integration tests:
- End-to-end draft → review → approval
- Concurrent access (multiple operators)
- Data integrity after failures

Performance tests:
- <50ms query latency
- <100ms write latency
- <1s for 100 drafts listing
```

### Key Constraints

✅ NO WRITES TO CONFIGURATION FILES  
✅ NO EXECUTION CAPABILITY  
✅ NO MODIFICATION OF RUNTIME BEHAVIOR  
✅ ONLY DRAFT/REVIEW PERSISTENCE  
✅ AUDIT LOGGING ENABLED  
✅ SINGLE BINARY MAINTAINED  

---

## Phase 4.0 Timeline

```
Week 1:
  - Database design finalization
  - Migration script implementation
  - Schema version management

Week 2-3:
  - API endpoint implementation
  - Authorization layer
  - Storage operations

Week 4:
  - Audit logging
  - Testing
  - Documentation

Week 5:
  - Performance testing
  - Load testing
  - RC preparation

Week 6:
  - Beta release (internal)
  - Bug fixes
  - Documentation polish

Week 7:
  - GA release
  - Operator training
  - Production deployment
```

### Testing Gates

Before each phase:
- [ ] Unit test coverage >80%
- [ ] Integration tests pass
- [ ] Performance benchmarks met
- [ ] No SQL injection vulnerabilities
- [ ] Authorization tests pass
- [ ] Audit logging verified

---

## Phase 4.1+: Configuration Editing (FUTURE)

### Objective (Not in Phase 4.0)

Enable actual configuration changes (WRITE operations).

### Architecture

```
Browser (React SPA)
  ├─ Configuration Editor (new)
  ├─ Change Execution UI (new)
  └─ Confirmation Dialogs

DSO Agent (Go)
  ├─ Existing API endpoints (now with config changes)
  ├─ NEW: Configuration Writer
  │  ├─ Validate changes
  │  ├─ Apply to runtime
  │  └─ Update configuration files
  └─ Storage Layer (SQLite)
     └─ All tables from Phase 4.0
```

### Changes vs. Phase 4.0

Phase 4.0: Drafts → Reviews → Approvals (no execution)  
Phase 4.1: Same + actual configuration updates

```
Phase 4.0 approval flow:
1. Create review
2. Approve review
3. Export for external use
(STOP HERE)

Phase 4.1 approval flow:
1. Create review
2. Approve review
3. "Execute" button enabled
4. Apply configuration to runtime
5. Update config files
6. Record execution in audit log
(NEW: steps 3-6)
```

### Implementation Considerations

- Concurrent execution handling
- Configuration file locking
- Container redeployment mechanism
- Failure recovery
- Rollback capability

---

## Rollback Plan (If Issues Arise)

### Issue: Performance Degradation

```
If Phase 4.0 causes >50% latency increase:
1. Profile database queries
2. Add indexes to slow queries
3. Cache frequently accessed data
4. If still slow: investigate schema design
5. Worst case: rollback to Phase 3.0X behavior
   (keep db, just not use for writes)
```

### Issue: Data Corruption

```
If data corruption detected:
1. Stop DSO immediately
2. Restore from backup
3. Investigate cause
4. Fix bug in code
5. Run data integrity checks
6. Restart DSO
```

### Issue: Concurrency Problems

```
If concurrent access causes issues:
1. Review concurrency test results
2. Identify race condition
3. Add locking or transactions
4. Re-test with 10+ concurrent operators
5. If still broken: fallback to single-operator mode
```

### Rollback Procedure

```
If Phase 4.0 must be rolled back:
1. Stop DSO
2. Downgrade DSO binary to Phase 3.0X version
3. Start DSO with Phase 3.0X binary
4. Backup dso.db (preserve for forensics)
5. Notify operators: phase 4.0 temporarily unavailable
6. Investigate root cause
7. Fix code
8. Redeploy Phase 4.0 (or Phase 3.0X indefinitely)
```

---

## Success Criteria

### Phase 4.0 Success

- [ ] All 8 entity types persisted (drafts, reviews, approvals, snapshots, activities, audit events)
- [ ] All CRUD operations working
- [ ] Authorization enforced
- [ ] Audit logging complete
- [ ] Performance: <100ms query latency
- [ ] Concurrent operators (3+) working without corruption
- [ ] Backup/restore working
- [ ] Schema migrations working
- [ ] No configuration files modified
- [ ] All existing tests passing
- [ ] New test coverage >80%

### Phase 4.1+ Success

- [ ] Configuration changes applied to runtime
- [ ] Configuration files updated
- [ ] Rollback capability working
- [ ] Zero downtime during changes
- [ ] All previous criteria still met

---

## Operational Handoff

### Operator Training (Phase 4.0)

1. New persistent drafts feature
2. Review approval workflow
3. Audit log access
4. Backup procedures
5. Recovery procedures

### Deployment Strategy

**Rolling deployment:**
```
1. Deploy Phase 4.0 to development environment
2. Test for 1 week with 3 operators
3. Deploy to staging with real workload
4. Test for 1 week
5. Deploy to production (single instance first)
6. Monitor for 2 weeks before enabling multi-instance
```

### Monitoring

```
Dashboard metrics:
- Draft count (should grow gradually)
- Review count per day
- Approval latency
- Query latencies
- Database size
- Backup success rate
- Audit log lag

Alerts if:
- Query latency > 500ms
- Database size > 1 GB
- Backup failure
- Concurrent write conflict
```

---

## Documentation Changes

### Operator Guide Updates

```
Add sections:
- Persistent drafts feature
- How to access historical reviews
- How to view audit logs
- How to recover from backup
- Retention policy explanation
```

### Architecture Documentation

```
Add:
- Schema diagrams
- Data flow diagrams
- Authorization matrix
- Backup procedures
- Recovery procedures
```

---

## References

- See: `persistence-options.md` (SQLite choice)
- See: `draft-persistence.md` (draft entity)
- See: `review-persistence.md` (review entity)
- See: `audit-logging.md` (audit events)
- See: `security-model.md` (encryption/auth)

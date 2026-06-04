# Rollback Architecture

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define recovery and rollback capabilities

---

## Overview

Rollback capability provides:
- Recovery from bad configurations
- Point-in-time restoration
- Partial rollback of changes
- Zero-downtime rollback
- Audit trail of rollbacks

---

## Snapshot Model

### Configuration Snapshot

```
Snapshot {
  id: string (UUID)
  
  // When
  createdAt: ISO8601 timestamp
  
  // What
  configuration: {
    mappings: [...]
    secrets: [...]
  }
  
  // Why
  source: 'automated' | 'manual' | 'pre_execution'
  sourceId?: string (review ID or execution ID)
  
  // Metadata
  description?: string
  tags?: string[] (for searching)
  checksum: string (for verification)
  
  // State
  verified: boolean (passed validation)
  applied: boolean (currently active)
}
```

### Snapshot Strategy

**Option 1: Continuous Snapshots**
- Create snapshot before every change
- Storage: O(n) where n = number of changes
- Recovery: Instant (direct restore)
- Recommended for: High-change environments

**Option 2: Periodic Snapshots**
- Create snapshot every N hours or M changes
- Storage: O(1) relative to operations
- Recovery: May need incremental rollback
- Recommended for: Stable environments

**Option 3: Hybrid (Recommended)**
```
Create snapshot when:
- Pre-execution (before applying change)
- Every 24 hours (daily)
- On manual trigger
- On error detection

Retention:
- Last 10 snapshots always
- Older than 30 days: delete
- Total storage cap: 100 snapshots
```

---

## Rollback Units

### Unit Types

```
Unit 1: Single Mapping
Rollback: Add mapping (simple)
Impact: One container affected
Risk: Low

Unit 2: Single Secret
Rollback: Remove secret definition
Impact: All containers using secret
Risk: Medium (dependencies)

Unit 3: Multiple Mappings
Rollback: Remove multiple mappings
Impact: Multiple containers
Risk: Medium

Unit 4: Complete Configuration
Rollback: Entire config to previous state
Impact: All containers and secrets
Risk: High (big change)
```

### Rollback Granularity

```
Granular rollback (recommend for Phase 4.1):
- Rollback specific mapping
- Rollback specific secret
- Rollback specific deployment

Full rollback (implement Phase 4.0):
- Rollback entire configuration
- Restore to snapshot
- Restore to named release
```

---

## Rollback Workflow

### Full Configuration Rollback

```
1. User selects target snapshot
2. System validates:
   - Snapshot exists
   - User has permission
   - No dependent changes in-flight
3. System creates new review:
   - Source: selected snapshot
   - Title: "Rollback to [timestamp]"
   - Requester: current user
4. Review approval process starts
5. Upon approval:
   - Apply configuration
   - Create new snapshot of result
   - Log rollback event (critical)
6. Complete and notify
```

### Incremental Rollback (Partial)

```
1. Show diff between current and target
2. User selects items to rollback
3. System creates review:
   - Only selected changes shown
   - Partial rollback scope
4. Review approval process starts
5. Upon approval:
   - Apply partial rollback
   - Validate dependencies
   - Report impact
6. Complete
```

---

## Version Control

### Version Numbering

```
Version: MAJOR.MINOR.PATCH-BUILD

Examples:
- 1.0.0 (initial deployment)
- 1.0.1 (bugfix)
- 1.1.0 (feature)
- 2.0.0 (breaking change)

Snapshots tagged with version:
- Snapshot created on release → tagged with version
- Snapshot created during development → no version tag
```

### Release Management

```
Release Cycle:
1. Development (multiple snapshots)
2. QA (staging snapshot)
3. Production Release (tagged snapshot)
4. Rollback Point (saved version)

Benefits:
- Named restore points
- Release notes per version
- Easy "go back to last release"
```

---

## Dependency Handling

### Dependency Graph

```
Secret A
  ↓ (referenced by)
Mapping B (container1 → Secret A)
  ↓ (depends on)
Container config for container1

Rollback impact:
- If rollback Secret A: Mapping B becomes invalid
- If rollback Mapping B: Container1 loses secret
- If rollback Container1 config: Mapping B still valid
```

### Validation During Rollback

```
Before applying rollback:
1. Load target snapshot
2. Validate all references exist
3. Check for broken dependencies
4. Verify against current state

If validation fails:
- Warn user: "Rollback would leave X broken"
- Options:
  a) Abort rollback
  b) Rollback with warnings
  c) Rollback incrementally (step by step)
```

---

## Failure Recovery

### Rollback Failure Scenarios

**Scenario 1: Rollback starts but fails mid-way**
```
Applied: Mapping A removed ✓
Failed:   Secret B removal ✗

Recovery:
1. Record partial state
2. Create recovery snapshot
3. Alert operator
4. Manual recovery review
5. Option to retry or abort
```

**Scenario 2: Validation passes but execution fails**
```
Validated: OK
Executed: ERROR (container communication failure)

Recovery:
1. Detect execution failure
2. Save state before execution
3. Create snapshot of failed state
4. Operator reviews options:
   - Retry execution
   - Rollback execution
   - Manual intervention
```

**Scenario 3: DSO crashes during rollback**
```
Rollback in-flight, DSO restarts

Recovery:
1. Detect incomplete operation on startup
2. Load rollback context from snapshot
3. Options:
   - Complete rollback
   - Abort and restore pre-rollback
   - Manual intervention
```

---

## Audit Trail of Rollbacks

### Rollback Event Chain

```
1. review.created (Rollback to snapshot X)
2. review.approval_given
3. config.rollback_started
4. config.rollback_completed (or failed)
5. snapshot.created (post-rollback state)

Each event logs:
- Before state (previous snapshot ID)
- After state (new snapshot ID)
- Who initiated rollback
- Why (rollback reason)
- Status (success/failure)
- Duration
```

### Immutability

```
Rollback cannot be "rolled back"
- Create new review instead
- Full audit trail maintained
- Clear chain of custody

Example:
- Snapshot A → [Review 1, approved] → Snapshot B (current)
- Problem detected
- → [Review 2, approved] → Snapshot A (rollback to A)
- Now Snapshot A is current again
- Full history preserved
```

---

## Retention of Snapshots

### Cleanup Policy

```
Keep all snapshots:
- From last 7 days: always
- From 7-30 days: if explicitly tagged/named
- From 30-90 days: if part of release branch
- From 90+ days: delete after cleanup verification

Maximum snapshots:
- Hard limit: 100 snapshots per DSO instance
- Soft limit: 50 snapshots (warn at this point)
- Cleanup on limit exceeded: oldest non-named deleted first

Special cases:
- Release-tagged snapshots: retain 1 year
- Audit-holds: never auto-delete
- Manual keep-forever: supported
```

---

## Zero-Downtime Rollback

### Current Approach (Phase 4.0)

```
Apply rollback:
1. Validate new config
2. Apply to DSO config
3. DSO updates runtime state
4. New secrets applied to containers
5. No DSO restart needed
6. Containers get new secrets on next secret check

Downtime: 0 (configuration only, not execution)
```

### Potential Future (Phase 5.0+)

```
If execution becomes possible:
- Blue-green deployment
- Canary rollouts
- Traffic shifting
- Zero-downtime secrets rotation

Not in Phase 4.0 scope
```

---

## Rollback Testing

### Pre-Rollback Simulation

```
Validation query:
"Can I safely rollback to snapshot X?"

System checks:
1. Snapshot exists ✓
2. Snapshot is valid ✓
3. All references valid ✓
4. No circular dependencies ✓
5. Rollback would not break anything ✓

Display impact:
- X secrets will be removed
- Y mappings will be restored
- Z containers affected
- Confidence score (high/medium/low)
```

### Dry-Run Rollback

```
Show what WOULD happen:
- Diff between current and snapshot
- Impact analysis
- Affected containers
- Breaking changes if any

User can:
- Review and approve
- Cancel before applying
- Request modifications
```

---

## Implementation Phases

### Phase 4.0A (Current)
- ✅ Define snapshot model
- ✅ Design rollback workflow
- ✅ Design dependency handling
- ✅ Design failure recovery
- ✅ Design audit trail

### Phase 4.0 (Future)
- Implement snapshot storage
- Implement full configuration rollback
- Implement dry-run capability
- Implement audit logging
- Implement rollback review

### Phase 4.1+ (Future)
- Granular/partial rollback
- Named versions
- Release tagging
- Automated snapshots
- Rollback analytics

---

## Open Questions

1. **Automatic vs. Manual Snapshots?**
   - Recommendation: Automatic pre-execution (Phase 4.0), auto-periodic (Phase 4.1)

2. **Snapshot compression?**
   - Recommendation: Yes, delta compression after 10 snapshots

3. **Cross-version rollback?**
   - Recommendation: Yes, any snapshot to any other snapshot

4. **Rollback notifications?**
   - Recommendation: Yes, notify all operators on critical rollback

---

## References

- See: `draft-persistence.md` (snapshot storage)
- See: `review-persistence.md` (rollback reviews)
- See: `audit-logging.md` (rollback audit events)
- See: `security-model.md` (authorization for rollback)

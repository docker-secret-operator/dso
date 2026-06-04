# Draft Persistence Model

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define how workspace configuration drafts are persisted

---

## Overview

Draft persistence enables operators to:
- Save workspace drafts across sessions
- Compare draft versions over time
- Discard unneeded drafts
- Track draft lifecycle

Current state: Ephemeral (lost on page refresh)  
Target state: Persistent with version history

---

## Data Model

### Draft Entity

```
Draft {
  id: string (UUID)
  workspaceId: string
  ownerId: string (operator user ID)
  
  // Lifecycle
  createdAt: ISO8601 timestamp
  modifiedAt: ISO8601 timestamp
  expiresAt?: ISO8601 timestamp (optional, for auto-cleanup)
  
  // Content
  title: string (required, max 256 chars)
  description: string (optional, max 1000 chars)
  
  // Configuration
  config: {
    mappings: WorkspaceMapping[]
    secrets: WorkspaceSecret[]
  }
  
  // State
  status: 'draft' | 'under_review' | 'approved' | 'rejected' | 'archived'
  
  // Versioning
  versionNumber: integer (auto-increment)
  parentVersionId?: string (for branching)
  
  // Relationships
  attachedReviewId?: string (reference to Review entity)
  
  // Metadata
  tags?: string[] (for organization)
  checksum: string (SHA256 of config for integrity)
}
```

### Draft Relationship Diagram

```
Operator
  ├─ owns many Drafts
  └─ creates Drafts

Draft
  ├─ has one parent Draft (previous version)
  ├─ has many child Drafts (branches)
  ├─ belongs to one Workspace (logical, not stored)
  └─ may have one Review

Review
  ├─ references one Draft
  └─ has many Approvals

Approval
  ├─ belongs to one Review
  └─ references one Reviewer (Operator)
```

---

## Lifecycle

### Draft States

```
┌─────────┐
│  DRAFT  │ ← Created (initial state)
└────┬────┘
     │
     ├─→ UNDER_REVIEW ← operator initiates review
     │   └─→ APPROVED ← all approvals pass
     │       └─→ (ready for execution)
     │
     └─→ REJECTED ← operator rejects
         └─→ (archived or deleted)
     
     └─→ ARCHIVED ← operator archives (cleanup)
```

### State Transitions

| From | To | Trigger | Requirements |
|------|----|---------|----|
| draft | under_review | operator clicks "Create Review" | None |
| under_review | approved | review checklist passes + approvals complete | validationPassed AND noCriticalErrors AND operatorApproved |
| under_review | rejected | operator rejects | operator provides reason |
| any | archived | operator archives | at least 30 days old (configurable) |
| archived | (deleted) | cleanup job runs | after 90 days (configurable) |

---

## Storage Requirements

### Per-Draft Storage

```
Typical draft:
- Metadata: ~500 bytes
- Configuration (100 mappings, 50 secrets): ~15 KB
- Version history pointer: ~100 bytes
- Total per draft: ~16 KB
```

### Retention Policy

| Draft Status | Retention | Reason |
|------|-----------|--------|
| draft | Until explicit deletion or 30 days | Temporary work |
| under_review | Until approval/rejection + 7 days | Active process |
| approved | 90 days minimum, then archive | Audit trail |
| rejected | 30 days, then delete | Reference only |
| archived | 1 year minimum | Compliance |

### Storage Estimates

```
Small environment (100 containers):
- Average draft size: 10 KB
- Drafts per operator per month: ~50
- Monthly storage: 500 KB per operator

Medium environment (500 containers):
- Average draft size: 50 KB
- Drafts per operator per month: ~50
- Monthly storage: 2.5 MB per operator

Large environment (1000+ containers):
- Average draft size: 100 KB
- Drafts per operator per month: ~50
- Monthly storage: 5 MB per operator
```

---

## Versioning

### Version History

```
Draft (v1) ← created
  ↓ operator modifies
Draft (v2) ← auto-saved
  ↓ operator changes status
Draft (v2.1) ← metadata update (no version bump)
  ↓ operator modifies config
Draft (v3) ← config changed
```

### Version Storage Strategy

**Option A: Full Copy**
- Every version stored completely
- Storage: O(n) where n = number of versions
- Retrieval: Fast (direct read)
- Recommended for <100 versions

**Option B: Deltas (Recommended)**
- Store only changes between versions
- Storage: O(1) per version after first
- Retrieval: Reconstruct from delta chain
- Recommended for >100 versions

### Chosen Strategy: Hybrid

- First 10 versions: Full copy
- Version 11+: Delta compression
- Automatic cleanup of old deltas after 1 year
- Full copy restore point every 100 versions

---

## Branching (Future)

```
v1: initial draft
├─ v2: modification A
│  ├─ v3: further change to A
│  └─ v3b: alternate direction (branch)
│
└─ v2b: modification B (parallel)
   └─ v3c: merge from v3 + v2b
```

Branching enables:
- Parallel exploration of changes
- Safe experimentation
- Three-way merging
- Branch reconciliation

---

## Query Access Patterns

### Primary Queries (Must Support)

```
Q1: Get draft by ID
SELECT * FROM drafts WHERE id = ?

Q2: List operator's drafts
SELECT * FROM drafts 
WHERE ownerId = ? 
ORDER BY modifiedAt DESC

Q3: Get draft version history
SELECT * FROM draft_versions 
WHERE draftId = ? 
ORDER BY versionNumber DESC

Q4: Find drafts by status
SELECT * FROM drafts 
WHERE status = ? 
ORDER BY modifiedAt DESC

Q5: Get drafts with attachedReview
SELECT * FROM drafts 
WHERE attachedReviewId = ?
```

### Secondary Queries (Nice to Have)

```
Q6: Search drafts by title/description
SELECT * FROM drafts 
WHERE title LIKE ? 
ORDER BY modifiedAt DESC

Q7: Get drafts created today
SELECT * FROM drafts 
WHERE DATE(createdAt) = DATE(NOW())

Q8: Get expired drafts (cleanup)
SELECT * FROM drafts 
WHERE expiresAt < NOW() 
AND status IN ('draft', 'rejected')
```

---

## Concurrency Handling

### Conflict Scenarios

1. **Simultaneous edits from same operator**
   - Solution: Last-write-wins with version counter
   - Detection: Compare versionNumber before update
   - Resolution: Reload draft, reapply edits

2. **Draft locked for review**
   - Solution: Read-only mode during review
   - Detection: Check attachedReviewId
   - Resolution: Wait for review completion or discard draft

3. **Deletion during review**
   - Solution: Prevent deletion while review active
   - Detection: Check attachedReviewId
   - Resolution: Archive instead of delete

### Optimistic Locking

```
Update Operation:
1. Load draft (versionNumber = 5)
2. Modify draft
3. Save with condition: WHERE versionNumber = 5
4. If update succeeds: increment versionNumber to 6
5. If fails (version changed): conflict detected
   → Reload and retry or notify operator
```

---

## Integrity & Safety

### Data Integrity

1. **Checksums**
   - Calculate SHA256 of config
   - Store with draft
   - Verify on retrieval
   - Detect corruption early

2. **Referential Integrity**
   - Prevent deletion of draft with active review
   - Cascade cleanup of old versions
   - Cascade archive of orphaned drafts

3. **Audit Trail**
   - Every state change logged
   - Immutable log entries
   - Correlate with approval events

### Deletion Safety

```
Soft Delete Strategy:
- Draft marked as deleted (status = 'archived')
- Not actually removed from storage
- Recoverable for 90 days
- Hard delete after retention period

Hard Delete:
- Remove from storage completely
- Only after retention expires
- Log deletion event
- Verify no references remain
```

---

## API Surface (Future Phase 4.0)

### Create Draft
```
POST /api/drafts
{
  "title": "Fix unmanaged secrets",
  "description": "Add mappings for PostgreSQL container",
  "config": { ... }
}
→ 201 Created { draft with id, versionNumber: 1 }
```

### Update Draft
```
PUT /api/drafts/{id}
{
  "title": "...",
  "config": { ... }
}
→ 200 OK { updated draft, versionNumber incremented }
```

### Get Draft
```
GET /api/drafts/{id}
→ 200 OK { full draft object }
```

### List Drafts
```
GET /api/drafts?status=draft&sort=-modifiedAt
→ 200 OK { array of drafts }
```

### Get Version History
```
GET /api/drafts/{id}/versions
→ 200 OK { array of versions with summaries }
```

### Archive Draft
```
POST /api/drafts/{id}/archive
→ 200 OK { draft with status: archived }
```

### Delete Draft
```
DELETE /api/drafts/{id}
→ 204 No Content
```

---

## Technical Constraints

### Backward Compatibility

- Schema migrations must support N-1 version reading
- Old draft formats must deserialize correctly
- Checksums must handle schema evolution

### Single-Binary Constraint

- All draft storage embedded in binary directory
- No external database required
- Migrations run on startup
- Data isolated per deployment environment

### Embedded Dashboard Constraint

- Draft persistence accessible to frontend
- No server-side computation required for drafts
- All filtering/sorting done client-side (for small N)
- Export drafts without server round-trip

---

## Security Considerations

### Access Control

- Drafts scoped to owner (operator)
- Cross-operator draft access requires explicit sharing
- Review creation requires draft owner or admin
- Delete requires draft owner or admin

### Data Protection

- Sensitive values never stored in drafts
- Only structure/relationships stored
- Encryption at rest (Phase 4.0+)
- TLS for transit (existing)

### Audit Requirements

- Every state change logged with timestamp/operator
- Immutable audit entries
- Queryable audit trail
- Export audit logs for compliance

---

## Implementation Phases

### Phase 4.0A (Current)
- ✅ Define data model
- ✅ Design storage schema
- ✅ Define query patterns
- ✅ Design conflict handling
- ✅ Document retention policies

### Phase 4.0 (Future - with chosen persistence tech)
- Implement Draft entity storage
- Implement versioning
- Implement conflict detection
- Create migration scripts
- Add API endpoints

### Phase 4.1 (Future)
- Branching support
- Advanced merging
- Draft templates
- Shared drafts

---

## Open Design Questions

1. **Branching vs. Linear Versioning?**
   - Recommendation: Start linear, design for branching later

2. **Retention Policy Tunable?**
   - Recommendation: Yes, expose as configuration

3. **Draft Templates?**
   - Recommendation: Future feature, not in 4.0A

4. **Cross-operator Sharing?**
   - Recommendation: Future feature, not in 4.0A

5. **Encryption at Rest?**
   - Recommendation: Phase 4.0+, not 4.0A design

---

## References

- See: `review-persistence.md` (linked reviews)
- See: `audit-logging.md` (audit trail)
- See: `persistence-options.md` (storage technology)
- See: `security-model.md` (encryption/access control)

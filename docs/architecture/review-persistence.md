# Review Persistence Model

**Phase:** 4.0A (Architecture Design)  
**Status:** Design Phase  
**Purpose:** Define how review workflows and approvals are persisted

---

## Overview

Review persistence enables:
- Multi-step approval workflows
- Audit trail of all decisions
- Rollback of approval chains
- Historical review records
- SLA tracking

Current state: Ephemeral (lost on page refresh)  
Target state: Persistent audit trail

---

## Data Model

### Review Entity

```
Review {
  id: string (UUID)
  draftId: string (references Draft)
  
  // Lifecycle
  createdAt: ISO8601 timestamp
  createdBy: string (operator ID)
  modifiedAt: ISO8601 timestamp
  closedAt?: ISO8601 timestamp
  
  // Metadata
  title: string (inherited from Draft + custom)
  description: string
  
  // State
  status: 'draft' | 'under_review' | 'approved' | 'rejected' | 'expired'
  
  // Checklist (immutable snapshot from draft)
  checklist: {
    validationPassed: boolean
    noCriticalErrors: boolean
    noMissingDependencies: boolean
    noProviderConflicts: boolean
    operatorApproved: boolean
  }
  
  // Risk (immutable snapshot from draft)
  riskAssessment: {
    score: 0-100
    level: 'low' | 'medium' | 'high' | 'critical'
    factors: { ... }
    explanation: string
  }
  
  // Relationships
  approvals: Approval[] (ordered by sequence)
  activities: ReviewActivity[] (ordered by timestamp)
  
  // Configuration
  requiredApprovals: integer (how many reviewers needed)
  approvalTimeoutHours: integer (optional SLA)
}
```

### Approval Entity

```
Approval {
  id: string (UUID)
  reviewId: string (references Review)
  
  // Who
  reviewerId: string (operator ID)
  reviewerName: string (denormalized for audit)
  
  // When
  createdAt: ISO8601 timestamp
  decidedAt?: ISO8601 timestamp
  
  // Decision
  decision: 'pending' | 'approved' | 'rejected' | 'abstained'
  comments?: string (max 2000 chars)
  
  // Rationale
  rejectionReason?: string (if rejected)
  
  // Sequence
  approvalSequence: integer (1, 2, 3...)
  isRequired: boolean (vs. optional)
}
```

### ReviewActivity Entity

```
ReviewActivity {
  id: string (UUID)
  reviewId: string (references Review)
  
  // Timeline
  timestamp: ISO8601 timestamp
  
  // What happened
  type: 'review_created' 
       | 'validation_performed'
       | 'approval_requested'
       | 'approval_given'
       | 'approval_rejected'
       | 'status_changed'
       | 'comment_added'
       | 'review_closed'
  
  // Who
  actorId: string (operator ID)
  
  // Details
  description: string
  metadata?: object (event-specific data)
}
```

---

## Workflow State Machine

### Simple Workflow (Single Approver)

```
┌─────────────────┐
│ Draft Created   │
└────────┬────────┘
         │
    Review.create(draftId)
         │
┌────────▼──────────────────┐
│ DRAFT                      │
│ - No approvals yet         │
│ - Review created           │
└────────┬──────────────────┘
         │
    Review.startReview()
         │
┌────────▼──────────────────────┐
│ UNDER_REVIEW                   │
│ - Approval(1) pending          │
│ - SLA timer started (if set)   │
└────────┬──────────────────────┘
         │
    Approval.decide()
     /           \
    /             \
APPROVED        REJECTED
  │                 │
  ▼                 ▼
APPROVED         REJECTED
(final)          (final)
```

### Complex Workflow (Multi-Step)

```
DRAFT
  ↓ startReview()
UNDER_REVIEW
  ├─ Approval(1) pending → approved ✓
  ├─ Approval(2) pending → approved ✓
  └─ Approval(3) pending → approved ✓
  ↓ all approvals complete
APPROVED

OR at any step:
  ├─ any Approval(n) → rejected ✗
  ↓ 
REJECTED
```

---

## Approval Workflow Patterns

### Pattern 1: Unanimous Approval

```
Requirement: All approvers must approve
Implementation:
  - requiredApprovals = approvalCount
  - Every approval must be 'approved'
  - If ANY is 'rejected': review is rejected
```

### Pattern 2: Majority Approval

```
Requirement: Majority must approve
Implementation:
  - requiredApprovals = ceil(approvalCount / 2)
  - Count 'approved' decisions
  - If count >= requiredApprovals: review is approved
```

### Pattern 3: First-to-Reject (High Risk)

```
Requirement: ANY rejection blocks
Implementation:
  - requiredApprovals = approvalCount
  - If ANY is 'rejected': immediately reject
  - Prevents wasting time on remaining approvals
```

### Pattern 4: Sequential (Escalation)

```
Requirement: Approvals in order (e.g., team lead, manager)
Implementation:
  - approvalSequence = 1, 2, 3...
  - Only request next approval when current approved
  - Timeout if approval SLA exceeded
```

---

## Data Relationships

```
Operator (1) ─── owns ─── (many) Review
              \
               └─ creates ─── (many) ReviewActivity

Draft (1) ────── references ──── (1) Review

Review (1) ────── has ────── (many) Approval
Review (1) ────── records ─── (many) ReviewActivity

Approval (many) ──── owned by ──── (1) Review

ReviewActivity (many) ──── belongs to ──── (1) Review
```

---

## Storage Requirements

### Per-Review Storage

```
Review metadata: ~1 KB
Checklist snapshot: ~500 bytes
Risk assessment snapshot: ~500 bytes
Activities (typical 10): ~5 KB
Approvals (typical 3): ~2 KB
Total per review: ~10 KB
```

### Retention Policy

| Review Status | Retention | Reason |
|------|-----------|--------|
| draft | Until closure + 7 days | Active work |
| under_review | Until closure + 30 days | Active process |
| approved | 1 year minimum | Audit compliance |
| rejected | 90 days minimum | Reference |
| expired | 7 days then delete | Automatic cleanup |

---

## Query Access Patterns

### Primary Queries (Must Support)

```
Q1: Get review by ID
SELECT * FROM reviews WHERE id = ?

Q2: Get review's approvals (ordered)
SELECT * FROM approvals 
WHERE reviewId = ? 
ORDER BY approvalSequence

Q3: List reviews for operator
SELECT * FROM reviews 
WHERE createdBy = ? 
ORDER BY modifiedAt DESC

Q4: Get review activity timeline
SELECT * FROM reviewActivities 
WHERE reviewId = ? 
ORDER BY timestamp DESC

Q5: Find reviews awaiting approval
SELECT * FROM reviews 
WHERE status = 'under_review'
  AND (SELECT COUNT(*) FROM approvals 
       WHERE reviewId = reviews.id 
         AND decision = 'pending') > 0
```

### Secondary Queries

```
Q6: Get pending approvals for reviewer
SELECT r.*, a.* FROM reviews r
  JOIN approvals a ON r.id = a.reviewId
WHERE a.reviewerId = ? 
  AND a.decision = 'pending'
ORDER BY a.createdAt

Q7: Get reviews with pending SLA
SELECT * FROM reviews 
WHERE status = 'under_review'
  AND approvalTimeoutHours IS NOT NULL
  AND (NOW() - createdAt) > (approvalTimeoutHours * 3600)

Q8: Approval metrics by reviewer
SELECT reviewerId, COUNT(*), 
       SUM(CASE WHEN decision = 'approved' THEN 1 ELSE 0 END)
FROM approvals
WHERE decidedAt BETWEEN ? AND ?
GROUP BY reviewerId
```

---

## Approval Rules Engine

### Decision Logic

```
CanApproveReview(review):
  return review.status == 'under_review'
    AND NO_PENDING_APPROVAL_BEFORE_THIS_ONE(review)

CanCompleteReview(review):
  approvalsDone = COUNT(approvals WHERE decision != 'pending')
  return approvalsDone >= review.requiredApprovals
    OR ANY(approvals WHERE decision == 'rejected')

ReviewShouldAutoApprove(review):
  return COUNT(approvals WHERE decision == 'approved')
      >= review.requiredApprovals

ReviewShouldAutoReject(review):
  return ANY(approvals WHERE decision == 'rejected')
    AND review.status != 'rejected'
```

---

## Concurrency Handling

### Race Condition: Multiple Approvers

**Scenario:** Two approvers approve simultaneously

**Handling:**
```
Approval 1: approved at 10:00:01
Approval 2: approved at 10:00:02

Both queries check: COUNT(approved) >= requiredApprovals
Both see: 1 approved (themselves)
Both try to set review.status = 'approved'

Solution: Use transaction with count-before-update
  BEGIN
    SELECT COUNT(*) FROM approvals 
    WHERE reviewId = ? AND decision = 'approved'
    IF count >= required:
      SET review.status = 'approved'
  COMMIT
```

### Race Condition: Approval After Status Check

**Scenario:** Review closes while approval in-flight

**Handling:**
```
Check review.status = 'under_review'
  ↓ (review changes to 'approved' by another user)
Add approval decision
  ↓ CONFLICT: review is no longer under_review

Solution: Prevent approval updates to closed reviews
  UPDATE approvals 
  SET decision = 'approved'
  WHERE reviewId = ? 
    AND (SELECT status FROM reviews WHERE id = ?) = 'under_review'
  ← condition fails, update rejected
```

---

## Audit Requirements

### Immutable Audit Trail

```
Every change must log:
- Timestamp (UTC)
- Actor (operator ID + name)
- Action (what changed)
- Old value (if applicable)
- New value (if applicable)
- Correlation ID (for request tracing)
```

### Event Categories

| Category | Events | Details |
|----------|--------|---------|
| Lifecycle | created, started, closed | When, by whom |
| Approvals | requested, approved, rejected | Who decided, when |
| SLA | triggered, violated, waived | Timeout handling |
| Audit | viewed, exported, analyzed | Access tracking |

---

## Integration with Draft Lifecycle

### Review Blocks Draft Modification

```
Draft states:
- draft: free to modify
- under_review: READ-ONLY (approval in progress)
- approved: READ-ONLY (can be executed)
- rejected: can re-open for new review
- archived: READ-ONLY (retention)

Enforcement:
PUT /api/drafts/{id} 
  ← fails if draft.status != 'draft'
```

### Review-Draft Coupling

```
Draft lifecycle drives Review lifecycle:

Draft created → Review.create()
Review started → Draft becomes read-only
Review approved → Draft.status = 'approved'
Review rejected → Draft.status = 'rejected'
Draft executed → Review.status = 'completed'
```

---

## SLA & Timeouts

### Approval SLA

```
Review.approvalTimeoutHours = 24

Created: 2026-06-05 10:00
Deadline: 2026-06-06 10:00

Status monitoring:
- Monitor: (NOW() - createdAt) > timeoutHours
- Action: Auto-escalate or auto-reject (configurable)
- Notification: Send reminder before deadline
```

### Expiration

```
Review auto-expires if:
- status = 'draft' AND modifiedAt < 30 days ago
- status = 'under_review' AND no decision for 7 days

Expired reviews are:
- Marked status = 'expired'
- No longer modifiable
- Retained for 7 days then deleted
```

---

## Security Considerations

### Approval Authority

```
Who can approve?
- Only assigned approvers can decide
- Cannot self-approve own draft review
- Admin can override, logs as special audit event

Authorization check on every approval update:
  IF current_user == draft.owner:
    DENY (conflict of interest)
  IF current_user NOT IN review.approvers:
    DENY (not authorized)
```

### Audit Integrity

```
Approval immutability:
- Decisions cannot be changed once made
- Only way to re-vote: cancel review and create new one
- All changes logged with timestamp + actor

Signature/verification (future):
- Sign approval decisions cryptographically
- Prevents tampering with approval history
- Required for compliance (SOC2, HIPAA)
```

---

## Implementation Phases

### Phase 4.0A (Current)
- ✅ Define Review entity schema
- ✅ Define Approval entity schema
- ✅ Define workflow state machine
- ✅ Design query patterns
- ✅ Design audit requirements

### Phase 4.0 (Future - with chosen persistence tech)
- Implement Review storage
- Implement Approval storage
- Implement audit logging
- Create workflow rules engine
- Implement SLA monitoring

### Phase 4.1+ (Future)
- Multi-level approval chains
- Custom approval workflows
- Role-based approval rules
- Approval delegation
- Approval analytics/metrics

---

## Open Design Questions

1. **Sequential vs. Parallel Approvals?**
   - Recommendation: Start with sequential, design for parallel

2. **Self-approval allowed?**
   - Recommendation: No (conflict of interest)

3. **Approval override by admin?**
   - Recommendation: Yes, with special audit event

4. **Timeout action: escalate or reject?**
   - Recommendation: Configurable, default to escalate

5. **Can approvers see each other's votes?**
   - Recommendation: Yes, with timestamps (transparency)

---

## References

- See: `draft-persistence.md` (linked drafts)
- See: `audit-logging.md` (audit trail requirements)
- See: `security-model.md` (authorization/signatures)
- See: `migration-plan.md` (phase progression)

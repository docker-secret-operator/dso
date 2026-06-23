# Recommendations — P8

## Purpose

P8 provides a deterministic, evidence-based advisory layer. Every recommendation is derived from observable system state (version history, drift findings, compliance records, policy rules). There is no AI, no LLM, no autonomy, and no forecasting — only facts.

**Key invariant:** A recommendation disappears the moment the underlying problem is resolved. Its ID is computed deterministically from the evidence, so if the evidence goes away, the ID will not appear in the next evaluation.

---

## Architecture

```
┌────────────────────────────────────────────┐
│             insights.Evaluator              │  (internal/insights/evaluator.go)
│  complianceEngine + driftStore + policyStore│
└─────────────────┬──────────────────────────┘
                  │ EvaluateAll()
                  ▼
        []*recommendation.Recommendation      │  (internal/recommendation/recommendation.go)
                  │
                  ▼
        api.RecommendationHandler              │  (internal/api/recommendation_handler.go)
        GET /api/recommendations
```

The `Evaluator` sits in `internal/insights/` — a separate package from `recommendation` and `compliance` to prevent import cycles (`sqlite` imports `recommendation`; `compliance` imports `sqlite`; `insights` imports both freely).

---

## Recommendation Rules

### Rotation Rules

| ID pattern | Trigger | Severity |
|------------|---------|----------|
| `rotation:never:{secretName}` | No entry in `secret_versions` | High |
| `rotation:overdue:{secretName}` | `next_rotation` timestamp is in the past | High |

### Drift Rules

| ID pattern | Trigger | Severity |
|------------|---------|----------|
| `drift:open:{secretName}` | Per-secret open drift count > 0 (from compliance) | High |
| `drift:finding:{findingId}` | Individual `DriftFinding` with `status == "detected"` | Critical (if severity=critical/high), High (medium) |

### Compliance Rules

| ID pattern | Trigger | Severity |
|------------|---------|----------|
| `compliance:noncompliant:{secretName}` | `OverallStatus == "non_compliant"` | Medium |

### Policy Rules

| ID pattern | Trigger | Severity |
|------------|---------|----------|
| `policy:disabled:{ruleId}` | Critical-severity policy rule is disabled | Critical |

---

## Severity Mapping

| Recommendation Priority | Meaning |
|------------------------|---------|
| `critical` | Immediate action required — disabled critical policy, critical drift |
| `high` | Significant risk — never rotated, overdue rotation, open drift |
| `medium` | Needs attention but not urgent — non-compliant status |
| `low` | Informational (currently unused by deterministic rules) |

---

## Disappear-When-Fixed Semantics

Recommendations are never persisted by the `Evaluator`. They are computed fresh on every `GET /api/recommendations` call. Example:

1. Secret `db-password` has no versions → `rotation:never:db-password` appears.
2. Operator rotates the secret → a `SecretVersion` row is written.
3. Next call to `GET /api/recommendations` → `EvaluateAll()` runs, finds a version entry, does not emit `rotation:never:db-password`. The recommendation is gone.

No cleanup, no state machine, no TTL.

---

## Evidence Chain

Each recommendation carries `reason` (a human-readable evidence statement), `resource` (the affected secret/rule name), and optional cross-links:

| Field | Points to |
|-------|-----------|
| `driftId` | A specific `DriftFinding.ID` in `/api/drift` |
| `policyId` | A `PolicyRule.ID` in `/api/policies` |
| `auditId` | An `AuditEvent.ID` in `/api/audit` |

---

## API

### `GET /api/recommendations`

Query params:
- `severity` — filter by priority (`critical`, `high`, `medium`, `low`)
- `category` — filter by category (`rotation`, `drift`, `compliance`, `policy`, `operational`)
- `page` — page number (default 1)
- `pageSize` — items per page (default 50, max 200)

Response:
```json
{
  "recommendations": [
    {
      "id": "rotation:never:db-password",
      "title": "Rotate db-password",
      "description": "This secret has never been rotated.",
      "reason": "No rotation history exists. There is no evidence this secret has ever changed.",
      "resource": "db-password",
      "priority": "high",
      "category": "rotation",
      "status": "open",
      "suggested_action": "Perform an initial rotation to establish a baseline version record.",
      "confidence": 1.0,
      "created_at": 1750640000
    }
  ],
  "count": 1,
  "total": 1,
  "page": 1,
  "pageSize": 50
}
```

### `GET /api/recommendations/metrics`

Legacy store-based metrics (total/open/acknowledged/implemented/dismissed counts).

### `POST /api/recommendations/:id/acknowledge`

Marks a store-based recommendation as acknowledged. Live evaluator recommendations are stateless — acknowledgement applies to the legacy store.

### `POST /api/recommendations/:id/dismiss`

Dismisses a recommendation from the store.

---

## Frontend

**`/recommendations` page** — full list with:
- Priority and category filter dropdowns
- Status tabs (open / implemented / dismissed)
- Click any row to open the detail drawer

**Detail drawer** shows:
- Priority badge + category badge
- Title and description
- **Evidence block** (`reason` field) — explains *why* this rec exists
- Suggested action
- Resource / confidence / status metadata
- Cross-links to `/drift`, `/policies`, `/audit` with the linked entity ID
- Acknowledge / Dismiss actions (for open recs)

**Dashboard integration** — Critical and High live recommendations surface in the "Needs Attention" section, capped at the existing `MAX_ATTENTION_ITEMS` limit. They are de-duplicated against existing drift/overdue items.

---

## Limitations

1. **Live recs are stateless.** Acknowledging a live recommendation (`rotation:never:db-password`) only works via the legacy store endpoint. If the underlying evidence remains, the rec will re-appear on the next evaluation unless the problem is actually fixed.

2. **`RotationOverdue` is currently never triggered.** The config's `Rotation.Enabled` field does not carry an interval. Until rotation scheduling is added to config, `next_rotation` is always zero, so `RotationOverdue` is never emitted. All rotated secrets are reported as `compliant`.

3. **No operational recommendations yet.** `CategoryOperational` exists but no rule currently emits it.

4. **Evaluate is O(secrets × drift)** per request. Large deployments should cache at the API gateway layer.

# Bulk Operations

Operators managing large secret estates should never need one click per secret.
P5 adds multi-select checkboxes and batch action toolbars to three pages.

---

## Supported actions

| Page | Action | Endpoint | Safety gate |
|------|--------|----------|-------------|
| Secrets | Bulk rotate | `POST /api/secrets/bulk-rotate` | Confirm if count > 50 |
| Drift | Bulk acknowledge | `POST /api/drift/bulk-ack` | None |
| Drift | Bulk resolve | `POST /api/drift/bulk-resolve` | None |
| Policies | Bulk enable | `POST /api/policies/bulk-enable` | None |
| Policies | Bulk disable | `POST /api/policies/bulk-disable` | Always confirm |
| Policies | Bulk delete | `POST /api/policies/bulk-delete` | Always confirm |

---

## Request / response format

### Secrets

```http
POST /api/secrets/bulk-rotate
Authorization: Bearer <token>
Content-Type: application/json

{"names": ["db-password", "api-key", "tls-cert"]}
```

```json
{
  "success": 2,
  "failed": 1,
  "failures": [
    {"name": "tls-cert", "error": "not configured"}
  ]
}
```

### Drift

```http
POST /api/drift/bulk-ack
POST /api/drift/bulk-resolve
Authorization: Bearer <token>
Content-Type: application/json

{"ids": ["drift_stalesecret_db-password", "drift_missingsecret_api-key"]}
```

```json
{
  "success": 2,
  "failed": 0,
  "failures": []
}
```

### Policies

```http
POST /api/policies/bulk-enable
POST /api/policies/bulk-disable
POST /api/policies/bulk-delete
Authorization: Bearer <token>
Content-Type: application/json

{"ids": ["rule-abc", "rule-def"]}
```

```json
{
  "success": 1,
  "failed": 1,
  "failures": [
    {"id": "rule-def", "error": "rule not found: rule-def"}
  ]
}
```

---

## Failure behavior

- All batch endpoints are **non-aborting**: every item in the list is attempted regardless of prior failures.
- The response always includes the full `failures` array with one entry per failed item.
- The UI surfaces failures inline in the bulk toolbar after the call settles:
  `13 succeeded · 1 failed: database-password`
- A failed item is never silently dropped.

---

## Audit events

| Action | Event name | Details |
|--------|-----------|---------|
| Bulk rotate | `bulk.rotate` | `success=N failed=M` |

Drift and policy bulk actions do not generate audit events. Each individual engine operation may have its own internal logging.

---

## Safety gates

| Condition | Behavior |
|-----------|----------|
| Bulk rotate > 50 secrets | Confirmation modal shown before dispatch |
| Bulk disable policies (any count) | Confirmation modal always shown |
| Bulk delete policies (any count) | Confirmation modal always shown |
| Bulk ack / resolve findings | No confirmation required |
| Bulk enable policies | No confirmation required |

---

## Known limits

- No upper bound on batch size is enforced server-side. The backend processes items sequentially.
- Very large batches (> 500 items) may time out depending on provider response times. Split into smaller batches if needed.
- Selection is cleared on clear-button click; navigating pages does not clear the selection.

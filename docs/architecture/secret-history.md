# Secret History — P6

## Overview

P6 makes every secret a first-class historical object. An operator at 2 AM can answer
"what changed, when, who did it, which containers were affected, and was drift resolved?"
without leaving the secret drawer.

---

## Schema

### `secret_versions` table (migration 0030)

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | `{secretName}-{unixNano}` |
| `secret_name` | TEXT | Secret identifier |
| `version` | INTEGER | Auto-increment per secret (1, 2, 3 …) |
| `provider` | TEXT | Provider name at time of rotation |
| `hash` | TEXT | Cryptographic hash of the secret value (never the value itself) |
| `rotated_by` | TEXT | Actor username, or `"system"` for automated rotations |
| `rotation_source` | TEXT | One of: `manual`, `bulk_rotate`, `scheduler`, `provider_sync` |
| `execution_id` | TEXT | Linked execution ID (if available) |
| `created_at` | TIMESTAMP | When the version was created |

Unique constraint on `(secret_name, version)` prevents duplicate version numbers.

**Secret values are never stored.** Only cryptographic hashes + metadata.

---

## Rotation Sources

| Source | Description |
|--------|-------------|
| `manual` | Operator clicked Rotate in the UI or called `POST /api/secrets/:name/rotate` |
| `bulk_rotate` | Included in a `POST /api/secrets/bulk-rotate` batch |
| `scheduler` | Triggered by the internal scheduler job |
| `provider_sync` | External provider pushed a `POST /api/events/secret-update` webhook |

---

## APIs

### `GET /api/secrets/:name/history`

Returns version list for a secret.

```json
{
  "currentVersion": 8,
  "versions": [
    {
      "version": 8,
      "createdAt": "2026-06-23T02:14:00Z",
      "rotatedBy": "scheduler",
      "rotationSource": "scheduler",
      "provider": "vault-prod",
      "executionId": "421"
    }
  ]
}
```

### `GET /api/secrets/:name/timeline`

Returns a unified chronological event stream (newest first) merging:
- Rotation versions (`type: "rotation"`)
- Audit events referencing the secret (`type: "audit"`)
- Drift findings where `Resource == secretName` (`type: "drift"`)

```json
[
  {
    "type": "rotation",
    "timestamp": "2026-06-23T02:14:00Z",
    "description": "Secret rotated",
    "version": 8,
    "actor": "scheduler",
    "source": "scheduler"
  },
  {
    "type": "drift",
    "timestamp": "2026-06-23T02:15:30Z",
    "description": "version_mismatch: container hash does not match current version",
    "driftId": "abc123"
  }
]
```

### `GET /api/secrets/:name/diff?v1=7&v2=8`

Compares two version records. **Never exposes secret values.** Returns metadata diff only.

```json
{
  "v1": 7,
  "v2": 8,
  "providerChanged": false,
  "rotationSourceChanged": true,
  "executionChanged": true,
  "hashChanged": true,
  "containersAffected": 3,
  "v1RotatedBy": "admin",
  "v2RotatedBy": "scheduler",
  "v1CreatedAt": "2026-06-22T18:00:00Z",
  "v2CreatedAt": "2026-06-23T02:14:00Z"
}
```

`containersAffected` counts drift findings for this secret detected between the two
version timestamps — a proxy for "how many containers saw the change."

---

## Timeline Model

The timeline merges three independent event streams, sorted newest-first:

```
rotation events  ─┐
audit events     ─┼──► sort by timestamp DESC ──► unified timeline
drift events     ─┘
```

Each event carries a `type` field (`rotation` | `audit` | `drift`) so the UI can
render them distinctly. Cross-link fields (`driftId`, `auditId`, `executionId`) let
operators navigate directly to the related record.

---

## Cross-links

```
Secret Drawer (History tab)
  └── version row → diff panel (inline)
  └── executionId → /operations?id=...

Secret Drawer (Timeline tab)
  └── drift event → /drift?id=...
  └── audit event → /audit?id=...

Secret Drawer (Overview tab)
  └── Containers count → /discovery?secret=...
  └── Recent Activity → /audit?q=...
```

Every link is within two clicks from the secret drawer.

---

## Diff Semantics

| Field | Meaning |
|-------|---------|
| `providerChanged` | The provider name changed between the two versions |
| `rotationSourceChanged` | The rotation was triggered differently (e.g. manual → scheduler) |
| `executionChanged` | A different execution record is linked |
| `hashChanged` | The secret value hash changed (only meaningful when both hashes are non-empty) |
| `containersAffected` | Drift findings detected on this secret between the two version timestamps |

---

## Limitations

1. **No retroactive history.** Versions are recorded from the moment P6 is deployed. Rotations that happened before the `secret_versions` table was created will not appear.

2. **Hash is best-effort.** The hash is pulled from the in-memory cache at rotation time. If the cache does not hold the value (e.g. the process restarted mid-rotation), `hash` will be empty and `hashChanged` will return `false` even if the value changed.

3. **`containersAffected` is approximate.** It counts drift *findings* in a time window, not actual container reinjections. A container that was reinjected without generating a drift finding will not be counted.

4. **Timeline does not include execution events** (yet). The execution service does not expose a "list executions by secret name" query. This is a known gap for a future phase.

5. **In-memory drift store.** If `dso` is run without a SQLite database, the drift store is in-memory and timeline drift events do not survive restarts.

---

## Recovery Workflow

When an operator is debugging an incident:

1. Open the secret drawer from `/secrets`
2. Check **Overview** — current version, provider, last rotated timestamp
3. Switch to **History** — click two versions to see the diff (what changed between rotations)
4. Switch to **Timeline** — see every rotation, audit action, and drift finding in chronological order
5. Click a drift event → navigates to `/drift` with the finding pre-filtered
6. Click an audit event → navigates to `/audit` with the record pre-filtered

The full incident chain (rotation → drift → resolution) is visible without leaving the UI.

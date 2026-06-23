# Compliance — P7

## Purpose

P7 makes DSO auditable. An auditor or operator should be able to answer
**"Can I prove what happened?"** without database access, through the API
and the secret drawer alone.

Compliance is **derived**, not stored. It is a live view over:
- `secret_versions` (P6) — rotation history
- `drift_findings` — open drift posture
- `audit_events` — who did what and when
- `policy_rules` — rule health (informational only; does not affect secret compliance score)

---

## Compliance Rules

### Rotation Status (four states)

| Status | Condition |
|--------|-----------|
| `compliant` | At least one version entry exists AND `next_rotation` is not past (or is unset) |
| `overdue` | Version history exists, but `next_rotation` timestamp has passed |
| `never_rotated` | No version entries in `secret_versions` — no evidence of rotation |
| `unknown` | `SecretVersionStore` is unavailable (e.g. running without SQLite) |

**`never_rotated` and `overdue` are operationally distinct.** `never_rotated` means no evidence exists at all. `overdue` means evidence exists but the rotation SLA has been violated. They are reported separately.

### Drift Status

| State | Condition |
|-------|-----------|
| `drift_free` | Zero open findings for this secret (`Status == "detected"`) |
| `has_drift` | One or more open findings |

### Overall Compliance Status

| Overall | Condition |
|---------|-----------|
| `compliant` | `RotationCompliant` AND `DriftFree` |
| `warning` | `RotationOverdue` OR `RotationUnknown` AND `DriftFree` |
| `non_compliant` | `RotationNeverRotated` OR has open drift findings |

**Policy state is NOT factored into the overall compliance score.** Policies are surfaced as a standalone report but do not distort the rotation+drift compliance signal.

---

## APIs

### `GET /api/compliance/summary`

Aggregate counts across all configured secrets.

```json
{
  "totalSecrets": 500,
  "compliant": 470,
  "warning": 20,
  "nonCompliant": 10
}
```

### `GET /api/compliance/secrets`

Paginated list of per-secret compliance records.

Query params: `status`, `provider`, `search`, `page`, `pageSize` (max 200).

```json
{
  "total": 500,
  "page": 1,
  "pageSize": 50,
  "items": [
    {
      "secretName": "db-password",
      "provider": "vault-prod",
      "rotationStatus": "compliant",
      "driftFree": true,
      "lastRotatedAt": "2026-06-23T02:14:00Z",
      "versionCount": 8,
      "openDriftFindings": 0,
      "auditEventCount": 42,
      "overallStatus": "compliant"
    }
  ]
}
```

### `GET /api/compliance/secrets/:name`

Per-secret compliance detail, used by the SecretDrawer Compliance tab.

```json
{
  "rotationStatus": "compliant",
  "openDrift": 0,
  "versionCount": 8,
  "auditCount": 42,
  "lastRotation": "2026-06-23T02:14:00Z",
  "overallStatus": "compliant"
}
```

### `GET /api/compliance/export?format=json|csv`

Flat export of compliance status for all secrets. Never includes secret values.

CSV columns:
```
secret, provider, rotation_status, version, open_drift, last_rotation, compliance_status
```

JSON example:
```json
[
  {
    "secret": "db-password",
    "provider": "vault-prod",
    "rotationStatus": "compliant",
    "version": 8,
    "openDrift": 0,
    "lastRotation": "2026-06-23T02:14:00Z",
    "complianceStatus": "compliant"
  }
]
```

---

## Reports

All reports are read-only, metadata-only exports. **Secret values are never included.**

### `GET /api/compliance/reports/rotation?format=json|csv`

Every recorded rotation event.

CSV columns: `secret_name, version, rotated_at, rotated_by, rotation_source, provider`

### `GET /api/compliance/reports/drift?format=json|csv`

All drift findings regardless of status.

CSV columns: `id, resource, type, severity, status, detected_at, description`

### `GET /api/compliance/reports/policy?format=json|csv`

All policy rules with current state.

CSV columns: `id, name, enabled, severity, last_run, last_result`

### `GET /api/compliance/reports/activity?format=json|csv`

All audit events across all secrets. The `execution_id` column links each action
to the execution that performed it — the core of the evidence chain.

CSV columns: `id, action, actor, resource_id, execution_id, timestamp, status`

---

## Evidence Chain

The P6+P7 evidence chain can reconstruct any incident:

```
Secret rotation triggered (audit event)
  └── execution_id: #421
  └── actor: scheduler
  └── timestamp: 2026-06-23T02:14:00Z

Version created (secret_versions)
  └── version: v8
  └── rotation_source: scheduler
  └── hash: sha256:abc...

Drift detected (drift_findings)
  └── resource: db-password
  └── type: version_mismatch
  └── detected_at: 2026-06-23T02:15:30Z
  └── status: detected

Drift acknowledged (audit event)
  └── action: drift.acknowledge
  └── actor: admin
  └── timestamp: 2026-06-23T02:20:00Z

Drift resolved (audit event)
  └── action: drift.resolve
  └── timestamp: 2026-06-23T02:25:00Z
```

All of this is queryable via the compliance API and the SecretDrawer timeline
without touching the database.

---

## SecretDrawer — Compliance Tab

The Compliance tab (4th tab in the drawer) shows three sections:

**Rotation**
- Status badge (Compliant / Overdue / Never Rotated / Unknown)
- Last rotation (relative time)
- Current version number

**Drift**
- Open findings count (green = 0, amber = >0)

**Evidence**
- Versions recorded count
- Audit events count
- Links to `/audit` and `/drift` for deeper investigation

---

## Export Semantics

- **JSON** exports are streaming-safe — the encoder writes directly to the response writer.
- **CSV** exports use standard RFC 4180 encoding.
- All timestamps are UTC ISO 8601.
- `execution_id` in the activity report is derived from `correlation_id` in the audit event, which is populated when the audit event originates from an execution.
- Boolean fields (e.g. `enabled` in policy report) are exported as `true`/`false` strings in CSV.

---

## Limitations

1. **Compliance is computed at query time.** Large installations with thousands of secrets and dense audit histories may see latency on `/api/compliance/summary`. Consider caching at the API gateway layer for high-traffic environments.

2. **`next_rotation` is not tracked in the current schema.** The `SecretMapping` config has a `Rotation.Enabled` flag but no `interval` field. As a result, `RotationOverdue` is never triggered — all rotated secrets are `compliant`. This will improve when a rotation schedule is added to config.

3. **Activity report has no pagination.** For installations with dense audit trails, filter by `resource_id` to limit results.

4. **Policy report reflects current rule state only.** Historical policy changes are visible in the audit trail but not in the policy report snapshot.

5. **`execution_id` in activity report is best-effort.** It is populated from `correlation_id` in the audit event. Events logged without a correlation ID will have an empty `execution_id`.

---

## Validation Scenarios

| Scenario | Expected Result |
|----------|----------------|
| Secret with no versions | `never_rotated` → `non_compliant` |
| Secret rotated once | `compliant` (no `next_rotation`) → `compliant` |
| Secret with open drift finding | `non_compliant` regardless of rotation status |
| Secret with drift resolved | `drift_free: true` → re-evaluate rotation |
| Policy rule disabled | Appears in policy report; does NOT affect secret compliance |
| DSO restart | `secret_versions` table preserved; compliance re-derived correctly |
| Bulk rotation | Each secret gets its own version entry with `rotation_source: bulk_rotate` |
| Scheduler rotation | `rotation_source: scheduler`; `rotated_by: system` |

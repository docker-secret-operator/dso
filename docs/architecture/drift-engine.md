# Drift Engine

Drift = desired state ≠ actual state. In DSO the desired state is what the secret provider holds right now; the actual state is what was last successfully injected into a container.

## Detection rules

| Type | Severity | Trigger |
|------|----------|---------|
| `missing_secret` | Critical | Secret is configured but not present in the provider cache (never fetched, or provider unreachable) |
| `missing_secret` | Critical | Secret is in the cache but there is no injection record — container may be running without it |
| `stale_secret` | High | Provider value changed since the last injection (provider hash ≠ injection-record hash) |

The `rotation_lag` type is defined in code but not currently emitted. It would require a per-secret rotation interval field, which does not exist in `RotationConfigV2`.

## Severity rules

| Severity | Meaning |
|----------|---------|
| Critical | Container is definitely out of sync or has no secret at all |
| High | Container is running a stale version; rotation happened but injection did not follow |

## Finding lifecycle

```
detected → acknowledged → resolved
    ↑___________|
    (rescan refreshes metadata but does not re-open acknowledged/resolved findings)
```

- **detected** — engine found a mismatch on this scan.
- **acknowledged** — operator saw it; it stays visible but is de-prioritised.
- **resolved** — operator confirmed it is fixed. Will not re-open on future scans (use "Run Scan" to verify manually).

Deterministic finding IDs (`drift_<type>_<secretname>`) prevent duplicates across scans. On rescan:
- If the finding is acknowledged or resolved: skip (do not re-open).
- If the finding is still detected: refresh description/metadata, preserve the original `DetectedAt`.
- If the condition is gone (hashes now match): the finding stays resolved/acknowledged — no automatic clearing. Operators manually resolve.

## Incremental scanning

`SecretVersionScanner` keeps a `lastScanState` map of `"provider:secretName" → providerHash`. If the provider hash is unchanged since the last scan, that secret is skipped entirely. This prevents unnecessary finding updates when nothing changed.

## Background scanning

The engine's `runLoop` fires a full scan every 1 hour. After a successful secret rotation via `POST /api/secrets/:name/rotate`, the server also triggers an immediate async `RunScan` so the `driftedCount` on the dashboard reflects the new state within seconds.

## Injection recording

When a rotation succeeds, the server calls `InjectionStore.RecordInjection(secretName, providerHash)`. This writes an upsert to the `injection_records` table:

```sql
CREATE TABLE injection_records (
    secret_name   TEXT NOT NULL PRIMARY KEY,
    provider_hash TEXT NOT NULL,
    injected_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

The scanner compares `InjectionRecord.ProviderHash` against the live cache hash to determine staleness.

## Cross-links

| Finding field | Links to |
|---------------|----------|
| `secret_name` | `/secrets?name=<secret_name>` |
| `container` | `/discovery?container=<container>` |

The drift page shows both columns as clickable links. The dashboard attention queue shows high/critical open findings with an href to `/drift`.

## Dashboard count

`GET /api/dashboard/posture` returns `driftedCount` = `engine.GetOpenCount()` (findings with status `detected`). This count is never derived from `status == error` — that is a separate `secretErrors` field.

## Known limitations

- No per-container injection tracking: `injection_records` is keyed by `secret_name`, not `(secret_name, container_name)`. A secret injected into multiple containers is treated as a single record.
- Automatic clearing: findings are never auto-resolved. An operator must click "Resolve" after confirming the fix.
- Rotation-lag detection is disabled: requires a per-secret `Interval` field that does not exist in the current config schema.
- Cache-miss gap: if a secret has never been fetched (e.g. the provider is temporarily unreachable at startup), it will show as `missing_secret/critical` until the cache is populated. This clears automatically on the next hourly scan once the provider is reachable.

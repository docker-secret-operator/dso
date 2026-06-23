# DSO Security Review — P11

This is a code-level security review of the DSO REST API, authentication layer, and data exposure
paths. It is not a penetration test. It covers what is verifiable from the source code alone.

---

## Scope

- RBAC enforcement on all API endpoints
- Secret value exposure in API responses
- Audit log integrity
- Export and report downloads
- `/api/status` exposure
- Config editing endpoints

Out of scope: network-level security, TLS configuration, host OS hardening, secret provider
credentials in transit.

---

## RBAC Model

Roles (from `internal/storage/types.go`):

| Role | Capabilities |
|---|---|
| `viewer` | Read-only access to all GET endpoints |
| `operator` | Read + rotate secrets |
| `reviewer` | Read + approve/reject |
| `approver` | Read + approve/reject + policy changes |
| `admin` | Full access including destructive operations |

RBAC is enforced at the handler level via `auth.CurrentUser(ctx)`. Every handler checks role before
executing mutations.

### Verified in Tests

| Endpoint | Test | Viewer | Admin |
|---|---|---|---|
| GET /api/recommendations | `TestRecommendationHandler_RBAC_ViewerCanRead` | ✅ 200 | ✅ 200 |
| POST /api/recommendations/:id/acknowledge | `TestRecommendationHandler_RBAC_ViewerCannotAcknowledge` | ✅ 403 | ✅ not 403 |
| GET /api/forecasts | `TestForecastHandler_RBAC_ViewerCanRead` | ✅ 200 | ✅ 200 |
| GET /api/recommendations (unauthenticated) | `TestRecommendationHandler_RBAC_UnauthRejected` | — | — |
| GET /api/forecasts (unauthenticated) | `TestForecastHandler_RBAC_UnauthRejected` | — | — |

---

## Secret Value Exposure

### API Responses

Secret values are never stored in the DSO database. DSO stores:
- Secret **names** (identifiers)
- Secret **metadata** (provider, rotation config, compliance state)
- **Version hashes** (for diff detection — not values)
- **Drift findings** (describe what changed, not the new value)

The only place a secret value appears is at rotation time, when DSO reads the new value from the
provider and writes it to the target. This path does not log or cache the value.

### Verified in Tests

- `TestRecommendationHandler_NoSecretValuesInResponse`: response body has no `"value"` field. ✅
- `TestForecastHandler_NoSecretValuesInResponse`: same. ✅

### Audit Logs

Audit entries record actor, action, resource name, and outcome — not secret values. This is
enforced by the `AuditEntry` struct in `internal/storage/types.go` which has no `Value` field.

---

## `/api/status` Exposure

`/api/status` is intentionally unauthenticated (to allow monitoring and health-check scripts
without credentials). It returns only:

- `last_recommendation_eval` — timestamp
- `last_forecast_eval` — timestamp
- `recommendation_eval_ms` — integer duration
- `forecast_eval_ms` — integer duration
- `recommendation_count` — integer
- `forecast_count` — integer
- `cache_ttl_seconds` — integer

No secret names, no resource names, no user information. This exposure level is acceptable.

---

## Config Editing

`POST /api/config/apply` is admin-only. It accepts a YAML config blob and applies it with
optimistic concurrency (version check prevents concurrent edits from silently overwriting each other).

**Risk:** A malicious admin could add a secret pointing to an arbitrary provider. This is a
trusted-admin risk — the same admin can already rotate secrets and read audit logs. It is not
a privilege escalation.

**Mitigation:** Every config apply is recorded in the audit log with the actor's identity and
a diff of what changed.

---

## Exports and Report Downloads

`GET /api/compliance/export` is admin-only. The export contains:
- Secret names
- Compliance status per secret
- Evidence summaries

It does not contain secret values. File format is JSON or CSV (no embedded formulas — no
CSV injection risk as values are identifiers, not user-controlled strings).

---

## Rollback Endpoints

Config rollback requires the current version number (optimistic lock). Without knowing the version,
an attacker cannot roll back the config. Version numbers are not secret but are not returned in
unauthenticated responses.

---

## Audit Trail

The audit log is append-only at the application level (no DELETE endpoint for audit entries).
An admin with direct database access could modify SQLite directly. This is out of scope for
an application-level security review.

---

## Known Limitations and Open Issues

1. **No rate limiting.** The `/api/status` and `GET /api/...` endpoints have no rate limiting.
   A determined attacker could cause high CPU load by flooding the cold-cache evaluation path.
   **Mitigation:** The 30-second TTL cache absorbs most of this. Add a reverse proxy (nginx/Caddy)
   with rate limiting for production deployments.

2. **No CSRF protection.** The API is JSON-only; browsers enforce CORS for cross-origin requests.
   If the UI and API are on the same origin, CSRF is a risk. Add CSRF tokens if serving the web
   UI and API from the same origin in production.

3. **JWT secret is static.** The JWT signing secret is read from config at startup. Rotation of
   the JWT secret requires a restart and invalidates all active sessions. This is acceptable for
   the current use case but would need improvement for long-lived deployments.

4. **No IP allowlisting.** All endpoints are accessible from any IP that can reach the server.
   In production, the API should be behind a VPN or network policy.

5. **Backup files.** SQLite backup files (if used) contain all secret metadata and are protected
   only by filesystem permissions. Encrypt backups at rest in production.

---

## Recommendations

| Priority | Finding | Suggested Fix |
|---|---|---|
| High | No rate limiting on cold-cache paths | Add reverse proxy rate limiting |
| Medium | CSRF risk if same-origin deployment | Add CSRF tokens to state-mutating endpoints |
| Medium | JWT secret rotation requires restart | Support JWT key rotation without restart |
| Low | Backup files unencrypted | Document encryption requirement in ops runbook |
| Low | Admin can point secrets at arbitrary providers | Acceptable; record in threat model |

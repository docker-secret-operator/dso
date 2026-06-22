# Spec: Edit DSO Config From the Dashboard (v1)

**Date:** 2026-06-22
**Status:** Approved (design)

Let operators view, validate, edit, apply, and roll back the DSO configuration
(`dso.yaml`) from the dashboard — admin-only, with strong safety guarantees and
**no live config reload** in v1. Mature-infra posture (Vault/Consul style): an
explicit, signposted restart is preferred over hidden state inconsistencies.

## Principles
- Admin-only. Validation gate before any write. Timestamped backups. Atomic
  writes. Audit logging. Dry-run preview. Rollback. Restart-required detection.
- No live reload of the running agent's in-memory config in v1. After a write
  that affects providers/global settings, the response flags `restartRequired`
  and the UI shows a banner.

---

## Architecture

```
Frontend — Configuration Page
  ├── View mode (existing)
  ├── Edit mode (Monaco YAML editor)
  ├── Validate
  ├── Dry-run plan preview
  ├── Save & Apply
  └── Rollback (from backups list)

API (internal/api/config.go, wired in internal/server/rest.go)
  GET  /api/config/raw        (enhanced)
  POST /api/config/validate
  POST /api/config/apply
  GET  /api/config/backups
  POST /api/config/rollback

Core (shared, new)
  internal/apply
    types.go     ApplyPlan, ApplyResult, PlanChange
    plan.go      ComputePlan(cfg) (*ApplyPlan, error)
    execute.go   Execute(cfg, plan, deps) (*ApplyResult, error)

Safety: admin RBAC · validation · timestamped backups · atomic writes ·
        audit logging · dry-run · rollback · restart-required detection
```

`computeApplyPlan`/`executeApplyPlan` move out of `internal/cli` into
`internal/apply` (exported `ComputePlan`/`Execute`). The CLI and API both call
the shared package — one implementation. Execute uses the server's in-process
agent dependencies (`triggerEngine`, `cache`) rather than the CLI's socket path.

---

## Backend

### Shared `internal/apply` package
- `types.go`: `ApplyPlan` (TotalSecrets, ContainersAffected, SecretsToUpdate,
  changes []PlanChange), `PlanChange{ Op: "create"|"update"|"remove",
  Kind: "provider"|"secret", Name string }`, `ApplyResult`.
- `plan.go`: `ComputePlan(cfg *config.Config) (*ApplyPlan, error)` — pure
  computation; diff the incoming config against the current on-disk config to
  produce `PlanChange`s and impact counts. No side effects (safe for dry-run).
- `execute.go`: `Execute(ctx, cfg, plan, deps) (*ApplyResult, error)` — best-effort
  reconcile via injected deps (agent trigger engine). Refactor CLI `apply.go` to
  call these; preserve existing CLI behavior/tests.

### `GET /api/config/raw` (enhanced)
Returns:
```json
{ "path": "~/.dso/dso.yaml", "yaml": "...", "modifiedAt": "...", "restartRequired": false }
```
`restartRequired` reflects whether a prior apply flagged a pending restart
(tracked in a small server-side flag; resets after restart since it's in-memory).

### `POST /api/config/validate`
Body: `{ "yaml": "..." }`. Parse via `yaml.Unmarshal` into `config.Config`, then
`cfg.Validate()` (existing, tested). Returns `{ "valid": bool, "errors": [string] }`.
Never writes. 400 on unparseable YAML (include parser message).

### `POST /api/config/apply`
Body: `{ "yaml": "...", "dryRun": bool }`.
Flow:
1. Parse + `cfg.Validate()`. On failure → 400 `{valid:false, errors}`.
2. If `dryRun` → `apply.ComputePlan(cfg)` and return `{ plan }` only. No write.
3. Else:
   a. Backup current `dso.yaml` → `dso.yaml.bak-<RFC3339-ts>`.
   b. Atomic write: write temp file in same dir, fsync, `os.Rename` over target.
   c. Audit-log (actor, action `config.apply`, resource `config`, backup path).
   d. `ComputePlan` + best-effort `Execute` (reconcile via trigger engine).
   e. Detect `restartRequired`: true if the providers map or agent/global
      settings differ from the previous config (diff-based; secret-only changes
      that the running agent can reconcile do not force a restart).
Returns:
```json
{ "success": true, "restartRequired": true, "backupPath": "...", "plan": [ ... ], "result": { ... } }
```
On write/backup failure → 500, no partial state (atomic), original file intact.
On reconcile failure → `success:true` (file is saved) with `result.error` set and
`restartRequired:true`; backup path returned so the operator can roll back.

### `GET /api/config/backups`
Lists `dso.yaml.bak-*` in the config dir:
```json
[ { "timestamp": "...", "path": "...", "size": 1234 } ]
```
Sorted newest-first.

### `POST /api/config/rollback`
Body: `{ "backupPath": "..." }`.
Flow: verify the backup path is a real `dso.yaml.bak-*` in the config dir (reject
path traversal / arbitrary files) → read it → `cfg.Validate()` → backup the
current file → atomic write the restored content → audit-log (`config.rollback`)
→ best-effort reconcile → return the same shape as apply. 400 on invalid/unknown
backup target.

### RBAC
Add to the permission matrix as `RoleAdmin`: `/api/config/validate`,
`/api/config/apply`, `/api/config/backups`, `/api/config/rollback`
(`/api/config*` is already admin-gated).

### Path safety
All file operations resolve within the configured config directory
(`resolveConfig()` dir). Backup/rollback paths are validated to live in that dir
and match the `dso.yaml.bak-*` pattern — no traversal, no writing outside it.

---

## Frontend (`app/configuration/page.tsx`)

Admin-only Edit mode (hidden for non-admins).

- **Editor:** Monaco (`@monaco-editor/react`) — YAML syntax highlighting, line
  numbers, search, folding, **minimap off**, JetBrains Mono, dark theme matching
  Tech Noir. Seeded from `GET /api/config/raw`.
- **Buttons:** `Validate` · `Save & Apply` · `Cancel`.
- **Validate:** calls `/api/config/validate`; shows inline errors or a success pill.
- **Save & Apply:** first calls apply with `dryRun:true` and shows a **Plan
  Preview** dialog:
  ```
  Plan Preview
  + Create provider vault-prod
  ~ Update secret database-password
  - Remove secret old-token
  Estimated impact: 2 containers affected
  [Cancel] [Apply Changes]
  ```
  On confirm → apply (`dryRun:false`). On success show "Configuration saved
  successfully." and, when `restartRequired`, a banner: "Some changes require an
  agent restart."
- **Rollback:** a backups list (`/api/config/backups`); selecting one shows a
  confirm, then `POST /api/config/rollback`.
- All requests use `apiFetch` (Bearer token). Reduced-motion respected; visible
  focus states.

Monaco is a new dependency (`@monaco-editor/react` + `monaco-editor`). By default
its loader fetches Monaco from a CDN; for air-gapped/embedded builds we must
configure the loader to use the bundled `monaco-editor` package (self-hosted
assets), not the CDN. The implementation plan must include this loader config and
verify no runtime CDN dependency.

---

## Testing

Backend (httptest + temp config dir):
- **Validate:** valid YAML → `{valid:true}`; malformed YAML → 400; `cfg.Validate`
  failure (e.g. bad provider type) → `{valid:false, errors}`.
- **Apply:** backup file created; atomic write replaces content; audit entry
  created; reconcile triggered (mock/asserted via injected deps); `restartRequired`
  true when providers/global change, false for secret-only.
- **Rollback:** restores previous file content; triggers reconcile; invalid/unknown
  backup target → 400; path-traversal target rejected.
- **RBAC:** admin → 200; non-admin → 403 for all four endpoints.
- **Failure cases:** write failure (original intact), backup failure (no write),
  reconcile failure (file saved + error surfaced), invalid rollback target.

Shared package: unit tests for `ComputePlan` diff output (create/update/remove)
and impact counts.

Frontend: component test that Validate surfaces errors and Save & Apply shows the
dry-run plan before applying (mocked apiFetch).

---

## Out of scope (v1)
- Live config hot-reload (explicit restart instead).
- Structured per-section forms (raw YAML only; forms are a later iteration).
- Governance draft/review/approval routing (direct admin edit; can integrate later).

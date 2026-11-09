# WebUI Phase 1 — Selective Platform Integration

Scope note for the Phase 1 port of six small features from the unmerged
`origin/advanced-platform` branch into `feature/webui-phase1`. This is a
capability port, not an architecture port: the existing Next.js static
export, Go embed packaging, and single-operator cookie-session auth model
are unchanged.

```text
Phase 1
├── Audit              -> /audit         (reuses EventStore, not a new DB)
├── Users / Access      -> /users         (honest single-operator view)
├── Settings            -> /settings      (real, supported functionality only)
├── Integrations        -> /integrations  (read-only config-derived summary)
├── Global Search        -> mounted in a new topbar (client-side nav search)
└── Notifications/Toasts -> mounted in the root layout
```

## Explicitly out of scope for this phase

```text
Secret versioning, Environment/project switcher, Service token management,
Secret sharing, Review workflow, Changesets, Drift, Policies, Alerts,
Analytics, Incidents, Backups, Dependency graph, Discovery, Autonomy,
Forecasting, Recommendations, Remediation, Scheduler, Plugins
```

None of these were touched. No SQLite, no `internal/plugins`,
`internal/execution`, `internal/correlation`, `internal/forecast`,
`internal/autonomy`, `internal/policy`, `internal/scheduler`, or
`internal/graph` were introduced.

## Why the advanced-platform pages could not be ported verbatim

Reconnaissance against `origin/advanced-platform` (via `git show`, branch
never checked out) found that its real backends for these features are
fully entangled with forbidden dependencies:

- **Audit** (`internal/api/audit_explorer.go`) queries a `sql.DB`
  (SQLite) `audit_events` table directly.
- **Users/Sessions** (`internal/api/user_handler.go`) is backed by
  `storage.UserStore`/`storage.SessionStore` (SQLite) plus a separate
  `internal/auth.AuthenticationService` — a different, heavier auth model
  than `internal/webuiauth`'s single-operator manager.
- **Integrations** (`internal/api/integration_handler.go`) depends on
  `internal/plugins.Manager` plus SQLite-backed config/delivery stores.

None of these can run without reintroducing exactly the architecture this
phase is meant to avoid. So instead of porting those Go handlers, Phase 1
adds small new handlers in `internal/server/rest.go` that serve
API-compatible-enough JSON shapes sourced only from data DSO already has
in memory: the existing `EventStore`, `webuiauth.Manager`'s session map,
and the already-loaded `config.Config`.

## New backend endpoints (all gated by the existing `authorized()` check)

| Endpoint | Method | Source | Notes |
|---|---|---|---|
| `/api/audit` | GET | `EventStore.GetLast` (same store as `/api/events`/`/api/logs`) | Presentational relabeling, not a new data source. Bounded, in-memory, non-persistent across restart — see the "Audit History" disclaimer in the UI. |
| `/api/sessions` | GET | `webuiauth.Manager.ListSessions()` | Lists the single operator's active sessions (one per logged-in browser/device). Never returns the raw session token. |
| `/api/sessions/revoke-others` | POST | `webuiauth.Manager.LogoutOthers()` | Signs out every session except the caller's own. |
| `/api/integrations` | GET | `config.Config.Agent.Watch.Webhook`, `config.Config.Providers` | Read-only. Surfaces the one real webhook integration and configured provider count. No mutation endpoints — none exist to port. |

`GET /api/auth/session` response also now includes `"username"` (was
`{"authenticated":true}` only) — additive, since DSO's session already
knows the operator's username server-side; no new capability was added,
just an existing fact exposed.

## Backend gaps deliberately NOT filled in this phase

- **No password-change endpoint.** `webuiauth.Manager`'s credential is
  loaded once from `dso.yaml` at startup; there is no mechanism today to
  persist a new bcrypt hash back to the config file. Building one requires
  a config-file-rewrite design (atomic write, format-preserving YAML edit,
  restart-safety) that is out of scope here. The Settings page's
  Authentication section is read-only for this reason — no fake
  "Change password" form was added.
- **No true multi-user list.** `/users` intentionally shows one operator
  identity plus session management, not a fake user table.
- **No CSV/JSON audit export.** Real feature, but additional surface;
  deferred rather than rushed.

## Frontend components ported/built fresh

- `toast-context.tsx` / `toast-container.tsx` / `toast-provider.tsx` —
  ported near-verbatim from advanced-platform (zero backend dependency).
- Global search — **rebuilt fresh** as a client-side command palette
  (cmdk, already a dependency) over the static route list. Advanced
  platform's version depended on `/api/audit` (now exists, in a different
  shape) and `/api/discovery/docker` (doesn't exist) — rather than porting
  a component with two broken data sources, this phase ships an honest
  navigation-only search.
- Notification bell — **rebuilt fresh**, small. Advanced platform's
  `notification-center.tsx` categorizes events using fields
  (`action`/`severity`/`message`) that don't exist on this branch's real
  `Event` shape (`event_type`/`status`/`secret`/`container`); porting it
  verbatim would have silently produced meaningless categories. The
  Phase 1 version surfaces recent failed/error events using the real
  fields only.

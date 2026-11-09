# Web UI

DSO's agent can optionally serve a small browser dashboard alongside its
REST API. It is a **read-mostly client of the existing DSO backend** —
login, dashboard, events, secrets (metadata), containers, audit, drift,
integrations, and configuration are all read-only views — not a second
backend, not a second authentication system, and not a replacement for
`DSO_AUTH_TOKEN`.

It does support exactly three write operations, each a thin entry point
into existing backend functionality (see [Write operations](#write-operations)
below): triggering a manual secret rotation, sending a test notification,
and revoking the operator's other sessions. It does not support creating,
editing, or viewing secrets; editing provider configuration; redeploying
Compose workloads; restarting arbitrary containers; rolling back a
rotation; or any other mutation beyond those three.

## Enabling it

Disabled by default — an existing deployment never starts serving a web UI
(or listening on a new address) just because a new DSO version was
installed.

```yaml
webui:
  enabled: true
  username: operator
  password_hash: "$2a$12$..."   # bcrypt hash; generate with `htpasswd -bnBC 12 "" '<password>' | tr -d ':\n'`
  session_idle_timeout: 30m     # optional, defaults to 30m
```

Once enabled, the UI is served from the same listener as the REST API
(`--api-addr`, default `127.0.0.1:8471`) — there is no separate port to
open or expose. If `listen_address` is set to something other than empty,
that field is currently accepted but not wired to a second listener; leave
it unset.

Open `http://<api-addr>/` in a browser and log in with the configured
operator credential.

## Pages

| Page | What it shows | Write action |
|---|---|---|
| `/login` | Operator login | — |
| `/dashboard` | Agent health, secret count, webhook capability flag | — |
| `/secrets` | Secret metadata (name, provider, status, injection type, rotation flag) — **never plaintext** | **Trigger rotation** (see below) |
| `/containers` | Container status (name, image, health, strategy, secret count) | — |
| `/events` | Recent rotation/recovery events + a live feed over `/api/events/ws` | — |
| `/audit` | Audit log | — |
| `/drift` | Config-vs-runtime drift status (diagnostic only) | — |
| `/integrations` | Configured notification integrations | **Send test notification** (see below) |
| `/users` | This operator's active sessions | **Revoke other sessions** (see below) |
| `/alerts` | Current alerts (auto-resolve when the underlying condition clears; no manual acknowledge) | — |
| `/analytics` | Rotation/health metrics | — |
| `/configuration` | Read-only view of `/api/config/raw` (redacted — secret names and provider names only) | — |
| `/logs` | Log tail (same underlying event store as `/events`) | — |
| `/settings` | Connection/auth status | — |

Every page above other than the three write actions is read-only: it
displays data DSO already tracks and cannot mutate agent, secret,
provider, or container state.

Deliberately not shipped: anything requiring functionality DSO doesn't
have today (autonomy, forecasting, correlation, scheduling, plugins,
policy engines, drift *reconciliation*, full multi-user/RBAC account
management). Specifically out of scope for the web UI: creating or
editing secrets, revealing plaintext secret values, changing provider
configuration, applying or redeploying Compose workloads, restarting an
arbitrary container, rolling back a rotation, changing rotation strategy,
changing `owner_id` or adopting ownerless containers, managing Compose
projects, and acknowledging/resolving alerts. A container/secret
discovery UI is also deferred — it doesn't correspond to any existing
backend feature.

## Write operations

The web UI's three write actions are thin, audited entry points into
functionality that already exists independently of the UI (the same
underlying mechanisms the CLI and REST API already use) — the UI does
not implement a second copy of any of this logic.

- **Manual secret rotation** — `POST /api/secrets/{name}/rotate`, exposed
  from the `/secrets` page. Triggering it calls the same rotation
  machinery used by scheduled/automatic rotation. The request is
  asynchronous: the POST response only confirms the request was
  accepted, not that rotation succeeded. The page's displayed status
  (in progress / succeeded / failed) reflects the real
  `rotation_succeeded`/`rotation_failed` event delivered over the
  existing `/api/events/ws` stream, not a fabricated timer. This action
  never exposes, returns, or logs the secret's value — only its name and
  the rotation outcome.
- **Test notification** — `POST /api/integrations/notifications/test`,
  exposed from the `/integrations` page. Sends one real, synthetic test
  event through your already-configured notification destination(s), so
  you can confirm delivery is working. It does not let you add, edit, or
  remove a notification destination from the UI — that remains a
  `dso.yaml` configuration change.
- **Revoke other sessions** — `POST /api/sessions/revoke-others`, exposed
  from the `/users` page. Invalidates every session for the single
  configured operator identity other than the current one. This is
  session housekeeping, not user/RBAC administration — there is still
  only one operator identity (see Authentication below).

Every write action is logged to the audit trail (`manual_rotation_triggered`,
`test_notification_sent`) with the actor, target name, and outcome — never
a secret value or credential.

## Authentication

The web UI uses a separate, minimal session layer from the token that
protects the REST API for non-browser consumers:

- **Model**: a single configured operator identity (`username` +
  `password_hash`), not a multi-user account system. There is no
  database — sessions live in memory and are lost on agent restart
  (operators simply log in again).
- **Login**: `POST /api/auth/login` with `{"username", "password"}`.
  On success, the response body is `{"status":"ok"}` — the session token
  is **never** returned in the body. It is only ever set via
  `Set-Cookie`, `HttpOnly` + `Secure` + `SameSite=Strict`. The browser
  never has JavaScript-visible access to it, and it is never written to
  `localStorage`/`sessionStorage`.
- **Session check**: `GET /api/auth/session` returns `200` if the cookie
  is valid, `401` otherwise. No user-profile data is exposed beyond that.
- **Logout**: `POST /api/auth/logout` invalidates the session and clears
  the cookie.
- **Idle expiry**: sessions expire after `session_idle_timeout` of
  inactivity (default 30m).
- **Both auth mechanisms coexist.** `/api/secrets*`, `/api/events`,
  `/api/events/ws`, `/api/discovery`, `/api/config/raw` all accept
  *either* a valid `DSO_AUTH_TOKEN` bearer token *or* a valid session
  cookie — enabling the web UI does not weaken or replace the existing
  token-based protection for scripted/API consumers.
- **WebSocket auth**: `/api/events/ws` is gated by the same check during
  the upgrade handshake; an invalid or missing session is rejected before
  the connection is established.

## Security model

- Secret plaintext is structurally unreachable from the web UI: the
  backend's `/api/secrets` handler has no plaintext field to return, and
  the frontend has no code path that would render one even if a
  malformed response contained it.
- `/api/config/raw` never returns provider configuration verbatim — only
  secret names, their provider, and the list of configured provider
  *names*. It requires a valid session (or bearer token) like every other
  protected route; in this single-operator model, "authenticated" and
  "authorized" are the same check.
- No config data is ever logged to the browser console, sent to any
  analytics, encoded into a URL, or persisted to `localStorage`/
  `sessionStorage`.
- The static frontend assets are embedded into the DSO binary at build
  time (`go:embed`) — there is no Node.js runtime dependency in
  production, no separate frontend process or container, and no
  server-side Next.js API routes.

## Development

The frontend lives under `web/` (Next.js 14, App Router, static export).
`web/next.config.js` sets `output: 'export'`, so there is no
`next start`/`next dev` server involved in production — the Go binary
embeds a static export and serves it directly.

### Source → build → embed

1. Edit the frontend under `web/`.
2. Regenerate the embedded assets: `make webui-build` (this runs
   `scripts/build-webui.sh`, which does a deterministic `npm ci` +
   `npm run build` in `web/` and copies the static export output from
   `web/out/` into `internal/webui/assets/`, which
   `internal/webui/embed.go` embeds via `//go:embed assets/*`).
3. Commit the resulting diff in `internal/webui/assets/` together with
   your `web/` source change.

Do not hand-edit files under `internal/webui/assets/` or copy `web/out/`
into it manually — always go through `make webui-build` so the two stay
byte-for-byte reproducible from the same source.

CI (`.github/workflows/ci.yml`, `webui` job) reruns `scripts/build-webui.sh`
on every push/PR and fails the build if the regenerated
`internal/webui/assets/` differs from what's committed, so a stale or
hand-edited build cannot merge unnoticed. `scripts/build-release.sh` runs
the same script before packaging a release, so a release build never
trusts whatever happens to be checked in.

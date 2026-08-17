# Web UI

DSO's agent can optionally serve a small browser dashboard alongside its
REST API. It is a **read-mostly client of the existing DSO backend** —
login, dashboard, events, secrets (metadata), and configuration (read-only)
— not a second backend, not a second authentication system, and not a
replacement for `DSO_AUTH_TOKEN`.

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

| Page | What it shows |
|---|---|
| `/login` | Operator login |
| `/dashboard` | Agent health, secret count, webhook capability flag |
| `/events` | Recent rotation/recovery events + a live feed over `/api/events/ws` |
| `/secrets` | Secret metadata (name, provider, status, injection type, rotation flag) — **never plaintext** |
| `/configuration` | Read-only view of `/api/config/raw` (redacted — secret names and provider names only) |

Deliberately not shipped: anything requiring functionality DSO doesn't
have today (autonomy, forecasting, correlation, drift, scheduling,
plugins, policy engines, multi-user accounts). A container/secret
discovery UI is also deferred — it doesn't correspond to any existing
backend feature.

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

```bash
cd web
npm ci
npm run build      # produces web/out/
```

Copy `web/out/*` into `internal/webui/assets/` to embed a new build into
the Go binary; `internal/webui/embed.go` picks it up via `//go:embed`.
`web/next.config.js` is configured for `output: 'export'` — there is no
`next start`/`next dev` server involved in production.

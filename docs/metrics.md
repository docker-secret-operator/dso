# REST control-plane metrics

Exposed by the agent's Prometheus exporter (see `pkg/observability`). Labels use
fixed value sets only: no usernames, addresses, token fingerprints or secret
names are ever used as labels.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `dso_rest_auth_failures_total` | counter | `reason` = `unauthenticated` \| `cross_origin` | Requests rejected by the auth gate (401) or CSRF origin check (403) |
| `dso_rest_login_attempts_total` | counter | `result` = `success` \| `failure` \| `rate_limited` | WebUI login attempts |
| `dso_rest_sessions_revoked_total` | counter | none | Sessions signed out by `POST /api/sessions/revoke` or `/revoke-others` |
| `dso_rest_manual_rotation_requests_total` | counter | `status` = `success` \| `failure` | Manual rotation requests over REST |
| `dso_rest_websocket_clients` | gauge | none | Connected WebSocket event clients |
| `dso_audit_durable_write_failures_total` | counter | none | Audit events not persisted to the durable file (still logged) |
| `dso_rest_active_sessions` | gauge | none | WebUI sessions neither idle- nor absolute-expired (computed at scrape time) |

Every metric above has a matching audit event where one applies (`auth_login`,
`session_revoke`, `sessions_revoke_others`, `manual_rotation_triggered`); metrics
are aggregate counters, the audit stream carries the attributed record.
Rotation, secret-fetch and cache metrics are listed in `pkg/observability/metrics.go`.

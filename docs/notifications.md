# Rotation Event Notifications

DSO's agent can notify external systems when secret rotations and crash
recoveries complete. Notifications are **strictly an observer** of
rotation:

> Notification delivery failure does not affect secret rotation state.
> DSO continues rotating secrets correctly even if every notification
> destination is completely unavailable.

Events are emitted *after* the authoritative rotation state transition,
via a non-blocking in-memory queue — rotation code cannot observe (and
therefore can never be affected by) delivery outcomes.

## Event types

| Event | Meaning |
|---|---|
| `rotation_succeeded` | A secret rotation completed and its containers were updated |
| `rotation_failed` | A rotation's reload failed (state marked for rollback; the next poll retries) |
| `recovery_succeeded` | Crash recovery restored a rotation's containers on agent startup |
| `recovery_failed` | Crash recovery could not restore state automatically — operator attention needed |

There is deliberately no `rotation_started` event: operators act on
outcomes, and start-events would double delivery volume with no
actionable signal.

## Configuration

Disabled by default — an existing deployment never starts making outbound
HTTP requests just because a new DSO version was installed.

```yaml
notifications:
  enabled: true
  webhooks:
    - url: https://hooks.example.com/dso
      timeout: 10s          # per delivery attempt (default 10s)
      max_retries: 2        # transient failures only (default 2; -1 = none)
      events: [rotation_failed, recovery_failed]   # empty = all events
    # Plain HTTP requires an explicit opt-in:
    - url: http://monitoring.internal:9090/hook
      allow_insecure_http: true
```

## Payload

`POST` with `Content-Type: application/json`. Example (fake data):

```json
{
  "event_id": "9f3a1c2b4d5e6f708192a3b4c5d6e7f8",
  "event_type": "rotation_failed",
  "timestamp": "2026-08-12T12:00:00Z",
  "provider": "vault",
  "secret_name": "db_password",
  "affected_containers": ["a1b2c3d4e5f6"],
  "duration_seconds": 4.2,
  "error_message": "health verification failed: container reported unhealthy"
}
```

Fields carry rotation *metadata only*. Secret values, provider
credentials, and environment contents are structurally excluded — the
event type has no field that could hold them — and `error_message` text
is additionally passed through DSO's redaction patterns before the event
is even queued, since provider errors can embed credentials.

## Delivery semantics

- **At-least-once, never exactly-once.** Retries and agent restarts can
  produce duplicates; deduplicate on `event_id` if your consumer needs to.
- **Bounded.** Each attempt has a timeout; transient failures (network
  errors, HTTP 5xx) are retried up to `max_retries` with linear backoff;
  HTTP 4xx is treated as permanent and never retried.
- **Redirects are not followed** — only the exact configured endpoint is
  ever contacted.
- **Queue is bounded (64 events).** If every destination is persistently
  slower than event production, new events are dropped and counted
  (`dso_notification_dropped_total`) — dropping observability is the
  designed trade; delaying rotation is not an option.
- **Shutdown drains briefly.** The agent's graceful shutdown waits up to
  10s for queued notifications; events still undelivered are abandoned.

## Metrics

| Metric | Labels |
|---|---|
| `dso_notification_attempts_total` | `destination`, `event_type` |
| `dso_notification_failures_total` | `destination`, `event_type` |
| `dso_notification_dropped_total` | — |

The `destination` label is `scheme://host` only — never the full URL,
which may embed tokens in its path (e.g. Slack webhook URLs). The same
rule applies to log lines.

## Security model

- Webhook endpoints are **operator-controlled configuration** in
  `dso.yaml` (0600, admin-managed) — the same trust level as provider
  endpoints. DSO does not accept destination URLs from any untrusted
  source.
- HTTPS is required unless `allow_insecure_http: true` is set explicitly.
  `file://`, `gopher://`, and other schemes are rejected at startup.
- Response bodies are read to a small bounded limit and discarded — never
  parsed or acted on.
- Invalid destination configuration fails agent startup loudly rather
  than silently skipping a destination the operator believes is active.

## Slack / PagerDuty

Both accept generic JSON webhooks (Slack via workflow webhooks /
transformation layers, PagerDuty via Events API-compatible proxies).
Dedicated adapters with native payload formats are a possible future
increment; the event contract above is the stable interface they would
build on.

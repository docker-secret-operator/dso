# CLI and Web UI Deep Review

**Date:** 2026-11-09  
**Scope:** Current working tree; CLI command paths, web UI/API flows, and backend handlers that directly serve those flows.  
**Method:** Static source trace. No application files were changed for this review. Tests and live UI behavior were not run.

## Confirmed findings

### CLI

#### CLI-01 — `system status` reports an inactive service as running

**Severity: P2 (misleading operational status)**

`systemctl show dso-agent --property=...` succeeds for a loaded but inactive unit. The success branch then prints `Status: ✓ running` without parsing `ActiveState` or `SubState`. Operators can therefore be told the agent is running when it is stopped or failed. Parse and report the returned state, and distinguish a missing unit from an inactive one.

Evidence: `internal/cli/system_mgmt.go:73-87`.

#### CLI-02 — `dso down` rejects valid argument values despite using no shell

**Severity: P3 (CLI compatibility)**

The command passes arguments directly to `exec.Command`, so shell metacharacters are not interpreted by a shell. Its precheck nevertheless rejects `;`, `&`, `|`, `$`, backticks, and double quotes anywhere in every argument. This blocks legitimate Compose values and paths containing those characters (for example, an `--env-file` path), while the help says arguments are forwarded unchanged. Remove the shell-oriented restriction or validate only actual Compose option constraints.

Evidence: `internal/cli/down.go:31-42`.

### Web UI and API

#### UI-01 — Rotation outcome handling reads the oldest buffered event

**Severity: P2 (rotation status can remain stale)**

`useWebSocket` prepends each new event, making index 0 newest. `SecretsContent` reads `liveEvents[liveEvents.length - 1]` as `latest`, which is the oldest retained event. After more than one event arrives, later rotation success/failure events may never update their secret badge; batches affecting multiple secrets are also not processed individually. Process new events by stable event identity/cursor, or reconcile all newly received events.

Evidence: `web/hooks/useWebSocket.ts:163-169`; `web/components/secrets-content.tsx:173-182`.

#### UI-02 — Session-check network errors are treated as unauthenticated

**Severity: P2 (avoidable redirect during API outage)**

`checkSession` catches every error and returns `false`. `AuthGuard` interprets `false` as an invalid session and redirects to `/login`. A network outage, timeout, or server error is therefore indistinguishable from a genuine 401 and can send a still-authenticated operator to the login page. Preserve the distinction between unauthorized and unavailable, and only redirect on 401.

Evidence: `web/lib/api/auth.ts:45-51`; `web/components/auth-guard.tsx:20-37`.

#### API-01 — Analytics “recent activity” returns the oldest matching entries

**Severity: P2 (dashboard presents stale activity)**

`EventStore.GetLast(0, "")` returns the retained events in insertion order (oldest to newest). `handleAnalytics` iterates that order and stops adding each activity category after ten entries. When a category has more than ten events, the API returns its oldest retained ten rather than its most recent ten, despite the field being called `recent_activity`. Iterate newest-to-oldest or select the tail of each category before encoding.

Evidence: `internal/server/eventstore.go:135-156`; `internal/server/rest.go:818-834`.

#### API-02 — Persisted event log grows without a bound and is fully loaded at startup

**Severity: P2 (disk and startup-memory exhaustion over time)**

`EventStore.Add` appends every event to `/var/run/dso/events.jsonl` and syncs it, while only the in-memory slice is capped. There is no file rotation, truncation, or byte/record retention limit. At startup, `replayHistory` decodes every valid line into a slice and truncates to the in-memory limit only after reading the whole file. Sustained event volume can therefore consume the filesystem and cause large startup memory use / slow startup. Bound retention on disk and stream only the newest configured number of records during replay.

Evidence: `internal/server/eventstore.go:22-31`, `79-107`, `110-123`.

#### UI-03 — Audit page says history resets on agent restart, but the backing store replays it

**Severity: P3 (incorrect operator expectation)**

The Audit UI says the event history “resets on agent restart,” and the API handler repeats that it is non-persistent across restarts. The current `EventStore` writes JSONL to disk and replays it when the agent starts, so agent-process restarts do not reset the retained history (although `/var/run` is volatile across host reboot, and this is not a tamper-evident audit trail). Update the UI and handler description to match the actual retention boundary.

Evidence: `web/components/audit-content.tsx:184-190`; `internal/server/rest.go:870-877`; `internal/server/eventstore.go:47-60`, `66-107`.

## Review notes

- The earlier `system doctor` no-op is no longer present in this working tree: `NewSystemCmd` registers it and it delegates to the real doctor command.
- The old client-side CSRF-token concern is not present in the current web client. The Go API enforces origin checks for unsafe session-authenticated requests.
- `dso diff` remains an explicitly documented unsupported command, so it is not reported as a defect.
- The review is static. Provider connectivity, browser behavior, command execution against Docker/systemd, and test outcomes remain unverified.
